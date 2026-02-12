#!/usr/bin/env bash
# List git worktrees with paths relative to the current directory's parent

parent_dir="$(dirname "$(pwd)")"

echo ""
git worktree list | sed "s|${parent_dir}/|../|" | grep -v "^../my-sideby-ai " | sed 's/ \[.*\]//'
echo ""
