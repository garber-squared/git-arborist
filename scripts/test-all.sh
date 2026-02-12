#!/bin/bash

# Run full test suite and launch Claude for debugging if tests fail
# Usage: ./test-all.sh [--new-session] [--full]
#
# Options:
#   --new-session    Force creation of a new Claude session (skip --continue flag)
#   --full           Force full E2E run (ignore --last-failed)
#
# Incremental E2E runs:
#   - Uses Playwright's --last-failed to re-run only failed tests
#   - Use --full to force a complete E2E run
#
# This script:
# 1. Seeds the database (always)
# 2. Runs unit tests (always)
# 3. Runs E2E tests (uses --last-failed if previous failures exist)
# 4. If tests fail, saves output to test-results.log and launches Claude
# 5. If tests pass, deletes test-results.log

set -eo pipefail

# Parse arguments
FORCE_NEW_SESSION=false
FORCE_FULL_RUN=false
while [[ $# -gt 0 ]]; do
    case $1 in
        --new-session)
            FORCE_NEW_SESSION=true
            shift
            ;;
        --full)
            FORCE_FULL_RUN=true
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [--new-session] [--full]"
            echo ""
            echo "Run the full test suite and launch Claude for debugging if tests fail."
            echo ""
            echo "Options:"
            echo "  --new-session    Force creation of a new Claude session"
            echo "  --full           Force full E2E run (ignore previous failures)"
            echo "  -h, --help       Show this help message"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Session continuation: use --continue to resume last conversation if not forcing new
SESSION_FLAG=""
if [ "$FORCE_NEW_SESSION" = false ]; then
    SESSION_FLAG="--continue"
fi

# Results file for Claude context
RESULTS_FILE="test-results.log"

# Check for previous E2E failures (Playwright's .last-run.json)
LAST_RUN_FILE="test-results/.last-run.json"
USE_LAST_FAILED=false

if [ -f "$LAST_RUN_FILE" ] && [ "$FORCE_FULL_RUN" = false ]; then
    # Check both: status is "failed" AND failedTests array is non-empty
    # Empty array case: "failedTests": [] - should NOT use --last-failed
    if grep -q '"status".*"failed"' "$LAST_RUN_FILE" 2>/dev/null && \
       ! grep -q '"failedTests".*\[\]' "$LAST_RUN_FILE" 2>/dev/null; then
        USE_LAST_FAILED=true
        echo -e "${CYAN}📋 Found previous E2E failures, will use --last-failed${NC}"
        echo ""
    fi
fi

# Create a temporary file to capture test output
TEST_OUTPUT=$(mktemp)
trap "rm -f $TEST_OUTPUT" EXIT

echo -e "${BLUE}╔═══════════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║                    FULL TEST SUITE                                ║${NC}"
echo -e "${BLUE}╚═══════════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Track which step failed
FAILED_STEP=""

# Function to run a step and capture failure
run_step() {
    local step_name="$1"
    local step_cmd="$2"
    local step_id="$3"

    echo -e "${YELLOW}${step_name}${NC}"

    # Run the command and capture output, but also display it
    if eval "$step_cmd" 2>&1 | tee -a "$TEST_OUTPUT"; then
        echo -e "${GREEN}✓ ${step_name} completed${NC}"
        echo ""
        return 0
    else
        FAILED_STEP="$step_id"
        return 1
    fi
}

# Run test suite - stop on first failure
TEST_FAILED=false

run_step "Step 1/4: Truncating test database tables..." "npm run seed:clean" "clean" || TEST_FAILED=true

if [ "$TEST_FAILED" = false ]; then
    run_step "Step 2/4: Seeding test database..." "npm run seed" "seed" || TEST_FAILED=true
fi

if [ "$TEST_FAILED" = false ]; then
    run_step "Step 3/4: Running unit tests (parallel)..." "VITEST_ENABLE_TESTS=true npm run test -- --run" "unit" || TEST_FAILED=true
fi

if [ "$TEST_FAILED" = false ]; then
    # PLAYWRIGHT_HTML_OPEN=never prevents the HTML report server from opening on failure
    if [ "$USE_LAST_FAILED" = true ]; then
        run_step "Step 4/4: Re-running failed E2E tests (--last-failed)..." \
            "PLAYWRIGHT_HTML_OPEN=never npx playwright test --config e2e/playwright.config.ts --last-failed -x" "e2e" || TEST_FAILED=true
    else
        run_step "Step 4/4: Running Playwright E2E tests (headless, parallel, fail-fast)..." \
            "PLAYWRIGHT_HTML_OPEN=never npx playwright test --config e2e/playwright.config.ts -x" "e2e" || TEST_FAILED=true
    fi
fi

# Check results
if [ "$TEST_FAILED" = true ]; then
    echo ""
    echo -e "${RED}╔═══════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${RED}║                 ❌ TESTS FAILED                                   ║${NC}"
    echo -e "${RED}╚═══════════════════════════════════════════════════════════════════╝${NC}"
    echo ""

    # Save results for Claude context
    cat "$TEST_OUTPUT" > "$RESULTS_FILE"
    echo -e "${CYAN}📝 Saved output to ${RESULTS_FILE}${NC}"
    echo ""

    echo -e "${YELLOW}🤖 Launching Claude to analyze failures...${NC}"
    echo ""

    # Get the last 200 lines of output for context (avoid massive prompts)
    FAILURE_CONTEXT=$(tail -n 200 "$TEST_OUTPUT")

    # Launch Claude with failure details (use --continue to resume previous conversation if exists)
    exec claude $SESSION_FLAG "
The test suite failed during: ${FAILED_STEP}

Here is the test output (last 200 lines):

\`\`\`
${FAILURE_CONTEXT}
\`\`\`

Please analyze the test failures and help me fix them. Focus on:
1. Identifying the root cause of the failure
2. Suggesting specific fixes for the failing tests
3. If the test is flaky, suggest how to make it more reliable
"
else
    # All tests passed - clean up results file
    if [ -f "$RESULTS_FILE" ]; then
        rm -f "$RESULTS_FILE"
        echo -e "${CYAN}🧹 Cleaned up ${RESULTS_FILE}${NC}"
    fi

    echo ""
    echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║                 ✅ ALL TESTS PASSED                               ║${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════════╝${NC}"
fi
