#!/bin/bash

# Fetch a GitHub issue and launch Claude to analyze and implement a solution
# Usage: ./fetch-issue.sh [ISSUE_NUMBER]
# If no issue number is provided, extracts it from branch name (e.g., "696-feature" -> 696)

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
OUTPUT_DIR="docs"

# Parse arguments
ISSUE_NUMBER="$1"

# Show help
if [[ "$1" == "-h" ]] || [[ "$1" == "--help" ]]; then
    echo "Usage: $0 [ISSUE_NUMBER]"
    echo ""
    echo "Fetches a GitHub issue and launches Claude to analyze and implement a solution"
    echo ""
    echo "Arguments:"
    echo "  ISSUE_NUMBER    The issue number to fetch (optional if branch name is prefixed with issue number)"
    echo ""
    echo "If no issue number is provided, the script will attempt to extract it from"
    echo "the current branch name (e.g., '696-feature-name' -> issue #696)"
    echo ""
    echo "Output:"
    echo "  Saves issue details to docs/ISSUE_<NUMBER>.md"
    echo "  Launches Claude with instructions to analyze and implement"
    exit 0
fi

# If no issue number provided, try to extract from branch name
if [[ -z "$ISSUE_NUMBER" ]]; then
    CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")

    # Extract numeric prefix from branch name (e.g., "696-feature-name" -> "696")
    if [[ "$CURRENT_BRANCH" =~ ^([0-9]+)- ]]; then
        ISSUE_NUMBER="${BASH_REMATCH[1]}"
        echo -e "${YELLOW}ℹ️  No issue number provided, using #${ISSUE_NUMBER} from branch '${CURRENT_BRANCH}'${NC}"
    else
        echo -e "${RED}❌ Error: Issue number is required${NC}"
        echo ""
        echo "Usage: $0 <ISSUE_NUMBER>"
        echo ""
        echo "Tip: You can also checkout a branch prefixed with the issue number"
        echo "     (e.g., '696-feature-name') and run without arguments."
        exit 1
    fi
fi

OUTPUT_FILE="${OUTPUT_DIR}/ISSUE_$ISSUE_NUMBER.md"

# Validate issue number

if ! [[ "$ISSUE_NUMBER" =~ ^[0-9]+$ ]]; then
    echo -e "${RED}❌ Error: Issue number must be a positive integer${NC}"
    exit 1
fi

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

# Check if we're in a git repository
if ! git rev-parse --git-dir &> /dev/null; then
    echo -e "${RED}❌ Error: Not in a git repository${NC}"
    exit 1
fi

echo -e "${BLUE}🔍 Fetching issue #${ISSUE_NUMBER}...${NC}"

# Fetch issue details
ISSUE_JSON=$(gh issue view "$ISSUE_NUMBER" --json number,title,body,state,author,labels,assignees,createdAt,url 2>/dev/null) || {
    echo -e "${RED}❌ Error: Could not fetch issue #${ISSUE_NUMBER}${NC}"
    echo "Make sure the issue exists and you have access to it."
    exit 1
}

# Extract fields
ISSUE_TITLE=$(echo "$ISSUE_JSON" | jq -r '.title')
ISSUE_BODY=$(echo "$ISSUE_JSON" | jq -r '.body // "_No description provided_"')
ISSUE_STATE=$(echo "$ISSUE_JSON" | jq -r '.state')
ISSUE_AUTHOR=$(echo "$ISSUE_JSON" | jq -r '.author.login')
ISSUE_URL=$(echo "$ISSUE_JSON" | jq -r '.url')
ISSUE_CREATED=$(echo "$ISSUE_JSON" | jq -r '.createdAt')
ISSUE_LABELS=$(echo "$ISSUE_JSON" | jq -r '[.labels[].name] | join(", ") // "None"')
ISSUE_ASSIGNEES=$(echo "$ISSUE_JSON" | jq -r '[.assignees[].login] | join(", ") // "Unassigned"')

echo -e "${GREEN}   ✓ Found: ${ISSUE_TITLE}${NC}"

