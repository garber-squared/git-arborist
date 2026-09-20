package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestWatcher builds a Watcher with its lookup tables filled in but no
// inotify handle: everything under test classifies paths rather than watching
// them.
func newTestWatcher(targets ...Target) *Watcher {
	w := &Watcher{
		gitDirs:  make(map[string]string),
		logDirs:  make(map[string]string),
		byBranch: make(map[string]string),
		lastFile: make(map[string]time.Time),
	}
	for _, t := range targets {
		gitDir := filepath.Join(t.Path, ".git")
		w.register(t, gitDir, gitDir)
	}
	return w
}

func TestGitDirsMainWorktree(t *testing.T) {
	root := t.TempDir()
	gitPath := filepath.Join(root, ".git")
	if err := os.Mkdir(gitPath, 0755); err != nil {
		t.Fatal(err)
	}

	gitDir, commonDir := gitDirs(root)
	if gitDir != gitPath || commonDir != gitPath {
		t.Errorf("gitDirs = %q, %q; want %q for both", gitDir, commonDir, gitPath)
	}
}

func TestGitDirsLinkedWorktree(t *testing.T) {
	root := t.TempDir()
	commonPath := filepath.Join(root, "repo", ".git")
	wtPath := filepath.Join(root, "repo", "worktrees", "feature")
	wantGitDir := filepath.Join(commonPath, "worktrees", "feature")

	if err := os.MkdirAll(wtPath, 0755); err != nil {
		t.Fatal(err)
	}
	// git writes the pointer with a trailing newline, which must not become
	// part of the path.
	pointer := "gitdir: " + wantGitDir + "\n"
	if err := os.WriteFile(filepath.Join(wtPath, ".git"), []byte(pointer), 0644); err != nil {
		t.Fatal(err)
	}

	gitDir, commonDir := gitDirs(wtPath)
	if gitDir != wantGitDir {
		t.Errorf("gitDir = %q, want %q", gitDir, wantGitDir)
	}
	if commonDir != commonPath {
		t.Errorf("commonDir = %q, want %q", commonDir, commonPath)
	}
}

func TestGitDirsMissing(t *testing.T) {
	if gitDir, commonDir := gitDirs(t.TempDir()); gitDir != "" || commonDir != "" {
		t.Errorf("a directory with no .git gave %q, %q; want empty", gitDir, commonDir)
	}
}

func TestSplitRemoteRef(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantRoot   string
		wantBranch string
		wantOK     bool
	}{
		{
			name:       "plain branch",
			path:       "/repo/.git/refs/remotes/origin/main",
			wantRoot:   "/repo/.git",
			wantBranch: "main",
			wantOK:     true,
		},
		{
			name:       "slashed branch keeps its slashes",
			path:       "/repo/.git/refs/remotes/origin/ds/fix-thing",
			wantRoot:   "/repo/.git",
			wantBranch: "ds/fix-thing",
			wantOK:     true,
		},
		{
			name:   "the remote's own directory is not a ref",
			path:   "/repo/.git/refs/remotes/origin",
			wantOK: false,
		},
		{
			name:   "a local branch is not a remote ref",
			path:   "/repo/.git/refs/heads/main",
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root, branch, ok := splitRemoteRef(tc.path)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if root != tc.wantRoot || branch != tc.wantBranch {
				t.Errorf("= %q, %q; want %q, %q", root, branch, tc.wantRoot, tc.wantBranch)
			}
		})
	}
}

func TestClassifyGitOperations(t *testing.T) {
	const wtPath = "/repo/worktrees/feature"
	w := newTestWatcher(Target{Path: wtPath, Branch: "feature"})
	gitDir := filepath.Join(wtPath, ".git")

	tests := []struct {
		name   string
		path   string
		wantOp string
	}{
		{"a rewritten index may be a git add", filepath.Join(gitDir, "index"), GitOpIndex},
		{"the HEAD reflog grows on commit", filepath.Join(gitDir, "logs", "HEAD"), GitOpCommit},
		{"COMMIT_EDITMSG is written on commit", filepath.Join(gitDir, "COMMIT_EDITMSG"), GitOpCommit},
		{"a moved remote ref is a push", filepath.Join(gitDir, "refs", "remotes", "origin", "feature"), GitOpPush},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path, op, ok := w.classifyGit(tc.path)
			if !ok {
				t.Fatalf("%s was not recognised as git activity", tc.path)
			}
			if op != tc.wantOp {
				t.Errorf("op = %q, want %q", op, tc.wantOp)
			}
			if path != wtPath {
				t.Errorf("worktree = %q, want %q", path, wtPath)
			}
		})
	}
}

func TestClassifyGitIgnoresOtherMetadata(t *testing.T) {
	const wtPath = "/repo/worktrees/feature"
	w := newTestWatcher(Target{Path: wtPath, Branch: "feature"})
	gitDir := filepath.Join(wtPath, ".git")

	for _, path := range []string{
		filepath.Join(gitDir, "ORIG_HEAD"),
		filepath.Join(gitDir, "objects", "ab", "cdef"),
		filepath.Join(gitDir, "refs", "remotes", "origin", "someone-elses-branch"),
	} {
		if _, _, ok := w.classifyGit(path); ok {
			t.Errorf("%s must not be reported as git activity", path)
		}
	}
}

