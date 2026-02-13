#!/bin/bash

# Fetch Copilot and Claude comments from a GitHub PR
# Usage: ./fetch-pr-comments.sh [PR_NUMBER] [--json|--md]
# If PR_NUMBER is omitted, attempts to detect PR for current branch
# Default output: docs/PR_COMMENTS.md (markdown format)

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
OUTPUT_FORMAT="md"
OUTPUT_DIR="docs"

# Parse arguments
PR_NUMBER=""
while [[ $# -gt 0 ]]; do
    case $1 in
        --json)
            OUTPUT_FORMAT="json"
            shift
            ;;
        --md|--markdown)
            OUTPUT_FORMAT="md"
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [PR_NUMBER] [--json|--md]"
            echo ""
            echo "Fetches Copilot and Claude comments from a GitHub PR"
            echo ""
            echo "Arguments:"
            echo "  PR_NUMBER    The PR number to fetch comments from (optional)"
            echo "               If omitted, auto-detects PR for current branch"
            echo "  --json       Output as JSON (docs/PR_COMMENTS.json)"
            echo "  --md         Output as Markdown (docs/PR_COMMENTS.md) [default]"
            echo ""
            echo "AI authors detected:"
            echo "  - Copilot (copilot[bot], github-copilot[bot])"
            echo "  - Claude (claude[bot], anthropic[bot], or 'Claude' in name)"
            exit 0
            ;;
        *)
            if [[ -z "$PR_NUMBER" ]]; then
                PR_NUMBER="$1"
            fi
            shift
            ;;
    esac
done

# Check if gh CLI is available
if ! command -v gh &> /dev/null; then
    echo -e "${RED}❌ Error: GitHub CLI (gh) is not installed${NC}"
    echo "Install it from: https://cli.github.com/"
    exit 1
fi

# Check if authenticated
if ! gh auth status &> /dev/null; then
    echo -e "${RED}❌ Error: Not authenticated with GitHub CLI${NC}"
    echo "Run: gh auth login"
    exit 1
fi

# Auto-detect PR number if not provided
if [[ -z "$PR_NUMBER" ]]; then
    echo -e "${YELLOW}🔍 No PR number provided, detecting from current branch...${NC}"

    # Check if we're in a git repository
    if ! git rev-parse --git-dir &> /dev/null; then
        echo -e "${RED}❌ Error: Not in a git repository${NC}"
        exit 1
    fi

    # Get PR number using gh CLI
    PR_NUMBER=$(gh pr view --json number -q '.number' 2>/dev/null) || true

    if [[ -z "$PR_NUMBER" ]]; then
        echo -e "${RED}❌ Error: No PR found for current branch${NC}"
        echo "Usage: $0 [PR_NUMBER] [--json|--md]"
        echo "Either provide a PR number or ensure your branch has an open PR."
        exit 1
    fi

    echo -e "${GREEN}   ✓ Detected PR #${PR_NUMBER}${NC}"
fi

# Validate PR number format
if ! [[ "$PR_NUMBER" =~ ^[0-9]+$ ]]; then
    echo -e "${RED}❌ Error: PR number must be a positive integer${NC}"
    exit 1
fi

echo -e "${BLUE}🔍 Fetching comments from PR #${PR_NUMBER}...${NC}"

# Create temp files for collecting data
TEMP_DIR=$(mktemp -d)
trap "rm -rf $TEMP_DIR" EXIT

# Fetch PR info
echo -e "${YELLOW}   Fetching PR metadata...${NC}"
PR_INFO=$(gh pr view "$PR_NUMBER" --json title,url,author,createdAt,state 2>/dev/null) || {
    echo -e "${RED}❌ Error: Could not fetch PR #${PR_NUMBER}${NC}"
    echo "Make sure the PR exists and you have access to it."
    exit 1
}

PR_TITLE=$(echo "$PR_INFO" | jq -r '.title')
PR_URL=$(echo "$PR_INFO" | jq -r '.url')
PR_AUTHOR=$(echo "$PR_INFO" | jq -r '.author.login')
PR_STATE=$(echo "$PR_INFO" | jq -r '.state')

echo -e "${GREEN}   ✓ Found: ${PR_TITLE}${NC}"

