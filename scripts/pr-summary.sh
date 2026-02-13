#!/bin/bash

# Generate PR summary using Claude
# Usage: ./pr-summary.sh [git-diff-args]
# Default: current branch vs staging

set -e

DIFF_FILE_NAME_ONLY="diff-name-only.txt"
DIFF_FILE="diff.txt"
OUTPUT_FILE="docs/PR_SUMMARY.md"

# Add --model flag to choose either "claude" or "codex" command
# Default to "claude"

MODEL="claude"
DIFF_ARGS=()
BRANCH_ARG=""

while [ "$#" -gt 0 ]; do
    case "$1" in
        --model)
            if [ -z "$2" ]; then
                echo "❌ Error: --model requires a value (claude|codex)"
                exit 1
            fi
            MODEL="$2"
            shift 2
            ;;
        --model=*)
            MODEL="${1#*=}"
            shift
            ;;
        --branch)
            if [ -z "$2" ]; then
                echo "❌ Error: --branch requires a value"
                exit 1
            fi
            BRANCH_ARG="$2"
            shift 2
            ;;
        --branch=*)
            BRANCH_ARG="${1#*=}"
            shift
            ;;
        *)
            DIFF_ARGS+=("$1")
            shift
            ;;
    esac
done

if [ "$MODEL" != "claude" ] && [ "$MODEL" != "codex" ]; then
    echo "❌ Error: Invalid --model '$MODEL'. Use 'claude' or 'codex'."
    exit 1
fi

MODEL_CMD="$MODEL"

EXCLUDES=(
    ":(exclude)diff.txt"
    ":(exclude)diff-name-only.txt"
    ":(exclude)docs/PR_SUMMARY.md"
)


# Generate diff
# DIFF_TARGET:
# If PR already open for this branch, diff against its base branch
# ELSE IF argument provided, diff against argument
# ELSE diff against staging



PR_BASE_BRANCH=$(gh pr view --json baseRefName --template '{{.baseRefName}}' 2>/dev/null || echo "")

if [ -n "$PR_BASE_BRANCH" ]; then
    echo "🔍 Detected open PR, generating diff against base branch '$PR_BASE_BRANCH'..."
    git diff "$PR_BASE_BRANCH" --name-only -- . "${EXCLUDES[@]}" > "$DIFF_FILE_NAME_ONLY"
    git diff "$PR_BASE_BRANCH" -- . "${EXCLUDES[@]}" > "$DIFF_FILE"
elif [ -n "$BRANCH_ARG" ]; then
    echo "📊 Generating diff against $BRANCH_ARG..."
    git diff "$BRANCH_ARG" --name-only -- . "${EXCLUDES[@]}" > "$DIFF_FILE_NAME_ONLY"
    git diff "$BRANCH_ARG" -- . "${EXCLUDES[@]}" > "$DIFF_FILE"
elif [ "${#DIFF_ARGS[@]}" -gt 0 ]; then
    echo "📊 Generating diff: ${DIFF_ARGS[*]}..."
    git diff "${DIFF_ARGS[@]}" --name-only -- . "${EXCLUDES[@]}" > "$DIFF_FILE_NAME_ONLY"
    git diff "${DIFF_ARGS[@]}" -- . "${EXCLUDES[@]}" > "$DIFF_FILE"
else
    echo "📊 Generating diff against staging..."
    git diff staging --name-only -- . "${EXCLUDES[@]}" > "$DIFF_FILE_NAME_ONLY"
    git diff staging -- . "${EXCLUDES[@]}" > "$DIFF_FILE"
fi

# Check if diff is empty
if [ ! -s "$DIFF_FILE" ]; then
    echo "❌ Error: No changes found in diff"
    rm -f "$DIFF_FILE"
    exit 1
fi

# Show diff stats
LINES_CHANGED=$(wc -l < "$DIFF_FILE")
echo "   Found $LINES_CHANGED lines of diff"

# Ensure docs directory exists
mkdir -p docs

# Generate PR summary using Claude or Codex
# Note: We pipe the diff via stdin to avoid "Argument list too long" errors with large diffs
echo "🤖 Generating PR summary with $MODEL..."
cat "$DIFF_FILE_NAME_ONLY" | "$MODEL_CMD" -p "Generate a concise PR summary. A list of changed filenames is provided via stdin:

If this branch is prefixed with a number, e.g. 123-feature-name:
  1. Use that number as the Issue number.
  2. Using 'gh issue view', read the Issue from the GitHub repository for context.
  3. Ascertain whether the code changes have satisfied the Issue requirements.

In the diff file, a git diff is provided showing the code changes made in this branch, read only source code files relevant to the Issue.
Do not read test files, documentation files, or configuration files unless they are essential to understanding the code changes.
Infer the intent of migration files from their filenames and the context of the Issue.

If no number prefix is found consider only the diff against the target branch.

- A brief title (one line)
- Summary of changes (2-3 sentences)
- Key changes as bullet points
- Any breaking changes or migration notes if applicable

Format as markdown suitable for a GitHub PR description.
If there is a PR for this branch already, update the description with this summary using 'gh pr edit <pr-number> --body-file \$OUTPUT_FILE'." > "$OUTPUT_FILE"
# Cleanup diff file
rm -f "$DIFF_FILE"

# Display the generated summary
echo ""
bat "$OUTPUT_FILE"


# Copy to clipboard, use xclip or wl-copy based on environment
# Detect if running on Wayland or X11
if [ -n "$WAYLAND_DISPLAY" ]; then
    # Wayland
    CLIP_CMD="wl-copy"
else
    # Assume X11
    CLIP_CMD="xclip -selection clipboard"
fi

cat "$OUTPUT_FILE" | $CLIP_CMD

echo ""
echo "✅ Saved to $OUTPUT_FILE and copied to clipboard"
