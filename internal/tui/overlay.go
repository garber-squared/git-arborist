package tui

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderScrollBox frames lines in the overlay box, clipped to the terminal
// and scrolled to m.overlayScroll (clamped here, so key handlers can just
// step it). Lines too wide for the terminal are truncated; ANSI color in them
// survives.
func (m *Model) renderScrollBox(title string, lines []string, closeHint string) string {
	visible := max(1, m.height-4) // border, title and footer
	m.overlayScroll = min(max(0, m.overlayScroll), max(0, len(lines)-visible))
	end := min(len(lines), m.overlayScroll+visible)

	lineW := max(10, m.width-4) // border and padding
	shown := make([]string, 0, end-m.overlayScroll)
	for _, line := range lines[m.overlayScroll:end] {
		shown = append(shown, truncateToWidth(line, lineW))
	}
	footer := closeHint
	if len(lines) > visible {
		footer = fmt.Sprintf("j/k: scroll (%d-%d of %d)  %s", m.overlayScroll+1, end, len(lines), closeHint)
	}
	return styleKeysBox.Padding(0, 1).Render(
		styleKeysHeading.Render(truncateToWidth(title, lineW)) + "\n" +
			strings.Join(shown, "\n") + "\n" +
			styleDim.Render(truncateToWidth(footer, lineW)))
}

// openGitStatus runs a colored `git status` in the focused worktree — the
// same one the watch window shows — and opens it in the `g` overlay.
func (m *Model) openGitStatus() {
	if m.cursorIdx >= len(m.rows) {
		return
	}
	row := m.rows[m.cursorIdx]
	out, err := exec.Command("git", "-C", row.Worktree.Path, "-c", "color.status=always", "status").CombinedOutput()
	if err != nil {
		m.message = fmt.Sprintf("git status failed: %s", strings.TrimSpace(string(out)))
		return
	}
	// git indents paths with tabs, which lipgloss cannot measure.
	text := strings.ReplaceAll(strings.TrimRight(string(out), "\n"), "\t", "    ")
	m.gitStatus = strings.Split(text, "\n")
	m.gitBranch = row.Worktree.Branch
	m.overlayScroll = 0
}

// renderGitStatusView renders the `g` overlay centred on screen.
func (m *Model) renderGitStatusView() string {
	box := m.renderScrollBox("git status · "+m.gitBranch, m.gitStatus, "g / esc / q: close")
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
