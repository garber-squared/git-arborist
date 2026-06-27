package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/garber-squared/git-arborist/internal/agent"
	"github.com/garber-squared/git-arborist/internal/docker"
	"github.com/garber-squared/git-arborist/internal/gitstatus"
	"github.com/garber-squared/git-arborist/internal/pr"
	"github.com/garber-squared/git-arborist/internal/register"
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
				// Stop and delete any docker compose containers tied to this worktree
				_ = docker.RemoveContainersForWorktree(row.Worktree.Path)
				// Remove worktree
				if err := worktree.Remove(row.Worktree); err != nil {
					m.message = fmt.Sprintf("delete failed: %v", err)
					return m, nil
				}
				m.register.RecordClose(row.Worktree.Path, row.Worktree.Branch, register.ReasonDeleted)
				_ = m.register.Save()
				m.message = fmt.Sprintf("Deleted worktree '%s'", row.Worktree.Branch)
				m.refreshAll()
			}
		} else {
			m.confirming = false
			m.message = ""
		}
		return m, nil
	}

	// Handle expanded overlay state
	if m.expanded {
		switch msg.String() {
		case "up", "esc":
			m.expanded = false
			return m, nil
		case "h", "l", "left", "right", "j", "k", "down", "d", "r", "g", "s", "n", "N":
			return m, nil
		case "q", "ctrl+c":
			m.expanded = false
			// fall through to normal handling
		case "enter", "o":
			m.expanded = false
			// fall through to normal handling
		}
	}

	switch {
	case msg.String() == "q" || msg.String() == "ctrl+c":
		if m.cursorIdx < len(m.rows) {
			_ = os.WriteFile(m.stateFile, []byte(m.rows[m.cursorIdx].Worktree.Path), 0644)
		}
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

	case msg.String() == "j" || msg.String() == "down":
		if m.visibleCols > 0 && m.gridRows > 1 {
			col := m.cursorIdx % m.visibleCols
			row := m.cursorIdx / m.visibleCols
			if row+1 < m.gridRows {
				target := (row+1)*m.visibleCols + col
				if target >= len(m.rows) {
					target = len(m.rows) - 1
				}
				m.cursorIdx = target
				m.ensureCursorVisible()
			}
		}

	case msg.String() == "k":
		if m.visibleCols > 0 && m.gridRows > 1 {
			col := m.cursorIdx % m.visibleCols
			row := m.cursorIdx / m.visibleCols
			if row > 0 {
				m.cursorIdx = (row-1)*m.visibleCols + col
				m.ensureCursorVisible()
			}
		}

	case msg.String() == "up":
		m.expanded = true

	case msg.String() == "r":
		m.message = "Refreshing..."
		m.refreshAll()
		m.message = ""

	case msg.String() == "e":
		if m.cursorIdx < len(m.rows) {
			row := m.rows[m.cursorIdx]
			if row.PaneTarget != "" {
				if err := tmux.SendKeys(row.PaneTarget, "Enter"); err != nil {
					m.message = fmt.Sprintf("send-keys failed: %v", err)
				} else {
					m.message = fmt.Sprintf("Sent Enter to %s", row.Worktree.Branch)
				}
			} else {
				m.message = "No tmux pane found for this worktree"
			}
		}

	case msg.String() == "t":
		if m.cursorIdx < len(m.rows) {
			row := m.rows[m.cursorIdx]
			if row.PaneTarget != "" {
				if err := tmux.SendKeys(row.PaneTarget, "Tab", "Enter"); err != nil {
					m.message = fmt.Sprintf("send-keys failed: %v", err)
				} else {
					m.message = fmt.Sprintf("Sent Tab+Enter to %s", row.Worktree.Branch)
				}
			} else {
				m.message = "No tmux pane found for this worktree"
			}
		}

	case msg.String() == "n":
		if m.cursorIdx < len(m.rows) {
			row := m.rows[m.cursorIdx]
			if row.PaneTarget != "" {
				m.message = fmt.Sprintf("tmux pane already exists for '%s'", row.Worktree.Branch)
			} else {
				if err := tmux.NewWindow(row.Worktree.Path, row.Worktree.Branch); err != nil {
					m.message = fmt.Sprintf("tmux new-window failed: %v", err)
				} else {
					m.message = fmt.Sprintf("Created tmux window for '%s'", row.Worktree.Branch)
					m.refreshPaneTargets()
					m.refreshPaneContent()
				}
			}
		}

	case msg.String() == "N":
		var created, failed int
		for _, row := range m.rows {
			if row.PaneTarget != "" {
				continue
			}
			if err := tmux.NewWindow(row.Worktree.Path, row.Worktree.Branch); err != nil {
				failed++
			} else {
				created++
			}
		}
		m.refreshPaneTargets()
		m.refreshPaneContent()
		switch {
		case failed > 0 && created > 0:
			m.message = fmt.Sprintf("Created %d tmux windows (%d failed)", created, failed)
		case failed > 0:
			m.message = fmt.Sprintf("Failed to create %d tmux windows", failed)
		case created == 0:
			m.message = "All worktrees already have tmux windows"
		default:
			m.message = fmt.Sprintf("Created %d tmux windows", created)
		}

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
		if m.cursorIdx < len(m.rows) {
			m.confirming = true
			m.message = fmt.Sprintf("Delete worktree '%s'? Press d to confirm, any other key to cancel", m.rows[m.cursorIdx].Worktree.Branch)
		}
	}
	return m, nil
}

