package tui

import (
	"time"

	"github.com/garber-squared/git-arborist/internal/activity"
)

// rowActive reports whether a worktree is doing something: a process is running
// in its pane, or its files or git metadata changed recently enough. This is
// what the active-only filter keeps on screen, so it uses the longer Linger
// window — a worktree you are working in should not drop out of sight during a
// pause between keystrokes.
func (m *Model) rowActive(row Row, now time.Time) bool {
	// A worktree with an agent in its pane is never hidden, even when the agent
	// is waiting rather than working: an agent holding a question is precisely
	// the tile the user needs to be able to find.
	if row.AgentPresent {
		return true
	}
	// Work running in the pane — a test run, a build — counts while it runs.
	// Something merely left running there does not.
	if row.Working {
		return true
	}
	_, ok := m.activity.Recent(row.Worktree.Path, now, activity.Linger)
	return ok
}

// recentActivity returns the worktree's last change when it is recent enough to
// flash — the short Window, not the filter's Linger, so the flash means "just
// now" while the tile stays visible for longer. Only disk activity flashes: a
// long-running process is already legible from the pane contents and the
// coloured agent name, whereas a write that happened a second ago leaves no
// other trace on the tile.
func (m *Model) recentActivity(row Row, now time.Time) (activity.Event, bool) {
	return m.activity.Recent(row.Worktree.Path, now, activity.Window)
}

// applyFilter rebuilds the displayed rows from allRows. With the active-only
// filter on, quiet worktrees drop out; the focused worktree keeps the cursor
// when it survives, and the cursor falls back to a neighbouring tile when it
// does not.
func (m *Model) applyFilter() {
	focused := m.focusedPath()

	if !m.activeOnly {
		m.rows = make([]Row, len(m.allRows))
		copy(m.rows, m.allRows)
	} else {
		now := time.Now()
		rows := make([]Row, 0, len(m.allRows))
		for _, row := range m.allRows {
			if m.rowActive(row, now) {
				rows = append(rows, row)
			}
		}
		m.rows = rows
	}

	m.restoreCursor(focused)
}

// focusedPath is the worktree under the cursor, or "" when there is none.
func (m *Model) focusedPath() string {
	if m.cursorIdx >= 0 && m.cursorIdx < len(m.rows) {
		return m.rows[m.cursorIdx].Worktree.Path
	}
	return ""
}

// restoreCursor puts the cursor back on the given worktree after the displayed
// rows changed, clamping it into range when that worktree is no longer shown.
func (m *Model) restoreCursor(path string) {
	if path != "" {
		for i, row := range m.rows {
			if row.Worktree.Path == path {
				m.cursorIdx = i
				m.ensureCursorVisible()
				return
			}
		}
	}
	if m.cursorIdx >= len(m.rows) {
		m.cursorIdx = max(0, len(m.rows)-1)
	}
	if m.cursorIdx < 0 {
		m.cursorIdx = 0
	}
	m.ensureCursorVisible()
}
