#!/bin/bash
#
# Compare local Supabase Edge Functions with remote deployed functions
#
# Usage:
#   ./scripts/compare-functions.sh              # Basic comparison
#   ./scripts/compare-functions.sh --diff       # Show git diff for functions modified locally
#   ./scripts/compare-functions.sh --diff main  # Compare local functions against a git ref
#   ./scripts/compare-functions.sh --verbose    # Show file details for each function
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color
BOLD='\033[1m'

# Parse arguments
SHOW_DIFF=false
VERBOSE=false
GIT_REF=""

while [[ $# -gt 0 ]]; do
    case $1 in
        --diff)
            SHOW_DIFF=true
            if [[ -n "$2" && ! "$2" =~ ^-- ]]; then
                GIT_REF="$2"
                shift
            fi
            shift
            ;;
        --verbose|-v)
            VERBOSE=true
            shift
            ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --diff [REF]   Show git diff for local functions (optionally against a git ref)"
            echo "  --verbose, -v  Show file details for each function"
            echo "  --help, -h     Show this help message"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

FUNCTIONS_DIR="supabase/functions"

# Temp files for comparison
REMOTE_FUNCS=$(mktemp)
LOCAL_FUNCS=$(mktemp)
CHANGED_FILES=$(mktemp)
trap "rm -f $REMOTE_FUNCS $LOCAL_FUNCS $CHANGED_FILES" EXIT

echo -e "${BOLD}Fetching remote functions from Supabase...${NC}"
supabase functions list 2>/dev/null | awk 'NR>2 && $3 !~ /^-/ && $3 != "" && $3 != "NAME" {print $3}' | sort > "$REMOTE_FUNCS"

