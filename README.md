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
| `f` | Show only worktrees that are active right now |
| `d` | Delete worktree (with confirmation) |
| `s` | Cycle scope (all / root / submodules) |
| `r` | Refresh all data |
| `q` / `Ctrl+C` | Quit |

Selection is remembered per worktree, so it survives refreshes and cursor
movement; tiles that leave the current scope drop out of it.

## Activity

A worktree that changes on disk flashes its border in cyan for five seconds, and
for those five seconds its tile says what happened: `⚡file`, `⚡add`, `⚡commit`
or `⚡push`. The signal comes from the filesystem watcher, so it is immediate
rather than polled.

What counts as activity:

| Signal | How it is detected |
|---|---|
| a file written, created or removed | the working tree is watched, minus `.git`, dependency, build and log directories |
| `git add` | the worktree's index was rewritten *and* its staged count went up |
| `git commit` | the worktree's own HEAD reflog or `COMMIT_EDITMSG` was written |
| `git push` | the remote-tracking ref for the worktree's branch moved |

The index is only read as a `git add` when staging actually grew, because git
rewrites the index on plain reads too — a bare `git status` refreshes the stat
information it caches. Commits are attributed through per-worktree files, and
pushes through the branch name, since remote-tracking refs are shared by every
worktree in the repository.

`f` hides every worktree that is not working. What stays on screen:

- **worktrees with an agent in their pane** — claude or codex, whether it is
  executing tools or waiting at its prompt. A session holding a question is the
  tile you most need to find, so it is never hidden. A state file an agent left
  behind does not count; the process has to actually be there.
- **worktrees running work** — a test run, a build, a linter. Something merely
  left running in the pane does not count: a `watch` loop, a log tail or an
  editor would otherwise pin its tile on screen for ever, which is the one thing
  the filter exists to prevent. The tile still shows what is running, dimmed
  rather than highlighted, so you can see why it is not counted. The list of
  commands that count as work is `workCommands` in `internal/agent`.
- **worktrees that changed recently** — see the linger window below.

The header shows how many tiles are hidden, and `f` again brings them back.

The filter uses a longer window than the flash: a tile flashes for five seconds
but stays on screen for 45. Tiles that appeared and vanished on the same
five-second timer would reflow the grid every time you paused typing, and a
worktree you are in the middle of editing would keep dropping out from under the
cursor. Both windows are one constant each, `Window` and `Linger` in
`internal/activity`.

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

## Study plan

arborist is not meant to replace bash, git, tmux or the GitHub CLI. It is meant
to teach them. Every key runs ordinary commands: `git`, `tmux`, `gh` and
`docker`, plus reads from `/proc` and inotify. This six-week plan has you do
each action by hand first, then press the key and check that arborist did the
same thing. The goal is that you can read any tile, flash or badge and name the
command behind it.

**Habit for the whole course:** before you press a key, say out loud which
command you think it will run. To check yourself, trace the commands arborist
actually starts:

```bash
strace -f -e trace=execve -o /tmp/arborist.trace ./arborist
grep -oE 'execve\("[^"]+", \[[^]]*' /tmp/arborist.trace | less
```

### Phase 0: Groundwork (2–3 days)

| Topic | Practice | Where it shows up in arborist |
|---|---|---|
| Shell, `PATH`, symlinks | `echo $PATH`, `ln -sf`, `readlink -f $(which arborist)` | `make install` symlinks `~/bin/arborist` |
| Exit codes, `&&` vs `;` | `false && echo a; false ; echo b` | `c` runs *setup && start*; `C` runs *setup ; watch* |
| Environment variables | `export`, `env`, `$1` in scripts | `ARBORIST_WORKTREE`, `ARBORIST_BRANCH`, `ARBORIST_ISSUE`, `ARBORIST_PORT` |
| `make` | Read the `Makefile`; run `make -n dashboard` | `make go-build`, `make hooks` |

- [ ] Explain in one sentence why `c` uses `&&` but `C` uses `;`

