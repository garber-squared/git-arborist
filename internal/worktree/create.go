package worktree

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// worktreesDirName is the directory, relative to a repository's main checkout,
// that new worktrees are created under.
const worktreesDirName = "worktrees"

// Candidate is a branch that a new worktree could be created for.
type Candidate struct {
	// Branch is the local branch name the worktree would check out.
	Branch string
	// Remote is the remote-tracking ref (e.g. "origin/fix-thing") the local
	// branch would be created from, or "" when the branch already exists
	// locally.
	Remote string
	// Repo is the submodule the branch belongs to, or "" for the superproject.
	Repo string
	// RepoRoot is the main checkout of the repository owning the branch; the
	// worktree is created there.
	RepoRoot string
}

// repoRef is one repository the dashboard covers: the superproject or an
// initialized submodule.
type repoRef struct {
	name string
	root string
}

// listRepos returns repoRoot plus each of its initialized submodules, matching
// the set of repositories DiscoverAll lists worktrees for.
func listRepos(repoRoot string) []repoRef {
	repos := []repoRef{{root: repoRoot}}
	for _, sub := range submodulePaths(repoRoot) {
		root := filepath.Join(repoRoot, sub)
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			continue // submodule not initialized / not checked out
		}
		repos = append(repos, repoRef{name: sub, root: root})
	}
	return repos
}

// ListCandidates returns the branches of repoRoot and its submodules that do
// not already have a worktree, most recently committed first. Branches that
// exist only on a remote are included; one that exists both locally and on a
// remote is listed once, as local.
func ListCandidates(repoRoot string) ([]Candidate, error) {
	taken, err := branchesWithWorktrees(repoRoot)
	if err != nil {
		return nil, err
	}
	var cands []Candidate
	for _, r := range listRepos(repoRoot) {
		cands = append(cands, candidatesForRepo(r, taken)...)
	}
	return cands, nil
}

// branchesWithWorktrees returns the set of branches already checked out in a
// worktree, keyed by owning repository. A branch can only live in one
// worktree, so these are not offered as candidates.
func branchesWithWorktrees(repoRoot string) (map[string]bool, error) {
	wts, err := DiscoverAll(repoRoot)
	if err != nil {
		return nil, err
	}
	taken := make(map[string]bool, len(wts))
	for _, wt := range wts {
		if wt.Branch != "" {
			taken[wt.RepoRoot+"\x00"+wt.Branch] = true
		}
	}
	return taken, nil
}

func candidatesForRepo(r repoRef, taken map[string]bool) []Candidate {
	var cands []Candidate
	seen := make(map[string]bool)
	// Local branches first so a branch that also exists on a remote is offered
	// as a plain checkout rather than as a branch to create.
	for _, refs := range [][]string{listRefs(r.root, "refs/heads"), listRefs(r.root, "refs/remotes")} {
		for _, ref := range refs {
			branch, remote := parseBranchRef(ref)
			if branch == "" || seen[branch] || taken[r.root+"\x00"+branch] {
				continue
			}
			seen[branch] = true
			cands = append(cands, Candidate{
				Branch:   branch,
				Remote:   remote,
				Repo:     r.name,
				RepoRoot: r.root,
			})
		}
	}
	return cands
}

// listRefs returns the full ref names under prefix, most recently committed
// first.
func listRefs(repoRoot, prefix string) []string {
	out, err := exec.Command("git", "-C", repoRoot, "for-each-ref",
		"--sort=-committerdate", "--format=%(refname)", prefix).Output()
	if err != nil {
		return nil
	}
	var refs []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			refs = append(refs, line)
		}
	}
	return refs
}

// parseBranchRef maps a full ref name to the local branch it implies, plus the
// remote-tracking ref that branch would be created from. Remote HEAD symrefs
// and bare remote names (refs/remotes/origin) are not branches and yield "".
func parseBranchRef(ref string) (branch, remote string) {
	if b, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
		return b, ""
	}
	rest, ok := strings.CutPrefix(ref, "refs/remotes/")
	if !ok {
		return "", ""
	}
	remoteName, b, ok := strings.Cut(rest, "/")
	if !ok || b == "" || b == "HEAD" {
		return "", ""
	}
	return b, remoteName + "/" + b
}

// BaseRefs lists the branch names a new branch could be based on: the
// repository's default branch first, then every other local and remote branch,
// most recently committed first. Names are plain (no remote prefix) because
// `gh issue develop --base` expects a remote branch name.
func BaseRefs(repoRoot string) []string {
	var bases []string
	seen := make(map[string]bool)
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		bases = append(bases, name)
	}
	add(DefaultBranch(repoRoot))
	for _, refs := range [][]string{listRefs(repoRoot, "refs/heads"), listRefs(repoRoot, "refs/remotes")} {
		for _, ref := range refs {
			branch, _ := parseBranchRef(ref)
			add(branch)
		}
	}
	return bases
}

// DefaultBranch returns the repository's default branch as recorded by
// origin/HEAD, or "" when the remote HEAD is not set.
func DefaultBranch(repoRoot string) string {
	out, err := exec.Command("git", "-C", repoRoot, "symbolic-ref", "--short",
		"refs/remotes/origin/HEAD").Output()
	if err != nil {
		return ""
	}
	_, branch, _ := strings.Cut(strings.TrimSpace(string(out)), "/")
	return branch
}

