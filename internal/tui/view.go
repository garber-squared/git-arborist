package tui

import (
	"fmt"
	"strings"
)

const (
	colBranch = 20
	colAgent  = 16
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
		if row.AgentState != nil {
			agentStr = row.AgentState.Status
		}
		agentStr = truncate(agentStr, colAgent)

		prStr := "—"
		if row.PR != nil {
			prStr = row.PR.String()
		}
		prStr = truncate(prStr, colPR)

		gitStr := truncate(row.GitStatus.String(), colGit)

		b.WriteString(fmt.Sprintf("%s%-*s  %-*s  %-*s  %-*s\n",
			cursor,
			colBranch, branch,
			colAgent, agentStr,
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
	b.WriteString("\n  j/k: navigate  enter: tmux jump  o: open PR  g: git status  r: refresh  q: quit\n")

	return b.String()
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