# Fetch issue comments
echo -e "${YELLOW}   Fetching comments...${NC}"
COMMENTS_JSON=$(gh api "repos/{owner}/{repo}/issues/${ISSUE_NUMBER}/comments" --paginate 2>/dev/null || echo "[]")
COMMENT_COUNT=$(echo "$COMMENTS_JSON" | jq 'length')
echo -e "${GREEN}   ✓ Found ${COMMENT_COUNT} comments${NC}"

# Ensure output directory exists
mkdir -p "$OUTPUT_DIR"

# Generate markdown output
echo -e "${YELLOW}   Writing to ${OUTPUT_FILE}...${NC}"

{
    echo "# Issue #${ISSUE_NUMBER}: ${ISSUE_TITLE}"
    echo ""
    echo "**URL:** ${ISSUE_URL}"
    echo "**State:** ${ISSUE_STATE}"
    echo "**Author:** ${ISSUE_AUTHOR}"
    echo "**Created:** ${ISSUE_CREATED}"
    echo "**Labels:** ${ISSUE_LABELS}"
    echo "**Assignees:** ${ISSUE_ASSIGNEES}"
    echo "**Fetched:** $(date '+%Y-%m-%d %H:%M:%S')"
    echo ""
    echo "---"
    echo ""
    echo "## Description"
    echo ""
    echo "$ISSUE_BODY"
    echo ""

    if [[ "$COMMENT_COUNT" -gt 0 ]]; then
        echo "---"
        echo ""
        echo "## Comments (${COMMENT_COUNT})"
        echo ""
        echo "$COMMENTS_JSON" | jq -r '.[] | "### Comment by \(.user.login)\n**Date:** \(.created_at)\n\n\(.body)\n\n---\n"'
    fi

    echo ""
    echo "---"
    echo ""
    echo "## Instructions for Claude"
    echo ""
    echo "1. **View the issue:** \`gh issue view ${ISSUE_NUMBER}\`"
    echo "2. **Analyze the codebase** to understand the relevant components"
    echo "3. **Generate a task list** breaking down the work required"
    echo "4. **Implement a solution** if the issue requires code changes"

} > "$OUTPUT_FILE"

echo -e "${GREEN}✅ Saved to ${OUTPUT_FILE}${NC}"

# Show summary
echo ""
echo -e "${BLUE}📋 Summary:${NC}"
echo "   Issue: #${ISSUE_NUMBER} - ${ISSUE_TITLE}"
echo "   State: ${ISSUE_STATE}"
echo "   Comments: ${COMMENT_COUNT}"
echo "   Output: ${OUTPUT_FILE}"

# Copy to clipboard if available
if [[ "$OSTYPE" == darwin* ]]; then
    if command -v pbcopy &> /dev/null; then
        cat "$OUTPUT_FILE" | pbcopy
        echo -e "${GREEN}   📋 Copied to clipboard${NC}"
    fi
elif [[ -n "$WAYLAND_DISPLAY" ]]; then
    if command -v wl-copy &> /dev/null; then
        cat "$OUTPUT_FILE" | wl-copy
        echo -e "${GREEN}   📋 Copied to clipboard${NC}"
    fi
elif [[ -n "$DISPLAY" ]]; then
    if command -v xclip &> /dev/null; then
        cat "$OUTPUT_FILE" | xclip -selection clipboard
        echo -e "${GREEN}   📋 Copied to clipboard${NC}"
    fi
fi

# Launch Claude with instructions
echo ""
echo -e "${BLUE}🤖 Launching Claude to process issue...${NC}"
exec claude "
1. Read docs/ISSUE_$ISSUE_NUMBER.md to understand the issue
2. Run 'make ctags-report' and refer to docs/ctags-report.md before analyzing the codebase to understand the relevant components and architecture
3. Generate a task list breaking down the work required to address this issue, and save to docs/ISSUE_${ISSUE_NUMBER}_TASKS.md
4. Implement a solution if code changes are required

Focus on understanding the issue context and producing a well-structured implementation plan before writing any code.
"
