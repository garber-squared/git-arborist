package watcher

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
)

// Kinds of change reported by FileChangedMsg.
const (
	KindAgent  = "agent"  // the agent state file
	KindGit    = "git"    // git metadata
	KindFile   = "file"   // a file in the working tree
	KindClaude = "claude" // a Claude Code session's transcript
)

// Git operations inferred from which metadata file changed.
const (
	GitOpNone   = ""
	GitOpIndex  = "index"  // the index was rewritten, which may be a git add
	GitOpCommit = "commit" // a commit landed in this worktree
	GitOpPush   = "push"   // a remote-tracking ref for the branch moved
)

// maxWatchDirs caps how many directories are watched across all worktrees.
// inotify watches are a per-user kernel resource, and a walk that wanders into
// a dependency tree can burn thousands; the cap keeps a large repo from
// exhausting the budget (and silently breaking other watchers) instead.
const maxWatchDirs = 4096

// fileDebounce is the shortest gap between two working-tree messages for the
// same worktree. A save that rewrites many files, or a build that touches a
// tree, would otherwise flood the update loop with identical messages.
const fileDebounce = 250 * time.Millisecond

// skipDirs are directory names never descended into when watching a working
// tree: git's own metadata (watched separately, and precisely), agent
// bookkeeping, and the dependency, build and log trees that churn without
// anyone editing them.
var skipDirs = map[string]bool{
	".git": true, ".sideby": true,
	"node_modules": true, "vendor": true, ".bundle": true, ".yarn": true,
	".pnpm-store": true, ".gradle": true, ".venv": true, "venv": true,
	"__pycache__": true, ".pytest_cache": true, ".mypy_cache": true,
	"tmp": true, "log": true, "logs": true, "coverage": true,
	"target": true, "dist": true, "build": true, ".next": true, ".nuxt": true,
	".cache": true, ".terraform": true, ".idea": true,
}

// claudeSkipDirs are the parts of a Claude project directory that are not
// transcripts: tool output spilled to disk, and memory files. Session
// directories themselves are descended into, because a subagent writes its own
// transcript under <session>/subagents while the main one sits idle.
var claudeSkipDirs = map[string]bool{
	"memory": true, "tool-results": true, "session-memory": true,
}

// claudeMaxDirName is the longest project directory name Claude Code writes;
// a longer encoded path is cut to this length and given a hash suffix.
const claudeMaxDirName = 200

// FileChangedMsg is sent when a watched file changes.
type FileChangedMsg struct {
	WorktreePath string
	Kind         string // KindAgent, KindGit, KindFile or KindClaude
	GitOp        string // which git operation, when Kind is KindGit
	At           time.Time
}

// Target is a worktree to watch. Branch is needed because a push updates
// refs/remotes/<remote>/<branch> in the repository's common git directory,
// which every worktree shares: the branch name is what attributes it back to
// one worktree.
type Target struct {
	Path   string
	Branch string
}

// Watcher watches worktrees for file changes and git activity.
type Watcher struct {
	fsw    *fsnotify.Watcher
	sendFn func(tea.Msg)

	mu        sync.Mutex
	watchDirs int                  // directories watched so far, against maxWatchDirs
	paths     []string             // worktree paths, longest first
	gitDirs   map[string]string    // worktree git dir → worktree path
	logDirs   map[string]string    // <git dir>/logs → worktree path
	byBranch  map[string]string    // refKey(common dir, branch) → worktree path
	lastFile  map[string]time.Time // worktree path and kind → last debounced message

	claudeRoot        string            // Claude Code's projects directory, "" when unknown
	claudeRootWatched bool              // whether claudeRoot itself is watched
	claudeDirs        map[string]string // claudeKey of a project directory → worktree path
}

