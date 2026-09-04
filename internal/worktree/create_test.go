package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// run executes a command in dir, failing the test on error.
func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// setupRepo builds a clone with: local branch "local-only", remote-only
// branch "remote-only", slashed branch "ds/slashed" already checked out in a
// worktree, plus main/staging.
func setupRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	src := filepath.Join(root, "src")

	run(t, root, "git", "init", "--bare", "-b", "main", origin)
	run(t, root, "git", "init", "-b", "main", src)
	run(t, src, "git", "config", "user.email", "t@t")
	run(t, src, "git", "config", "user.name", "t")
	os.WriteFile(filepath.Join(src, "f"), []byte("x"), 0644)
	run(t, src, "git", "add", ".")
	run(t, src, "git", "commit", "-m", "init")
	run(t, src, "git", "branch", "staging")
	run(t, src, "git", "branch", "remote-only")
	run(t, src, "git", "branch", "ds/slashed")
	run(t, src, "git", "remote", "add", "origin", origin)
	run(t, src, "git", "push", "-u", "origin", "main", "staging", "remote-only", "ds/slashed")

	clone := filepath.Join(root, "clone")
	run(t, root, "git", "clone", origin, clone)
	run(t, clone, "git", "config", "user.email", "t@t")
	run(t, clone, "git", "config", "user.name", "t")
	run(t, clone, "git", "branch", "local-only")
	// ds/slashed already lives in a worktree.
	run(t, clone, "git", "worktree", "add", filepath.Join(clone, "worktrees", "ds", "slashed"), "ds/slashed")
	return clone
}

func TestListCandidates(t *testing.T) {
	clone := setupRepo(t)

	cands, err := ListCandidates(clone)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range cands {
		got[c.Branch] = c.Remote
	}
	t.Logf("candidates: %+v", got)

	if remote, ok := got["local-only"]; !ok || remote != "" {
		t.Errorf("local-only: want local candidate, got %q ok=%v", remote, ok)
	}
	if remote := got["remote-only"]; remote != "origin/remote-only" {
		t.Errorf("remote-only: want origin/remote-only, got %q", remote)
	}
	if _, ok := got["ds/slashed"]; ok {
		t.Error("ds/slashed already has a worktree; should not be offered")
	}
	// main is checked out in the clone's main worktree, so it is taken.
	if _, ok := got["main"]; ok {
		t.Error("main is checked out in the main worktree; should not be offered")
	}
	if _, ok := got["origin"]; ok {
		t.Error("bare remote name offered as a branch")
	}
	if _, ok := got["HEAD"]; ok {
		t.Error("remote HEAD offered as a branch")
	}
}

func TestRefHelpers(t *testing.T) {
	clone := setupRepo(t)

	if def := DefaultBranch(clone); def != "main" {
		t.Errorf("DefaultBranch = %q, want main", def)
	}
	bases := BaseRefs(clone)
	if len(bases) == 0 || bases[0] != "main" {
		t.Errorf("BaseRefs = %v, want main first", bases)
	}
	for _, want := range []string{"staging", "remote-only", "ds/slashed"} {
		found := false
		for _, b := range bases {
			if b == want {
				found = true
			}
		}
		if !found {
			t.Errorf("BaseRefs missing %q: %v", want, bases)
		}
	}
	if ref := RemoteRef(clone, "ds/slashed"); ref != "origin/ds/slashed" {
		t.Errorf("RemoteRef(ds/slashed) = %q", ref)
	}
	if ref := RemoteRef(clone, "local-only"); ref != "" {
		t.Errorf("RemoteRef(local-only) = %q, want empty", ref)
	}
	if ref, err := StartRefFor(clone, "local-only"); err != nil || ref != "" {
		t.Errorf("StartRefFor(local-only) = %q, %v; want checkout-in-place", ref, err)
	}
	if ref, err := StartRefFor(clone, "remote-only"); err != nil || ref != "origin/remote-only" {
		t.Errorf("StartRefFor(remote-only) = %q, %v", ref, err)
	}
	if _, err := StartRefFor(clone, "nope"); err == nil {
		t.Error("StartRefFor(nope): want error")
	}
	if ref := BaseStartRef(clone, "staging"); ref != "origin/staging" {
		t.Errorf("BaseStartRef(staging) = %q, want the remote tip", ref)
	}
	if ref := BaseStartRef(clone, "local-only"); ref != "local-only" {
		t.Errorf("BaseStartRef(local-only) = %q", ref)
	}
}

