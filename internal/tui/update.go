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

// prFetchedMsg carries the PR lookup for a single worktree, fetched in the
// background so the dashboard can render tiles before the (network-bound)
// GitHub queries complete.
type prFetchedMsg struct {
	wt worktree.Worktree
	pr *pr.PullRequest
}

// fetchPRCmd looks up the PR for a worktree off the main update loop.
func fetchPRCmd(wt worktree.Worktree) tea.Cmd {
	return func() tea.Msg {
		return prFetchedMsg{wt: wt, pr: pr.Fetch(wt.Path)}
	}
}

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

// paneRefreshMsg triggers a one-shot refresh of the focused pane (unlike
// paneTickMsg it does not re-arm a recurring tick).
type paneRefreshMsg struct{}

// paneRefreshCmd re-captures the focused pane shortly after keys are sent to
// it, so the tile reflects the send immediately instead of waiting for the
// next 2s tick. The small delay gives the pane's program time to redraw.
func paneRefreshCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return paneRefreshMsg{}
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
		m.input.Width = max(20, m.width-10)

	case tea.KeyMsg:
		return m.handleKey(msg)

	case refreshMsg:
		return m, m.refreshAll()

	case prFetchedMsg:
		m.applyPR(msg.wt, msg.pr)

	case agentTickMsg:
		m.refreshAgents()
		return m, agentTickCmd()

	case paneTickMsg:
		m.refreshPaneContent()
		return m, paneTickCmd()

	case paneRefreshMsg:
		m.refreshFocusedPane()

	case watcher.FileChangedMsg:
		m.refreshRow(msg.WorktreePath, msg.Kind)
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Insert mode captures every key until the text is sent or cancelled.
	// Up/down move a selection through the (fuzzy-filtered) history list
	// rendered above the input; enter sends the selection if there is one,
	// otherwise the typed text.
	if m.inserting {
		switch msg.String() {
		case "enter":
			text := m.input.Value()
			if filtered := m.filteredHistory(); m.histSel >= 0 && m.histSel < len(filtered) {
				text = filtered[m.histSel]
			}
			m.inserting = false
			m.histSel = -1
			m.input.Blur()
			if text == "" {
				return m, nil
			}
			if m.cursorIdx >= len(m.rows) {
				return m, nil
			}
			row := m.rows[m.cursorIdx]
			if row.PaneTarget == "" {
				m.message = "No tmux pane found for this worktree"
				return m, nil
			}
			if err := tmux.SendText(row.PaneTarget, text); err != nil {
				m.message = fmt.Sprintf("send-keys failed: %v", err)
				return m, nil
			}
			m.history = appendHistory(m.history, text)
			saveHistory(m.histFile, m.history)
			m.message = fmt.Sprintf("Sent text to %s", row.Worktree.Branch)
			return m, paneRefreshCmd()
		case "esc":
			m.inserting = false
			m.histSel = -1
			m.input.Blur()
			return m, nil
		case "up":
			if filtered := m.filteredHistory(); len(filtered) > 0 {
				if m.histSel == -1 {
					m.histSel = len(filtered) - 1
				} else if m.histSel > 0 {
					m.histSel--
				}
			}
			return m, nil
		case "down":
			if m.histSel >= 0 {
				m.histSel++
				if m.histSel >= len(m.filteredHistory()) {
					m.histSel = -1
				}
			}
			return m, nil
		}
		// Typing changes the filter, so any prior selection no longer points
		// at what the user saw — drop it.
		before := m.input.Value()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.input.Value() != before {
			m.histSel = -1
		}
		return m, cmd
	}

	// Handle confirmation state first
	if m.confirming {
		if msg.String() == "d" {
			m.confirming = false
			var cmd tea.Cmd
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
				cmd = m.refreshAll()
			}
			return m, cmd
		}
		m.confirming = false
		m.message = ""
		return m, nil
	}

	// Handle expanded overlay state
	if m.expanded {
		switch msg.String() {
		case "l", "esc":
			// Return to the normal pane view.
			m.expanded = false
			return m, nil
		case "j":
			return m, m.sendToPane("Down")
		case "k":
			return m, m.sendToPane("Up")
		case "left", "right", "up", "down", "h", "d", "r", "g", "s", "n", "N":
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

	case msg.String() == "h":
		m.expanded = true

	case msg.String() == "l":
		// l is reserved for returning to the normal pane view; in the normal
		// view (not expanded) it is a no-op.

	case msg.String() == "left":
		if m.cursorIdx > 0 {
			m.cursorIdx--
		}
		m.ensureCursorVisible()

	case msg.String() == "right":
		if m.cursorIdx < len(m.rows)-1 {
			m.cursorIdx++
		}
		m.ensureCursorVisible()

	case msg.String() == "down":
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

	case msg.String() == "up":
		if m.visibleCols > 0 && m.gridRows > 1 {
			col := m.cursorIdx % m.visibleCols
			row := m.cursorIdx / m.visibleCols
			if row > 0 {
				m.cursorIdx = (row-1)*m.visibleCols + col
				m.ensureCursorVisible()
			}
		}

	case msg.String() == "j":
		return m, m.sendToPane("Down")

	case msg.String() == "k":
		return m, m.sendToPane("Up")

	case msg.String() == "r":
		m.message = ""
		return m, m.refreshAll()

	case msg.String() == "s":
		switch m.scope {
		case ScopeAll:
			m.scope = ScopeRoot
		case ScopeRoot:
			m.scope = ScopeSubmodules
		default:
			m.scope = ScopeAll
		}
		// Keep the focused worktree selected across the toggle when it
		// survives the new scope; otherwise clamp to a valid tile.
		var focused string
		if m.cursorIdx < len(m.rows) {
			focused = m.rows[m.cursorIdx].Worktree.Path
		}
		cmd := m.refreshAll()
		m.cursorIdx = 0
		for i, row := range m.rows {
			if row.Worktree.Path == focused {
				m.cursorIdx = i
				break
			}
		}
		m.ensureCursorVisible()
		m.message = fmt.Sprintf("Scope: %s", m.scope)
		return m, cmd

	case msg.String() == "e":
		if m.cursorIdx < len(m.rows) {
			row := m.rows[m.cursorIdx]
			if row.PaneTarget != "" {
				if err := tmux.SendKeys(row.PaneTarget, "Enter"); err != nil {
					m.message = fmt.Sprintf("send-keys failed: %v", err)
				} else {
					m.message = fmt.Sprintf("Sent Enter to %s", row.Worktree.Branch)
					return m, paneRefreshCmd()
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
					return m, paneRefreshCmd()
				}
			} else {
				m.message = "No tmux pane found for this worktree"
			}
		}

	case msg.String() == "i":
		if m.cursorIdx < len(m.rows) {
			row := m.rows[m.cursorIdx]
			if row.PaneTarget == "" {
				m.message = "No tmux pane found for this worktree"
			} else {
				m.inserting = true
				m.histSel = -1
				m.message = ""
				m.input.SetValue("")
				m.input.Width = max(20, m.width-10)
				return m, m.input.Focus()
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

	case msg.String() == "I":
		if m.cursorIdx < len(m.rows) {
			row := m.rows[m.cursorIdx]
			num := pr.IssueNumberFromBranch(row.Worktree.Branch)
			if num == "" {
				m.message = fmt.Sprintf("No issue number in branch name '%s'", row.Worktree.Branch)
			} else if err := pr.OpenIssueInBrowser(row.Worktree.Path, num); err != nil {
				m.message = fmt.Sprintf("gh issue view %s failed: %v", num, err)
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

// sendToPane forwards a tmux key name (e.g. "Down", "Up") to the focused
// worktree's tmux pane so the user can drive an agent's option list from the
// dashboard. On success it returns a command that re-captures the pane.
func (m *Model) sendToPane(key string) tea.Cmd {
	if m.cursorIdx >= len(m.rows) {
		return nil
	}
	row := m.rows[m.cursorIdx]
	if row.PaneTarget == "" {
		m.message = "No tmux pane found for this worktree"
		return nil
	}
	if err := tmux.SendKeys(row.PaneTarget, key); err != nil {
		m.message = fmt.Sprintf("send-keys failed: %v", err)
		return nil
	}
	m.message = fmt.Sprintf("Sent %s to %s", key, row.Worktree.Branch)
	return paneRefreshCmd()
}

// refreshFocusedPane re-captures only the focused row's pane content.
func (m *Model) refreshFocusedPane() {
	if m.cursorIdx >= len(m.rows) {
		return
	}
	if target := m.rows[m.cursorIdx].PaneTarget; target != "" {
		m.rows[m.cursorIdx].PaneContent = tmux.CapturePaneContent(target)
	}
}

// refreshAll rediscovers worktrees and repopulates the dashboard. Fast, local
// data (git status, agent state) is gathered synchronously so tiles render
// immediately; the network-bound PR lookups run in the background via the
// returned command, and merged/closed worktrees are pruned when those return
// (see applyPR).
func (m *Model) refreshAll() tea.Cmd {
	worktrees, err := worktree.DiscoverAll(m.repoRoot)
	if err != nil {
		m.message = fmt.Sprintf("discovery error: %v", err)
		return nil
	}

	// Carry over already-known PR data so a refresh doesn't blank the PR
	// badges until the background lookups return.
	prevPR := make(map[string]*pr.PullRequest, len(m.rows))
	for _, row := range m.rows {
		if row.PR != nil {
			prevPR[row.Worktree.Path] = row.PR
		}
	}

	// Gather fast, local data in parallel. The PR lookup is network-bound and
	// deferred to the background command below.
	allRows := make([]Row, len(worktrees))
	var wg sync.WaitGroup
	for i, wt := range worktrees {
		wg.Add(1)
		go func(i int, wt worktree.Worktree) {
			defer wg.Done()
			allRows[i] = Row{
				Worktree:   wt,
				GitStatus:  gitstatus.Get(wt.Path),
				AgentState: agent.ReadState(wt.Path),
				PR:         prevPR[wt.Path],
			}
		}(i, wt)
	}
	wg.Wait()

	// Skip main worktrees; record bookkeeping and apply the scope filter for
	// display. Every non-main worktree gets a background PR lookup so that
	// merged/closed ones are pruned even when hidden by the current scope.
	currentPaths := make(map[string]bool)
	var rows []Row
	var cmds []tea.Cmd
	for _, row := range allRows {
		if row.Worktree.IsMain {
			continue // skip main worktrees (superproject root + submodule git dirs)
		}
		cmds = append(cmds, fetchPRCmd(row.Worktree))
		currentPaths[row.Worktree.Path] = true
		m.register.RecordOpen(row.Worktree.Path, row.Worktree.Branch)
		// Bookkeeping above tracks every live worktree so close-detection
		// stays accurate; the scope filter only affects what is displayed.
		if m.scope == ScopeRoot && row.Worktree.Repo != "" {
			continue
		}
		if m.scope == ScopeSubmodules && row.Worktree.Repo == "" {
			continue
		}
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

	return tea.Batch(cmds...)
}

// applyPR records a background PR lookup on the matching tile. A merged or
// closed PR means the branch is done: its tmux window, containers, and
// worktree are torn down and the tile removed.
func (m *Model) applyPR(wt worktree.Worktree, p *pr.PullRequest) {
	if p != nil && (p.State == "MERGED" || p.State == "CLOSED") {
		var st *agent.State
		for i := range m.rows {
			if m.rows[i].Worktree.Path == wt.Path {
				st = m.rows[i].AgentState
				break
			}
		}
		if st != nil && st.TMUX.Session != "" {
			_ = tmux.KillWindow(st.TMUX.Session, st.TMUX.Window)
		} else {
			_ = tmux.KillWindowByPath(wt.Path)
		}
		_ = docker.RemoveContainersForWorktree(wt.Path)
		_ = worktree.ForceRemove(wt)
		reason := register.ReasonMerged
		if p.State == "CLOSED" {
			reason = register.ReasonClosed
		}
		m.register.RecordClose(wt.Path, wt.Branch, reason)
		_ = m.register.Save()
		m.removeRowByPath(wt.Path)
		return
	}
	for i := range m.rows {
		if m.rows[i].Worktree.Path == wt.Path {
			m.rows[i].PR = p
			return
		}
	}
}

// removeRowByPath drops the tile for the given worktree path and keeps the
// cursor within bounds.
func (m *Model) removeRowByPath(path string) {
	for i := range m.rows {
		if m.rows[i].Worktree.Path == path {
			m.rows = append(m.rows[:i], m.rows[i+1:]...)
			if m.cursorIdx >= len(m.rows) {
				m.cursorIdx = max(0, len(m.rows)-1)
			}
			m.ensureCursorVisible()
			return
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
