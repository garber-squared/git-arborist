package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/garber-squared/git-arborist/internal/agent"
	"github.com/garber-squared/git-arborist/internal/register"
)

const (
	maxVisibleCols = 3
	maxVisibleRows = 4
	dashHeaderH    = 2 // title + blank
	dashFooterH    = 3 // message + help + blank
	minTileBodyH   = 3
)

var (
	styleRunning = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	styleWaiting = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
	styleIdle    = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
	styleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("8")) // dim gray

	borderSelected   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("4")).Background(lipgloss.Color("#2b2a1a")) // blue border + gentle yellow bg
	borderUnselected = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8"))                                    // dim gray
	borderExpanded   = lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(lipgloss.Color("4")).Background(lipgloss.Color("#2b2a1a"))
)

// View renders the tiled dashboard.
func (m *Model) View() string {
	m.computeLayout()

	if m.expanded && m.cursorIdx < len(m.rows) {
		return m.renderExpandedView()
	}
	return m.renderNormalView()
}

func (m *Model) renderNormalView() string {
	var b strings.Builder

	b.WriteString("\n  Worktree Dashboard\n")

	if len(m.rows) == 0 {
		b.WriteString(m.renderEmptyState())
		b.WriteString("\n  r: refresh  q: quit\n")
		return b.String()
	}

	m.clampScroll()

	// Render grid rows
	var gridRows []string
	for r := m.scrollRow; r < m.scrollRow+m.visibleRows && r < m.gridRows; r++ {
		var rowTiles []string
		startIdx := r * m.visibleCols
		endIdx := startIdx + m.visibleCols
		if endIdx > len(m.rows) {
			endIdx = len(m.rows)
		}
		for i := startIdx; i < endIdx; i++ {
			rowTiles = append(rowTiles, m.renderTile(m.rows[i], i == m.cursorIdx))
		}
		gridRows = append(gridRows, lipgloss.JoinHorizontal(lipgloss.Top, rowTiles...))
	}
	b.WriteString(strings.Join(gridRows, "\n"))
	b.WriteString("\n")

	// Scroll indicator
	if m.gridRows > m.visibleRows {
		b.WriteString(fmt.Sprintf("  [%d/%d]", m.cursorIdx+1, len(m.rows)))
		if m.scrollRow > 0 {
			b.WriteString(" ▲")
		}
		if m.scrollRow+m.visibleRows < m.gridRows {
			b.WriteString(" ▼")
		}
		b.WriteString("\n")
	} else if m.gridRows == 1 && len(m.rows) > m.visibleCols {
		b.WriteString(fmt.Sprintf("  [%d/%d]", m.cursorIdx+1, len(m.rows)))
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
	if m.gridRows > 1 {
		b.WriteString("\n  h/l: navigate  j/k: up/down  up: expand  enter: tmux jump  o: open PR  g: git status  d: delete  r: refresh  q: quit\n")
	} else {
		b.WriteString("\n  h/l: navigate  up: expand  enter: tmux jump  o: open PR  g: git status  d: delete  r: refresh  q: quit\n")
	}

	return b.String()
}

func (m *Model) renderExpandedView() string {
	row := m.rows[m.cursorIdx]

	// 80% of terminal width, full available height
	expW := m.width * 80 / 100
	if expW < 40 {
		expW = 40
	}
	expH := m.height - dashHeaderH - dashFooterH
	if expH < minTileBodyH+5 {
		expH = minTileBodyH + 5
	}

	tile := m.renderTileAt(row, expW, expH, borderExpanded)

	// Help line below the tile
	help := "  esc/up: close  enter: tmux jump  o: open PR  q: quit"

	content := tile + "\n" + help
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m *Model) computeLayout() {
	n := len(m.rows)
	if n == 0 {
		m.visibleCols = 0
		m.gridRows = 0
		m.visibleRows = 0
		return
	}

	switch {
	case n <= 3:
		m.visibleCols = n
		m.gridRows = 1
	case n == 4:
		m.visibleCols = 2
		m.gridRows = 2
	case n <= 6:
		m.visibleCols = 3
		m.gridRows = 2
	case n <= 9:
		m.visibleCols = 3
		m.gridRows = 3
	default:
		m.visibleCols = 4
		m.gridRows = (n + 3) / 4
	}

	m.visibleRows = m.gridRows
	if m.visibleRows > maxVisibleRows {
		m.visibleRows = maxVisibleRows
	}

	w := m.width
	if w < 20 {
		w = 20
	}
	m.tileW = w / m.visibleCols

	available := m.height - dashHeaderH - dashFooterH
	minAvailable := (minTileBodyH + 5) * m.visibleRows
	if available < minAvailable {
		available = minAvailable
	}
	m.tileH = available / m.visibleRows
}

func (m *Model) clampScroll() {
	if m.gridRows > m.visibleRows {
		max := m.gridRows - m.visibleRows
		if m.scrollRow > max {
			m.scrollRow = max
		}
		if m.scrollRow < 0 {
			m.scrollRow = 0
		}
	} else {
		m.scrollRow = 0
	}
	if m.gridRows == 1 {
		max := len(m.rows) - m.visibleCols
		if max < 0 {
			max = 0
		}
		if m.scrollCol > max {
			m.scrollCol = max
		}
	}
}

func (m *Model) ensureCursorVisible() {
	if m.visibleCols <= 0 {
		return
	}
	if m.gridRows <= 1 {
		// Single-row: horizontal scroll
		if m.cursorIdx < m.scrollCol {
			m.scrollCol = m.cursorIdx
		} else if m.cursorIdx >= m.scrollCol+m.visibleCols {
			m.scrollCol = m.cursorIdx - m.visibleCols + 1
		}
		if m.scrollCol < 0 {
			m.scrollCol = 0
		}
		return
	}
	// Grid: vertical scroll
	cursorRow := m.cursorIdx / m.visibleCols
	if cursorRow < m.scrollRow {
		m.scrollRow = cursorRow
	} else if cursorRow >= m.scrollRow+m.visibleRows {
		m.scrollRow = cursorRow - m.visibleRows + 1
	}
	if m.scrollRow < 0 {
		m.scrollRow = 0
	}
}

func (m *Model) renderTile(row Row, selected bool) string {
	var style lipgloss.Style
	if selected {
		style = borderSelected
	} else {
		style = borderUnselected
	}
	return m.renderTileAt(row, m.tileW, m.tileH, style)
}

func (m *Model) renderTileAt(row Row, tileW, tileH int, style lipgloss.Style) string {
	// Inner width = tile width - border (2 chars: 1 left + 1 right)
	innerW := tileW - 4
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
	bodyH := tileH - 2 - 3
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

	style = style.Width(innerW).Height(tileH - 2)

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
	if lipgloss.Width(s) <= maxW {
		return s
	}
	if maxW <= 3 {
		return ansi.Truncate(s, maxW, "")
	}
	return ansi.Truncate(s, maxW-1, "…")
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

func (m *Model) renderEmptyState() string {
	var b strings.Builder

	closed := m.register.RecentlyClosed()
	if len(closed) == 0 {
		b.WriteString("\n  No worktrees yet.\n")
		return b.String()
	}

	b.WriteString("\n  No open worktrees.\n")
	b.WriteString("\n  " + styleDim.Render("Recently closed:") + "\n")
	for _, c := range closed {
		b.WriteString("    " + formatClosedEntry(c) + "\n")
	}
	return b.String()
}

func formatClosedEntry(c register.ClosedEntry) string {
	branch := c.Branch
	if branch == "" {
		branch = "(unknown)"
	}
	branchPart := lipgloss.NewStyle().Bold(true).Render(branch)
	meta := styleDim.Render(fmt.Sprintf("%s · %s", reasonLabel(c.Reason), relativeTime(c.ClosedAt)))
	return fmt.Sprintf("%s  %s", branchPart, meta)
}

func reasonLabel(reason string) string {
	switch reason {
	case register.ReasonMerged:
		return "PR merged"
	case register.ReasonDeleted:
		return "deleted"
	case register.ReasonExternal:
		return "removed externally"
	default:
		return "closed"
	}
}

func relativeTime(t time.Time) string {
	if t.IsZero() {
		return "unknown time"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
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
