package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Worktree struct {
	Path   string
	Branch string
	// Repo is the submodule name this worktree belongs to, or "" for the
	// superproject (top-level repository).
	Repo string
	// RepoRoot is the working directory of the owning repository's main
	// checkout. Worktree operations (remove, prune) must be run from here so
	// they target the correct repo when submodules are involved.
	RepoRoot string
	// IsMain reports whether this is the main worktree of its repository
	// (the first entry returned by `git worktree list`). Main worktrees are
	// the repo root / submodule git dir and are not feature worktrees.
	IsMain bool
}

// Discover returns all worktrees for the repository at repoRoot.
func Discover(repoRoot string) ([]Worktree, error) {
	return discoverRepo(repoRoot, "")
}

// DiscoverAll returns worktrees for repoRoot and, when repoRoot is a git
// superproject, for each of its submodules as well. Every worktree is tagged
// with its owning Repo/RepoRoot and whether it is the main worktree. Stale
// entries are pruned for each repository before listing.
func DiscoverAll(repoRoot string) ([]Worktree, error) {
	Prune(repoRoot)
	all, err := discoverRepo(repoRoot, "")
	if err != nil {
		return nil, err
	}

	for _, sub := range submodulePaths(repoRoot) {
		subRoot := filepath.Join(repoRoot, sub)
		if info, statErr := os.Stat(subRoot); statErr != nil || !info.IsDir() {
			continue // submodule not initialized / not checked out
		}
		Prune(subRoot)
		subWts, err := discoverRepo(subRoot, sub)
		if err != nil {
			continue // not a git repo (uninitialized submodule)
		}
		all = append(all, subWts...)
	}
	return all, nil
}

// discoverRepo lists the worktrees of a single repository rooted at repoRoot,
// tagging each with the given repo name (submodule name, or "" for the
// superproject).
func discoverRepo(repoRoot, repoName string) ([]Worktree, error) {
	cmd := exec.Command("git", "-C", repoRoot, "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	wts := parsePorcelain(string(out))
	for i := range wts {
		wts[i].Repo = repoName
		wts[i].RepoRoot = repoRoot
		wts[i].IsMain = i == 0
	}
	return wts, nil
}

// submodulePaths returns the relative paths of the submodules declared in
// repoRoot's .gitmodules file. Returns nil when repoRoot is not a superproject.
func submodulePaths(repoRoot string) []string {
	gitmodules := filepath.Join(repoRoot, ".gitmodules")
	cmd := exec.Command("git", "config", "--file", gitmodules, "--get-regexp", `\.path$`)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		// Each line is "submodule.<name>.path <relative-path>".
		if fields := strings.Fields(line); len(fields) == 2 {
			paths = append(paths, fields[1])
		}
	}
	return paths
}

// Prune removes stale worktree entries whose working directories no longer exist.
func Prune(repoRoot string) {
	_ = exec.Command("git", "-C", repoRoot, "worktree", "prune").Run()
}

// Remove removes a worktree using git worktree remove, run from the worktree's
// owning repository so it targets the correct repo for submodule worktrees.
func Remove(wt Worktree) error {
	return exec.Command("git", "-C", wt.RepoRoot, "worktree", "remove", wt.Path).Run()
}

// ForceRemove removes a worktree even if it has uncommitted changes.
func ForceRemove(wt Worktree) error {
	return exec.Command("git", "-C", wt.RepoRoot, "worktree", "remove", "--force", wt.Path).Run()
}

func parsePorcelain(raw string) []Worktree {
	var worktrees []Worktree
	var current Worktree

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "worktree "):
			current = Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimPrefix(line, "branch ")
			current.Branch = filepath.Base(ref)
		case line == "":
			if current.Path != "" {
				worktrees = append(worktrees, current)
				current = Worktree{}
			}
		}
	}
	if current.Path != "" {
		worktrees = append(worktrees, current)
	}
	return worktrees
}