// RemoteRef returns the remote-tracking ref for a branch (e.g.
// "origin/123-fix"), or "" when no remote has it.
func RemoteRef(repoRoot, branch string) string {
	out, err := exec.Command("git", "-C", repoRoot, "for-each-ref",
		"--format=%(refname:short)", "refs/remotes/*/"+branch).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			return line
		}
	}
	return ""
}

// StartRefFor reports how an existing branch should be checked out into a new
// worktree: "" when the branch exists locally (check it out directly), or the
// remote-tracking ref to create the local branch from. It fails when neither
// exists.
func StartRefFor(repoRoot, branch string) (string, error) {
	if runGit(repoRoot, "show-ref", "--verify", "--quiet", "refs/heads/"+branch) == nil {
		return "", nil
	}
	if ref := RemoteRef(repoRoot, branch); ref != "" {
		return ref, nil
	}
	return "", fmt.Errorf("branch %q not found locally or on any remote", branch)
}

// BaseStartRef resolves a base branch name to the ref a new branch should
// start from, preferring the remote-tracking ref so the branch starts at what
// has been pushed rather than a possibly stale local copy.
func BaseStartRef(repoRoot, base string) string {
	if ref := RemoteRef(repoRoot, base); ref != "" {
		return ref
	}
	return base
}

// setupScripts are the worktree-relative locations arborist looks for a setup
// script, in order. The script keeps arborist framework-agnostic: it decides
// whether the worktree needs `npm install`, `bundle install`, `mix deps.get`,
// or nothing at all.
var setupScripts = []string{
	filepath.Join("scripts", "worktree-setup.sh"),
	"worktree-setup.sh",
}

// defaultWatchCommand is what watch mode runs when the repository does not
// override it: a live, colored git status, for observing a worktree from
// another pane without an agent in it.
const defaultWatchCommand = `watch -n 1 -c 'git -c color.status=always status'`

// WindowMode selects what runs in a worktree's tmux window once setup has
// finished.
type WindowMode int

const (
	// ModeWork runs the project's start command — in an agent-driven repo, the
	// command that launches the agent.
	ModeWork WindowMode = iota
	// ModeWatch leaves the start command out and watches git status instead.
	ModeWatch
)

func (m WindowMode) String() string {
	if m == ModeWatch {
		return "watch"
	}
	return "work"
}

// WindowCommand returns the shell command arborist runs in a worktree's new
// tmux window: the project's setup, then either its start command
// (`git config arborist.start`) or, in watch mode, the watch command
// (`git config arborist.watch`, defaulting to a git status watcher). It
// returns "" when the repository defines neither half, leaving a plain shell.
//
// The operators differ per mode on purpose. The start command runs only if
// setup succeeded — there is no point launching an agent into a half-installed
// worktree — while the watcher runs either way, since a failed setup is
// exactly when you want to watch the worktree.
func WindowCommand(wt Worktree, mode WindowMode) string {
	setup := SetupCommand(wt)
	if mode == ModeWatch {
		watch := gitConfig(wt.Path, "arborist.watch")
		if watch == "" {
			watch = defaultWatchCommand
		}
		return joinCommands(setup, "; ", watch)
	}
	return joinCommands(setup, " && ", gitConfig(wt.Path, "arborist.start"))
}

// joinCommands glues two optional commands with sep, dropping the separator
// when either side is absent.
func joinCommands(first, sep, second string) string {
	switch {
	case first == "":
		return second
	case second == "":
		return first
	default:
		return first + sep + second
	}
}

// SetupCommand returns the command that installs or builds whatever a worktree
// needs before work can start, or "" when the repository defines none. In
// order:
//
//  1. `git config arborist.setup` — a per-clone (or --global) override, so one
//     machine can deviate without touching the tree
//  2. the worktree's own scripts/worktree-setup.sh, then worktree-setup.sh,
//     called with the worktree path as its argument
//
// The script is taken from the worktree rather than the main checkout, so a
// branch that changes the setup gets its own version. A script that exists but
// lost its exec bit is run through bash rather than silently skipped.
func SetupCommand(wt Worktree) string {
	if cmd := gitConfig(wt.Path, "arborist.setup"); cmd != "" {
		return cmd
	}

	for _, rel := range setupScripts {
		info, err := os.Stat(filepath.Join(wt.Path, rel))
		if err != nil || info.IsDir() {
			continue
		}
		// tmux opens the window with the worktree as its working directory, so
		// a relative path is both correct and readable in the pane; the path is
		// passed on as well, since a setup script may accept it.
		cmd := shellQuote("."+string(filepath.Separator)+rel) + " " + shellQuote(wt.Path)
		if info.Mode().Perm()&0o111 == 0 {
			cmd = "bash " + cmd
		}
		return cmd
	}
	return ""
}

