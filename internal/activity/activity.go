// Package activity tracks recent signs of work in a worktree: files written in
// the working tree, and the git operations (staging, committing, pushing) that
// leave their mark in git's metadata. The dashboard uses it to flash the border
// of a worktree that just changed, and to hide the ones that have gone quiet.
package activity

import "time"

// Window is how long an event counts as recent: how long a tile flashes and
// shows what happened.
const Window = 5 * time.Second

// Linger is how long a worktree stays on screen after its last event while the
// active-only filter is on. It is deliberately longer than Window: a tile that
// vanished the moment you stopped typing would be gone while you were still
// working in it, and a tile appearing and disappearing under the cursor moves
// every other tile with it.
const Linger = 45 * time.Second

// Kind is what happened in a worktree.
type Kind int

const (
	// KindNone means nothing worth reporting.
	KindNone Kind = iota
	// KindFile is a file in the working tree written, created or removed.
	KindFile
	// KindStage is a git add: the index gained staged changes.
	KindStage
	// KindCommit is a git commit: the worktree's HEAD reflog grew.
	KindCommit
	// KindPush is a git push: a remote-tracking ref for the branch moved.
	KindPush
)

// Label is the short name shown on a tile.
func (k Kind) Label() string {
	switch k {
	case KindFile:
		return "file"
	case KindStage:
		return "add"
	case KindCommit:
		return "commit"
	case KindPush:
		return "push"
	default:
		return ""
	}
}

// Event is one thing that happened in a worktree.
type Event struct {
	Kind Kind
	At   time.Time
}

// Tracker remembers the most recent event per worktree path. It belongs to the
// Bubble Tea model and is only touched from the update loop, so it does not
// lock; watcher goroutines report through messages rather than writing here.
type Tracker struct {
	last map[string]Event
}

// NewTracker creates an empty tracker.
func NewTracker() *Tracker {
	return &Tracker{last: make(map[string]Event)}
}

// Record notes activity for a worktree, keeping whichever event is newer. When
// two events share a timestamp the more specific one wins: a commit rewrites
// files in the working tree, and "commit" says more about what just happened
// than "file".
func (t *Tracker) Record(path string, kind Kind, at time.Time) {
	if path == "" || kind == KindNone {
		return
	}
	if prev, ok := t.last[path]; ok {
		if prev.At.After(at) {
			return
		}
		if prev.At.Equal(at) && prev.Kind > kind {
			return
		}
	}
	t.last[path] = Event{Kind: kind, At: at}
}

// Recent returns the worktree's last event when it falls within window of now.
// An event timestamped in the future (a clock that stepped backwards) counts as
// recent rather than being discarded.
func (t *Tracker) Recent(path string, now time.Time, window time.Duration) (Event, bool) {
	ev, ok := t.last[path]
	if !ok {
		return Event{}, false
	}
	if now.Sub(ev.At) > window {
		return Event{}, false
	}
	return ev, true
}

// Forget drops a worktree's history, for when its tile goes away.
func (t *Tracker) Forget(path string) {
	delete(t.last, path)
}
