// Package register persists a history of worktrees opened and closed by
// arborist. The empty-state dashboard uses RecentlyClosed to tell the user
// which worktree they most recently closed, so an empty board is
// distinguishable from a never-populated one.
package register

import (
	"encoding/json"
	"os"
	"time"
)

const maxClosed = 10

// Reasons a worktree may have been closed.
const (
	ReasonMerged   = "merged"   // auto-removed because its PR was merged
	ReasonDeleted  = "deleted"  // user pressed `d` in the dashboard
	ReasonExternal = "external" // disappeared between refreshes without arborist removing it
)

type OpenEntry struct {
	Path     string    `json:"path"`
	Branch   string    `json:"branch"`
	OpenedAt time.Time `json:"opened_at"`
}

type ClosedEntry struct {
	Path     string    `json:"path"`
	Branch   string    `json:"branch"`
	OpenedAt time.Time `json:"opened_at,omitempty"`
	ClosedAt time.Time `json:"closed_at"`
	Reason   string    `json:"reason"`
}

type Register struct {
	KnownOpen []OpenEntry   `json:"known_open"`
	Closed    []ClosedEntry `json:"closed"`

	path string
}

// Load reads the register at path. A missing file yields an empty register;
// any other read or parse error also yields an empty register so a corrupt
// file never blocks the dashboard.
func Load(path string) *Register {
	r := &Register{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return r
	}
	_ = json.Unmarshal(data, r)
	r.path = path
	return r
}

// Save writes the register atomically (write to temp, rename).
func (r *Register) Save() error {
	if r.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

// RecordOpen marks a worktree as currently open. If it was already known
// open, the existing OpenedAt is preserved.
func (r *Register) RecordOpen(path, branch string) {
	for i := range r.KnownOpen {
		if r.KnownOpen[i].Path == path {
			r.KnownOpen[i].Branch = branch
			return
		}
	}
	r.KnownOpen = append(r.KnownOpen, OpenEntry{
		Path:     path,
		Branch:   branch,
		OpenedAt: time.Now(),
	})
}

// RecordClose moves a worktree from KnownOpen into Closed with the given
// reason. If the worktree was never recorded as open (e.g. closed before
// arborist first saw it), it is still appended to Closed with a zero
// OpenedAt. The Closed list is capped at maxClosed, most-recent first.
func (r *Register) RecordClose(path, branch, reason string) {
	var opened time.Time
	for i := range r.KnownOpen {
		if r.KnownOpen[i].Path == path {
			opened = r.KnownOpen[i].OpenedAt
			if branch == "" {
				branch = r.KnownOpen[i].Branch
			}
			r.KnownOpen = append(r.KnownOpen[:i], r.KnownOpen[i+1:]...)
			break
		}
	}
	entry := ClosedEntry{
		Path:     path,
		Branch:   branch,
		OpenedAt: opened,
		ClosedAt: time.Now(),
		Reason:   reason,
	}
	r.Closed = append([]ClosedEntry{entry}, r.Closed...)
	if len(r.Closed) > maxClosed {
		r.Closed = r.Closed[:maxClosed]
	}
}

// Reconcile compares KnownOpen against the set of paths currently present
// on disk. Any KnownOpen entry not in currentPaths is moved to Closed with
// ReasonExternal — the worktree disappeared without arborist initiating it
// (e.g. `git worktree remove` from a shell).
func (r *Register) Reconcile(currentPaths map[string]bool) {
	stale := r.KnownOpen[:0:0]
	for _, e := range r.KnownOpen {
		if !currentPaths[e.Path] {
			stale = append(stale, e)
		}
	}
	for _, e := range stale {
		r.RecordClose(e.Path, e.Branch, ReasonExternal)
	}
}

// RecentlyClosed returns the closed entries, most-recent first.
func (r *Register) RecentlyClosed() []ClosedEntry {
	return r.Closed
}
