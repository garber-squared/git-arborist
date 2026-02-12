#!/bin/bash

# =============================================================================
# Selective Test Runner - Run tests relevant to changed files
# =============================================================================
#
# Usage: ./scripts/test-changed.sh [base_branch] [options]
#
# Arguments:
#   base_branch    Branch to compare against (default: staging)
#
# Options:
#   --dry-run      Show which tests would run without executing
#   --skip-seed    Skip database seeding
#   --vitest-only  Run only Vitest unit tests
#   --e2e-only     Run only Playwright E2E tests
#   -h, --help     Show this help message
#
# Examples:
#   ./scripts/test-changed.sh                    # Compare with staging
#   ./scripts/test-changed.sh main               # Compare with main
#   ./scripts/test-changed.sh staging --dry-run  # Preview tests without running
#
# =============================================================================

set -eo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
NC='\033[0m' # No Color

# Default values
BASE_BRANCH="staging"
DRY_RUN=false
SKIP_SEED=false
VITEST_ONLY=false
E2E_ONLY=false

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --skip-seed)
            SKIP_SEED=true
            shift
            ;;
        --vitest-only)
            VITEST_ONLY=true
            shift
            ;;
        --e2e-only)
            E2E_ONLY=true
            shift
            ;;
        -h|--help)
            head -35 "$0" | tail -31
            exit 0
            ;;
        -*)
            echo -e "${RED}Unknown option: $1${NC}"
            echo "Use --help for usage information"
            exit 1
            ;;
        *)
            BASE_BRANCH="$1"
            shift
            ;;
    esac
done

# =============================================================================
# Step 1: Get changed files from git diff
# =============================================================================

echo -e "${BLUE}╔═══════════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║              SELECTIVE TEST RUNNER                                ║${NC}"
echo -e "${BLUE}╚═══════════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Verify base branch exists
if ! git rev-parse --verify "$BASE_BRANCH" >/dev/null 2>&1; then
    # Try with origin/ prefix
    if git rev-parse --verify "origin/$BASE_BRANCH" >/dev/null 2>&1; then
        BASE_BRANCH="origin/$BASE_BRANCH"
    else
        echo -e "${RED}Error: Branch '$BASE_BRANCH' not found${NC}"
        exit 1
    fi
fi

CURRENT_BRANCH=$(git branch --show-current)
echo -e "${CYAN}📊 Comparing: ${YELLOW}$CURRENT_BRANCH${CYAN} ← ${YELLOW}$BASE_BRANCH${NC}"
echo ""

# Get list of changed files (only src/, tests/, e2e/ directories)
CHANGED_FILES=$(git diff --name-only "$BASE_BRANCH"...HEAD -- 'src/' 'tests/' 'e2e/' 'supabase/' 2>/dev/null || \
                git diff --name-only "$BASE_BRANCH" HEAD -- 'src/' 'tests/' 'e2e/' 'supabase/' 2>/dev/null || \
                echo "")

if [ -z "$CHANGED_FILES" ]; then
    echo -e "${GREEN}✅ No relevant changes detected. Nothing to test.${NC}"
    exit 0
fi

echo -e "${YELLOW}📝 Changed files:${NC}"
echo "$CHANGED_FILES" | sed 's/^/   /'
echo ""

# =============================================================================
# Step 2: Map changed files to relevant tests
# =============================================================================

# Arrays to collect tests
declare -a VITEST_TESTS=()
declare -a PLAYWRIGHT_TESTS=()

# Map a changed file path to one or more relevant tests.
# The mapping is implemented with pattern checks and case logic inside
# the map_file_to_tests() function below.

