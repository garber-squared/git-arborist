#!/bin/bash

# Create a selection menu from `git branch` output using read and select

git fetch

echo "Select a branch to create a worktree for:"
select BRANCH in $(git branch --all --format='%(refname:short)'); do
    if [ -n "$BRANCH" ]; then
        break
    else
        echo "Invalid selection. Please try again."
    fi
done

if git show-ref --verify --quiet "refs/heads/$BRANCH"; then
    echo "Creating new worktree for local branch $BRANCH..."
    git worktree add "../$BRANCH" "$BRANCH"
    WORKTREE_DIR="../$BRANCH"
    echo "Worktree created at $WORKTREE_DIR."
elif git show-ref --verify --quiet "refs/remotes/$BRANCH"; then
    LOCAL_BRANCH="${BRANCH#*/}"
    echo "Creating new worktree for remote branch $BRANCH (local: $LOCAL_BRANCH)..."
    git worktree add -b "$LOCAL_BRANCH" "../$LOCAL_BRANCH" "$BRANCH"
    WORKTREE_DIR="../$LOCAL_BRANCH"
    echo "Worktree created at $WORKTREE_DIR."
else
    echo "Branch not found locally or on remotes: $BRANCH"
    exit 1
fi

WORKTREE_ABS=$(cd "$WORKTREE_DIR" && pwd)
tmux new-window -c "$WORKTREE_ABS" "cd '$WORKTREE_ABS' && cp ../my-sideby-ai/.env . && npm install && make fetch-issue; exec $SHELL"
