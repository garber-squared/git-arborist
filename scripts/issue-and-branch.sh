#!/bin/bash

# Create a GitHub issue and checkout a corresponding feature branch
# Usage: ./issue-and-branch.sh [--title "Issue Title"] [--body "Issue body"]
# If no arguments provided, opens interactive issue creation

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Parse arguments
TITLE=""
BODY=""

while [[ $# -gt 0 ]]; do
    case $1 in
        -t|--title)
            TITLE="$2"
            shift 2
            ;;
        -b|--body)
            BODY="$2"
            shift 2
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Create a GitHub issue and checkout a corresponding feature branch"
            echo ""
            echo "Options:"
            echo "  -t, --title TITLE    Issue title (required for non-interactive mode)"
            echo "  -b, --body BODY      Issue body (optional)"
            echo "  -h, --help           Show this help message"
            echo ""
            echo "If no options are provided, opens interactive issue creation via 'gh issue create'"
            echo ""
            echo "Examples:"
            echo "  $0                                    # Interactive mode"
            echo "  $0 -t \"Fix login bug\"                 # Non-interactive with title only"
            echo "  $0 -t \"Add feature\" -b \"Details...\"   # Non-interactive with title and body"
            exit 0
            ;;
        *)
            echo -e "${RED}❌ Unknown option: $1${NC}"
            echo "Use --help for usage information"
            exit 1
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

# Check if we're in a git repository
if ! git rev-parse --git-dir &> /dev/null; then
    echo -e "${RED}❌ Error: Not in a git repository${NC}"
    exit 1
fi

# Check for uncommitted changes
if ! git diff-index --quiet HEAD -- 2>/dev/null; then
    echo -e "${YELLOW}⚠️  Warning: You have uncommitted changes${NC}"
    read -p "Continue anyway? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo -e "${YELLOW}Aborted.${NC}"
        exit 0
    fi
fi

echo -e "${BLUE}📝 Creating GitHub issue...${NC}"

# Create the issue (interactive or non-interactive)
if [[ -n "$TITLE" ]]; then
    # Non-interactive mode - safe to capture output directly
    if [[ -n "$BODY" ]]; then
        ISSUE_URL=$(gh issue create --title "$TITLE" --body "$BODY")
    else
        ISSUE_URL=$(gh issue create --title "$TITLE" --body "")
    fi

    if [[ $? -ne 0 ]] || [[ ! "$ISSUE_URL" =~ /issues/[0-9]+ ]]; then
        echo -e "${RED}❌ Error: Failed to create issue${NC}"
        echo "$ISSUE_URL"
        exit 1
    fi
else
    # Interactive mode - run gh directly so user can interact with prompts
    # Use a temp file to capture the URL since we can't use $() without breaking interactivity
    TEMP_FILE=$(mktemp)
    trap "rm -f $TEMP_FILE" EXIT

    # gh issue create prints the URL to stdout as its last line
    # We use 'script' to capture output while preserving TTY for interactivity
    # Note: 'script' syntax differs between Linux and macOS
    if [[ "$(uname)" == "Darwin" ]]; then
        # macOS syntax: script -q outputfile command args
        script -q "$TEMP_FILE" gh issue create
        SCRIPT_EXIT=$?
    else
        # Linux syntax: script -q -c "command args" outputfile
        script -q -c "gh issue create" "$TEMP_FILE"
        SCRIPT_EXIT=$?
    fi

    if [[ $SCRIPT_EXIT -ne 0 ]]; then
        echo -e "${RED}❌ Error: Failed to create issue${NC}"
        rm -f "$TEMP_FILE"
        exit 1
    fi

    ISSUE_URL=$(grep -oE 'https://github.com/[^/]+/[^/]+/issues/[0-9]+' "$TEMP_FILE" | tail -1)
    rm -f "$TEMP_FILE"

    if [[ ! "$ISSUE_URL" =~ /issues/[0-9]+ ]]; then
        echo -e "${RED}❌ Error: Could not find issue URL in output${NC}"
        exit 1
    fi
fi

# Extract issue number from URL (e.g., https://github.com/owner/repo/issues/719 -> 719)
ISSUE_NUMBER=$(echo "$ISSUE_URL" | grep -oE '/issues/[0-9]+' | grep -oE '[0-9]+')

if [[ -z "$ISSUE_NUMBER" ]]; then
    echo -e "${RED}❌ Error: Could not extract issue number from URL${NC}"
    echo "URL: $ISSUE_URL"
    exit 1
fi

echo -e "${GREEN}✅ Created issue #${ISSUE_NUMBER}${NC}"
echo -e "   ${BLUE}${ISSUE_URL}${NC}"

# Fetch the issue title if we used interactive mode
if [[ -z "$TITLE" ]]; then
    TITLE=$(gh issue view "$ISSUE_NUMBER" --json title -q '.title')
fi

# Slugify the title for branch name
# - Convert to lowercase
# - Replace spaces and special chars with hyphens
# - Remove consecutive hyphens
# - Remove leading/trailing hyphens
# - Truncate to reasonable length
SLUG=$(echo "$TITLE" | \
    tr '[:upper:]' '[:lower:]' | \
    sed 's/[^a-z0-9]/-/g' | \
    sed 's/-\+/-/g' | \
    sed 's/^-//;s/-$//' | \
    cut -c1-50 | \
    sed 's/-$//')

BRANCH_NAME="${ISSUE_NUMBER}-${SLUG}"

echo -e "${BLUE}🌿 Creating branch '${BRANCH_NAME}'...${NC}"

# Create and checkout the new branch
if git show-ref --verify --quiet "refs/heads/${BRANCH_NAME}"; then
    echo -e "${YELLOW}⚠️  Branch '${BRANCH_NAME}' already exists${NC}"
    read -p "Switch to existing branch? (y/N) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        git checkout "$BRANCH_NAME"
    else
        echo -e "${YELLOW}Aborted. Issue was created but branch was not checked out.${NC}"
        exit 0
    fi
else
    git checkout -b "$BRANCH_NAME"
fi

echo ""
echo -e "${GREEN}✅ Ready to work on issue #${ISSUE_NUMBER}${NC}"
echo ""
echo -e "${BLUE}📋 Summary:${NC}"
echo "   Issue:  #${ISSUE_NUMBER} - ${TITLE}"
echo "   URL:    ${ISSUE_URL}"
echo "   Branch: ${BRANCH_NAME}"
echo ""
echo -e "${YELLOW}💡 Tip: Use 'scripts/fetch-issue.sh' to fetch full issue details for Claude${NC}"
