package tui

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/garber-squared/git-arborist/internal/activity"
	"github.com/garber-squared/git-arborist/internal/agent"
	"github.com/garber-squared/git-arborist/internal/docker"
	"github.com/garber-squared/git-arborist/internal/gitstatus"
	"github.com/garber-squared/git-arborist/internal/port"
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

// flashTickMsg drives the border flash on active tiles and re-evaluates the
// active-only filter, so a worktree appears or disappears within one tick of
// starting or stopping work.
type flashTickMsg struct{}

// flashInterval is the flash's half-period: the border alternates on every tick.
const flashInterval = 500 * time.Millisecond

func flashTickCmd() tea.Cmd {
	return tea.Tick(flashInterval, func(time.Time) tea.Msg {
		return flashTickMsg{}
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
		flashTickCmd(),
	)
}

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = max(20, m.width-10)
		m.create.input.Width = m.createInputWidth()

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

	case flashTickMsg:
		m.flashOn = !m.flashOn
		// Activity expires with time rather than with an event, so the filter
		// has to be re-evaluated on a tick.
		m.applyFilter()
		return m, flashTickCmd()

	case paneRefreshMsg:
		m.refreshSentPanes()

	case createCandidatesMsg:
		m.applyCandidates(msg)

	case worktreeCreatedMsg:
		return m, m.applyCreated(msg)

	case watcher.FileChangedMsg:
		m.recordChange(msg)
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The create-worktree picker is modal: it owns every key until a worktree
	// is created or the flow is cancelled.
	if m.create.active {
		return m.handleCreateKey(msg)
	}

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
			targets := m.insertTargets()
			if len(targets) == 0 {
				return m, nil
			}
			var sent, failed []string
			for _, row := range targets {
				if row.PaneTarget == "" {
					failed = append(failed, row.Worktree.Branch)
					continue
				}
				if err := tmux.SendText(row.PaneTarget, text); err != nil {
					failed = append(failed, row.Worktree.Branch)
					continue
				}
				sent = append(sent, row.Worktree.Branch)
			}
			if len(sent) == 0 {
				m.message = fmt.Sprintf("send failed for %s", strings.Join(failed, ", "))
				return m, nil
			}
			m.history = appendHistory(m.history, text)
			saveHistory(m.histFile, m.history)
			m.message = fmt.Sprintf("Sent text to %s", strings.Join(sent, ", "))
			if len(failed) > 0 {
				m.message += fmt.Sprintf(" (no pane: %s)", strings.Join(failed, ", "))
			}
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

	// The keybinding overlay swallows keys until it is closed; ctrl+c still
	// quits.
	if m.showKeys {
		switch msg.String() {
		case "?", "esc", "q":
			m.showKeys = false
			return m, nil
		case "j", "down":
			m.overlayScroll++ // clamped when rendered
			return m, nil
		case "k", "up":
			m.overlayScroll = max(0, m.overlayScroll-1)
			return m, nil
		case "ctrl+c":
			m.showKeys = false
		default:
			return m, nil
		}
	}

	// The `g` git status overlay behaves the same way.
	if m.gitStatus != nil {
		switch msg.String() {
		case "g", "esc", "q":
			m.gitStatus = nil
			return m, nil
		case "j", "down":
			m.overlayScroll++
			return m, nil
		case "k", "up":
			m.overlayScroll = max(0, m.overlayScroll-1)
			return m, nil
		case "ctrl+c":
			m.gitStatus = nil
		default:
			return m, nil
		}
	}

	// Handle confirmation state first
	if m.confirming {
		if msg.String() == "d" {
			m.confirming = false
			return m.deleteRow(false)
		}
		m.confirming = false
		m.message = ""
		return m, nil
	}

	// A dirty worktree needs a second, explicit confirmation before its
	// uncommitted work is thrown away.
	if m.forceConfirming {
		m.forceConfirming = false
		if msg.String() == "D" {
			return m.deleteRow(true)
		}
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
		case "left", "right", "up", "down", "h", "d", "r", "s", "n", "N", " ", "a", "f":
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

	case msg.String() == "?":
		m.showKeys = true
		m.overlayScroll = 0

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

	case msg.String() == "f":
		m.activeOnly = !m.activeOnly
		m.applyFilter()
		if m.activeOnly {
			m.message = fmt.Sprintf("Active only: showing %d of %d worktrees", len(m.rows), len(m.allRows))
		} else {
			m.message = "Showing all worktrees"
		}

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
		targets := m.insertTargets()
		if len(targets) == 0 {
			break
		}
		hasPane := false
		for _, row := range targets {
			if row.PaneTarget != "" {
				hasPane = true
				break
			}
		}
		if !hasPane {
			m.message = "No tmux pane found for this worktree"
			break
		}
		m.inserting = true
		m.histSel = -1
		m.message = ""
		m.input.SetValue("")
		m.input.Width = max(20, m.width-10)
		return m, m.input.Focus()

	case msg.String() == " ":
		if m.cursorIdx < len(m.rows) {
			path := m.rows[m.cursorIdx].Worktree.Path
			if m.selected[path] {
				delete(m.selected, path)
			} else {
				m.selected[path] = true
			}
			m.message = m.selectionMessage()
		}

	case msg.String() == "a":
		if len(m.rows) > 0 && len(m.selected) == len(m.rows) {
			m.clearSelection()
		} else {
			for _, row := range m.rows {
				m.selected[row.Worktree.Path] = true
			}
			m.message = m.selectionMessage()
		}

	case msg.String() == "esc":
		m.clearSelection()

	case msg.String() == "n":
		if m.cursorIdx < len(m.rows) {
			row := m.rows[m.cursorIdx]
			if row.PaneTarget != "" {
				m.message = fmt.Sprintf("tmux pane already exists for '%s'", row.Worktree.Branch)
			} else {
				// `n` and `N` are deliberate keypresses, so they run the
				// repo's setup command like `c` does. refreshAll's window
				// creation stays plain: it fires for every pane-less worktree
				// on every refresh, so setup there would re-run installs and
				// relaunch agents unprompted.
				setup := worktree.WindowCommand(row.Worktree, worktree.ModeWork)
				if err := tmux.NewWindowWithCommand(row.Worktree.Path, row.Worktree.Branch, setup, windowEnv(row.Worktree)...); err != nil {
					m.message = fmt.Sprintf("tmux new-window failed: %v", err)
				} else {
					m.message = fmt.Sprintf("Created tmux window for '%s'", row.Worktree.Branch)
					if setup != "" {
						m.message += " · running setup command"
					}
					m.refreshPaneTargets()
					m.refreshPaneContent()
				}
			}
		}

	case msg.String() == "N":
		var created, failed, withSetup int
		for _, row := range m.rows {
			if row.PaneTarget != "" {
				continue
			}
			setup := worktree.WindowCommand(row.Worktree, worktree.ModeWork)
			if err := tmux.NewWindowWithCommand(row.Worktree.Path, row.Worktree.Branch, setup, windowEnv(row.Worktree)...); err != nil {
				failed++
			} else {
				created++
				if setup != "" {
					withSetup++
				}
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
		if withSetup > 0 {
			m.message += fmt.Sprintf(" · running setup in %d", withSetup)
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
		m.openGitStatus()

	case msg.String() == "c":
		return m, m.openCreate(worktree.ModeWork)

	case msg.String() == "C":
		// Watch mode: set the worktree up, then watch its git status instead
		// of starting an agent in it.
		return m, m.openCreate(worktree.ModeWatch)

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

// selectedRows returns the marked tiles in display order, or nil when nothing
// is marked.
func (m *Model) selectedRows() []Row {
	if len(m.selected) == 0 {
		return nil
	}
	var out []Row
	for _, row := range m.rows {
		if m.selected[row.Worktree.Path] {
			out = append(out, row)
		}
	}
	return out
}

// insertTargets returns the rows insert mode sends to: every marked tile when
// there is a selection, otherwise just the focused tile. The expanded view
// always acts on the single tile it is showing.
func (m *Model) insertTargets() []Row {
	if !m.expanded {
		if rows := m.selectedRows(); len(rows) > 0 {
			return rows
		}
	}
	if m.cursorIdx < len(m.rows) {
		return []Row{m.rows[m.cursorIdx]}
	}
	return nil
}

func (m *Model) clearSelection() {
	if len(m.selected) == 0 {
		return
	}
	m.selected = make(map[string]bool)
	m.message = "Selection cleared"
}

func (m *Model) selectionMessage() string {
	switch n := len(m.selected); n {
	case 0:
		return "Selection cleared"
	case 1:
		return "1 tile selected"
	default:
		return fmt.Sprintf("%d tiles selected", n)
	}
}

// pruneSelection drops marks for worktrees that no longer exist, so a scope
// change or a removed worktree can't leave a phantom selection behind. It reads
// allRows rather than the displayed rows: a tile hidden by the active-only
// filter is still there, and hiding it must not silently unmark it.
func (m *Model) pruneSelection() {
	if len(m.selected) == 0 {
		return
	}
	visible := make(map[string]bool, len(m.allRows))
	for _, row := range m.allRows {
		visible[row.Worktree.Path] = true
	}
	for path := range m.selected {
		if !visible[path] {
			delete(m.selected, path)
		}
	}
}

// refreshSentPanes re-captures the panes a send could have touched: the
// focused row's plus every marked row's, so a fan-out send updates each tile
// rather than only the one under the cursor.
func (m *Model) refreshSentPanes() {
	sent := make(map[string]bool, len(m.selected)+1)
	for i, row := range m.rows {
		if i == m.cursorIdx || m.selected[row.Worktree.Path] {
			sent[row.Worktree.Path] = true
		}
	}
	for i, row := range m.allRows {
		if row.PaneTarget == "" || !sent[row.Worktree.Path] {
			continue
		}
		m.allRows[i].PaneContent = tmux.CapturePaneContent(row.PaneTarget)
	}
	m.applyFilter()
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
	prevPR := make(map[string]*pr.PullRequest, len(m.allRows))
	for _, row := range m.allRows {
		if row.PR != nil {
			prevPR[row.Worktree.Path] = row.PR
		}
	}

	// One registry read per repository, shared by the rows below: the port is
	// display data, so a refresh must not re-read it per worktree.
	ports := make(map[string]*port.Registry)
	for _, wt := range worktrees {
		if _, ok := ports[wt.RepoRoot]; !ok {
			ports[wt.RepoRoot] = port.Load(wt.RepoRoot)
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
				Port:       ports[wt.RepoRoot].Port(wt.Path),
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
	m.allRows = rows
	m.pruneSelection()
	m.applyFilter()

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
	for _, row := range m.allRows {
		if row.PaneTarget == "" {
			_ = tmux.NewWindow(row.Worktree.Path, row.Worktree.Branch)
		}
	}
	m.refreshPaneTargets()
	m.refreshPaneContent()

	// Set up file watchers. Every worktree in scope is watched, including the
	// ones the active-only filter is hiding: a hidden worktree has to be able to
	// announce that work started in it, or it could never come back on screen.
	if m.watcher != nil {
		m.watcher.Close()
	}
	if m.sendFn != nil {
		w, err := watcher.New(m.sendFn)
		if err == nil {
			m.watcher = w
			for _, row := range m.allRows {
				w.Watch(watcher.Target{
					Path:   row.Worktree.Path,
					Branch: row.Worktree.Branch,
				})
			}
		}
	}

	return tea.Batch(cmds...)
}

// applyPR records a background PR lookup on the matching tile. A merged PR
// means the branch is done: its tmux window, containers, and worktree are
// torn down and the tile removed. A PR closed without merging is left alone —
// the work usually still lives on the branch.
func (m *Model) applyPR(wt worktree.Worktree, p *pr.PullRequest) {
	if p != nil && p.State == "MERGED" {
		var st *agent.State
		for i := range m.allRows {
			if m.allRows[i].Worktree.Path == wt.Path {
				st = m.allRows[i].AgentState
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
		// The containers holding this port are gone, so it can be reused.
		_ = port.Load(wt.RepoRoot).Release(wt.Path)
		m.register.RecordClose(wt.Path, wt.Branch, register.ReasonMerged)
		_ = m.register.Save()
		m.removeRowByPath(wt.Path)
		return
	}
	for i := range m.allRows {
		if m.allRows[i].Worktree.Path == wt.Path {
			m.allRows[i].PR = p
			m.applyFilter()
			return
		}
	}
}

// removeRowByPath drops the tile for the given worktree path and keeps the
// cursor within bounds.
func (m *Model) removeRowByPath(path string) {
	for i := range m.allRows {
		if m.allRows[i].Worktree.Path == path {
			m.allRows = append(m.allRows[:i], m.allRows[i+1:]...)
			delete(m.selected, path)
			m.activity.Forget(path)
			m.applyFilter()
			return
		}
	}
}

func (m *Model) refreshAgents() {
	detected := agent.DetectAll()
	for i, row := range m.allRows {
		info, found := detected[row.Worktree.Path]
		// AgentPresent means a process is in the pane now. The AgentState branch
		// below is a file an agent left behind, which says nothing about whether
		// it is still there, so it must not keep a tile on screen.
		m.allRows[i].AgentPresent = found && info.Name != ""
		switch {
		case found && info.Name != "":
			m.allRows[i].ActiveAgent = info.Name
			m.allRows[i].AgentActivity = info.Activity
		case row.AgentState != nil && row.AgentState.Agent != "":
			m.allRows[i].ActiveAgent = row.AgentState.Agent
			m.allRows[i].AgentActivity = agent.ActivityIdle
		default:
			m.allRows[i].ActiveAgent = ""
			m.allRows[i].AgentActivity = agent.ActivityIdle
		}
		// A non-agent process is reported separately: the tile shows what is
		// running either way, and the filter keeps the worktree on screen only
		// while that process is work.
		m.allRows[i].Command = ""
		m.allRows[i].Working = false
		if found && info.Name == "" {
			m.allRows[i].Command = info.Command
			m.allRows[i].Working = info.Working
		}
	}
	m.refreshPaneTargets()
	m.applyFilter()
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

	for i, row := range m.allRows {
		// Prefer agent state TMUX ref if available
		if row.AgentState != nil && row.AgentState.TMUX.Session != "" {
			m.allRows[i].PaneTarget = fmt.Sprintf("%s:%d.%d",
				row.AgentState.TMUX.Session,
				row.AgentState.TMUX.Window,
				row.AgentState.TMUX.Pane)
		} else if target, ok := pathToTarget[row.Worktree.Path]; ok {
			m.allRows[i].PaneTarget = target
		} else {
			m.allRows[i].PaneTarget = ""
		}
	}
}

func (m *Model) refreshPaneContent() {
	for i, row := range m.allRows {
		if row.PaneTarget != "" {
			m.allRows[i].PaneContent = tmux.CapturePaneContent(row.PaneTarget)
		} else {
			m.allRows[i].PaneContent = ""
		}
	}
	m.applyFilter()
}

// recordChange folds a watcher event into the model: it refreshes the affected
// worktree's data and notes the activity that drives the flashing border and the
// active-only filter.
func (m *Model) recordChange(msg watcher.FileChangedMsg) {
	at := msg.At
	if at.IsZero() {
		at = time.Now()
	}

	switch msg.Kind {
	case watcher.KindAgent:
		m.refreshAgentState(msg.WorktreePath)

	case watcher.KindFile:
		m.refreshGitStatus(msg.WorktreePath)
		m.activity.Record(msg.WorktreePath, activity.KindFile, at)

	case watcher.KindGit:
		staged := m.refreshGitStatus(msg.WorktreePath)
		switch msg.GitOp {
		case watcher.GitOpCommit:
			m.activity.Record(msg.WorktreePath, activity.KindCommit, at)
		case watcher.GitOpPush:
			m.activity.Record(msg.WorktreePath, activity.KindPush, at)
		case watcher.GitOpIndex:
			// The index is rewritten by reads as well as by writes, so only a
			// gain in staged changes is reported as a git add. git reset and the
			// index refresh a bare `git status` performs are not activity.
			if staged > 0 {
				m.activity.Record(msg.WorktreePath, activity.KindStage, at)
			}
		}
	}

	m.applyFilter()
}

// refreshGitStatus recomputes a worktree's git status and returns by how much its
// staged count moved, which is how a git add is told apart from the index
// rewrites that ordinary reads cause.
func (m *Model) refreshGitStatus(wtPath string) int {
	for i, row := range m.allRows {
		if row.Worktree.Path != wtPath {
			continue
		}
		before := row.GitStatus.Staged
		m.allRows[i].GitStatus = gitstatus.Get(wtPath)
		return m.allRows[i].GitStatus.Staged - before
	}
	return 0
}

func (m *Model) refreshAgentState(wtPath string) {
	for i, row := range m.allRows {
		if row.Worktree.Path == wtPath {
			m.allRows[i].AgentState = agent.ReadState(wtPath)
			return
		}
	}
}

// deleteRow tears down the worktree under the cursor: its tmux window, its
// docker containers, and finally the worktree itself. The git removal runs
// first so a refusal (uncommitted changes, a locked worktree) leaves the
// worktree and its window intact rather than half torn down. When plain
// removal is refused because the worktree is dirty, the user is asked to
// confirm a force delete.
func (m *Model) deleteRow(force bool) (tea.Model, tea.Cmd) {
	if m.cursorIdx >= len(m.rows) {
		return m, nil
	}
	row := m.rows[m.cursorIdx]

	remove := worktree.Remove
	if force {
		remove = worktree.ForceRemove
	}
	if err := remove(row.Worktree); err != nil {
		m.message = fmt.Sprintf("delete failed: %v", err)
		if !force && !row.GitStatus.Clean {
			m.forceConfirming = true
			m.message = fmt.Sprintf("'%s' has uncommitted changes. Press D to delete anyway, any other key to cancel", row.Worktree.Branch)
		}
		return m, nil
	}

	if row.AgentState != nil && row.AgentState.TMUX.Session != "" {
		_ = tmux.KillWindow(row.AgentState.TMUX.Session, row.AgentState.TMUX.Window)
	} else {
		_ = tmux.KillWindowByPath(row.Worktree.Path)
	}
	// Stop and delete any docker compose containers tied to this worktree.
	_ = docker.RemoveContainersForWorktree(row.Worktree.Path)
	// With those gone the worktree's dev port is free for the next one.
	_ = port.Load(row.Worktree.RepoRoot).Release(row.Worktree.Path)

	m.register.RecordClose(row.Worktree.Path, row.Worktree.Branch, register.ReasonDeleted)
	_ = m.register.Save()
	m.message = fmt.Sprintf("Deleted worktree '%s'", row.Worktree.Branch)
	return m, m.refreshAll()
}
