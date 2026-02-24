package tui

import (
	"fmt"
	"os/exec"
	"time"

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

// agentTickMsg triggers a periodic agent detection refresh.
type agentTickMsg struct{}

// paneTickMsg triggers a periodic pane content refresh.
type paneTickMsg struct{}

func agentTickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return agentTickMsg{}
	})
}

func paneTickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return paneTickMsg{}
	})
}

// Init initializes the model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg { return refreshMsg{} },
		agentTickCmd(),
		paneTickCmd(),
	)
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

	case agentTickMsg:
		m.refreshAgents()
		return m, agentTickCmd()

	case paneTickMsg:
		m.refreshPaneContent()
		return m, paneTickCmd()

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
			if m.cursorIdx < len(m.rows) {
				row := m.rows[m.cursorIdx]
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

	case msg.String() == "h" || msg.String() == "left":
		if m.cursorIdx > 0 {
			m.cursorIdx--
		}
		m.ensureCursorVisible()

	case msg.String() == "l" || msg.String() == "right":
		if m.cursorIdx < len(m.rows)-1 {
			m.cursorIdx++
		}
		m.ensureCursorVisible()

	case msg.String() == "r":
		m.message = "Refreshing..."
		m.refreshAll()
		m.message = ""

	case msg.String() == "enter":
		if m.cursorIdx < len(m.rows) {
			row := m.rows[m.cursorIdx]
			if row.PaneTarget != "" {
				err := tmux.JumpToPane(row.PaneTarget)
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
		if m.cursorIdx < len(m.rows) {
			row := m.rows[m.cursorIdx]
			if row.PR != nil {
				_ = pr.OpenInBrowser(row.Worktree.Path)
			} else {
				m.message = "No PR for this branch"
			}
		}

	case msg.String() == "g":
		if m.cursorIdx < len(m.rows) {
			row := m.rows[m.cursorIdx]
			cmd := exec.Command("git", "-C", row.Worktree.Path, "status", "--short")
			out, err := cmd.Output()
			if err == nil {
				m.message = string(out)
			}
		}

	case msg.String() == "d":
		if m.cursorIdx == 0 {
			m.message = "Cannot delete the main worktree"
		} else if m.cursorIdx < len(m.rows) {
			m.confirming = true
			m.message = fmt.Sprintf("Delete worktree '%s'? Press d to confirm, any other key to cancel", m.rows[m.cursorIdx].Worktree.Branch)
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
			Worktree:  wt,
			GitStatus: gitstatus.Get(wt.Path),
			PR:        pr.Fetch(wt.Path),
			AgentState: agent.ReadState(wt.Path),
		}
	}
	m.rows = rows
	if m.cursorIdx >= len(m.rows) {
		m.cursorIdx = max(0, len(m.rows)-1)
	}

	m.refreshAgents()
	m.refreshPaneTargets()
	m.refreshPaneContent()

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

func (m *Model) refreshAgents() {
	detected := agent.DetectAll()
	for i, row := range m.rows {
		if info, ok := detected[row.Worktree.Path]; ok {
			m.rows[i].ActiveAgent = info.Name
			m.rows[i].AgentActivity = info.Activity
		} else if row.AgentState != nil && row.AgentState.Agent != "" {
			m.rows[i].ActiveAgent = row.AgentState.Agent
			m.rows[i].AgentActivity = agent.ActivityIdle
		} else {
			m.rows[i].ActiveAgent = ""
			m.rows[i].AgentActivity = agent.ActivityIdle
		}
	}
	m.refreshPaneTargets()
}

func (m *Model) refreshPaneTargets() {
	panes, err := tmux.ListPanes()
	if err != nil {
		return
	}

	// Build path → target map
	pathToTarget := make(map[string]string)
	for _, p := range panes {
		pathToTarget[p.Path] = p.Target
	}

	for i, row := range m.rows {
		// Prefer agent state TMUX ref if available
		if row.AgentState != nil && row.AgentState.TMUX.Session != "" {
			m.rows[i].PaneTarget = fmt.Sprintf("%s:%d.%d",
				row.AgentState.TMUX.Session,
				row.AgentState.TMUX.Window,
				row.AgentState.TMUX.Pane)
		} else if target, ok := pathToTarget[row.Worktree.Path]; ok {
			m.rows[i].PaneTarget = target
		} else {
			m.rows[i].PaneTarget = ""
		}
	}
}

func (m *Model) refreshPaneContent() {
	for i, row := range m.rows {
		if row.PaneTarget != "" {
			m.rows[i].PaneContent = tmux.CapturePaneContent(row.PaneTarget)
		} else {
			m.rows[i].PaneContent = ""
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