### Phase 1: How git stores state (week 1)

This phase explains the Git Status column and the `⚡add`, `⚡commit` and
`⚡push` flashes.

| Concept | Commands | Where it shows up in arborist |
|---|---|---|
| Working tree, index, HEAD | `git status --porcelain` (learn the two-column `XY` codes), `ls -l --time-style=full-iso .git/index` | The clean/dirty column. A plain `git status` rewrites the index, so arborist counts it as a `git add` only when the staged count goes up |
| Ahead and behind | `git rev-list --left-right --count @{upstream}...HEAD`, `git push -u` | The `↑` / `↓` arrows on a tile |
| Reflog and refs | `git reflog`, `cat .git/HEAD`, `ls .git/refs/remotes/origin` | The `⚡commit` and `⚡push` flashes |

- [ ] Make one commit and push it. After each step, list the files under `.git/` that changed (`touch /tmp/marker` first, then `find .git -newer /tmp/marker`)
- [ ] Explain what `@{upstream}` and the `...` range mean

### Phase 2: Worktrees (weeks 1–2)

A worktree is a second checkout that shares one repository. Learn which files
it has for itself and which it shares with the others.

- **Lifecycle:** `git worktree add worktrees/foo -b foo`, `git worktree list --porcelain`, `git worktree remove`, `git worktree prune`
- **Layout:** `cat worktrees/foo/.git`. A worktree's `.git` is a file that points into `.git/worktrees/foo/`. Each worktree has its own `HEAD`, `index` and `COMMIT_EDITMSG`, and shares refs, objects and config with the others.
- **Ignoring:** `.git/info/exclude` compared with `.gitignore`

- [ ] Repeat what `c` does, entirely by hand:
    1. Create the worktree.
    2. Add `worktrees/` to `.git/info/exclude`.
    3. Symlink the `.env*` files.
    4. Copy `.claude/settings.local.json`.
- [ ] Press `c` on a new branch and compare the two results with `diff -r` and `ls -la`

### Phase 3: Branches, remotes and discovery (week 2)

This phase explains the `c` picker's branch list.

- **Refreshing and listing:** `git fetch --prune`, `git for-each-ref refs/heads refs/remotes`
- **Default branch:** `git symbolic-ref --short refs/remotes/origin/HEAD`
- **Local branch from a remote-only one:** `git switch --track origin/x`

- [ ] Build the picker's list yourself: branches without a worktree, local ones before remote-only ones. Use `for-each-ref`, `worktree list` and `comm` or `grep -v`.

### Phase 4: tmux (weeks 2–3)

Most of the dashboard's keys are tmux commands. Learn how panes are named:
`session:window.pane`, or a pane id such as `%5`.

| Key | tmux command to practice |
|---|---|
| `Enter` | `tmux select-window -t session:3` |
| `n` / `N` | `tmux new-window -c <path> -n <name>` |
| `i`, `j`/`k`, `e`/`t` | `tmux send-keys -t %5 -l -- 'text'`, `tmux send-keys -t %5 Enter` |
| Tile preview | `tmux capture-pane -p -e -t %5` |
| `d` | `tmux kill-window -t …` |
| Discovery | `tmux list-panes -a -F '#{pane_id} #{pane_current_command} #{pane_current_path}'` |

- [ ] Write a 10-line script that sends the same command to every pane whose path is under `worktrees/`. That reproduces `Space` + `a` + `i`.

### Phase 5: GitHub from the shell (week 3)

This phase explains the PR column, `o`, `I`, and creating a worktree from an
issue number.

- **Pull requests:** `gh pr view --json number,state,title,isDraft`, `--jq`, `gh pr view --web`
- **Issue branches:** `gh issue develop 123 --base staging`, `gh issue develop --list 123`
- **Labels:** `gh label list --search`, `gh label create`, `gh issue edit --add-label`