map_file_to_tests() {
    local file="$1"
    local basename=$(basename "$file" | sed 's/\.[^.]*$//')  # Remove extension
    local dirname=$(dirname "$file")

    # Skip if file is already a test
    if [[ "$file" == *".test."* ]] || [[ "$file" == *".spec."* ]]; then
        if [[ "$file" == tests/* ]]; then
            VITEST_TESTS+=("$file")
        elif [[ "$file" == e2e/* ]]; then
            PLAYWRIGHT_TESTS+=("$file")
        fi
        return
    fi

    # =========================================================================
    # Vitest Mappings (Unit/Component Tests)
    # =========================================================================

    # Direct test file match: src/components/Foo.tsx → tests/Foo.test.tsx
    local direct_test="tests/${basename}.test.tsx"
    if [ -f "$direct_test" ]; then
        VITEST_TESTS+=("$direct_test")
    fi

    direct_test="tests/${basename}.test.ts"
    if [ -f "$direct_test" ]; then
        VITEST_TESTS+=("$direct_test")
    fi

    # Component-specific mappings
    case "$file" in
        # Beta feature system
        src/hooks/useBetaFeature.ts|src/hooks/useBetaStatus.ts|src/types/beta.ts)
            [ -f "tests/BetaFeature.test.tsx" ] && VITEST_TESTS+=("tests/BetaFeature.test.tsx")
            [ -f "tests/AdminBetaManagement.test.ts" ] && VITEST_TESTS+=("tests/AdminBetaManagement.test.ts")
            ;;

        # Beta admin hooks
        src/components/admin/beta/*)
            [ -f "tests/AdminBetaManagement.test.ts" ] && VITEST_TESTS+=("tests/AdminBetaManagement.test.ts")
            # Check for hook-specific tests
            if [[ "$file" == *hooks/* ]]; then
                local hook_test="src/components/admin/beta/hooks/__tests__/${basename}.test.ts"
                [ -f "$hook_test" ] && VITEST_TESTS+=("$hook_test")
            fi
            ;;

        # Onboarding components (including UpduoReflectionTask)
        src/components/dashboard/onboarding/*)
            [ -f "tests/UpduoReflectionTask.test.tsx" ] && VITEST_TESTS+=("tests/UpduoReflectionTask.test.tsx")
            ;;

        # Crew-related files
        src/components/crew/*|src/utils/crewTagging.ts)
            [ -f "tests/CrewPage.test.tsx" ] && VITEST_TESTS+=("tests/CrewPage.test.tsx")
            [ -f "tests/crewTagging.test.ts" ] && VITEST_TESTS+=("tests/crewTagging.test.ts")
            ;;

        # Database types - affects many tests
        src/integrations/supabase/types.ts)
            VITEST_TESTS+=("tests/BetaFeature.test.tsx")
            VITEST_TESTS+=("tests/AdminBetaManagement.test.ts")
            [ -f "tests/UpduoReflectionTask.test.tsx" ] && VITEST_TESTS+=("tests/UpduoReflectionTask.test.tsx")
            PLAYWRIGHT_TESTS+=("e2e/tests/smoke/08-reflection-routing.smoke.spec.ts")
            ;;
    esac

    # =========================================================================
    # Playwright Mappings (E2E Tests)
    # =========================================================================

    case "$file" in
        # Authentication changes
        src/hooks/useAuth.tsx|src/hooks/auth/*|src/utils/auth/*|src/pages/Login.tsx)
            PLAYWRIGHT_TESTS+=("e2e/tests/smoke/01-auth.smoke.spec.ts")
            ;;

        # Navigation/routing changes
        src/App.tsx|src/pages/*.tsx)
            PLAYWRIGHT_TESTS+=("e2e/tests/smoke/02-navigation.smoke.spec.ts")
            ;;

        # Dashboard changes
        src/components/dashboard/*|src/pages/Dashboard.tsx)
            PLAYWRIGHT_TESTS+=("e2e/tests/smoke/03-dashboard.smoke.spec.ts")
            ;;

        # First-time user / onboarding
        src/components/dashboard/onboarding/*|src/hooks/useOnboardingStatus.ts)
            PLAYWRIGHT_TESTS+=("e2e/tests/smoke/04-first-time-user.smoke.spec.ts")
            PLAYWRIGHT_TESTS+=("e2e/tests/smoke/08-reflection-routing.smoke.spec.ts")
            ;;

        # Ideas/chat functionality
        src/components/dashboard/ideas/*|src/hooks/useMassagePrompts.ts)
            PLAYWRIGHT_TESTS+=("e2e/tests/smoke/05-massage-chat.smoke.spec.ts")
            ;;

        # Badge enrollment
        src/hooks/useBadgeEnrollment.ts|src/components/dashboard/learning/*)
            PLAYWRIGHT_TESTS+=("e2e/tests/smoke/06-badge-enrollment.smoke.spec.ts")
            ;;

        # Admin badge opt-ins
        src/components/admin/badge-opt-ins/*|src/hooks/useAdminBadgeOptIns.ts)
            PLAYWRIGHT_TESTS+=("e2e/tests/smoke/07-admin-badge-opt-ins.smoke.spec.ts")
            ;;

        # Beta management admin
        src/components/admin/beta/*|src/hooks/useBetaFeature.ts|src/types/beta.ts)
            PLAYWRIGHT_TESTS+=("e2e/tests/admin/beta-management.spec.ts")
            PLAYWRIGHT_TESTS+=("e2e/tests/smoke/08-reflection-routing.smoke.spec.ts")
            ;;

        # Migrations may affect E2E tests
        supabase/migrations/*)
            # Run smoke tests for migration changes
            PLAYWRIGHT_TESTS+=("e2e/tests/smoke/01-auth.smoke.spec.ts")
            PLAYWRIGHT_TESTS+=("e2e/tests/smoke/03-dashboard.smoke.spec.ts")
            ;;
    esac
}

# Process each changed file
while IFS= read -r file; do
    [ -n "$file" ] && map_file_to_tests "$file"
done <<< "$CHANGED_FILES"

# Remove duplicates
VITEST_TESTS=($(printf '%s\n' "${VITEST_TESTS[@]}" | sort -u))
PLAYWRIGHT_TESTS=($(printf '%s\n' "${PLAYWRIGHT_TESTS[@]}" | sort -u))

# Filter to only existing files
VITEST_TESTS=($(for f in "${VITEST_TESTS[@]}"; do [ -f "$f" ] && echo "$f"; done))
PLAYWRIGHT_TESTS=($(for f in "${PLAYWRIGHT_TESTS[@]}"; do [ -f "$f" ] && echo "$f"; done))

# =============================================================================
# Step 3: Display test plan
# =============================================================================

echo -e "${MAGENTA}╔═══════════════════════════════════════════════════════════════════╗${NC}"
echo -e "${MAGENTA}║                      TEST PLAN                                    ║${NC}"
echo -e "${MAGENTA}╚═══════════════════════════════════════════════════════════════════╝${NC}"
echo ""

if [ ${#VITEST_TESTS[@]} -gt 0 ] && [ "$E2E_ONLY" = false ]; then
    echo -e "${YELLOW}📦 Vitest Tests (${#VITEST_TESTS[@]}):${NC}"
    printf '   %s\n' "${VITEST_TESTS[@]}"
    echo ""
else
    echo -e "${CYAN}📦 Vitest Tests: None${NC}"
    echo ""
fi

if [ ${#PLAYWRIGHT_TESTS[@]} -gt 0 ] && [ "$VITEST_ONLY" = false ]; then
    echo -e "${YELLOW}🎭 Playwright Tests (${#PLAYWRIGHT_TESTS[@]}):${NC}"
    printf '   %s\n' "${PLAYWRIGHT_TESTS[@]}"
    echo ""
else
    echo -e "${CYAN}🎭 Playwright Tests: None${NC}"
    echo ""
fi

# Check if there are any tests to run
TOTAL_TESTS=$((${#VITEST_TESTS[@]} + ${#PLAYWRIGHT_TESTS[@]}))
if [ "$VITEST_ONLY" = true ]; then
    TOTAL_TESTS=${#VITEST_TESTS[@]}
elif [ "$E2E_ONLY" = true ]; then
    TOTAL_TESTS=${#PLAYWRIGHT_TESTS[@]}
fi

if [ $TOTAL_TESTS -eq 0 ]; then
    echo -e "${YELLOW}⚠️  No relevant tests found for the changed files.${NC}"
    echo -e "${CYAN}   Consider running the full test suite: ./scripts/test-all.sh${NC}"
    exit 0
fi

# Dry run - just show what would run
if [ "$DRY_RUN" = true ]; then
    echo -e "${CYAN}🔍 Dry run complete. Use without --dry-run to execute tests.${NC}"
    exit 0
fi

# =============================================================================
# Step 4: Reseed database
# =============================================================================

if [ "$SKIP_SEED" = false ]; then
    echo -e "${BLUE}╔═══════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║                    DATABASE SEEDING                               ║${NC}"
    echo -e "${BLUE}╚═══════════════════════════════════════════════════════════════════╝${NC}"
    echo ""

    echo -e "${YELLOW}Step 1/2: Truncating test database tables...${NC}"
    if npm run seed:clean; then
        echo -e "${GREEN}✓ Database cleaned${NC}"
    else
        echo -e "${RED}✗ Failed to clean database${NC}"
        exit 1
    fi
    echo ""

    echo -e "${YELLOW}Step 2/2: Seeding test database...${NC}"
    if npm run seed; then
        echo -e "${GREEN}✓ Database seeded${NC}"
    else
        echo -e "${RED}✗ Failed to seed database${NC}"
        exit 1
    fi
    echo ""
fi

# =============================================================================
# Step 5: Run tests
# =============================================================================

TEST_FAILED=false

# Run Vitest tests
if [ ${#VITEST_TESTS[@]} -gt 0 ] && [ "$E2E_ONLY" = false ]; then
    echo -e "${BLUE}╔═══════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║                    VITEST UNIT TESTS                              ║${NC}"
    echo -e "${BLUE}╚═══════════════════════════════════════════════════════════════════╝${NC}"
    echo ""

    # Join test files with spaces for vitest command
    VITEST_FILES=$(printf '%s ' "${VITEST_TESTS[@]}")

    echo -e "${YELLOW}Running: VITEST_ENABLE_TESTS=true npm run test -- --run ${VITEST_FILES}${NC}"
    echo ""

    if VITEST_ENABLE_TESTS=true npm run test -- --run ${VITEST_FILES}; then
        echo -e "${GREEN}✓ Vitest tests passed${NC}"
    else
        echo -e "${RED}✗ Vitest tests failed${NC}"
        TEST_FAILED=true
    fi
    echo ""
fi

# Run Playwright tests
if [ ${#PLAYWRIGHT_TESTS[@]} -gt 0 ] && [ "$VITEST_ONLY" = false ] && [ "$TEST_FAILED" = false ]; then
    echo -e "${BLUE}╔═══════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║                   PLAYWRIGHT E2E TESTS                            ║${NC}"
    echo -e "${BLUE}╚═══════════════════════════════════════════════════════════════════╝${NC}"
    echo ""

    # Join test files with spaces for playwright command
    PLAYWRIGHT_FILES=$(printf '%s ' "${PLAYWRIGHT_TESTS[@]}")

    echo -e "${YELLOW}Running: npx playwright test --config e2e/playwright.config.ts ${PLAYWRIGHT_FILES}${NC}"
    echo ""

    if PLAYWRIGHT_HTML_OPEN=never npx playwright test --config e2e/playwright.config.ts ${PLAYWRIGHT_FILES}; then
        echo -e "${GREEN}✓ Playwright tests passed${NC}"
    else
        echo -e "${RED}✗ Playwright tests failed${NC}"
        TEST_FAILED=true
    fi
    echo ""
fi

# =============================================================================
# Final Summary
# =============================================================================

echo ""
if [ "$TEST_FAILED" = true ]; then
    echo -e "${RED}╔═══════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${RED}║                    ❌ TESTS FAILED                                ║${NC}"
    echo -e "${RED}╚═══════════════════════════════════════════════════════════════════╝${NC}"
    exit 1
else
    echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║                 ✅ ALL TESTS PASSED                               ║${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════════╝${NC}"
fi