// A push updates a ref in the shared common directory, so the branch name is
// what says which worktree did it.
func TestClassifyGitAttributesPushesByBranch(t *testing.T) {
	const (
		one = "/repo/worktrees/one"
		two = "/repo/worktrees/two"
	)
	w := &Watcher{
		gitDirs:  make(map[string]string),
		logDirs:  make(map[string]string),
		byBranch: make(map[string]string),
		lastFile: make(map[string]time.Time),
	}
	common := "/repo/.git"
	w.register(Target{Path: one, Branch: "feature-one"}, filepath.Join(common, "worktrees", "one"), common)
	w.register(Target{Path: two, Branch: "feature-two"}, filepath.Join(common, "worktrees", "two"), common)

	path, op, ok := w.classifyGit(filepath.Join(common, "refs", "remotes", "origin", "feature-two"))
	if !ok || op != GitOpPush {
		t.Fatalf("classifyGit = %q, %q, %v; want a push", path, op, ok)
	}
	if path != two {
		t.Errorf("push attributed to %q, want %q", path, two)
	}
}

func TestClassifyFileChange(t *testing.T) {
	const wtPath = "/repo/worktrees/feature"
	w := newTestWatcher(Target{Path: wtPath, Branch: "feature"})

	msg, ok := w.classify(filepath.Join(wtPath, "app", "models", "user.rb"))
	if !ok {
		t.Fatal("a file in the working tree must be reported")
	}
	if msg.Kind != KindFile {
		t.Errorf("Kind = %q, want %q", msg.Kind, KindFile)
	}
	if msg.WorktreePath != wtPath {
		t.Errorf("WorktreePath = %q, want %q", msg.WorktreePath, wtPath)
	}
	if msg.At.IsZero() {
		t.Error("the message must carry the time of the change")
	}
}

// Git's own bookkeeping must not read as an edit, or every git command would
// look like work in progress.
func TestClassifyIgnoresGitInternals(t *testing.T) {
	const wtPath = "/repo/worktrees/feature"
	w := newTestWatcher(Target{Path: wtPath, Branch: "feature"})

	for _, path := range []string{
		filepath.Join(wtPath, ".git", "ORIG_HEAD"),
		filepath.Join(wtPath, ".git", "objects", "pack", "tmp_pack_x"),
	} {
		if msg, ok := w.classify(path); ok {
			t.Errorf("%s was reported as %q activity", path, msg.Kind)
		}
	}
}

func TestClassifyIgnoresLockFiles(t *testing.T) {
	const wtPath = "/repo/worktrees/feature"
	w := newTestWatcher(Target{Path: wtPath, Branch: "feature"})

	if _, ok := w.classify(filepath.Join(wtPath, ".git", "index.lock")); ok {
		t.Error("a lock file must not be reported; the rename into place is the event")
	}
}

func TestClassifyAgentState(t *testing.T) {
	const wtPath = "/repo/worktrees/feature"
	w := newTestWatcher(Target{Path: wtPath, Branch: "feature"})

	msg, ok := w.classify(filepath.Join(wtPath, ".sideby", "agent", "state.json"))
	if !ok {
		t.Fatal("the agent state file must be reported")
	}
	if msg.Kind != KindAgent {
		t.Errorf("Kind = %q, want %q", msg.Kind, KindAgent)
	}
	if msg.WorktreePath != wtPath {
		t.Errorf("WorktreePath = %q, want %q", msg.WorktreePath, wtPath)
	}
}

func TestClassifyIgnoresUnwatchedPaths(t *testing.T) {
	w := newTestWatcher(Target{Path: "/repo/worktrees/feature", Branch: "feature"})

	if _, ok := w.classify("/somewhere/else/file.go"); ok {
		t.Error("a path outside every watched worktree must not be reported")
	}
}

// Worktrees commonly live inside the main checkout, so the innermost worktree
// has to claim its own files.
func TestWorktreeForPrefersTheNestedWorktree(t *testing.T) {
	const (
		outer = "/repo"
		inner = "/repo/worktrees/feature"
	)
	w := newTestWatcher(Target{Path: outer}, Target{Path: inner})

	got, ok := w.worktreeFor(filepath.Join(inner, "app", "user.rb"))
	if !ok {
		t.Fatal("a file in the nested worktree must match")
	}
	if got != inner {
		t.Errorf("worktreeFor = %q, want %q", got, inner)
	}
}

func TestWorktreeForRejectsASiblingWithASharedPrefix(t *testing.T) {
	w := newTestWatcher(Target{Path: "/repo/worktrees/feature"})

	if got, ok := w.worktreeFor("/repo/worktrees/feature-two/app.rb"); ok {
		t.Errorf("worktreeFor matched %q; a name prefix is not a parent directory", got)
	}
}

func TestAllowFileDebounces(t *testing.T) {
	const wtPath = "/repo/worktrees/feature"
	w := newTestWatcher(Target{Path: wtPath})
	start := time.Now()

	if !w.allowFile(wtPath, start) {
		t.Fatal("the first change must be reported")
	}
	if w.allowFile(wtPath, start.Add(fileDebounce/2)) {
		t.Error("a change inside the debounce interval must be suppressed")
	}
	if !w.allowFile(wtPath, start.Add(2*fileDebounce)) {
		t.Error("a change after the debounce interval must be reported")
	}
}

func TestAllowFileIsPerWorktree(t *testing.T) {
	w := newTestWatcher()
	now := time.Now()

	if !w.allowFile("/repo/worktrees/one", now) {
		t.Fatal("the first change must be reported")
	}
	if !w.allowFile("/repo/worktrees/two", now) {
		t.Error("one busy worktree must not silence another")
	}
}