// New creates a new file watcher that sends Bubble Tea messages via sendFn.
func New(sendFn func(tea.Msg)) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		fsw:      fsw,
		sendFn:   sendFn,
		gitDirs:  make(map[string]string),
		logDirs:  make(map[string]string),
		byBranch: make(map[string]string),
		lastFile: make(map[string]time.Time),

		claudeRoot: claudeProjectsDir(),
		claudeDirs: make(map[string]string),
	}
	go w.loop()
	return w, nil
}

// Watch sets up watches for one worktree: its working tree, its agent state file,
// the git metadata that reveals staging, commits and pushes, and the transcripts
// of Claude sessions running in it.
func (w *Watcher) Watch(t Target) {
	w.WatchAll([]Target{t})
}

// WatchAll sets up watches for several worktrees. The small, precise watches —
// agent state, git metadata, Claude transcripts — are placed for every worktree
// before any working tree is walked: working trees can be large enough to spend
// the whole watch budget, and one big checkout must not leave the worktrees
// after it unable to report a commit or a Claude session.
func (w *Watcher) WatchAll(targets []Target) {
	var paths []string
	for _, t := range targets {
		if t.Path == "" {
			continue
		}
		gitDir, commonDir := gitDirs(t.Path)
		w.register(t, gitDir, commonDir)
		w.watchAgentState(t.Path)
		w.watchGitMeta(gitDir, commonDir)
		w.watchClaude(t.Path)
		paths = append(paths, t.Path)
	}
	// fsnotify reports only the directories it is given, so each working tree
	// needs a watch per directory.
	for _, p := range paths {
		w.addTree(p)
	}
}

// WatchWorktree watches a worktree by path alone. Pushes cannot be attributed
// without a branch name, so prefer Watch.
func (w *Watcher) WatchWorktree(worktreePath string) {
	w.Watch(Target{Path: worktreePath})
}

// Close stops the watcher.
func (w *Watcher) Close() {
	_ = w.fsw.Close()
}

func (w *Watcher) register(t Target, gitDir, commonDir string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !slices.Contains(w.paths, t.Path) {
		w.paths = append(w.paths, t.Path)
		// Longest first, so a worktree nested inside another one claims its own
		// events rather than the outer worktree swallowing them.
		slices.SortFunc(w.paths, func(a, b string) int { return len(b) - len(a) })
	}
	if gitDir != "" {
		w.gitDirs[gitDir] = t.Path
		w.logDirs[filepath.Join(gitDir, "logs")] = t.Path
	}
	if commonDir != "" && t.Branch != "" {
		w.byBranch[refKey(commonDir, t.Branch)] = t.Path
	}
}

// addTree watches a directory and every directory below it, minus the skip
// list. New watches descend rather than covering only the top: a directory that
// was just created can already have children (mkdir -p, a checkout, an unpacked
// archive), and those children would otherwise never be watched.
func (w *Watcher) addTree(root string) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable directory should not abandon the walk
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && skipDirs[d.Name()] {
			return fs.SkipDir
		}
		if !w.addDir(path) {
			return fs.SkipAll // watch budget spent
		}
		return nil
	})
}

func (w *Watcher) watchAgentState(worktreePath string) {
	agentDir := filepath.Join(worktreePath, ".sideby", "agent")
	if _, err := os.Stat(agentDir); err == nil {
		w.addDir(agentDir)
	}
}

// watchGitMeta watches the metadata that shows a git operation ran. The index,
// the HEAD reflog and COMMIT_EDITMSG live in the worktree's own git directory,
// so they attribute directly; remote-tracking refs live in the shared common
// directory and are attributed by branch name.
func (w *Watcher) watchGitMeta(gitDir, commonDir string) {
	if gitDir == "" {
		return
	}
	w.addDir(gitDir) // index, COMMIT_EDITMSG
	if logs := filepath.Join(gitDir, "logs"); dirExists(logs) {
		w.addDir(logs) // logs/HEAD: commits
	}
	if commonDir == "" {
		return
	}
	// A push writes refs/remotes/<remote>/<branch>, and a slashed branch name
	// puts it in a subdirectory, so the whole (small) tree is watched.
	w.addTree(filepath.Join(commonDir, "refs", "remotes"))
}

