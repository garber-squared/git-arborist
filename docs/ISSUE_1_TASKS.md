# Issue #1 Task Breakdown: TUI Dashboard for Git Worktrees

## Phase 1 — MVP

### Task 1: Initialize Go module and project structure
- `go mod init github.com/garber-squared/git-arborist`
- Install dependencies: bubbletea, lipgloss, bubbles (table), fsnotify
- Create directory layout:
  ```
  cmd/arborist/main.go
  internal/worktree/discovery.go
  internal/agent/state.go
  internal/gitstatus/gitstatus.go
  internal/pr/pr.go
  internal/watcher/watcher.go
  internal/tmux/tmux.go
  internal/tui/model.go
  internal/tui/view.go
  internal/tui/update.go
  internal/tui/keys.go
  ```

### Task 2: Worktree discovery
- Parse `git worktree list --porcelain` output
- Extract worktree path and branch name for each entry
- Return `[]Worktree` slice

### Task 3: Git status per worktree
- Run `git -C <path> status --porcelain` for dirty/clean
- Run `git -C <path> rev-list --left-right --count @{upstream}...HEAD` for ahead/behind
- Detect staged changes and untracked files
- Return structured `GitStatus`

### Task 4: Agent state reader
- Read `.sideby/agent/state.json` from each worktree path
- Parse JSON into `AgentState` struct
- Handle missing file gracefully (agent not running)
- Validate against known states: starting, running, awaiting_input, idle, error, stopped

### Task 5: PR detection
- Run `gh pr view --json number,state,title,isDraft` from each worktree path
- Parse JSON output into `PullRequest` struct
- Handle "no PR" case gracefully

### Task 6: fsnotify file watcher
- Watch `.sideby/agent/state.json` per worktree for agent state changes
- Watch `.git/HEAD`, `.git/index`, `.git/refs/` for git state changes
- Send Bubble Tea messages on file change events
- Support adding/removing watchers when worktrees change

### Task 7: Bubble Tea TUI — model, view, and keybindings
- Table layout: Branch | Agent | PR | Git Status
- Row navigation with j/k
- Manual refresh with `r`
- Quit with `q`
- Open PR in browser with `o` (via `gh pr view --web`)
- Jump to TMUX window with `enter`
- Show detailed git status with `g`

### Task 8: TMUX integration
- Read `tmux.session` and `tmux.window` from agent state
- Execute `tmux select-window -t <session>:<window>` on enter
- Fallback: find tmux window by worktree path if agent state has no tmux info

### Task 9: Wire everything together in main.go
- Discover worktrees on startup
- Populate initial state (git status, agent state, PR)
- Start fsnotify watchers
- Launch Bubble Tea program
- Clean shutdown on quit

### Task 10: Add Makefile target and build
- Add `make dashboard` / `make tui` target
- Add `make build` target for Go binary
- Verify end-to-end: launch from repo root, see worktrees, manual refresh works