# Function to check if a user is an AI bot
is_ai_author() {
    local author="$1"
    local author_lower=$(echo "$author" | tr '[:upper:]' '[:lower:]')

    # Check for known AI bot patterns
    case "$author_lower" in
        *copilot*|*claude*|*anthropic*|*openai*|*gpt*)
            return 0
            ;;
    esac
    return 1
}

# Fetch review comments (inline code comments)
echo -e "${YELLOW}   Fetching review comments...${NC}"
REVIEW_COMMENTS=$(gh api "repos/{owner}/{repo}/pulls/${PR_NUMBER}/comments" --paginate 2>/dev/null || echo "[]")

# Fetch issue comments (general PR comments)
echo -e "${YELLOW}   Fetching issue comments...${NC}"
ISSUE_COMMENTS=$(gh api "repos/{owner}/{repo}/issues/${PR_NUMBER}/comments" --paginate 2>/dev/null || echo "[]")

# Fetch review summaries
echo -e "${YELLOW}   Fetching review summaries...${NC}"
REVIEWS=$(gh api "repos/{owner}/{repo}/pulls/${PR_NUMBER}/reviews" --paginate 2>/dev/null || echo "[]")

# Filter for AI comments using jq
echo -e "${YELLOW}   Filtering for AI comments...${NC}"

# Create jq filter for AI authors
AI_FILTER='.user.login | ascii_downcase | test("copilot|claude|anthropic|openai|gpt")'

# Filter review comments
AI_REVIEW_COMMENTS=$(echo "$REVIEW_COMMENTS" | jq "[.[] | select($AI_FILTER)]")

# Filter issue comments
AI_ISSUE_COMMENTS=$(echo "$ISSUE_COMMENTS" | jq "[.[] | select($AI_FILTER)]")

# Filter reviews
AI_REVIEWS=$(echo "$REVIEWS" | jq "[.[] | select($AI_FILTER)]")

# Count results
REVIEW_COUNT=$(echo "$AI_REVIEW_COMMENTS" | jq 'length')
ISSUE_COUNT=$(echo "$AI_ISSUE_COMMENTS" | jq 'length')
REVIEWS_COUNT=$(echo "$AI_REVIEWS" | jq 'length')
TOTAL_COUNT=$((REVIEW_COUNT + ISSUE_COUNT + REVIEWS_COUNT))

echo -e "${GREEN}   ✓ Found ${TOTAL_COUNT} AI comments:${NC}"
echo -e "     - Review comments (inline): ${REVIEW_COUNT}"
echo -e "     - Issue comments (general): ${ISSUE_COUNT}"
echo -e "     - Review summaries: ${REVIEWS_COUNT}"

# Ensure output directory exists
mkdir -p "$OUTPUT_DIR"

# Generate output
if [[ "$OUTPUT_FORMAT" == "json" ]]; then
    OUTPUT_FILE="${OUTPUT_DIR}/PR_COMMENTS.json"

    # Create structured JSON output
    jq -n \
        --arg pr_number "$PR_NUMBER" \
        --arg pr_title "$PR_TITLE" \
        --arg pr_url "$PR_URL" \
        --arg pr_author "$PR_AUTHOR" \
        --arg pr_state "$PR_STATE" \
        --arg fetched_at "$(date -Iseconds)" \
        --argjson review_comments "$AI_REVIEW_COMMENTS" \
        --argjson issue_comments "$AI_ISSUE_COMMENTS" \
        --argjson reviews "$AI_REVIEWS" \
        '{
            metadata: {
                pr_number: ($pr_number | tonumber),
                pr_title: $pr_title,
                pr_url: $pr_url,
                pr_author: $pr_author,
                pr_state: $pr_state,
                fetched_at: $fetched_at,
                total_ai_comments: (($review_comments | length) + ($issue_comments | length) + ($reviews | length))
            },
            review_comments: [
                $review_comments[] | {
                    id: .id,
                    author: .user.login,
                    path: .path,
                    line: .line,
                    body: .body,
                    created_at: .created_at,
                    updated_at: .updated_at,
                    url: .html_url
                }
            ],
            issue_comments: [
                $issue_comments[] | {
                    id: .id,
                    author: .user.login,
                    body: .body,
                    created_at: .created_at,
                    updated_at: .updated_at,
                    url: .html_url
                }
            ],
            reviews: [
                $reviews[] | {
                    id: .id,
                    author: .user.login,
                    state: .state,
                    body: .body,
                    submitted_at: .submitted_at,
                    url: .html_url
                }
            ]
        }' > "$OUTPUT_FILE"

    echo -e "${GREEN}✅ Saved to ${OUTPUT_FILE}${NC}"

