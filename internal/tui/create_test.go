package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/garber-squared/git-arborist/internal/worktree"
)

// labels renders picker options as "kind:value" strings so expectations read
// as the flow the user sees.
func labels(opts []createOption) []string {
	var out []string
	for _, o := range opts {
		switch o.kind {
		case optBranch:
			out = append(out, "branch:"+o.cand.Branch)
		case optIssue:
			out = append(out, "issue:"+o.text)
		case optNewBranch:
			out = append(out, "new:"+o.text)
		case optBase:
			if o.raw {
				out = append(out, "rawbase:"+o.text)
			} else {
				out = append(out, "base:"+o.text)
			}
		}
	}
	return out
}

func eq(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v, want %v", name, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s = %v, want %v", name, got, want)
			return
		}
	}
}

func TestBranchOptions(t *testing.T) {
	cands := []worktree.Candidate{
		{Branch: "1905-twilio-exit"},
		{Branch: "ds/loading-splash", Remote: "origin/ds/loading-splash"},
		{Branch: "fix/admin-ui", Repo: "web"},
	}

	eq(t, "empty query", labels(branchOptions(cands, "")),
		[]string{"branch:1905-twilio-exit", "branch:ds/loading-splash", "branch:fix/admin-ui"})

	// Fuzzy subsequence, plus the offer to create what was typed.
	eq(t, "fuzzy", labels(branchOptions(cands, "dsload")),
		[]string{"branch:ds/loading-splash", "new:dsload"})

	// Submodule name is searchable.
	eq(t, "repo match", labels(branchOptions(cands, "web/admin")),
		[]string{"branch:fix/admin-ui", "new:web/admin"})

	// A digit run offers the issue's branch after any branch matches.
	eq(t, "issue with match", labels(branchOptions(cands, "1905")),
		[]string{"branch:1905-twilio-exit", "issue:1905"})
	eq(t, "issue without match", labels(branchOptions(cands, "2001")),
		[]string{"issue:2001"})

	// An exact branch name is not also offered as a new branch.
	eq(t, "exact", labels(branchOptions(cands, "fix/admin-ui")),
		[]string{"branch:fix/admin-ui"})

	eq(t, "no match", labels(branchOptions(cands, "zzz")), []string{"new:zzz"})
}

func TestBaseOptions(t *testing.T) {
	bases := []string{"main", "staging", "release-1.6.0"}
	eq(t, "empty", labels(baseOptions(bases, "")),
		[]string{"base:main", "base:staging", "base:release-1.6.0"})
	eq(t, "filter", labels(baseOptions(bases, "stag")),
		[]string{"base:staging", "rawbase:stag"})
	eq(t, "exact", labels(baseOptions(bases, "main")), []string{"base:main"})
	eq(t, "raw ref", labels(baseOptions(bases, "v1.2.3")), []string{"rawbase:v1.2.3"})
}

func TestIsDigits(t *testing.T) {
	for _, s := range []string{"1", "1905"} {
		if !isDigits(s) {
			t.Errorf("isDigits(%q) = false", s)
		}
	}
	for _, s := range []string{"", "19a", "1-fix", " 12"} {
		if isDigits(s) {
			t.Errorf("isDigits(%q) = true", s)
		}
	}
}

// TestCreateFlow walks the picker's state machine without touching git.
func TestCreateFlow(t *testing.T) {
	m := NewModel("/repo", "")
	m.width, m.height = 120, 40
	m.create.active = true
	m.create.cands = []worktree.Candidate{
		{Branch: "local", RepoRoot: "/repo"},
		{Branch: "remoted", Remote: "origin/remoted", RepoRoot: "/repo"},
	}

	// Existing local branch → straight to creation.
	if _, cmd := m.chooseCreateOption(createOption{kind: optBranch, cand: m.create.cands[0]}); cmd == nil {
		t.Error("picking an existing branch should start creation")
	}
	if !m.create.busy || m.create.req.branch != "local" || m.create.req.startRef != "" {
		t.Errorf("req = %+v busy=%v", m.create.req, m.create.busy)
	}

	// Remote-only branch carries the start ref.
	m.create.busy = false
	m.chooseCreateOption(createOption{kind: optBranch, cand: m.create.cands[1]})
	if m.create.req.startRef != "origin/remoted" {
		t.Errorf("startRef = %q", m.create.req.startRef)
	}

	// New branch → base stage, no creation yet.
	m.create.busy = false
	m.create.stage = stageBranch
	m.create.input.SetValue("feat/x")
	if _, cmd := m.chooseCreateOption(createOption{kind: optNewBranch, text: "feat/x"}); cmd != nil {
		t.Error("a new branch must ask for a base before creating")
	}
	if m.create.stage != stageBase || m.create.busy {
		t.Errorf("stage = %v busy = %v", m.create.stage, m.create.busy)
	}
	if _, cmd := m.chooseCreateOption(createOption{kind: optBase, text: "staging"}); cmd == nil {
		t.Error("picking a base should start creation")
	}
	if m.create.req.branch != "feat/x" || m.create.req.base != "staging" {
		t.Errorf("req = %+v", m.create.req)
	}

	// Issue → base stage, request carries the issue rather than a branch.
	m.create.busy = false
	m.create.stage = stageBranch
	m.create.input.SetValue("1905")
	m.chooseCreateOption(createOption{kind: optIssue, text: "1905"})
	if m.create.stage != stageBase || m.create.req.issue != "1905" || m.create.req.branch != "" {
		t.Errorf("stage = %v req = %+v", m.create.stage, m.create.req)
	}
	// esc steps back to the branch question, drops the half-built request, and
	// puts the typed query back.
	m.handleCreateKey(keyMsg("esc"))
	if m.create.stage != stageBranch || m.create.req.issue != "" || !m.create.active {
		t.Errorf("after esc: stage = %v req = %+v active = %v", m.create.stage, m.create.req, m.create.active)
	}
	if got := m.create.input.Value(); got != "1905" {
		t.Errorf("after esc: input = %q, want the branch-stage query back", got)
	}
	// esc again leaves the flow.
	m.handleCreateKey(keyMsg("esc"))
	if m.create.active {
		t.Error("esc at the branch stage should close the picker")
	}
}

