package tui

import (
	"fmt"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/garber-squared/git-arborist/internal/agent"
	"github.com/garber-squared/git-arborist/internal/gitstatus"
	"github.com/garber-squared/git-arborist/internal/pr"
	"github.com/garber-squared/git-arborist/internal/tmux"
	"github.com/garber-squared/git-arborist/internal/watcher"
	"github.com/garber-squared/git-arborist/internal/worktree"
)

// refreshMsg triggers a full data refresh.
type refreshMsg struct{}

// Init initializes the model.
func (m *Model) Init() tea.Cmd {
	return func() tea.Msg { return refreshMsg{} }
}

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		return m.handleKey(msg)

	case refreshMsg:
		m.refreshAll()

	case watcher.FileChangedMsg:
		m.refreshRow(msg.WorktreePath, msg.Kind)
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle confirmation state first
	if m.confirming {
		if msg.String() == "d" {
			m.confirming = false
			if m.cursor < len(m.rows) {
				row := m.rows[m.cursor]
				// Kill tmux window
				if row.AgentState != nil && row.AgentState.TMUX.Session != "" {
					_ = tmux.KillWindow(row.AgentState.TMUX.Session, row.AgentState.TMUX.Window)
				} else {
					_ = tmux.KillWindowByPath(row.Worktree.Path)
				}
				// Remove worktree
				if err := worktree.Remove(row.Worktree.Path); err != nil {
					m.message = fmt.Sprintf("delete failed: %v", err)
					return m, nil
				}
				m.message = fmt.Sprintf("Deleted worktree '%s'", row.Worktree.Branch)
				m.refreshAll()
			}
		} else {
			m.confirming = false
			m.message = ""
		}
		return m, nil
	}

	switch {
	case msg.String() == "q" || msg.String() == "ctrl+c":
		if m.watcher != nil {
			m.watcher.Close()
		}
		return m, tea.Quit

	case msg.String() == "k" || msg.String() == "up":
		if m.cursor > 0 {
			m.cursor--
		}

	case msg.String() == "j" || msg.String() == "down":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}

	case msg.String() == "r":
		m.message = "Refreshing..."
		m.refreshAll()
		m.message = ""

	case msg.String() == "enter":
		if m.cursor < len(m.rows) {
			row := m.rows[m.cursor]
			if row.AgentState != nil && row.AgentState.TMUX.Session != "" {
				err := tmux.JumpToWindow(row.AgentState.TMUX.Session, row.AgentState.TMUX.Window)
				if err != nil {
					m.message = fmt.Sprintf("tmux jump failed: %v", err)
				}
			} else {
				err := tmux.FindWindowByPath(row.Worktree.Path)
				if err != nil {
					m.message = fmt.Sprintf("tmux: %v", err)
				}
			}
		}

	case msg.String() == "o":
		if m.cursor < len(m.rows) {
			row := m.rows[m.cursor]
			if row.PR != nil {
				_ = pr.OpenInBrowser(row.Worktree.Path)
			} else {
				m.message = "No PR for this branch"
			}
		}

	case msg.String() == "g":
		if m.cursor < len(m.rows) {
			row := m.rows[m.cursor]
			cmd := exec.Command("git", "-C", row.Worktree.Path, "status", "--short")
			out, err := cmd.Output()
			if err == nil {
				m.message = string(out)
			}
		}

	case msg.String() == "d":
		if m.cursor == 0 {
			m.message = "Cannot delete the main worktree"
		} else if m.cursor < len(m.rows) {
			m.confirming = true
			m.message = fmt.Sprintf("Delete worktree '%s'? Press d to confirm, any other key to cancel", m.rows[m.cursor].Worktree.Branch)
		}
	}
	return m, nil
}

func (m *Model) refreshAll() {
	worktrees, err := worktree.Discover(m.repoRoot)
	if err != nil {
		m.message = fmt.Sprintf("discovery error: %v", err)
		return
	}

	rows := make([]Row, len(worktrees))
	for i, wt := range worktrees {
		rows[i] = Row{
			Worktree:   wt,
			GitStatus:  gitstatus.Get(wt.Path),
			PR:         pr.Fetch(wt.Path),
			AgentState: agent.ReadState(wt.Path),
		}
	}
	m.rows = rows
	if m.cursor >= len(m.rows) {
		m.cursor = max(0, len(m.rows)-1)
	}

	// Set up file watchers
	if m.watcher != nil {
		m.watcher.Close()
	}
	if m.sendFn != nil {
		w, err := watcher.New(m.sendFn)
		if err == nil {
			m.watcher = w
			for _, row := range m.rows {
				w.WatchWorktree(row.Worktree.Path)
			}
		}
	}
}

func (m *Model) refreshRow(wtPath, kind string) {
	for i, row := range m.rows {
		if row.Worktree.Path == wtPath {
			switch kind {
			case "agent":
				m.rows[i].AgentState = agent.ReadState(wtPath)
			case "git":
				m.rows[i].GitStatus = gitstatus.Get(wtPath)
			default:
				m.rows[i].GitStatus = gitstatus.Get(wtPath)
				m.rows[i].AgentState = agent.ReadState(wtPath)
			}
			return
		}
	}
}
