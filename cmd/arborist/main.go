package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/garber-squared/git-arborist/internal/tui"
)

func main() {
	repoRoot, err := gitRepoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: not inside a git repository")
		os.Exit(1)
	}

	focusPath, _ := os.Getwd()

	m := tui.NewModel(repoRoot, focusPath)
	p := tea.NewProgram(&m, tea.WithAltScreen())

	// Wire up the send function so the model can create watchers
	// that feed events back into the Bubble Tea event loop.
	m.SetSendFn(p.Send)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// gitRepoRoot returns the dashboard's root repository. When invoked from inside
// a submodule (its main checkout or one of its linked worktrees), it resolves
// up to the superproject working tree so the dashboard reflects the whole
// superproject rather than just the submodule.
func gitRepoRoot() (string, error) {
	// A submodule's git directory lives under <superproject>/.git/modules/<name>.
	// Splitting the common git dir on that segment yields the superproject root,
	// and this works even for linked worktrees where
	// --show-superproject-working-tree reports nothing.
	if commonDir, err := gitOutput("rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil {
		if idx := strings.Index(commonDir, "/.git/modules/"); idx >= 0 {
			return commonDir[:idx], nil
		}
	}

	// We are in the superproject. --show-toplevel would report a linked
	// worktree's own path (e.g. a feature worktree), not the main checkout.
	// Per-submodule discovery needs the main checkout: a submodule inside a
	// linked worktree resolves back to the superproject's git dir, so listing
	// its worktrees from there yields the superproject's worktrees instead of
	// the submodule's — surfacing the same worktree once per submodule. The
	// main worktree is the first entry of `git worktree list`.
	if main, err := mainWorktree(); err == nil {
		return main, nil
	}
	return gitOutput("rev-parse", "--show-toplevel")
}

// mainWorktree returns the path of the current repository's main worktree,
// which `git worktree list` always reports first.
func mainWorktree() (string, error) {
	out, err := gitOutput("worktree", "list", "--porcelain")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			return strings.TrimSpace(path), nil
		}
	}
	return "", fmt.Errorf("no worktree found")
}

func gitOutput(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
