package tui

import (
	"fmt"
	"strconv"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/garber-squared/git-arborist/internal/activity"
	"github.com/garber-squared/git-arborist/internal/pr"
)

// A worktree family is a parent GitHub issue's worktree plus the worktrees of
// its sub-issues. The workflow it serves: a Claude session in the parent's
// worktree plans the work and makes the changes in the sub-issues' worktrees,
// which have no session of their own. The parent is told apart by GitHub's
// sub-issue link, not by guessing from activity, so the roles hold while every
// worktree is idle.

type familyRole int

const (
	roleNone   familyRole = iota
	roleParent            // its issue has sub-issues with worktrees here
	roleSub               // its issue is a sub-issue of another issue
)

type familyInfo struct {
	Role familyRole
	// Issue is the family's parent issue number: the worktree's own issue for a
	// parent, its parent's for a sub. It is what colours a family alike.
	Issue int
	// Subs is how many sub-issue worktrees a parent has on the dashboard.
	Subs int
	// ParentPath is a sub's parent worktree, "" when the parent issue has none.
	ParentPath string
}

// familyView limits the grid to one side of the parent/sub-issue split (m).
// It filters on the role alone, so a sub whose parent has no worktree here
// still counts as a sub.
type familyView int

const (
	familyAll     familyView = iota
	familyParents            // parent worktrees only
	familyBoth               // parents and subs, no unrelated worktrees
	familySubs               // sub-issue worktrees only
)

func (v familyView) String() string {
	switch v {
	case familyParents:
		return "masters"
	case familyBoth:
		return "masters + slaves"
	case familySubs:
		return "slaves"
	default:
		return "all"
	}
}

// next is the view the m key cycles to.
func (v familyView) next() familyView {
	return (v + 1) % (familySubs + 1)
}

// keeps reports whether a worktree with this role is shown in the view.
func (v familyView) keeps(r familyRole) bool {
	switch v {
	case familyParents:
		return r == roleParent
	case familyBoth:
		return r != roleNone
	case familySubs:
		return r == roleSub
	default:
		return true
	}
}

// grouped reports whether the worktree shares the dashboard with at least one
// other member of its family: a parent always does, a sub only when its
// parent's worktree is here.
func (f familyInfo) grouped() bool {
	return f.Role == roleParent || f.ParentPath != ""
}

// rowIssue is the issue a worktree's branch was created for, 0 when none.
func rowIssue(row Row) int {
	n, _ := strconv.Atoi(pr.LinkedIssueNumber(row.Worktree.Branch))
	return n
}

// buildFamilies places every worktree in its family. Issue numbers are only
// unique within a repository, so a parent and its subs must share a RepoRoot.
func buildFamilies(rows []Row) map[string]familyInfo {
	type key struct {
		repo  string
		issue int
	}
	parents := make(map[key]string) // issue → worktree path
	for _, row := range rows {
		if n := rowIssue(row); n != 0 {
			parents[key{row.Worktree.RepoRoot, n}] = row.Worktree.Path
		}
	}

	fam := make(map[string]familyInfo)
	for _, row := range rows {
		if row.ParentIssue == 0 {
			continue
		}
		k := key{row.Worktree.RepoRoot, row.ParentIssue}
		parentPath := parents[k]
		if fam[row.Worktree.Path].Role != roleParent {
			fam[row.Worktree.Path] = familyInfo{Role: roleSub, Issue: row.ParentIssue, ParentPath: parentPath}
		}
		if parentPath == "" {
			continue
		}
		p := fam[parentPath]
		// A worktree that is both a sub of one issue and the parent of others
		// shows as a parent: the sessions it drives matter more on its tile.
		p.Role, p.Issue = roleParent, k.issue
		p.Subs++
		p.ParentPath = ""
		fam[parentPath] = p
	}
	return fam
}

// rowByPath finds a worktree's row among all rows, including filtered ones.
func (m *Model) rowByPath(path string) (Row, bool) {
	for _, row := range m.allRows {
		if row.Worktree.Path == path {
			return row, true
		}
	}
	return Row{}, false
}

// drivenBy reports whether a sub-issue worktree is being changed by its
// parent's session right now: it changed on disk within the flash window, it
// has no agent of its own, and its parent worktree has one. The parent's
// issue number is returned for the label.
func (m *Model) drivenBy(row Row, now time.Time) (int, bool) {
	f := m.family[row.Worktree.Path]
	if f.Role != roleSub || f.ParentPath == "" || row.AgentPresent {
		return 0, false
	}
	ev, ok := m.recentActivity(row, now)
	if !ok || ev.Kind == activity.KindClaude {
		return 0, false
	}
	parent, ok := m.rowByPath(f.ParentPath)
	if !ok || !parent.AgentPresent {
		return 0, false
	}
	return f.Issue, true
}

// driving lists the sub-issues a parent's session is changing right now.
func (m *Model) driving(row Row, now time.Time) []int {
	if m.family[row.Worktree.Path].Role != roleParent {
		return nil
	}
	var subs []int
	for _, sub := range m.allRows {
		if m.family[sub.Worktree.Path].ParentPath != row.Worktree.Path {
			continue
		}
		if _, ok := m.drivenBy(sub, now); ok {
			subs = append(subs, rowIssue(sub))
		}
	}
	return subs
}

// familyPalette colours families so a parent and its subs match at a glance.
// Like repoPalette it avoids blue (selection), magenta (marks), cyan and bright
// green (activity flashes); it is warmer so families and submodules differ.
var familyPalette = []lipgloss.Color{
	lipgloss.Color("214"), // orange
	lipgloss.Color("141"), // lavender
	lipgloss.Color("209"), // salmon
	lipgloss.Color("186"), // khaki
	lipgloss.Color("217"), // peach
	lipgloss.Color("179"), // amber
}

func familyColor(issue int) lipgloss.Color {
	return familyPalette[issue%len(familyPalette)]
}

// familyBadge is the info-line label for a worktree's role in its family, ""
// when it has none: "◆ 3 subs" on a parent (with "→#2613" for each sub its
// session is changing right now), "↳ #2611" on a sub. A sub whose parent has
// no worktree here is dimmed, since nothing on the dashboard can drive it.
func (m *Model) familyBadge(row Row, now time.Time) string {
	f := m.family[row.Worktree.Path]
	style := lipgloss.NewStyle().Foreground(familyColor(f.Issue)).Bold(true)
	switch f.Role {
	case roleParent:
		label := fmt.Sprintf("◆ %d sub", f.Subs)
		if f.Subs != 1 {
			label += "s"
		}
		for _, n := range m.driving(row, now) {
			label += fmt.Sprintf(" →#%d", n)
		}
		return style.Render(label)
	case roleSub:
		if f.ParentPath == "" {
			return styleDim.Render(fmt.Sprintf("↳ #%d", f.Issue))
		}
		return style.Render(fmt.Sprintf("↳ #%d", f.Issue))
	}
	return ""
}