else
    OUTPUT_FILE="${OUTPUT_DIR}/PR_COMMENTS.md"

    # Generate Markdown output
    {
        echo "# AI Comments from PR #${PR_NUMBER}"
        echo ""
        echo "**PR:** [${PR_TITLE}](${PR_URL})"
        echo "**Author:** ${PR_AUTHOR}"
        echo "**State:** ${PR_STATE}"
        echo "**Fetched:** $(date '+%Y-%m-%d %H:%M:%S')"
        echo "**Total AI Comments:** ${TOTAL_COUNT}"
        echo ""
        echo "---"
        echo ""

        # Review summaries
        if [[ "$REVIEWS_COUNT" -gt 0 ]]; then
            echo "## Review Summaries"
            echo ""
            echo "$AI_REVIEWS" | jq -r '.[] | "### Review by \(.user.login)\n**State:** \(.state)\n**Submitted:** \(.submitted_at)\n\n\(.body // "_No body_")\n\n---\n"'
            echo ""
        fi

        # Issue comments (general PR comments)
        if [[ "$ISSUE_COUNT" -gt 0 ]]; then
            echo "## General Comments"
            echo ""
            echo "$AI_ISSUE_COMMENTS" | jq -r '.[] | "### Comment by \(.user.login)\n**Date:** \(.created_at)\n**URL:** \(.html_url)\n\n\(.body)\n\n---\n"'
            echo ""
        fi

        # Review comments (inline code comments)
        if [[ "$REVIEW_COUNT" -gt 0 ]]; then
            echo "## Inline Code Comments"
            echo ""
            echo "$AI_REVIEW_COMMENTS" | jq -r '.[] | "### \(.path):\(.line // "N/A")\n**Author:** \(.user.login)\n**Date:** \(.created_at)\n**URL:** \(.html_url)\n\n```\n\(.diff_hunk // "")\n```\n\n\(.body)\n\n---\n"'
        fi

        if [[ "$TOTAL_COUNT" -eq 0 ]]; then
            echo "## No AI Comments Found"
            echo ""
            echo "No comments from Copilot, Claude, or other AI assistants were found on this PR."
            echo ""
            echo "AI authors detected:"
            echo "- copilot[bot]"
            echo "- github-copilot[bot]"
            echo "- claude[bot]"
            echo "- anthropic[bot]"
            echo "- Usernames containing: copilot, claude, anthropic, openai, gpt"
        fi

    } > "$OUTPUT_FILE"

    echo -e "${GREEN}✅ Saved to ${OUTPUT_FILE}${NC}"
fi

# Show summary
echo ""
echo -e "${BLUE}📋 Summary:${NC}"
echo "   Output: ${OUTPUT_FILE}"
echo "   Format: ${OUTPUT_FORMAT^^}"
echo "   AI Comments: ${TOTAL_COUNT}"

# Copy to clipboard if available (detect display server type)
if [[ "$OSTYPE" == darwin* ]]; then
    # macOS
    if command -v pbcopy &> /dev/null; then
        cat "$OUTPUT_FILE" | pbcopy
        echo -e "${GREEN}   📋 Copied to clipboard${NC}"
    fi
elif [[ -n "$WAYLAND_DISPLAY" ]]; then
    # Wayland session
    if command -v wl-copy &> /dev/null; then
        cat "$OUTPUT_FILE" | wl-copy
        echo -e "${GREEN}   📋 Copied to clipboard${NC}"
    fi
elif [[ -n "$DISPLAY" ]]; then
    # X11 session
    if command -v xclip &> /dev/null; then
        cat "$OUTPUT_FILE" | xclip -selection clipboard
        echo -e "${GREEN}   📋 Copied to clipboard${NC}"
    fi
fi

# Launch Claude interactively with instructions
echo ""
echo -e "${BLUE}🤖 Launching Claude to process comments...${NC}"
exec claude "
1. Read docs/PR_COMMENTS.md
2. Assess each comment, and using 'gh pr':
  a. If comment valid and not addressed, add to task list
  b. If comment not valid or already addressed AND NOT replied to in PR, reply to the comment in the PR
  c. If comment already addressed or resolved and already replied to in PR, do nothing
3. Implement task list
"
