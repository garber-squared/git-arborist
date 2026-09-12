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
| `←` / `→` / `↑` / `↓` | Move cursor between tiles |
| `Space` | Toggle selection of the focused tile |
| `a` | Select all tiles / clear the selection |
| `Esc` | Clear the selection |
| `i` | Insert text, sent to every selected tile (or the focused one) |
| `j` / `k` | Send Down / Up to the focused pane |
| `e` / `t` | Send Enter / Tab+Enter to the focused pane |
| `h` / `l` | Expand / collapse the focused tile |
| `Enter` | Jump to worktree's tmux window |
| `c` | Create a new worktree |
| `C` | Create a new worktree in watch mode (no agent, watches git status) |
| `n` / `N` | Open a tmux window for this worktree / for every worktree missing one |
| `o` | Open PR in browser |
| `I` | Open the branch's issue in browser |
| `g` | Show detailed git status |
| `d` | Delete worktree (with confirmation) |
| `s` | Cycle scope (all / root / submodules) |
| `r` | Refresh all data |
| `q` / `Ctrl+C` | Quit |

Selection is remembered per worktree, so it survives refreshes and cursor
movement; tiles that leave the current scope drop out of it.

## Creating worktrees

`c` opens a fuzzy-filtered picker over every branch that does not already have
a worktree — local branches first, then branches that exist only on a remote.
Remote-tracking refs are refreshed with a background `git fetch --prune` while
the picker is open, so branches pushed from another machine show up too.

What you type decides what gets created:

- **an existing branch** — checked out directly, or created locally from its
  remote when only the remote has it
- **a new branch name** — arborist asks which branch to start it from
  (the repository's default branch is preselected)
- **an issue number** — arborist uses the branch GitHub already links to that
  issue, or creates one with `gh issue develop --base <base>`

The worktree lands at `<repo>/worktrees/<branch>` (nested for branch names
with slashes). Setup that a hand-made worktree would need is done for you:
`worktrees/` is added to `.git/info/exclude` so it stays out of `git status`,
top-level `.env*` files are symlinked from the main checkout, and
`.claude/settings.local.json` is copied. A tmux window for the new worktree is
created by the refresh that follows.

New branches (and issue branches) are created in the superproject. To put a
worktree on a *submodule's* new branch, create the branch with git and then
pick it from the list.

### What runs in the new window

Creating a worktree is only half the job — the window it opens has to make the
worktree usable. arborist composes that command from three pieces, none of
which name a framework:

| Piece | Where it comes from | Example |
|---|---|---|
| **setup** | the worktree's `scripts/worktree-setup.sh`, else `worktree-setup.sh`, called with the worktree path as `$1`. Override with `git config arborist.setup` | `npm install` / `bundle install` |
| **start** | `git config arborist.start` | `make issue-fetch ISSUE=$ARBORIST_ISSUE` |
| **watch** | `git config arborist.watch`, defaulting to `watch -n 1 -c 'git -c color.status=always status'` | a live git status |

`c` runs **setup && start**, and `C` runs **setup ; watch** — the same two
modes as a `worktree.sh` / `worktree.sh --watch` pair. Watch mode never runs
the start command: that is the point of it, since a start command usually ends
in `exec claude`, and sometimes you want to observe a worktree rather than put
an agent in it. The operators differ deliberately too — no agent is launched
into a half-installed worktree (`&&`), while the watcher runs either way (`;`),
because a failed setup is exactly when you want to see the status.

The picker's title says which mode you are in (`New worktree · watch`), so a
watch worktree is never created by accident.

Every piece runs with the worktree as its working directory, and the pane drops
into an interactive shell afterwards — so a failed setup leaves its output on
screen (visible right in the tile) and a usable window, rather than a dead
pane. The setup script is taken from the worktree itself, so a branch that
changes setup gets its own version; one that lost its exec bit is run through
`bash` rather than skipped.

Three variables are exported into the window:

| Variable | Value |
|---|---|
| `ARBORIST_WORKTREE` | absolute path of the worktree |
| `ARBORIST_BRANCH` | branch checked out in it |
| `ARBORIST_ISSUE` | leading number of the branch name, empty if it has none |

A repo like this needs no arborist-specific files at all beyond the one config
line:

```bash
git config arborist.start 'make issue-fetch ISSUE=$ARBORIST_ISSUE'
```

`n` and `N` open windows in work mode (setup && start). The windows a *refresh*
opens automatically for pane-less worktrees stay plain shells: those happen
unprompted, on every refresh, so running setup there would re-run installs and
relaunch agents behind your back.

### Dev ports

Worktrees that each run a dev server or container stack need a port apiece.
arborist keeps that assignment in `<main repo root>/.worktree-ports`, one
`<dir> <port>` line per worktree, counting up from a base (8080) with the main
checkout holding the base itself:

```
/repo/worktrees/1905-twilio-exit 8081
/repo/worktrees/ds/loading-splash 8082
```

- **Assigned** when arborist opens a worktree's window (`c`, `C`, `n`, `N`),
  and exported into it as `ARBORIST_PORT` — so a start command can be
  `make up DEV_PORT=$ARBORIST_PORT`. Assignment is idempotent: a worktree's
  port never moves.
- **Shown** on the tile (`clean │ #1906 │ :8081`), so the dashboard tells you
  which localhost port belongs to which branch.
- **Released** when arborist tears the worktree down — `d`, or the automatic
  teardown of a merged PR — right after its containers are removed, so the port
  is free for the next worktree instead of climbing forever. Entries other
  tools left behind are never touched.

This is off unless the repo asks for it, so no repository grows a stray file:
it turns on when `.worktree-ports` already exists, or when a base is set with
`git config arborist.portBase 8080`. The file and format are the ones
`worktree-port.sh` uses, so a `make` target that resolves its own port
(`DEV_PORT := $(shell ./scripts/worktree-port.sh)`) keeps working, and both
sides agree on the answer. Pruning entries for worktrees removed *outside*
arborist is still that script's job (`--cleanup`).

### Issue labels

A worktree usually means "I am working on this now", which is worth saying on
the issue:

```bash
git config arborist.issueLabel status:in-development
```

With that set, creating a worktree adds the label to the linked issue —
creating the label in the repository first if it does not exist (green,
"Issue is actively being worked on"). The issue is the one you picked when you
typed a number, or else the one the branch name carries.

Only the `<number>-slug` form `gh issue develop` produces counts, on the last
path segment: `1905-twilio-exit` and `ds/1905-loading-splash` resolve to
#1905, while `release-1.6.0`, `v2-refactor` and `gh-123` resolve to nothing.
Labelling writes to someone's issue tracker, so a wrong guess is worse than no
guess — which is also why this is opt-in. (The looser rule, any digits
anywhere, still drives `I` and `ARBORIST_ISSUE`, where a bad guess costs a
browser tab.)

A label that cannot be applied is reported in the status line and never fails
the worktree:

```
Created worktree '4242-fix' · running setup · port 8083 · label failed: none of the git remotes…
```

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
