package tui

import (
	"testing"
	"time"

	"github.com/garber-squared/git-arborist/internal/activity"
	"github.com/garber-squared/git-arborist/internal/worktree"
)

// filterModel builds a model holding one tile per branch, with the layout fields
// the cursor helpers read already set.
func filterModel(branches ...string) *Model {
	m := &Model{
		activity:    activity.NewTracker(),
		selected:    make(map[string]bool),
		visibleCols: len(branches),
		gridRows:    1,
		visibleRows: 1,
	}
	for _, b := range branches {
		m.allRows = append(m.allRows, Row{
			Worktree: worktree.Worktree{Path: "/repo/worktrees/" + b, Branch: b},
		})
	}
	m.applyFilter()
	return m
}

func branches(rows []Row) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Worktree.Branch)
	}
	return out
}

func TestFilterOffShowsEveryWorktree(t *testing.T) {
	m := filterModel("one", "two", "three")

	if got := branches(m.rows); len(got) != 3 {
		t.Errorf("rows = %v, want all three worktrees", got)
	}
}

func TestFilterKeepsWorktreesWithRecentChanges(t *testing.T) {
	m := filterModel("quiet", "busy")
	m.activity.Record("/repo/worktrees/busy", activity.KindFile, time.Now())

	m.activeOnly = true
	m.applyFilter()

	got := branches(m.rows)
	if len(got) != 1 || got[0] != "busy" {
		t.Errorf("rows = %v, want only [busy]", got)
	}
}

func TestFilterDropsWorktreesWhoseChangesAged(t *testing.T) {
	m := filterModel("stale")
	m.activity.Record("/repo/worktrees/stale", activity.KindFile, time.Now().Add(-activity.Linger-time.Second))

	m.activeOnly = true
	m.applyFilter()

	if got := branches(m.rows); len(got) != 0 {
		t.Errorf("rows = %v, want none: the change fell outside the linger window", got)
	}
}

// Between the two windows a worktree keeps its tile but stops flashing: the
// filter lingers so tiles do not vanish during a pause in typing, while the
// flash still means "just now".
func TestFilterLingersAfterTheFlashStops(t *testing.T) {
	m := filterModel("paused")
	now := time.Now()
	at := now.Add(-activity.Window - 2*time.Second)
	if at.Before(now.Add(-activity.Linger)) {
		t.Fatal("Linger must be longer than Window for this to be testable")
	}
	m.activity.Record("/repo/worktrees/paused", activity.KindFile, at)

	m.activeOnly = true
	m.applyFilter()

	got := branches(m.rows)
	if len(got) != 1 || got[0] != "paused" {
		t.Errorf("rows = %v, want [paused] to linger", got)
	}
	if _, flashing := m.recentActivity(m.rows[0], now); flashing {
		t.Error("a change older than the flash window must not flash")
	}
}

// A test run or an agent executing tools keeps a worktree on screen for as long
// as it runs, with no need for anything to be written.
func TestFilterKeepsWorktreesWithAProcessRunning(t *testing.T) {
	m := filterModel("running", "idle")
	m.allRows[0].Busy = true
	m.allRows[0].Command = "rspec"

	m.activeOnly = true
	m.applyFilter()

	got := branches(m.rows)
	if len(got) != 1 || got[0] != "running" {
		t.Errorf("rows = %v, want only [running]", got)
	}
}

func TestFilterKeepsTheCursorOnItsWorktree(t *testing.T) {
	m := filterModel("one", "two", "three")
	m.cursorIdx = 2 // "three"
	m.activity.Record("/repo/worktrees/two", activity.KindFile, time.Now())
	m.activity.Record("/repo/worktrees/three", activity.KindCommit, time.Now())

	m.activeOnly = true
	m.applyFilter()

	if got := m.rows[m.cursorIdx].Worktree.Branch; got != "three" {
		t.Errorf("cursor is on %q, want %q: hiding other tiles must not move it", got, "three")
	}
}

func TestFilterClampsTheCursorWhenItsWorktreeIsHidden(t *testing.T) {
	m := filterModel("one", "two")
	m.cursorIdx = 1 // "two", which is about to be filtered out
	m.activity.Record("/repo/worktrees/one", activity.KindFile, time.Now())

	m.activeOnly = true
	m.applyFilter()

	if len(m.rows) != 1 {
		t.Fatalf("rows = %v, want only [one]", branches(m.rows))
	}
	if m.cursorIdx != 0 {
		t.Errorf("cursorIdx = %d, want 0: the cursor must stay in range", m.cursorIdx)
	}
}

func TestFilterOffRestoresHiddenWorktrees(t *testing.T) {
	m := filterModel("one", "two")
	m.activeOnly = true
	m.applyFilter()
	if len(m.rows) != 0 {
		t.Fatalf("rows = %v, want none while everything is quiet", branches(m.rows))
	}

	m.activeOnly = false
	m.applyFilter()

	if got := branches(m.rows); len(got) != 2 {
		t.Errorf("rows = %v, want both worktrees back", got)
	}
}

// The filter reads a copy: the refreshers write to allRows, and a stale display
// row must never be able to write back over them.
func TestFilterDoesNotShareStorageWithAllRows(t *testing.T) {
	m := filterModel("one")
	m.rows[0].PaneContent = "stale capture"

	if m.allRows[0].PaneContent != "" {
		t.Error("writing to a displayed row changed allRows")
	}
}

func TestRecentActivityReportsTheKind(t *testing.T) {
	m := filterModel("one")
	now := time.Now()
	m.activity.Record("/repo/worktrees/one", activity.KindPush, now)

	ev, ok := m.recentActivity(m.rows[0], now)
	if !ok {
		t.Fatal("a push just now must be recent")
	}
	if ev.Kind.Label() != "push" {
		t.Errorf("label = %q, want %q", ev.Kind.Label(), "push")
	}
}