// TestRenderCreateView is a smoke test: the picker must render without
// panicking at both stages and while busy.
func TestRenderCreateView(t *testing.T) {
	m := NewModel("/repo", "")
	m.width, m.height = 100, 30
	m.create.active = true
	m.create.cands = []worktree.Candidate{{Branch: "aaa"}, {Branch: "bbb", Remote: "origin/bbb", Repo: "web"}}
	if out := m.View(); out == "" {
		t.Error("empty branch-stage view")
	}
	m.create.stage = stageBase
	m.create.bases = []string{"main", "staging"}
	if out := m.View(); out == "" {
		t.Error("empty base-stage view")
	}
	m.create.busy = true
	m.create.req = createRequest{issue: "1905"}
	if out := m.View(); out == "" {
		t.Error("empty busy view")
	}
	// Narrow terminals must not blow up the box math.
	m.create.busy = false
	m.width, m.height = 20, 8
	if out := m.View(); out == "" {
		t.Error("empty narrow view")
	}
}

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestWrapHelp(t *testing.T) {
	items := []string{"a: one", "b: two", "c: three"}

	eq(t, "fits on one line", wrapHelp(items, 100), []string{"a: one  b: two  c: three"})
	eq(t, "breaks at the width", wrapHelp(items, 16), []string{"a: one  b: two", "c: three"})
	// A hint is never split, even when it alone exceeds the width.
	eq(t, "unsplittable hint", wrapHelp([]string{"x: a very long hint"}, 5), []string{"x: a very long hint"})

	// Every hint must survive wrapping, at any width, and no line may exceed
	// it — that overflow is what cut the footer off at the terminal edge.
	m := NewModel("/repo", "")
	m.gridRows = 2
	for _, width := range []int{20, 40, 80, 100, 150, 400} {
		lines := wrapHelp(m.helpItems(), width)
		joined := strings.Join(lines, "  ")
		for _, item := range m.helpItems() {
			if !strings.Contains(joined, item) {
				t.Errorf("width %d: dropped %q", width, item)
			}
		}
		for _, line := range lines {
			if width >= 40 && lipgloss.Width(line) > width {
				t.Errorf("width %d: line overflows (%d): %q", width, lipgloss.Width(line), line)
			}
		}
	}
}

// TestFooterLeavesRoomForTiles guards the layout math: the wrapped footer must
// be accounted for, so tiles plus footer never exceed the terminal height.
func TestFooterLeavesRoomForTiles(t *testing.T) {
	for _, width := range []int{60, 100, 150} {
		m := NewModel("/repo", "")
		m.width, m.height = width, 40
		m.rows = make([]Row, 6)
		// A long status message wraps; the footer must account for that too.
		m.message = "Created worktree '4321-a-long-branch-name-from-an-issue' · gh branched '4321-a-long-branch-name-from-an-issue' off staging · running setup command"
		m.computeLayout()
		used := dashHeaderH + m.tileH*m.visibleRows + m.footerH()
		if used > m.height {
			t.Errorf("width %d: layout wants %d rows of %d (help = %d lines, message = %d)",
				width, used, m.height, len(m.helpLines), m.messageH())
		}
	}
}

// TestCreateModeCarried checks that the window mode chosen at the keypress
// survives every stage of the picker, including a step back.
func TestCreateModeCarried(t *testing.T) {
	m := NewModel("/repo", "")
	m.create.active = true
	m.create.mode = worktree.ModeWatch

	// Existing branch: straight to creation, in watch mode.
	m.chooseCreateOption(createOption{kind: optBranch, cand: worktree.Candidate{Branch: "b", RepoRoot: "/repo"}})
	if m.create.req.mode != worktree.ModeWatch {
		t.Error("existing branch dropped watch mode")
	}

	// Issue: through the base stage, and back again via esc.
	m.create.busy = false
	m.create.stage = stageBranch
	m.chooseCreateOption(createOption{kind: optIssue, text: "1905"})
	if m.create.req.mode != worktree.ModeWatch {
		t.Error("issue dropped watch mode")
	}
	m.handleCreateKey(keyMsg("esc"))
	if m.create.mode != worktree.ModeWatch || m.create.req.mode != worktree.ModeWatch {
		t.Errorf("esc dropped watch mode: state=%v req=%v", m.create.mode, m.create.req.mode)
	}
	m.chooseCreateOption(createOption{kind: optNewBranch, text: "feat/x"})
	m.chooseCreateOption(createOption{kind: optBase, text: "main"})
	if m.create.req.mode != worktree.ModeWatch || m.create.req.base != "main" {
		t.Errorf("req = %+v", m.create.req)
	}

	// The mode is visible in the title, so nobody creates a watch worktree by
	// accident.
	m.create.mode = worktree.ModeWatch
	m.create.stage = stageBranch
	if title, _ := m.createPrompt(); !strings.Contains(title, "watch") {
		t.Errorf("watch title = %q", title)
	}
	m.create.mode = worktree.ModeWork
	if title, _ := m.createPrompt(); strings.Contains(title, "watch") {
		t.Errorf("work title = %q", title)
	}
}
