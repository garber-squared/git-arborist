package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/garber-squared/git-arborist/internal/agent"
)

const (
	colBranch = 20
	colAgent  = 12
	colPR     = 50
	colGit    = 14
)

// View renders the dashboard.
func (m *Model) View() string {
	var b strings.Builder

	b.WriteString("\n  Worktree Dashboard (repo-local)\n\n")

	// Header
	b.WriteString(fmt.Sprintf("  %-*s  %-*s  %-*s  %-*s\n",
		colBranch, "Branch",
		colAgent, "Agent",
		colPR, "PR",
		colGit, "Git Status",
	))
	b.WriteString(fmt.Sprintf("  %s  %s  %s  %s\n",
		strings.Repeat("─", colBranch),
		strings.Repeat("─", colAgent),
		strings.Repeat("─", colPR),
		strings.Repeat("─", colGit),
	))

	// Rows
	for i, row := range m.rows {
		cursor := "  "
		if i == m.cursor {
			cursor = "▸ "
		}

		branch := truncate(row.Worktree.Branch, colBranch)

		agentStr := "—"
		if row.ActiveAgent != "" {
			agentStr = row.ActiveAgent
		}
		agentStr = truncate(agentStr, colAgent)
		agentStr = styleAgent(agentStr, row.ActiveAgent, row.AgentActivity)

		prStr := "—"
		if row.PR != nil {
			prStr = row.PR.String()
		}
		prStr = truncate(prStr, colPR)

		gitStr := truncate(row.GitStatus.String(), colGit)

		// Agent column is styled with ANSI codes, so pad it manually
		// to avoid fmt miscount of visible width.
		agentPad := colAgent - lipgloss.Width(agentStr)
		if agentPad < 0 {
			agentPad = 0
		}

		b.WriteString(fmt.Sprintf("%s%-*s  %s%s  %-*s  %-*s\n",
			cursor,
			colBranch, branch,
			agentStr, strings.Repeat(" ", agentPad),
			colPR, prStr,
			colGit, gitStr,
		))
	}

	if len(m.rows) == 0 {
		b.WriteString("  No worktrees found.\n")
	}

	// Status message
	b.WriteString("\n")
	if m.message != "" {
		b.WriteString("  " + m.message + "\n")
	}

	// Help
	b.WriteString("\n  j/k: navigate  enter: tmux jump  o: open PR  g: git status  d: delete  r: refresh  q: quit\n")

	return b.String()
}

var (
	styleRunning = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))  // green
	styleWaiting = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))  // red
	styleIdle    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))  // yellow
)

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
