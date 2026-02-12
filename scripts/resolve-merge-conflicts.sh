#!/bin/bash

# resolve-merge-conflicts.sh
# Automates merge conflict resolution using Claude Code
# Usage: ./scripts/resolve-merge-conflicts.sh

set -e

DOCS_DIR="docs"
CONFLICTS_FILE="$DOCS_DIR/MERGE_CONFLICTS.txt"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}🔍 Checking for merge conflicts...${NC}"

# Step 1: Run git status and identify conflicted files
CONFLICTED_FILES=$(git status --porcelain | grep "^UU\|^AA\|^DD\|^DU\|^UD\|^AU\|^UA" | awk '{print $2}')

if [ -z "$CONFLICTED_FILES" ]; then
    echo -e "${GREEN}✅ No merge conflicts detected.${NC}"
    exit 0
fi

echo -e "${RED}⚠️  Merge conflicts found in the following files:${NC}"
echo "$CONFLICTED_FILES"

# Step 2: Create docs directory if it doesn't exist
mkdir -p "$DOCS_DIR"

# Step 3: Save merge conflict details to file
echo "# Merge Conflict Report" > "$CONFLICTS_FILE"
echo "Generated: $(date)" >> "$CONFLICTS_FILE"
echo "Branch: $(git branch --show-current 2>/dev/null || echo 'DETACHED HEAD')" >> "$CONFLICTS_FILE"
echo "" >> "$CONFLICTS_FILE"

# Get rebase/merge status
if [ -d ".git/rebase-merge" ] || [ -d ".git/rebase-apply" ]; then
    echo "Operation: REBASE in progress" >> "$CONFLICTS_FILE"
    if [ -f ".git/rebase-merge/head-name" ]; then
        echo "Rebasing onto: $(cat .git/rebase-merge/onto 2>/dev/null || echo 'unknown')" >> "$CONFLICTS_FILE"
    fi
elif [ -f ".git/MERGE_HEAD" ]; then
    echo "Operation: MERGE in progress" >> "$CONFLICTS_FILE"
    echo "Merging: $(cat .git/MERGE_HEAD)" >> "$CONFLICTS_FILE"
fi

echo "" >> "$CONFLICTS_FILE"
echo "## Conflicted Files" >> "$CONFLICTS_FILE"
echo "" >> "$CONFLICTS_FILE"

# For each conflicted file, extract and save the conflict markers
for file in $CONFLICTED_FILES; do
    echo "### $file" >> "$CONFLICTS_FILE"
    echo "" >> "$CONFLICTS_FILE"

    if [ -f "$file" ]; then
        echo '```' >> "$CONFLICTS_FILE"
        # Extract lines with conflict markers and surrounding context
        grep -n "^<<<<<<<\|^=======\|^>>>>>>>" "$file" 2>/dev/null >> "$CONFLICTS_FILE" || true
        echo '```' >> "$CONFLICTS_FILE"
        echo "" >> "$CONFLICTS_FILE"

        # Count number of conflicts in this file
        CONFLICT_COUNT=$(grep -c "^<<<<<<<" "$file" 2>/dev/null || echo "0")
        echo "Number of conflicts: $CONFLICT_COUNT" >> "$CONFLICTS_FILE"
    else
        echo "File was deleted or does not exist" >> "$CONFLICTS_FILE"
    fi
    echo "" >> "$CONFLICTS_FILE"
done

echo -e "${GREEN}📄 Conflict details saved to: $CONFLICTS_FILE${NC}"

# Step 4: Prompt Claude Code to resolve conflicts
echo ""
echo -e "${YELLOW}🤖 Launching Claude Code to resolve conflicts...${NC}"
echo ""

claude --print "A rebase or merge has started on the current branch. Read docs/MERGE_CONFLICTS.txt, create a task list and resolve the conflicts."