// watchClaude watches the directory where Claude Code keeps the transcripts of
// sessions started in a worktree. A session appends to its transcript on every
// prompt, response and tool call, so a write there means Claude is working even
// when it is only reading or thinking and no project file changes. A worktree
// nobody has run Claude in yet has no directory; the projects directory itself
// is watched so the first session's directory is picked up when it appears.
func (w *Watcher) watchClaude(worktreePath string) {
	if w.claudeRoot == "" {
		return
	}
	key := claudeKey(claudeDirName(worktreePath))
	w.mu.Lock()
	w.claudeDirs[key] = worktreePath
	watchRoot := !w.claudeRootWatched
	w.claudeRootWatched = true
	w.mu.Unlock()

	if watchRoot && dirExists(w.claudeRoot) {
		w.addDir(w.claudeRoot)
	}
	entries, err := os.ReadDir(w.claudeRoot)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && claudeKey(e.Name()) == key {
			w.addClaudeTree(filepath.Join(w.claudeRoot, e.Name()))
		}
	}
}

// addClaudeTree watches a Claude project directory and the session directories
// below it, minus the ones that hold no transcripts.
func (w *Watcher) addClaudeTree(root string) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if path != root && claudeSkipDirs[d.Name()] {
			return fs.SkipDir
		}
		if !w.addDir(path) {
			return fs.SkipAll
		}
		return nil
	})
}

// claudeOwner returns the worktree whose Claude project directory a path lies
// in: the projects directory's child named after the worktree, or anything
// below it.
func (w *Watcher) claudeOwner(name string) (string, bool) {
	if w.claudeRoot == "" {
		return "", false
	}
	rel, ok := strings.CutPrefix(name, w.claudeRoot+string(filepath.Separator))
	if !ok {
		return "", false
	}
	project, _, _ := strings.Cut(rel, string(filepath.Separator))
	w.mu.Lock()
	defer w.mu.Unlock()
	path, ok := w.claudeDirs[claudeKey(project)]
	return path, ok
}

// addDir watches one directory. It reports false only when the watch budget is
// spent, which means "stop walking"; a directory that cannot be watched on its
// own (unreadable, or gone again already) is skipped without ending the walk.
func (w *Watcher) addDir(path string) bool {
	w.mu.Lock()
	if w.watchDirs >= maxWatchDirs {
		w.mu.Unlock()
		return false
	}
	w.watchDirs++
	w.mu.Unlock()

	if err := w.fsw.Add(path); err != nil {
		w.mu.Lock()
		w.watchDirs--
		w.mu.Unlock()
	}
	return true
}

func (w *Watcher) loop() {
	for {
		select {
		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handle(event)

		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		}
	}
}

func (w *Watcher) handle(event fsnotify.Event) {
	const interesting = fsnotify.Write | fsnotify.Create | fsnotify.Remove | fsnotify.Rename
	if event.Op&interesting == 0 {
		return
	}

	// A directory created inside a watched tree needs a watch of its own, or
	// everything written inside it goes unseen.
	if event.Op&fsnotify.Create != 0 {
		if _, ok := w.claudeOwner(event.Name); ok {
			if dirExists(event.Name) && !claudeSkipDirs[filepath.Base(event.Name)] {
				w.addClaudeTree(event.Name)
			}
		} else if w.watchable(event.Name) {
			w.addTree(event.Name)
		}
	}

	msg, ok := w.classify(event.Name)
	if !ok {
		return
	}
	// A transcript is written as often as files are, so it is debounced the
	// same way, but separately: Claude saving a file and then logging that it
	// did must not suppress one another.
	switch msg.Kind {
	case KindFile:
		if !w.allowFile(msg.WorktreePath, msg.At) {
			return
		}
	case KindClaude:
		if !w.allowFile(msg.WorktreePath+"\x00"+KindClaude, msg.At) {
			return
		}
	}
	w.sendFn(msg)
}

