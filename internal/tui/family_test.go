package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/garber-squared/git-arborist/internal/activity"
	"github.com/garber-squared/git-arborist/internal/worktree"
)

func famRow(repo, path, branch string, parent int) Row {
	return Row{Worktree: worktree.Worktree{RepoRoot: repo, Path: path, Branch: branch}, ParentIssue: parent}
}

func TestBuildFamilies(t *testing.T) {
	rows := []Row{
		famRow("/r", "/r/wt/2611", "2611-database-hardening", 0),
		famRow("/r", "/r/wt/2613", "2613-r1-email", 2611),
		famRow("/r", "/r/wt/2614", "2614-r3-tables", 2611),
		// Its parent has no worktree here.
		famRow("/r", "/r/wt/2700", "2700-orphan", 2699),
		// Same issue number in another repo must not join the family.
		famRow("/other", "/other/wt/5", "5-x", 2611),
		// Unrelated.
		famRow("/r", "/r/wt/2439", "2439-harden", 0),
		// Both a sub (of 2611) and a parent (of 2800): shown as a parent.
		famRow("/r", "/r/wt/2612", "2612-r4", 2611),
		famRow("/r", "/r/wt/2800", "2800-r4a", 2612),
	}
	fam := buildFamilies(rows)

	if f := fam["/r/wt/2611"]; f.Role != roleParent || f.Subs != 3 || f.Issue != 2611 {
		t.Errorf("2611 = %+v, want parent of 3", f)
	}
	if f := fam["/r/wt/2613"]; f.Role != roleSub || f.ParentPath != "/r/wt/2611" || f.Issue != 2611 {
		t.Errorf("2613 = %+v, want sub of /r/wt/2611", f)
	}
	if f := fam["/r/wt/2700"]; f.Role != roleSub || f.ParentPath != "" || f.grouped() {
		t.Errorf("2700 = %+v, want ungrouped sub", f)
	}
	if f := fam["/other/wt/5"]; f.ParentPath != "" {
		t.Errorf("cross-repo sub joined a family: %+v", f)
	}
	if f, ok := fam["/r/wt/2439"]; ok {
		t.Errorf("unrelated worktree got %+v", f)
	}
	if f := fam["/r/wt/2612"]; f.Role != roleParent || f.Subs != 1 || f.Issue != 2612 {
		t.Errorf("2612 = %+v, want parent of 1", f)
	}
}

func TestDrivenBy(t *testing.T) {
	now := time.Now()
	parent := famRow("/r", "/r/wt/2611", "2611-db", 0)
	parent.AgentPresent = true
	sub := famRow("/r", "/r/wt/2613", "2613-r1", 2611)

	m := Model{activity: activity.NewTracker(), allRows: []Row{parent, sub}}
	m.applyFilter()

	if _, ok := m.drivenBy(sub, now); ok {
		t.Fatal("a quiet sub is not being driven")
	}
	m.activity.Record(sub.Worktree.Path, activity.KindFile, now)
	if n, ok := m.drivenBy(sub, now); !ok || n != 2611 {
		t.Fatalf("drivenBy = %d,%v, want 2611,true", n, ok)
	}
	if got := m.driving(parent, now); len(got) != 1 || got[0] != 2613 {
		t.Fatalf("driving = %v, want [2613]", got)
	}

	// With its own agent, a sub's changes are its own.
	sub.AgentPresent = true
	if _, ok := m.drivenBy(sub, now); ok {
		t.Error("a sub with its own agent is not driven by its parent")
	}
	sub.AgentPresent = false

	// No session in the parent: nobody to attribute the change to.
	m.allRows[0].AgentPresent = false
	if _, ok := m.drivenBy(sub, now); ok {
		t.Error("a parent without an agent drives nothing")
	}
}

func TestFamilyViewFilters(t *testing.T) {
	m := Model{activity: activity.NewTracker(), allRows: []Row{
		famRow("/r", "/r/wt/2611", "2611-db", 0),
		famRow("/r", "/r/wt/2613", "2613-r1", 2611),
		famRow("/r", "/r/wt/2700", "2700-orphan", 2699), // sub, parent absent
		famRow("/r", "/r/wt/2439", "2439-harden", 0),
	}}
	shown := func() []string {
		var paths []string
		for _, r := range m.rows {
			paths = append(paths, r.Worktree.Path)
		}
		return paths
	}
	want := map[familyView][]string{
		familyAll:     {"/r/wt/2611", "/r/wt/2613", "/r/wt/2700", "/r/wt/2439"},
		familyParents: {"/r/wt/2611"},
		familyBoth:    {"/r/wt/2611", "/r/wt/2613", "/r/wt/2700"},
		familySubs:    {"/r/wt/2613", "/r/wt/2700"},
	}
	v := familyAll
	for range 4 {
		m.familyView = v
		m.applyFilter()
		if got := shown(); strings.Join(got, ",") != strings.Join(want[v], ",") {
			t.Errorf("%s: got %v, want %v", v, got, want[v])
		}
		v = v.next()
	}
	if v != familyAll {
		t.Errorf("cycle did not return to all, got %s", v)
	}
}
