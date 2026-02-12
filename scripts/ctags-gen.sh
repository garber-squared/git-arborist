#!/usr/bin/env bash
# Generate ctags for the project.
#
# Usage:
#   scripts/ctags-gen.sh          # one-shot generation
#   scripts/ctags-gen.sh --watch  # regenerate on file changes (uses inotifywait)

set -euo pipefail
PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TAGS_FILE="$PROJECT_ROOT/tags"
LOCK="/tmp/ctags-gen-sideby.lock"

generate() {
  # Avoid parallel runs stomping on each other
  if [ -f "$LOCK" ]; then
    return
  fi
  trap 'rm -f "$LOCK"' EXIT
  touch "$LOCK"

  # Write to temp file then atomic-move — editors never see a half-written tags file
  local tmp
  tmp=$(mktemp "$TAGS_FILE.XXXXXX")
  if ctags -f "$tmp" -R "$PROJECT_ROOT/src" "$PROJECT_ROOT/supabase/functions" 2>/dev/null; then
    mv "$tmp" "$TAGS_FILE"
    echo "[ctags] $(date +%H:%M:%S) — $(wc -l < "$TAGS_FILE") tags"
  else
    rm -f "$tmp"
    echo "[ctags] generation failed" >&2
  fi

  rm -f "$LOCK"
  trap - EXIT
}

if [ "${1:-}" = "--watch" ]; then
  if ! command -v inotifywait &>/dev/null; then
    echo "inotifywait not found. Install inotify-tools." >&2
    exit 1
  fi

  echo "[ctags] Watching src/ and supabase/functions/ for changes..."
  generate

  inotifywait -m -r \
    -e modify,create,delete,move \
    --include '\.(ts|tsx|js|jsx|css)$' \
    "$PROJECT_ROOT/src" "$PROJECT_ROOT/supabase/functions" |
  while read -r _dir _event _file; do
    # Debounce: sleep briefly then drain any queued events
    sleep 1
    while read -r -t 0.1 _ _ _; do :; done
    generate
  done
else
  generate
fi