// IssueLabel returns the label arborist adds to a new worktree's linked issue
// (`git config arborist.issueLabel`), or "" when the repository does not want
// issues labelled. It is opt-in because applying it writes to GitHub — and may
// create the label — which is not something a dashboard should do uninvited.
//
//	git config arborist.issueLabel status:in-development
func IssueLabel(dir string) string {
	return gitConfig(dir, "arborist.issueLabel")
}

// gitConfig reads one config value from the repository containing dir,
// returning "" when it is unset.
func gitConfig(dir, key string) string {
	out, err := exec.Command("git", "-C", dir, "config", "--get", key).Output()
	if err != nil {
		return "" // unset (exit 1) or not a repo
	}
	return strings.TrimSpace(string(out))
}

// shellQuote quotes an argument for the shell that runs the window command, so
// a repository living under a path with spaces still works.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Fetch updates the remote-tracking refs of a repository. It is network-bound,
// so callers run it off the UI loop.
func Fetch(repoRoot string) {
	_ = exec.Command("git", "-C", repoRoot, "fetch", "--prune").Run()
}

// FetchAll fetches repoRoot and each of its submodules.
func FetchAll(repoRoot string) {
	for _, r := range listRepos(repoRoot) {
		Fetch(r.root)
	}
}

// CreateOptions describes the worktree Create should add.
type CreateOptions struct {
	// RepoRoot is the main checkout of the repository to create the worktree
	// in; Repo is its submodule name, or "" for the superproject.
	RepoRoot string
	Repo     string
	// Branch is the local branch to check out, or to create when StartRef is
	// set.
	Branch string
	// StartRef is the ref the branch is created from. Empty checks out an
	// existing local branch.
	StartRef string
}

// Create adds a worktree at <RepoRoot>/worktrees/<Branch>, keeps that
// directory out of `git status`, and shares the main checkout's untracked
// local config with it.
func Create(opts CreateOptions) (Worktree, error) {
	branch := strings.TrimSpace(opts.Branch)
	if branch == "" {
		return Worktree{}, errors.New("no branch name given")
	}
	if err := runGit(opts.RepoRoot, "check-ref-format", "--branch", branch); err != nil {
		return Worktree{}, fmt.Errorf("invalid branch name %q", branch)
	}

	dir := filepath.Join(opts.RepoRoot, worktreesDirName, filepath.FromSlash(branch))
	if err := os.MkdirAll(filepath.Dir(dir), 0755); err != nil {
		return Worktree{}, err
	}
	// A missing exclude entry only makes `git status` noisier, so a failure
	// here must not block the worktree itself.
	_ = exclude(opts.RepoRoot, worktreesDirName+"/", ".worktree-ports")

	args := []string{"worktree", "add", dir, branch}
	if opts.StartRef != "" {
		args = []string{"worktree", "add", "-b", branch, dir, opts.StartRef}
	}
	if err := runGit(opts.RepoRoot, args...); err != nil {
		return Worktree{}, err
	}

	shareLocalFiles(opts.RepoRoot, dir)

	return Worktree{
		Path:     dir,
		Branch:   branch,
		Repo:     opts.Repo,
		RepoRoot: opts.RepoRoot,
	}, nil
}

// exclude adds entries to the repository's info/exclude, keeping the files
// arborist creates at the repo root out of `git status`. That file is per-clone
// and never committed, so the project's tracked .gitignore is left alone.
func exclude(repoRoot string, entries ...string) error {
	out, err := exec.Command("git", "-C", repoRoot, "rev-parse",
		"--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return err
	}
	infoDir := filepath.Join(strings.TrimSpace(string(out)), "info")
	if err := os.MkdirAll(infoDir, 0755); err != nil {
		return err
	}

	path := filepath.Join(infoDir, "exclude")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	present := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		present[strings.TrimSpace(line)] = true
	}
	var add strings.Builder
	// Don't glue the first entry onto an unterminated final line.
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		add.WriteString("\n")
	}
	for _, entry := range entries {
		if !present[entry] {
			add.WriteString(entry + "\n")
			present[entry] = true
		}
	}
	if add.Len() == 0 {
		return nil
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(add.String())
	return err
}

// shareLocalFiles gives a new worktree the main checkout's untracked local
// config: top-level .env* files are symlinked so edits stay in sync, and
// .claude/settings.local.json is copied so per-worktree permission grants
// don't leak back. Files already present in the worktree are left alone, and
// every step is best-effort — a repo without them is not an error.
func shareLocalFiles(repoRoot, dir string) {
	if entries, err := os.ReadDir(repoRoot); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasPrefix(e.Name(), ".env") {
				continue
			}
			target := filepath.Join(dir, e.Name())
			if _, err := os.Lstat(target); err == nil {
				continue
			}
			_ = os.Symlink(filepath.Join(repoRoot, e.Name()), target)
		}
	}

	data, err := os.ReadFile(filepath.Join(repoRoot, ".claude", "settings.local.json"))
	if err != nil {
		return
	}
	dst := filepath.Join(dir, ".claude", "settings.local.json")
	if _, err := os.Lstat(dst); err == nil {
		return
	}
	if os.MkdirAll(filepath.Dir(dst), 0755) == nil {
		_ = os.WriteFile(dst, data, 0644)
	}
}