// watchable reports whether a newly created directory deserves a watch: one in
// a worktree's working tree, or a remote-ref directory created by a push to a
// slashed branch name. Directories inside git's own storage are refused, since
// .git/objects alone would exhaust the budget.
func (w *Watcher) watchable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	if skipDirs[filepath.Base(path)] {
		return false
	}
	if _, _, ok := splitRemoteRef(path); ok {
		return true
	}
	if insideGitDir(path) {
		return false
	}
	_, ok := w.worktreeFor(path)
	return ok
}

// classify turns a changed path into a message, or reports false when the path
// is not something the dashboard cares about.
func (w *Watcher) classify(name string) (FileChangedMsg, bool) {
	at := time.Now()
	base := filepath.Base(name)

	// git writes through a lock file and renames it into place; the rename
	// itself is the event worth reporting.
	if strings.HasSuffix(base, ".lock") {
		return FileChangedMsg{}, false
	}

	if path, ok := w.claudeOwner(name); ok {
		// Only the transcripts say Claude is working; the rest of the project
		// directory is bookkeeping.
		if filepath.Ext(base) != ".jsonl" {
			return FileChangedMsg{}, false
		}
		return FileChangedMsg{WorktreePath: path, Kind: KindClaude, At: at}, true
	}

	if base == "state.json" && strings.Contains(name, agentStateDir) {
		return FileChangedMsg{
			WorktreePath: agentWorktreePath(name),
			Kind:         KindAgent,
			At:           at,
		}, true
	}

	if path, op, ok := w.classifyGit(name); ok {
		return FileChangedMsg{WorktreePath: path, Kind: KindGit, GitOp: op, At: at}, true
	}

	// Anything else inside git's storage is bookkeeping, not work: reporting it
	// as a file change would make every git command look like an edit.
	if insideGitDir(name) {
		return FileChangedMsg{}, false
	}

	if path, ok := w.worktreeFor(name); ok {
		return FileChangedMsg{WorktreePath: path, Kind: KindFile, At: at}, true
	}
	return FileChangedMsg{}, false
}

// classifyGit maps a changed git metadata path to the worktree it belongs to and
// the operation it implies.
func (w *Watcher) classifyGit(name string) (worktreePath, op string, ok bool) {
	if root, branch, isRemote := splitRemoteRef(name); isRemote {
		w.mu.Lock()
		path := w.byBranch[refKey(root, branch)]
		w.mu.Unlock()
		if path == "" {
			return "", "", false // another worktree's branch, or an unknown one
		}
		return path, GitOpPush, true
	}

	dir := filepath.Dir(name)
	base := filepath.Base(name)

	w.mu.Lock()
	ownerByGitDir, inGitDir := w.gitDirs[dir]
	ownerByLogDir, inLogDir := w.logDirs[dir]
	w.mu.Unlock()

	switch {
	case inLogDir && base == "HEAD":
		// The HEAD reflog is per-worktree and grows on every commit.
		return ownerByLogDir, GitOpCommit, true
	case inGitDir && base == "COMMIT_EDITMSG":
		return ownerByGitDir, GitOpCommit, true
	case inGitDir && base == "index":
		// Plain reads rewrite the index too (git status refreshes cached stat
		// information), so the caller decides whether this was a git add.
		return ownerByGitDir, GitOpIndex, true
	}
	return "", "", false
}

// allowFile rate-limits working-tree and transcript messages per key: the
// worktree path, qualified by kind for anything but working-tree changes. The event's own
// timestamp is what the dashboard records, so a suppressed message costs at
// most this interval of accuracy.
func (w *Watcher) allowFile(path string, at time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if last, ok := w.lastFile[path]; ok && at.Sub(last) < fileDebounce {
		return false
	}
	w.lastFile[path] = at
	return true
}

