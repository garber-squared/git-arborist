package activity

import (
	"testing"
	"time"
)

const wt = "/repo/worktrees/feature"

func TestRecentWithinWindow(t *testing.T) {
	now := time.Now()
	tr := NewTracker()
	tr.Record(wt, KindFile, now.Add(-2*time.Second))

	ev, ok := tr.Recent(wt, now, Window)
	if !ok {
		t.Fatal("a change 2s ago must count as recent in a 5s window")
	}
	if ev.Kind != KindFile {
		t.Errorf("Kind = %v, want KindFile", ev.Kind)
	}
}

func TestRecentOutsideWindow(t *testing.T) {
	now := time.Now()
	tr := NewTracker()
	tr.Record(wt, KindFile, now.Add(-6*time.Second))

	if _, ok := tr.Recent(wt, now, Window); ok {
		t.Error("a change 6s ago must fall outside a 5s window")
	}
}

func TestRecentUnknownWorktree(t *testing.T) {
	if _, ok := NewTracker().Recent(wt, time.Now(), Window); ok {
		t.Error("a worktree with no recorded activity must not be recent")
	}
}

func TestRecordKeepsTheNewerEvent(t *testing.T) {
	now := time.Now()
	tr := NewTracker()
	tr.Record(wt, KindCommit, now.Add(-time.Second))
	// An older event arriving late must not overwrite the newer one.
	tr.Record(wt, KindFile, now.Add(-4*time.Second))

	ev, _ := tr.Recent(wt, now, Window)
	if ev.Kind != KindCommit {
		t.Errorf("Kind = %v, want KindCommit: the newer event must win", ev.Kind)
	}
}

func TestRecordPrefersTheSpecificKindOnATie(t *testing.T) {
	now := time.Now()
	at := now.Add(-time.Second)
	tr := NewTracker()
	tr.Record(wt, KindFile, at)
	// A commit rewrites files, so both can land on the same instant; "commit"
	// is the more useful label.
	tr.Record(wt, KindCommit, at)

	ev, _ := tr.Recent(wt, now, Window)
	if ev.Kind != KindCommit {
		t.Errorf("Kind = %v, want KindCommit", ev.Kind)
	}
}

func TestRecordIgnoresEmptyInput(t *testing.T) {
	now := time.Now()
	tr := NewTracker()
	tr.Record("", KindFile, now)
	tr.Record(wt, KindNone, now)

	if _, ok := tr.Recent("", now, Window); ok {
		t.Error("an empty path must not be recorded")
	}
	if _, ok := tr.Recent(wt, now, Window); ok {
		t.Error("KindNone must not be recorded")
	}
}

func TestRecentSurvivesAClockThatStepsBack(t *testing.T) {
	now := time.Now()
	tr := NewTracker()
	// An event stamped in the future is still the most recent thing we know of.
	tr.Record(wt, KindPush, now.Add(time.Second))

	if _, ok := tr.Recent(wt, now, Window); !ok {
		t.Error("an event in the future must count as recent, not be discarded")
	}
}

func TestForget(t *testing.T) {
	now := time.Now()
	tr := NewTracker()
	tr.Record(wt, KindFile, now)
	tr.Forget(wt)

	if _, ok := tr.Recent(wt, now, Window); ok {
		t.Error("a forgotten worktree must have no activity")
	}
}

func TestKindLabel(t *testing.T) {
	for kind, want := range map[Kind]string{
		KindNone:   "",
		KindFile:   "file",
		KindStage:  "add",
		KindCommit: "commit",
		KindPush:   "push",
	} {
		if got := kind.Label(); got != want {
			t.Errorf("Kind(%d).Label() = %q, want %q", kind, got, want)
		}
	}
}