- [ ] Set `git config arborist.issueLabel status:in-development`, create a worktree from an issue number, and trace which `gh` calls ran
- [ ] Explain why a branch named `release-1.6.0` does *not* get the label (see [Issue labels](#issue-labels))

### Phase 6: Processes and /proc (weeks 3–4)

This phase explains the `f` filter, which decides whether a pane holds an agent
or running work.

- **Process trees:** `ps -o pid,ppid,comm`, `pstree -p`, and which process is in the terminal's foreground
- **Reading /proc:** `cat /proc/<pid>/comm`, `tr '\0' ' ' < /proc/<pid>/cmdline`, `cat /proc/<pid>/task/<pid>/children`

- [ ] Starting from the `pane_pid` tmux gives you, walk down the process tree until you reach `claude`, `rspec` or `go test`
- [ ] Read `workCommands` in `internal/agent/detect.go` and explain why `watch`, `tail -f` and `vim` are left out on purpose

### Phase 7: Filesystem events (week 4)

This phase explains why arborist updates the moment something changes instead
of polling.

- **Watching live:** run `inotifywait -m -r -e modify,create,delete,move .` in one pane while you edit, `git add` and `git commit` in another
- **Limits:** `cat /proc/sys/fs/inotify/max_user_watches`. Compare it with `maxWatchDirs = 4096` in `internal/watcher/watcher.go`.

- [ ] Say which inotify events cause each flash: `⚡file`, `⚡add`, `⚡commit`, `⚡push`
- [ ] Explain why a push is matched by branch name but a commit by per-worktree files (see Phase 2)

### Phase 8: Configuration and setup scripts (weeks 4–5)

arborist keeps all its settings in git config, under `arborist.*` keys.

- **git config as a key-value store:** `--get`, `--unset`, `--get-regexp`, `--file .gitmodules`, and local vs global scope
- **Your own setup:** write a `scripts/worktree-setup.sh` that takes the worktree path as `$1`. Then set `arborist.start` and `arborist.watch`.

- [ ] Break your setup script on purpose, for example with `exit 1`. Check that `c` doesn't start the agent but `C` still starts the watcher, and explain why.

### Phase 9: Ports, Docker and git hooks (week 5)

- **Dev ports:** `.worktree-ports` and `git config arborist.portBase 8080`. Check who is listening with `ss -ltnp`.
- **Docker:** `docker compose up`, then `docker ps -aq --filter label=com.docker.compose.project.working_dir=$PWD`. This is how `d` finds the containers to remove.
- **Git hooks:** `git config core.hooksPath githooks`. Read `githooks/pre-push` and `githooks/post-merge`.

- [ ] Name the four fields `pre-push` reads from stdin (`local_ref local_sha remote_ref remote_sha`) and what each one holds
- [ ] Start a compose stack in a worktree, delete the worktree with `d`, and confirm the containers and the port entry are gone

### Phase 10: Reading the source (week 6)

- **Go basics:** `exec.Command`, error handling, maps. Use `grep -rn 'exec.Command' internal` as your index into the code: every call is an external command you now know.
- **Bubble Tea's Elm architecture:** Model → Update → View, in `internal/tui/model.go`, `update.go` and `view.go`

- [ ] Read `internal/worktree/create_test.go` closely. It builds real repositories and remotes from scratch, so it also works as a git tutorial.

### Capstone

Write your own `worktree.sh` that supports `--watch`, using only shell, git,
tmux and gh. Then decide which parts are worth the dashboard and which are
better as a one-liner.

- [ ] Create the worktree
- [ ] Run setup
- [ ] Open the tmux window
- [ ] Export the `ARBORIST_*` variables
- [ ] Assign a port
- [ ] Label the issue

### References

- **Man pages:** `man git-worktree`, `man git-status`, `man git-rev-list`, `man githooks`, `man tmux` (the FORMATS and COMMANDS sections), `man proc`, `man inotify`, `gh help formatting`
- **Pro Git:** chapters 2, 3, 7 and 10 (git internals)
- **tmux 2** by Brian Hogan
- **The Linux Command Line** by William Shotts

## License

MIT
