package tui

import (
	"path/filepath"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/garber-squared/git-arborist/internal/agent"
	"github.com/garber-squared/git-arborist/internal/gitstatus"
	"github.com/garber-squared/git-arborist/internal/pr"
	"github.com/garber-squared/git-arborist/internal/register"
	"github.com/garber-squared/git-arborist/internal/watcher"
	"github.com/garber-squared/git-arborist/internal/worktree"
)

// ViewScope controls which worktrees the dashboard displays.
type ViewScope int

const (
	// ScopeAll shows worktrees for the superproject and all its submodules.
	ScopeAll ViewScope = iota
	// ScopeRoot shows only worktrees belonging to the superproject root.
	ScopeRoot
	// ScopeSubmodules shows only worktrees belonging to submodules.
	ScopeSubmodules
)

func (s ViewScope) String() string {
	switch s {
	case ScopeRoot:
		return "root"
	case ScopeSubmodules:
		return "submodules"
	default:
		return "all"
	}
}

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
	expanded   bool
	inserting  bool            // insert mode: typing text destined for the focused pane
	input      textinput.Model // insert-mode text field
	scope      ViewScope       // which worktrees to display (all vs. root-only)

	// Layout
	visibleCols int // grid columns
	gridRows    int // total rows in grid
	visibleRows int // rows visible at once (capped at 4)
	tileW       int
	tileH       int
	scrollCol   int // single-row horizontal scroll (legacy)
	scrollRow   int // grid vertical scroll (for >9 items)

	// Persist cursor across sessions
	stateFile string
	restored  bool

	// Path to focus on first open (overrides stateFile)
	focusPath string


	// Tracks open + recently-closed worktrees so the empty state can
	// distinguish "nothing yet" from "you just closed your last one".
	register *register.Register
}

// NewModel creates a new dashboard model.
func NewModel(repoRoot, focusPath string) Model {
	ti := textinput.New()
	ti.Prompt = "> "
	return Model{
		repoRoot:  repoRoot,
		stateFile: filepath.Join(repoRoot, ".git", "arborist-state"),
		register:  register.Load(filepath.Join(repoRoot, ".git", "arborist-register.json")),
		focusPath: focusPath,
		input:     ti,
	}
}

// SetSendFn sets the function used to send messages to the Bubble Tea program.
// Must be called after tea.NewProgram is created but before Run.
func (m *Model) SetSendFn(fn func(tea.Msg)) {
	m.sendFn = fn
}
