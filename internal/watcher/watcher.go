package watcher

import (
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
)

// FileChangedMsg is sent when a watched file changes.
type FileChangedMsg struct {
	WorktreePath string
	Kind         string // "agent", "git"
}

// Watcher watches agent state files and git metadata for changes.
type Watcher struct {
	fsw    *fsnotify.Watcher
	sendFn func(tea.Msg)
}

// New creates a new file watcher that sends Bubble Tea messages via sendFn.
func New(sendFn func(tea.Msg)) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{fsw: fsw, sendFn: sendFn}
	go w.loop()
	return w, nil
}

// WatchWorktree sets up watches for agent state and git metadata in a worktree.
func (w *Watcher) WatchWorktree(worktreePath string) {
	// Watch agent state directory
	agentDir := filepath.Join(worktreePath, ".sideby", "agent")
	if _, err := os.Stat(agentDir); err == nil {
		_ = w.fsw.Add(agentDir)
	}

	// Watch git metadata
	gitDir := filepath.Join(worktreePath, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		return
	}

	if info.IsDir() {
		// Main worktree: .git is a directory
		_ = w.fsw.Add(gitDir)
		refsDir := filepath.Join(gitDir, "refs")
		if _, err := os.Stat(refsDir); err == nil {
			_ = w.fsw.Add(refsDir)
			headsDir := filepath.Join(refsDir, "heads")
			if _, err := os.Stat(headsDir); err == nil {
				_ = w.fsw.Add(headsDir)
			}
		}
	} else {
		// Linked worktree: .git is a file pointing to the gitdir
		data, err := os.ReadFile(gitDir)
		if err == nil {
			// Format: "gitdir: /path/to/main/.git/worktrees/<name>"
			line := string(data)
			if len(line) > 8 {
				gitdirPath := line[8:] // skip "gitdir: "
				gitdirPath = filepath.Clean(gitdirPath)
				_ = w.fsw.Add(gitdirPath)
			}
		}
	}
}

// Close stops the watcher.
func (w *Watcher) Close() {
	_ = w.fsw.Close()
}

func (w *Watcher) loop() {
	for {
		select {
		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			kind := "git"
			if filepath.Base(event.Name) == "state.json" {
				kind = "agent"
			}
			// Derive worktree path from the changed file
			wtPath := deriveWorktreePath(event.Name, kind)
			w.sendFn(FileChangedMsg{WorktreePath: wtPath, Kind: kind})

		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		}
	}
}

func deriveWorktreePath(changedPath, kind string) string {
	if kind == "agent" {
		// .sideby/agent/state.json → worktree root is 3 levels up
		return filepath.Dir(filepath.Dir(filepath.Dir(changedPath)))
	}
	// Git files: could be .git/HEAD, .git/index, .git/refs/heads/branch
	// Walk up until we find a directory that doesn't end in a git-internal name
	dir := filepath.Dir(changedPath)
	for {
		base := filepath.Base(dir)
		if base == ".git" || base == "refs" || base == "heads" || base == "remotes" || base == "worktrees" {
			dir = filepath.Dir(dir)
			continue
		}
		return dir
	}
}