func (m *Model) refreshAll() {
	worktrees, err := worktree.DiscoverAll(m.repoRoot)
	if err != nil {
		m.message = fmt.Sprintf("discovery error: %v", err)
		return
	}

	// Fetch all worktree data in parallel
	allRows := make([]Row, len(worktrees))
	var wg sync.WaitGroup
	for i, wt := range worktrees {
		wg.Add(1)
		go func(i int, wt worktree.Worktree) {
			defer wg.Done()
			allRows[i] = Row{
				Worktree:   wt,
				GitStatus:  gitstatus.Get(wt.Path),
				PR:         pr.Fetch(wt.Path),
				AgentState: agent.ReadState(wt.Path),
			}
		}(i, wt)
	}
	wg.Wait()

	// Skip main worktree (index 0), filter out stale worktrees (merged PR)
	currentPaths := make(map[string]bool)
	var rows []Row
	for _, row := range allRows {
		if row.Worktree.IsMain {
			continue // skip main worktrees (superproject root + submodule git dirs)
		}
		if row.PR != nil && (row.PR.State == "MERGED" || row.PR.State == "CLOSED") {
			if row.AgentState != nil && row.AgentState.TMUX.Session != "" {
				_ = tmux.KillWindow(row.AgentState.TMUX.Session, row.AgentState.TMUX.Window)
			} else {
				_ = tmux.KillWindowByPath(row.Worktree.Path)
			}
			_ = docker.RemoveContainersForWorktree(row.Worktree.Path)
			_ = worktree.ForceRemove(row.Worktree)
			reason := register.ReasonMerged
			if row.PR.State == "CLOSED" {
				reason = register.ReasonClosed
			}
			m.register.RecordClose(row.Worktree.Path, row.Worktree.Branch, reason)
			continue
		}
		currentPaths[row.Worktree.Path] = true
		m.register.RecordOpen(row.Worktree.Path, row.Worktree.Branch)
		rows = append(rows, row)
	}
	m.register.Reconcile(currentPaths)
	_ = m.register.Save()
	m.rows = rows
	if m.cursorIdx >= len(m.rows) {
		m.cursorIdx = max(0, len(m.rows)-1)
	}

	if !m.restored {
		m.restored = true
		matched := false
		if m.focusPath != "" {
			fp := strings.TrimRight(m.focusPath, "/")
			for i, row := range m.rows {
				wtp := strings.TrimRight(row.Worktree.Path, "/")
				if fp == wtp || strings.HasPrefix(fp, wtp+"/") {
					m.cursorIdx = i
					m.ensureCursorVisible()
					matched = true
					break
				}
			}
		}
		if !matched {
			if data, err := os.ReadFile(m.stateFile); err == nil {
				saved := strings.TrimSpace(string(data))
				for i, row := range m.rows {
					if row.Worktree.Path == saved {
						m.cursorIdx = i
						m.ensureCursorVisible()
						break
					}
				}
			}
		}
	}

	m.refreshAgents()
	m.refreshPaneTargets()
	for _, row := range m.rows {
		if row.PaneTarget == "" {
			_ = tmux.NewWindow(row.Worktree.Path, row.Worktree.Branch)
		}
	}
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