func TestCreate(t *testing.T) {
	clone := setupRepo(t)
	// Untracked local config the new worktree should inherit.
	os.WriteFile(filepath.Join(clone, ".env"), []byte("A=1"), 0644)
	os.WriteFile(filepath.Join(clone, ".env.local"), []byte("B=2"), 0644)
	os.MkdirAll(filepath.Join(clone, ".claude"), 0755)
	os.WriteFile(filepath.Join(clone, ".claude", "settings.local.json"), []byte(`{"permissions":{}}`), 0644)

	// Existing local branch.
	wt, err := Create(CreateOptions{RepoRoot: clone, Branch: "local-only"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(clone, "worktrees", "local-only"); wt.Path != want {
		t.Errorf("path = %q, want %q", wt.Path, want)
	}
	if out := run(t, wt.Path, "git", "branch", "--show-current"); out != "local-only" {
		t.Errorf("checked out %q", out)
	}
	for _, f := range []string{".env", ".env.local"} {
		target, err := os.Readlink(filepath.Join(wt.Path, f))
		if err != nil || target != filepath.Join(clone, f) {
			t.Errorf("%s symlink = %q, %v", f, target, err)
		}
	}
	if _, err := os.Stat(filepath.Join(wt.Path, ".claude", "settings.local.json")); err != nil {
		t.Errorf("claude settings not copied: %v", err)
	}
	excl, _ := os.ReadFile(filepath.Join(clone, ".git", "info", "exclude"))
	if !strings.Contains(string(excl), "worktrees/") {
		t.Errorf("info/exclude = %q", excl)
	}

	// Remote-only branch: creates a local branch tracking the remote tip.
	wt2, err := Create(CreateOptions{RepoRoot: clone, Branch: "remote-only", StartRef: "origin/remote-only"})
	if err != nil {
		t.Fatal(err)
	}
	if out := run(t, wt2.Path, "git", "branch", "--show-current"); out != "remote-only" {
		t.Errorf("checked out %q", out)
	}

	// Brand new branch off a base, with a slash in the name.
	wt3, err := Create(CreateOptions{RepoRoot: clone, Branch: "feat/new-thing", StartRef: BaseStartRef(clone, "staging")})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(clone, "worktrees", "feat", "new-thing"); wt3.Path != want {
		t.Errorf("path = %q, want %q", wt3.Path, want)
	}
	if out := run(t, wt3.Path, "git", "branch", "--show-current"); out != "feat/new-thing" {
		t.Errorf("checked out %q", out)
	}

	// Discovery must round-trip the slashed branch name.
	wts, err := DiscoverAll(clone)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, w := range wts {
		names = append(names, w.Branch)
	}
	t.Logf("discovered: %v", names)
	found := false
	for _, n := range names {
		if n == "feat/new-thing" {
			found = true
		}
	}
	if !found {
		t.Errorf("DiscoverAll lost the slashed branch name: %v", names)
	}

	// Creating it again must fail with git's own message, not a bare exit code.
	if _, err := Create(CreateOptions{RepoRoot: clone, Branch: "local-only"}); err == nil {
		t.Error("duplicate create: want error")
	} else {
		t.Logf("duplicate create error: %v", err)
	}

	// Exclude entry must not be duplicated.
	excl, _ = os.ReadFile(filepath.Join(clone, ".git", "info", "exclude"))
	if n := strings.Count(string(excl), "worktrees/"); n != 1 {
		t.Errorf("exclude has %d worktrees/ entries:\n%s", n, excl)
	}

	// Invalid branch names are rejected before touching git.
	if _, err := Create(CreateOptions{RepoRoot: clone, Branch: "bad branch~name"}); err == nil {
		t.Error("invalid branch name: want error")
	}
}

func TestSetupCommand(t *testing.T) {
	clone := setupRepo(t)
	wt, err := Create(CreateOptions{RepoRoot: clone, Branch: "local-only"})
	if err != nil {
		t.Fatal(err)
	}

	if got := SetupCommand(wt); got != "" {
		t.Errorf("no config and no script: got %q, want empty", got)
	}

	// A script in the worktree is found without any configuration.
	script := filepath.Join(wt.Path, "scripts", "worktree-setup.sh")
	os.MkdirAll(filepath.Dir(script), 0755)
	os.WriteFile(script, []byte("#!/usr/bin/env bash\nbundle install\n"), 0755)
	if got, want := SetupCommand(wt), "'./scripts/worktree-setup.sh' '"+wt.Path+"'"; got != want {
		t.Errorf("script: got %q, want %q", got, want)
	}

	// Without the exec bit it still runs, through bash.
	os.Chmod(script, 0644)
	if got, want := SetupCommand(wt), "bash './scripts/worktree-setup.sh' '"+wt.Path+"'"; got != want {
		t.Errorf("non-executable script: got %q, want %q", got, want)
	}

	// The repo root is the fallback location.
	os.Remove(script)
	root := filepath.Join(wt.Path, "worktree-setup.sh")
	os.WriteFile(root, []byte("#!/usr/bin/env bash\nmix deps.get\n"), 0755)
	if got, want := SetupCommand(wt), "'./worktree-setup.sh' '"+wt.Path+"'"; got != want {
		t.Errorf("root script: got %q, want %q", got, want)
	}

	// scripts/ wins over the root.
	os.MkdirAll(filepath.Dir(script), 0755)
	os.WriteFile(script, []byte("#!/usr/bin/env bash\n"), 0755)
	if got, want := SetupCommand(wt), "'./scripts/worktree-setup.sh' '"+wt.Path+"'"; got != want {
		t.Errorf("precedence between locations: got %q, want %q", got, want)
	}

	// git config overrides the script entirely.
	run(t, clone, "git", "config", "arborist.setup", "make bootstrap")
	if got, want := SetupCommand(wt), "make bootstrap"; got != want {
		t.Errorf("config override: got %q, want %q", got, want)
	}
	// ...and it is read through the worktree, which shares the repo's config.
	run(t, clone, "git", "config", "--unset", "arborist.setup")
	if got, want := SetupCommand(wt), "'./scripts/worktree-setup.sh' '"+wt.Path+"'"; got != want {
		t.Errorf("after unset: got %q, want %q", got, want)
	}

	if got := SetupCommand(Worktree{Path: t.TempDir()}); got != "" {
		t.Errorf("outside a repo with no script: got %q, want empty", got)
	}
}

func TestShellQuote(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"./worktree-setup.sh", "'./worktree-setup.sh'"},
		{"/my repo/setup.sh", "'/my repo/setup.sh'"},
		{"it's.sh", `'it'\''s.sh'`},
	} {
		if got := shellQuote(tc.in); got != tc.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWindowCommand(t *testing.T) {
	clone := setupRepo(t)
	wt, err := Create(CreateOptions{RepoRoot: clone, Branch: "local-only"})
	if err != nil {
		t.Fatal(err)
	}
	watcher := `watch -n 1 -c 'git -c color.status=always status'`

	// Nothing configured: work mode leaves a plain shell, watch mode still
	// watches — that is its whole purpose.
	if got := WindowCommand(wt, ModeWork); got != "" {
		t.Errorf("bare repo, work: got %q, want empty", got)
	}
	if got := WindowCommand(wt, ModeWatch); got != watcher {
		t.Errorf("bare repo, watch: got %q, want %q", got, watcher)
	}

	// A setup script alone.
	script := filepath.Join(wt.Path, "scripts", "worktree-setup.sh")
	os.MkdirAll(filepath.Dir(script), 0755)
	os.WriteFile(script, []byte("#!/usr/bin/env bash\nnpm install\n"), 0755)
	setup := "'./scripts/worktree-setup.sh' '" + wt.Path + "'"
	if got := WindowCommand(wt, ModeWork); got != setup {
		t.Errorf("script, work: got %q, want %q", got, setup)
	}
	// Watch runs after setup with ";" — a failed setup is exactly when the
	// status matters.
	if got, want := WindowCommand(wt, ModeWatch), setup+"; "+watcher; got != want {
		t.Errorf("script, watch: got %q, want %q", got, want)
	}

	// The start command joins with "&&": no agent in a half-installed tree.
	run(t, clone, "git", "config", "arborist.start", "make issue-fetch")
	if got, want := WindowCommand(wt, ModeWork), setup+" && make issue-fetch"; got != want {
		t.Errorf("start, work: got %q, want %q", got, want)
	}
	// Watch mode must never run the start command.
	if got, want := WindowCommand(wt, ModeWatch), setup+"; "+watcher; got != want {
		t.Errorf("start must not leak into watch: got %q, want %q", got, want)
	}

	// The watcher is overridable.
	run(t, clone, "git", "config", "arborist.watch", "watch -n 5 git log --oneline -5")
	if got, want := WindowCommand(wt, ModeWatch), setup+"; watch -n 5 git log --oneline -5"; got != want {
		t.Errorf("watch override: got %q, want %q", got, want)
	}

	// A start command with no setup at all.
	os.Remove(script)
	if got, want := WindowCommand(wt, ModeWork), "make issue-fetch"; got != want {
		t.Errorf("start without setup: got %q, want %q", got, want)
	}

	if got, want := ModeWork.String()+"/"+ModeWatch.String(), "work/watch"; got != want {
		t.Errorf("mode names = %q, want %q", got, want)
	}
}

func TestIssueLabel(t *testing.T) {
	clone := setupRepo(t)
	wt, err := Create(CreateOptions{RepoRoot: clone, Branch: "local-only"})
	if err != nil {
		t.Fatal(err)
	}
	// Off unless the repo asks: labelling writes to GitHub.
	if got := IssueLabel(wt.Path); got != "" {
		t.Errorf("unset: got %q, want empty", got)
	}
	run(t, clone, "git", "config", "arborist.issueLabel", "status:in-development")
	if got, want := IssueLabel(wt.Path), "status:in-development"; got != want {
		t.Errorf("IssueLabel = %q, want %q", got, want)
	}
}
