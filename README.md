# git-arborist

A terminal dashboard for monitoring Git worktrees. Built for parallel development workflows where you manage multiple worktrees with AI coding agents in tmux.

![Go](https://img.shields.io/badge/Go-1.25-blue) ![TUI](https://img.shields.io/badge/TUI-Bubble%20Tea-ff69b4)

## What it does

git-arborist gives you a single view of all your Git worktrees and their statuses:

```
  Worktree Dashboard (my-repo)

  Branch                PR                                                  Git Status
  ────────────────────  ──────────────────────────────────────────────────  ──────────────
  ▸ feature/auth        #123 Add OAuth login (open)                         clean ↑1
    fix/parsing         —                                                   dirty
    exp/refactor        #130 Refactor parser module (draft)                 clean
```

Each row shows:
- **Branch** name
- **PR** number, title, and status from GitHub
- **Git status** (clean/dirty, ahead/behind upstream)

Updates are event-driven via filesystem watching — no polling.

## Prerequisites

- **Go 1.25+** (to build)
- **git** with worktree support
- **gh** (GitHub CLI) for PR detection
- **tmux** for window navigation

## Install

```bash
git clone https://github.com/garber-squared/git-arborist.git
cd git-arborist
make go-build
```

The binary is placed at `./bin/arborist`.

## Usage

Run from the root of a Git repository that has worktrees:

```bash
./bin/arborist
```

Or use the Makefile:

```bash
make dashboard
```

### Keybindings

| Key | Action |
|---|---|
| `j` / `↓` | Move cursor down |
| `k` / `↑` | Move cursor up |
| `Enter` | Jump to worktree's tmux window |
| `o` | Open PR in browser |
| `g` | Show detailed git status |
| `d` | Delete worktree (with confirmation) |
| `r` | Refresh all data |
| `q` / `Ctrl+C` | Quit |

## Agent State

git-arborist reads agent state from a JSON file in each worktree:

```
<worktree>/.sideby/agent/state.json
```

```json
{
  "agent": "claude",
  "state": "awaiting_input",
  "updated_at": "2026-02-11T14:03:22-05:00",
  "detail": "Needs approval to apply patch",
  "tmux": {
    "session": "repo",
    "window": 3,
    "pane": 1
  }
}
```

The `tmux` field is used for the `Enter` key jump-to-window feature. If absent, arborist falls back to searching tmux windows by worktree path.

## Architecture

```
cmd/arborist/main.go          Entry point
internal/
  tui/                         Bubble Tea model, view, update
  worktree/                    Discover worktrees via `git worktree list`
  agent/                       Read agent state from JSON
  gitstatus/                   Clean/dirty, ahead/behind via git commands
  pr/                          PR detection via `gh pr view`
  tmux/                        Jump to tmux windows
  watcher/                     fsnotify file watcher for live updates
```

File changes (`.git/` and `.sideby/agent/`) trigger targeted row refreshes. Manual refresh with `r` reloads everything.

## License

MIT