// worktreeFor returns the watched worktree a path belongs to.
func (w *Watcher) worktreeFor(name string) (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, p := range w.paths { // longest first
		if name == p || strings.HasPrefix(name, p+string(filepath.Separator)) {
			return p, true
		}
	}
	return "", false
}

const agentStateDir = ".sideby" + string(filepath.Separator) + "agent"

// agentWorktreePath maps .sideby/agent/state.json back to the worktree root.
func agentWorktreePath(statePath string) string {
	return filepath.Dir(filepath.Dir(filepath.Dir(statePath)))
}

// refKey identifies a branch within one repository. Two repositories in a
// superproject can have a branch of the same name, so the common git directory
// is part of the key.
func refKey(commonDir, branch string) string {
	return commonDir + "\x00" + branch
}

// splitRemoteRef recognises a remote-tracking ref and splits it into the
// directory holding refs/ and the branch name. The branch keeps its slashes:
// refs/remotes/origin/ds/fix-thing is the branch "ds/fix-thing".
func splitRemoteRef(name string) (root, branch string, ok bool) {
	const marker = string(filepath.Separator) + "refs" + string(filepath.Separator) + "remotes" + string(filepath.Separator)
	idx := strings.Index(name, marker)
	if idx < 0 {
		return "", "", false
	}
	rest := name[idx+len(marker):]
	slash := strings.Index(rest, string(filepath.Separator))
	if slash < 0 {
		return "", "", false // the remote's own directory, not a ref in it
	}
	return name[:idx], rest[slash+1:], true
}

// insideGitDir reports whether a path lives in git's own storage. Linked
// worktrees keep their metadata under <main>/.git/worktrees/<name>, so the one
// check covers both layouts.
func insideGitDir(path string) bool {
	sep := string(filepath.Separator)
	return strings.Contains(path, sep+".git"+sep) || strings.HasSuffix(path, sep+".git")
}

// gitDirs returns a worktree's own git directory and its repository's common git
// directory. For the main worktree .git is that directory; for a linked
// worktree .git is a file pointing at <common>/worktrees/<name>.
func gitDirs(worktreePath string) (gitDir, commonDir string) {
	dotGit := filepath.Join(worktreePath, ".git")
	info, err := os.Stat(dotGit)
	if err != nil {
		return "", ""
	}
	if info.IsDir() {
		return dotGit, dotGit
	}

	data, err := os.ReadFile(dotGit)
	if err != nil {
		return "", ""
	}
	// "gitdir: /path/to/main/.git/worktrees/<name>". The value must be trimmed:
	// the trailing newline would otherwise become part of the path.
	rest, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return "", ""
	}
	gitDir = strings.TrimSpace(rest)
	if gitDir == "" {
		return "", ""
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktreePath, gitDir)
	}
	gitDir = filepath.Clean(gitDir)

	if parent := filepath.Dir(gitDir); filepath.Base(parent) == "worktrees" {
		commonDir = filepath.Dir(parent)
	}
	return gitDir, commonDir
}

// claudeProjectsDir is where Claude Code keeps per-directory session
// transcripts: $CLAUDE_CONFIG_DIR/projects, or ~/.claude/projects.
func claudeProjectsDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "projects")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// claudeDirName encodes a working directory the way Claude Code names its
// project directory: every character other than an ASCII letter or digit
// becomes a dash. Claude Code counts in UTF-16 code units, so a character
// outside the Basic Multilingual Plane becomes two.
func claudeDirName(path string) string {
	var b strings.Builder
	for _, r := range path {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r > 0xFFFF:
			b.WriteString("--")
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// claudeKey reduces a project directory name to what can be matched against a
// worktree. A name over the limit is stored as its first claudeMaxDirName
// characters plus a hash of the full path, which arborist does not reproduce,
// so long names are matched on the truncated part alone.
func claudeKey(dirName string) string {
	if len(dirName) > claudeMaxDirName {
		return dirName[:claudeMaxDirName]
	}
	return dirName
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