echo -e "${BOLD}Scanning local functions...${NC}"
ls -d "$FUNCTIONS_DIR"/*/ 2>/dev/null | xargs -n1 basename | grep -v '^_' | grep -v '^shared$' | sort > "$LOCAL_FUNCS"

REMOTE_COUNT=$(wc -l < "$REMOTE_FUNCS" | tr -d ' ')
LOCAL_COUNT=$(wc -l < "$LOCAL_FUNCS" | tr -d ' ')

echo ""
echo -e "${BOLD}════════════════════════════════════════════════════════════════${NC}"
echo -e "${BOLD}                    SUPABASE FUNCTIONS COMPARISON${NC}"
echo -e "${BOLD}════════════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "  Remote functions: ${CYAN}$REMOTE_COUNT${NC}"
echo -e "  Local functions:  ${CYAN}$LOCAL_COUNT${NC}"
echo ""

# Functions only on remote (orphaned)
ORPHANED=$(comm -23 "$REMOTE_FUNCS" "$LOCAL_FUNCS")
if [[ -z "$ORPHANED" ]]; then
    ORPHANED_COUNT=0
else
    ORPHANED_COUNT=$(echo "$ORPHANED" | wc -l | xargs)
fi

if [[ $ORPHANED_COUNT -gt 0 ]]; then
    echo -e "${RED}${BOLD}⚠ ORPHANED REMOTE FUNCTIONS ($ORPHANED_COUNT)${NC}"
    echo -e "${RED}  These are deployed but don't exist locally - consider deleting:${NC}"
    echo ""
    echo "$ORPHANED" | while read -r func; do
        echo -e "    ${RED}✗${NC} $func"
        if $VERBOSE; then
            echo -e "      ${YELLOW}Delete with:${NC} supabase functions delete $func"
        fi
    done
    echo ""
fi

# Functions only local (not deployed)
NOT_DEPLOYED=$(comm -13 "$REMOTE_FUNCS" "$LOCAL_FUNCS")
if [[ -z "$NOT_DEPLOYED" ]]; then
    NOT_DEPLOYED_COUNT=0
else
    NOT_DEPLOYED_COUNT=$(echo "$NOT_DEPLOYED" | wc -l | xargs)
fi

if [[ $NOT_DEPLOYED_COUNT -gt 0 ]]; then
    echo -e "${YELLOW}${BOLD}⚡ LOCAL-ONLY FUNCTIONS ($NOT_DEPLOYED_COUNT)${NC}"
    echo -e "${YELLOW}  These exist locally but aren't deployed:${NC}"
    echo ""
    echo "$NOT_DEPLOYED" | while read -r func; do
        echo -e "    ${YELLOW}○${NC} $func"
        if $VERBOSE; then
            FUNC_PATH="$FUNCTIONS_DIR/$func"
            FILE_COUNT=$(find "$FUNC_PATH" -type f -name "*.ts" | wc -l | tr -d ' ')
            echo -e "      Files: $FILE_COUNT TypeScript files"
        fi
    done
    echo ""
fi

# Functions in both places
IN_SYNC=$(comm -12 "$REMOTE_FUNCS" "$LOCAL_FUNCS")
if [[ -z "$IN_SYNC" ]]; then
    IN_SYNC_COUNT=0
else
    IN_SYNC_COUNT=$(echo "$IN_SYNC" | wc -l | xargs)
fi

echo -e "${GREEN}${BOLD}✓ SYNCED FUNCTIONS ($IN_SYNC_COUNT)${NC}"
echo -e "${GREEN}  These exist both locally and remotely${NC}"
echo ""

if $VERBOSE || $SHOW_DIFF; then
    echo "$IN_SYNC" | while read -r func; do
        FUNC_PATH="$FUNCTIONS_DIR/$func"

        if $VERBOSE; then
            echo -e "    ${GREEN}✓${NC} ${BOLD}$func${NC}"

            # Show main file info
            MAIN_FILE="$FUNC_PATH/index.ts"
            if [[ -f "$MAIN_FILE" ]]; then
                LINES=$(wc -l < "$MAIN_FILE" | tr -d ' ')
                MODIFIED=$(stat -c %y "$MAIN_FILE" 2>/dev/null | cut -d' ' -f1 || stat -f %Sm -t %Y-%m-%d "$MAIN_FILE" 2>/dev/null)
                echo -e "      ${CYAN}index.ts${NC}: $LINES lines, modified $MODIFIED"
            fi

            # Count other files
            OTHER_FILES=$(find "$FUNC_PATH" -type f -name "*.ts" ! -name "index.ts" 2>/dev/null | wc -l | tr -d ' ')
            if [[ $OTHER_FILES -gt 0 ]]; then
                echo -e "      + $OTHER_FILES other TypeScript files"
            fi
        fi

        if $SHOW_DIFF; then
            if [[ -n "$GIT_REF" ]]; then
                # Compare against specific git ref
                DIFF_OUTPUT=$(git diff --stat "$GIT_REF" -- "$FUNC_PATH" 2>/dev/null || echo "")
                CHANGED=$(git diff --name-only "$GIT_REF" -- "$FUNC_PATH" 2>/dev/null || echo "")
            else
                # Show uncommitted changes
                DIFF_OUTPUT=$(git diff --stat HEAD -- "$FUNC_PATH" 2>/dev/null || echo "")
                CHANGED=$(git diff --name-only HEAD -- "$FUNC_PATH" 2>/dev/null || echo "")
            fi

            if [[ -n "$DIFF_OUTPUT" ]]; then
                if ! $VERBOSE; then
                    echo -e "    ${BLUE}◆${NC} ${BOLD}$func${NC} (has changes)"
                fi
                echo -e "      ${BLUE}Changes:${NC}"
                echo "$DIFF_OUTPUT" | sed 's/^/        /'
                echo ""
                # Record changed files for showing diffs later
                echo "$CHANGED" >> "$CHANGED_FILES"
            fi
        fi
    done
else
    # Just list them in columns
    echo "$IN_SYNC" | pr -3 -t -w 80 | sed 's/^/    /'
fi

echo ""
echo -e "${BOLD}════════════════════════════════════════════════════════════════${NC}"

# Summary
echo ""
echo -e "${BOLD}Summary:${NC}"
if [[ $ORPHANED_COUNT -gt 0 ]]; then
    echo -e "  ${RED}• $ORPHANED_COUNT orphaned functions need cleanup${NC}"
fi
if [[ $NOT_DEPLOYED_COUNT -gt 0 ]]; then
    echo -e "  ${YELLOW}• $NOT_DEPLOYED_COUNT local functions not deployed${NC}"
fi
echo -e "  ${GREEN}• $IN_SYNC_COUNT functions in sync${NC}"
echo ""

# Provide cleanup command if there are orphans
if [[ $ORPHANED_COUNT -gt 0 ]]; then
    echo -e "${BOLD}To delete all orphaned functions:${NC}"
    echo ""
    echo "$ORPHANED" | while read -r func; do
        echo "  supabase functions delete $func"
    done
    echo ""
fi

# Show individual file diffs at the end
if $SHOW_DIFF && [[ -s "$CHANGED_FILES" ]]; then
    echo -e "${BOLD}════════════════════════════════════════════════════════════════${NC}"
    echo -e "${BOLD}                         FILE DIFFS${NC}"
    echo -e "${BOLD}════════════════════════════════════════════════════════════════${NC}"
    echo ""

    # Process each changed file
    sort -u "$CHANGED_FILES" | while read -r file; do
        if [[ -n "$file" ]]; then
            echo -e "${CYAN}${BOLD}── $file ──${NC}"
            if [[ -n "$GIT_REF" ]]; then
                git diff --color=always "$GIT_REF" -- "$file" 2>/dev/null || echo "  (unable to diff)"
            else
                git diff --color=always HEAD -- "$file" 2>/dev/null || echo "  (unable to diff)"
            fi
            echo ""
        fi
    done
fi
