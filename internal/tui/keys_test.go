package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/garber-squared/git-arborist/internal/worktree"
)

// TestKeysOverlay checks that ? opens the overlay, that it swallows other
// keys instead of acting on them, and that it fits the terminal.
func TestKeysOverlay(t *testing.T) {
	m := NewModel("/repo", "")
	m.width, m.height = 90, 40
	m.handleKey(keyMsg("?"))
	if !m.showKeys {
		t.Fatal("? did not open the overlay")
	}
	m.handleKey(keyMsg("h"))
	if m.expanded || !m.showKeys {
		t.Error("overlay let h through")
	}
	// Tall enough for one column, and short enough to need two.
	for _, height := range []int{40, 30} {
		m.height = height
		view := m.View()
		if h := lipgloss.Height(view); h > height {
			t.Errorf("height %d: overlay is %d rows", height, h)
		}
		if w := lipgloss.Width(view); w > m.width {
			t.Errorf("height %d: overlay is %d columns wide, terminal is %d", height, w, m.width)
		}
	}
	if !strings.Contains(m.View(), "Keybindings") {
		t.Error("overlay not rendered")
	}
	m.handleKey(keyMsg("esc"))
	if m.showKeys {
		t.Error("esc did not close the overlay")
	}
}

// TestKeysOverlayFits checks the overlay never overflows the terminal, and
// that every binding is shown in full at any usable width, scrolling when it
// cannot all fit at once. Below 60 columns descriptions are truncated.
func TestKeysOverlayFits(t *testing.T) {
	for _, w := range []int{40, 60, 80, 100, 170} {
		for _, h := range []int{10, 15, 20, 25, 30, 35, 40, 45} {
			m := NewModel("/repo", "")
			m.width, m.height = w, h
			m.showKeys = true
			seen := ""
			for i := 0; i < 60; i++ {
				view := m.View()
				if vw, vh := lipgloss.Width(view), lipgloss.Height(view); vh > h || vw > w {
					t.Fatalf("%dx%d: overlay is %dx%d", w, h, vw, vh)
				}
				seen += view
				m.handleKey(keyMsg("j"))
			}
			if w < 60 {
				continue
			}
			for _, sec := range keySections() {
				for _, kb := range sec.bindings {
					if !strings.Contains(seen, kb.desc) {
						t.Errorf("%dx%d: %q (%s) never shown", w, h, kb.key, kb.desc)
					}
				}
			}
		}
	}
}

// TestGitStatusOverlay checks g opens a colored git status of the focused
// worktree, and that g closes it again.
func TestGitStatusOverlay(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewModel(dir, "")
	m.width, m.height = 100, 30
	m.rows = []Row{{Worktree: worktree.Worktree{Path: dir, Branch: "main"}}}
	m.handleKey(keyMsg("g"))
	view := m.View()
	if !strings.Contains(view, "new.txt") || !strings.Contains(view, "git status · main") {
		t.Fatalf("overlay missing the status:\n%s", view)
	}
	if !strings.Contains(strings.Join(m.gitStatus, "\n"), "\x1b[") {
		t.Error("git status is not colored")
	}
	m.handleKey(keyMsg("g"))
	if m.gitStatus != nil {
		t.Error("g did not close the overlay")
	}
}
