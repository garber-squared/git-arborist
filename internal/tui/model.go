package tui

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/garber-squared/git-arborist/internal/agent"
	"github.com/garber-squared/git-arborist/internal/gitstatus"
	"github.com/garber-squared/git-arborist/internal/pr"
	"github.com/garber-squared/git-arborist/internal/watcher"
	"github.com/garber-squared/git-arborist/internal/worktree"
)

// Row represents a single worktree tile in the dashboard.
type Row struct {
	Worktree      worktree.Worktree
	GitStatus     gitstatus.Status
	PR            *pr.PullRequest
	AgentState    *agent.State
	ActiveAgent   string
	AgentActivity agent.Activity
	PaneTarget    string // tmux target like "session:1.0"
	PaneContent   string // captured pane text
}

// Model is the Bubble Tea model for the dashboard.
type Model struct {
	rows     []Row
	cursorIdx int
	repoRoot string
	watcher  *watcher.Watcher
	sendFn   func(tea.Msg)
	width    int
	height   int
	message  string
	confirming bool

	// Layout
	visibleCols int // tiles visible at once: min(len(rows), 3)
	tileW       int
	tileH       int
	scrollCol   int // index of leftmost visible tile

	// Persist cursor across sessions
	stateFile string
	restored  bool
}

// NewModel creates a new dashboard model.
func NewModel(repoRoot string) Model {
	return Model{
		repoRoot:  repoRoot,
		stateFile: filepath.Join(repoRoot, ".git", "arborist-state"),
	}
}

// SetSendFn sets the function used to send messages to the Bubble Tea program.
// Must be called after tea.NewProgram is created but before Run.
func (m *Model) SetSendFn(fn func(tea.Msg)) {
	m.sendFn = fn
}
