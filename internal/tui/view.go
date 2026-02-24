package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/garber-squared/git-arborist/internal/agent"
)

const (
	maxVisibleCols = 3
	dashHeaderH    = 2 // title + blank
	dashFooterH    = 3 // message + help + blank
	minTileBodyH   = 3
)

var (
	styleRunning = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	styleWaiting = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
	styleIdle    = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
	styleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("8")) // dim gray

	borderSelected   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("4")) // blue
	borderUnselected = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8")) // dim gray
)

// View renders the tiled dashboard.
func (m *Model) View() string {
	m.computeLayout()

	var b strings.Builder

	b.WriteString("\n  Worktree Dashboard\n")

	if len(m.rows) == 0 {
		b.WriteString("\n  No worktrees found.\n")
		b.WriteString("\n  r: refresh  q: quit\n")
		return b.String()
	}

	// Clamp scrollCol
	maxScroll := len(m.rows) - m.visibleCols
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scrollCol > maxScroll {
		m.scrollCol = maxScroll
	}

	// Render visible tiles
	var tiles []string
	for i := m.scrollCol; i < m.scrollCol+m.visibleCols && i < len(m.rows); i++ {
		selected := i == m.cursorIdx
		tiles = append(tiles, m.renderTile(m.rows[i], selected))
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, tiles...))
	b.WriteString("\n")

	// Scroll indicator
	if len(m.rows) > m.visibleCols {
		pos := m.cursorIdx + 1
		total := len(m.rows)
		b.WriteString(fmt.Sprintf("  [%d/%d]", pos, total))
		if m.scrollCol > 0 {
			b.WriteString(" ◀")
		}
		if m.scrollCol+m.visibleCols < len(m.rows) {
			b.WriteString(" ▶")
		}
		b.WriteString("\n")
	}

	// Status message
	if m.message != "" {
		b.WriteString("  " + m.message + "\n")
	}

	// Help
	b.WriteString("\n  h/l: navigate  enter: tmux jump  o: open PR  g: git status  d: delete  r: refresh  q: quit\n")

	return b.String()
}

func (m *Model) computeLayout() {
	n := len(m.rows)
	if n == 0 {
		m.visibleCols = 0
		return
	}

	// 1 tile → full, 2 → half, 3+ → thirds
	if n == 1 {
		m.visibleCols = 1
	} else if n == 2 {
		m.visibleCols = 2
	} else {
		m.visibleCols = 3
	}

	w := m.width
	if w < 20 {
		w = 20
	}
	m.tileW = w / m.visibleCols

	available := m.height - dashHeaderH - dashFooterH
	if available < minTileBodyH+5 {
		available = minTileBodyH + 5
	}
	m.tileH = available
}

func (m *Model) ensureCursorVisible() {
	if m.visibleCols <= 0 {
		return
	}
	if m.cursorIdx < m.scrollCol {
		m.scrollCol = m.cursorIdx
	} else if m.cursorIdx >= m.scrollCol+m.visibleCols {
		m.scrollCol = m.cursorIdx - m.visibleCols + 1
	}
	if m.scrollCol < 0 {
		m.scrollCol = 0
	}
}

func (m *Model) renderTile(row Row, selected bool) string {
	// Inner width = tile width - border (2 chars: 1 left + 1 right)
	innerW := m.tileW - 4
	if innerW < 10 {
		innerW = 10
	}

	// Header line: bold branch name
	branch := truncate(row.Worktree.Branch, innerW)
	branchStyle := lipgloss.NewStyle().Bold(true)
	header := branchStyle.Render(branch)

	// Info line: agent | git status | PR#
	var infoParts []string
	if row.ActiveAgent != "" {
		infoParts = append(infoParts, styleAgent(row.ActiveAgent, row.ActiveAgent, row.AgentActivity))
	}
	infoParts = append(infoParts, row.GitStatus.String())
	if row.PR != nil {
		infoParts = append(infoParts, fmt.Sprintf("#%d", row.PR.Number))
	}
	infoLine := strings.Join(infoParts, " │ ")

	// Separator
	sep := styleDim.Render(strings.Repeat("─", innerW))

	// Body: pane content or placeholder
	// border top/bottom = 2, header = 1, info = 1, separator = 1
	bodyH := m.tileH - 2 - 3
	if bodyH < minTileBodyH {
		bodyH = minTileBodyH
	}

	var body string
	if row.PaneContent != "" {
		body = cropPaneContent(row.PaneContent, innerW, bodyH)
	} else {
		placeholder := "No tmux pane"
		pad := (bodyH - 1) / 2
		lines := make([]string, bodyH)
		for i := range lines {
			if i == pad {
				left := (innerW - len(placeholder)) / 2
				if left < 0 {
					left = 0
				}
				lines[i] = styleDim.Render(strings.Repeat(" ", left) + placeholder)
			} else {
				lines[i] = ""
			}
		}
		body = strings.Join(lines, "\n")
	}

	content := strings.Join([]string{header, infoLine, sep, body}, "\n")

	var style lipgloss.Style
	if selected {
		style = borderSelected
	} else {
		style = borderUnselected
	}
	style = style.Width(innerW).Height(m.tileH - 2)

	return style.Render(content)
}

func cropPaneContent(content string, width, maxLines int) string {
	lines := strings.Split(content, "\n")

	// Remove trailing empty lines
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	// Take last maxLines
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}

	// Truncate each line to width
	for i, line := range lines {
		if lipgloss.Width(line) > width {
			lines[i] = truncateToWidth(line, width)
		}
	}

	// Pad to maxLines
	for len(lines) < maxLines {
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

func truncateToWidth(s string, maxW int) string {
	if maxW <= 3 {
		return s[:maxW]
	}
	runes := []rune(s)
	if len(runes) <= maxW {
		return s
	}
	return string(runes[:maxW-1]) + "…"
}

func styleAgent(display, name string, activity agent.Activity) string {
	if name == "" {
		return display
	}
	switch activity {
	case agent.ActivityRunning:
		return styleRunning.Render(display)
	case agent.ActivityWaiting:
		return styleWaiting.Render(display)
	default:
		return styleIdle.Render(display)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
