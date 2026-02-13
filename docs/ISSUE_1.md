# Issue #1: TUI Dashboard for Git Worktrees in TMUX (Bubble Tea)

**URL:** https://github.com/garber-squared/git-arborist/issues/1
**State:** OPEN
**Author:** clockworkpc
**Created:** 2026-02-12T13:18:49Z
**Labels:** enhancement
**Assignees:** clockworkpc
**Fetched:** 2026-02-12 08:26:59

---

## Description

Build a repo-local terminal dashboard (TUI) using **Bubble Tea** to monitor and manage git worktrees where each worktree runs in its own TMUX window.

The dashboard provides a real-time operational view of:

* Branch name
* Agent state (Claude/Codex)
* Associated PR (if open)
* Git status

Updates must be **event-driven**, not polling-based.

This tool acts as a control plane for multi-branch AI-assisted development workflows.

---

## Context

Current workflow:

* Multiple `git worktrees`
* Each worktree mapped to a dedicated TMUX window
* Claude/Codex processes running per worktree
* PRs opened per branch

Pain points:

* No centralized visibility into branch state
* No reliable way to see if an agent is awaiting input
* No quick overview of git cleanliness across worktrees
* No at-a-glance PR visibility
* Manual inspection required per window

The goal is to reduce context switching and operational overhead.

---

## Scope & Constraints

### Scope

* Repo-local only (current repo)
* Single repository only
* Worktrees within that repository only

### Explicitly Out of Scope

* Multi-repo aggregation
* Global dashboard spanning multiple repositories
* Heuristic parsing of terminal output for agent state

---

## Architecture Overview

### 1. Worktree Discovery

Use:

```bash
git worktree list --porcelain
```

Extract:

* Worktree path
* Branch name

Each worktree becomes a dashboard row.

---

### 2. Agent State Detection (Source of Truth)

#### Design Decision

Agent state must be determined via **explicit state files**, not tmux output parsing.

Each worktree must contain:

```
.sideby/agent/state.json
```

Optional:

```
.sideby/agent/events.log
.sideby/agent/pid
```

The TUI will watch these files using `fsnotify` for event-driven updates.

#### Example State Schema

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

#### Valid States

* `starting`
* `running`
* `awaiting_input`
* `idle`
* `error`
* `stopped`

#### State Transitions

Agent wrappers (shell scripts or Go binaries) must:

* Write state on start
* Update state on input wait
* Update state on completion
* Update state on error
* Remove or mark stopped on exit

The dashboard is read-only and does not infer state.

---

### 3. Event-Driven Update Model

The dashboard must be event-driven wherever possible.

#### Agent State

* Watch `.sideby/agent/state.json` via `fsnotify`
* Re-render affected row on change

#### Git State

Watch relevant git/worktree files:

* `.git/HEAD`
* `.git/index`
* `.git/refs/*`
* Worktree-specific `.git` file

On change:

* Recompute git status
* Update UI

Fallback:

* Manual refresh hotkey (`r`)

#### PR State

PR state is remote and cannot be purely event-driven.

Strategy:

* Refresh PR status when:

  * Branch changes
  * Commit changes detected
  * Manual refresh
* Use `gh pr view` or `gh pr status`

Manual refresh remains available.

---

## UI Specification

### Layout

```
┌────────────────────────────────────────────────────────────┐
│ Worktree Dashboard (repo-local)                           │
├────────────┬──────────────┬──────────────┬───────────────┤
│ Branch     │ Agent        │ PR           │ Git Status    │
├────────────┼──────────────┼──────────────┼───────────────┤
│ feature/x  │ awaiting     │ #123 (open)  │ clean ↑1      │
│ fix/y      │ running      │ —            │ dirty         │
│ exp/z      │ idle         │ #130 (draft) │ clean         │
└────────────┴──────────────┴──────────────┴───────────────┘
```

### Git Status Indicators

Display:

* Clean / Dirty
* Ahead/behind counts
* Staged changes
* Untracked files (indicator only)

### Color Semantics (Phase 2)

* Green → clean / idle
* Yellow → awaiting input
* Blue → running
* Red → error / dirty

---

## Keybindings

* `j` / `k` → navigate rows
* `enter` → jump to associated TMUX window
* `o` → open PR in browser
* `g` → show detailed git status
* `r` → manual refresh
* `q` → quit

---

## Data Model (Go)

```go
type Worktree struct {
    Path       string
    Branch     string
    GitStatus  GitStatus
    PR         *PullRequest
    AgentState AgentState
}

type AgentState struct {
    Agent     string
    State     string
    Detail    string
    UpdatedAt time.Time
    TMUX      TMUXRef
}

type TMUXRef struct {
    Session string
    Window  int
    Pane    int
}
```

---

## Repo-Local Design

The dashboard is strictly repo-local:

* Launched from repository root
* Only shows worktrees from that repo
* Config/state stored under:

```
.sideby/dashboard/
```

No use of global `~/.config`.

---

## Implementation Plan

### Phase 1 (MVP)

* Discover worktrees
* Display branch names
* Read `.sideby/agent/state.json`
* Show git clean/dirty + ahead/behind
* Show PR if exists
* Manual refresh
* TMUX jump

### Phase 2

* Full fsnotify integration
* Colorized statuses
* Sorting (awaiting input first, dirty first)
* Filtering
* Detail panel view
* Optional event log viewer

---

## Acceptance Criteria

* Launching from repo root shows all worktrees
* Agent state updates instantly when `state.json` changes
* Git status updates on file changes (HEAD/index/ref updates)
* PR information displays correctly
* TMUX jump works reliably
* No polling loop required for agent state

---

## Non-Goals

* Multi-repo support
* Parsing TMUX output for state inference
* Managing agents directly from dashboard (read-only visibility tool)

---

## Why This Matters

This dashboard becomes an operational command center for parallel AI-assisted development across git worktrees.

It:

* Reduces context-switching cost
* Makes agent blocking states visible
* Surfaces git/PR hygiene issues immediately
* Scales cleanly with additional worktrees

This is infrastructure for structured AI-driven branch workflows.


---

## Instructions for Claude

1. **View the issue:** `gh issue view 1`
2. **Analyze the codebase** to understand the relevant components
3. **Generate a task list** breaking down the work required
4. **Implement a solution** if the issue requires code changes
