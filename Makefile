# Makefile for my-sideby-ai development

# Environment configuration
# ENV_FILE = .env.development.local

# Read sensitive credentials from .env file
# export

# =============================================================================
# Docker Commands
# =============================================================================
.PHONY: up up-d stop down build rebuild logs log-server shell start kill restart

start:
	/bin/bash bin/start.sh

kill:
	/bin/bash bin/kill.sh

up:
	docker compose up

up-d:
	docker compose up -d

stop:
	docker compose stop

down:
	docker compose down

build:
	docker compose build

rebuild:
	docker compose build --no-cache

logs:
	docker compose logs -f

# Start log server to capture browser logs to file (logs/app.log)
log-server:
	@node scripts/log-server.js

shell:
	docker compose exec app sh

restart:
	make stop && make up-d && make reseed && make logs
# =============================================================================
# Development Commands (run inside container)
# =============================================================================
.PHONY: test test-watch test-local test-file test-functions test-functions-coverage test-all-complete lint lint-all typecheck validate format

# Run all tests (requires VITEST_ENABLE_TESTS=true) and exit
test:
	docker compose exec -e VITEST_ENABLE_TESTS=true app npm run test -- --run

# Run tests in watch mode
test-watch:
	docker compose exec -e VITEST_ENABLE_TESTS=true app npm run test:watch

# Run tests locally (without Docker)
test-local:
	VITEST_ENABLE_TESTS=true npm run test

# Run a specific test file locally
# Usage: make test-file FILE=tests/UpduoReflectionTask.test.tsx
test-file:
ifndef FILE
	$(error FILE is required. Usage: make test-file FILE=tests/YourTest.test.tsx)
endif
	VITEST_ENABLE_TESTS=true npm run test -- --run $(FILE)

# Run edge function tests (Deno)
test-functions:
	@echo "Running Deno edge function tests..."
	@cd supabase/functions && deno test --allow-env --allow-net

# Run edge function tests with coverage
test-functions-coverage:
	@echo "Running Deno edge function tests with coverage..."
	@cd supabase/functions && deno test --allow-env --allow-net --coverage=coverage
	@echo "Generating coverage report..."
	@cd supabase/functions && deno coverage coverage --lcov --output=coverage.lcov

# Run complete test suite (Vitest + Deno edge functions)
test-all-complete:
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "Running Complete Test Suite"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "1️⃣  Running Vitest tests..."
	@make test || (echo "❌ Vitest tests failed" && exit 1)
	@echo ""
	@echo "✅ Vitest tests passed"
	@echo ""
	@echo "2️⃣  Running Deno edge function tests..."
	@cd supabase/functions && deno test --allow-env --allow-net || (echo "❌ Deno tests failed" && exit 1)
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "✅ All tests passed!"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Run linter only on files changed from staging branch
lint:
	@FILES=$$(git diff staging --name-only | grep -E '\.(ts|tsx|js|jsx)$$' | tr '\n' ' '); \
	if [ -n "$$FILES" ]; then \
		echo "Linting changed files: $$FILES"; \
		docker compose exec app npx eslint $$FILES; \
	else \
		echo "No relevant files changed from staging"; \
	fi

# Run linter on all files
lint-all:
	docker compose exec app npm run lint

typecheck:
	docker compose exec app npm run typecheck

validate:
	docker compose exec app npm run validate

format:
	docker compose exec app npm run format

# =============================================================================
# E2E Testing Commands (Playwright)
# =============================================================================
.PHONY: e2e e2e-smoke e2e-smoke-ff e2e-smoke-headed e2e-demo e2e-login e2e-badge e2e-admin e2e-ui e2e-headed e2e-report test-all test-changed

# Run all e2e tests
e2e:
	npm run test:e2e

# =============================================================================
# Full Test Suite
# =============================================================================
# Run the complete test suite: clean db → seed → unit tests → E2E tests
# All tests run in parallel for maximum speed
# Prerequisites: Local stack must be running (make local)
#
# If tests fail, automatically launches Claude to analyze failures.
# Uses scripts/test-all.sh for implementation.
#
# Usage:
#   make test-all                    - Run tests, reuse today's Claude session if exists
#   make test-all NEW_SESSION=1      - Run tests, force a new Claude session
test-all:
	@./scripts/test-all.sh $(if $(NEW_SESSION),--new-session,)

test-changed:
	@./scripts/test-changed.sh

# Run smoke tests only (fast sanity check)
e2e-smoke:
	npm run test:e2e:smoke

# Run smoke tests with fail-fast (stop on first failure)
e2e-smoke-ff:
	npx playwright test --config e2e/playwright.config.ts tests/smoke/ --project=setup --project=chromium -x

# Run smoke tests headed for demos (slow motion, sequential, stop on first failure)
# - VISUAL_DEBUG=true enables element highlighting for existence checks
# - Tests run in numerical order: 01-auth → 02-navigation → 03-dashboard → 04-first-time-user
e2e-smoke-headed:
	VISUAL_DEBUG=true npx playwright test --config e2e/playwright.config.ts tests/smoke/ --project=setup --project=demo --headed -x --workers=1

# Run single-browser demo (all smoke tests in one browser with cache resets between sections)
# - Best for stakeholder demos: no browser open/close, visual continuity
# - Uses slowMo from demo project (1000ms default)
# - Override speed: SLOW_MO=2000 make e2e-demo (for slower demos)
e2e-demo:
	VISUAL_DEBUG=true npx playwright test --config e2e/playwright.config.ts tests/demo/smoke-demo.spec.ts --project=setup --project=demo --headed --workers=1

# Run login/auth related e2e tests
e2e-login:
	npx playwright test --config e2e/playwright.config.ts tests/smoke/01-auth --project=setup --project=chromium

# Run badge related e2e tests
e2e-badge:
	npx playwright test --config e2e/playwright.config.ts tests/**/badge --project=setup --project=chromium

# Run admin e2e tests (seeds database first for predictable data)
e2e-admin:
	@echo "Seeding database for admin tests..."
	@npm run seed
	@echo "Running admin e2e tests..."
	npx playwright test --config e2e/playwright.config.ts --grep "Admin" --project=setup --project=admin

# Open Playwright UI mode for interactive debugging
e2e-ui:
	npm run test:e2e:ui

# Run e2e tests in headed browser mode with visual debugging
e2e-headed:
	VISUAL_DEBUG=true npm run test:e2e:headed

# Show Playwright HTML report
e2e-report:
	npm run test:e2e:report

# =============================================================================
# Supabase Function Sync Commands (Remote)
# =============================================================================
.PHONY: pull-functions push-functions sync-schema supabase-link-prod supabase-link-staging

pull-functions:
	@ENV_PATH="$(ENV_FILE)" ./scripts/pull-functions.sh

push-functions:
	@ENV_PATH="$(ENV_FILE)" ./scripts/push-functions.sh

sync-schema:
	@ENV_PATH="$(ENV_FILE)" ./scripts/sync-schema.sh

supabase-link-prod:
	supabase link --project-ref $SUPABASE_PROJECT_REF_PROD

supabase-link-staging:
	supabase link --project-ref $SUPABASE_PROJECT_REF_STAGING

# =============================================================================
# Supabase Local Development Commands
# =============================================================================
.PHONY: supabase-start supabase-stop supabase-status supabase-migrate sql functions-serve local local-stop seed seed-login seed-badge seed-clean reseed

# Start local Supabase services
supabase-start:
	@echo "Starting Supabase local development..."
	@supabase start
	@echo ""
	@echo "Creating Docker network for frontend integration..."
	@docker network create supabase_network_sideby-dev 2>/dev/null || true
	@docker network connect supabase_network_sideby-dev supabase_db_sideby-dev 2>/dev/null || true
	@echo "Supabase is ready!"

# Stop local Supabase services
supabase-stop:
	@echo "Stopping Supabase local development..."
	@supabase stop

# Show Supabase status and credentials
supabase-status:
	@supabase status

# Run migrations and generate TypeScript types from local Supabase database
supabase-migrate:
	supabase migration up
	supabase gen types typescript --local > src/integrations/supabase/types.ts

# Run SQL query against local Supabase database
# Usage: make sql QUERY="SELECT * FROM profiles LIMIT 5"
sql:
	@if [ -z "$(QUERY)" ]; then \
		echo "Error: No query provided. Usage: make sql QUERY=\"SELECT * FROM profiles\""; \
		exit 1; \
	fi; \
	DB_CONTAINER=$$(docker ps --format "{{.Names}}" | grep -E "supabase_db_"); \
	if [ -z "$$DB_CONTAINER" ]; then \
		echo "Error: No supabase_db container found. Is Supabase running?"; \
		exit 1; \
	fi; \
	docker exec $$DB_CONTAINER psql -U postgres -d postgres -c "$(QUERY)"

# Serve edge functions locally with hot-reload
functions-serve:
	@echo "Starting edge functions server..."
	@supabase functions serve --env-file supabase/.env.local 

# Start full local development stack (Supabase + Frontend)
local:
	@/bin/bash bin/start-local.sh

# Start local stack using production/staging schema dump (bypasses migrations)
local-from-dump:
	@/bin/bash bin/start-local-from-dump.sh

# Seed database with TypeScript seed scripts (like rails db:seed:replant)
# Uses Admin API for proper password hashing - no manual curl needed!
# Runs on host machine for faster execution and docker CLI access
seed:
	@echo "Running TypeScript seed scripts..."
	@npm run seed

# Seed only login test users
seed-login:
	@echo "Running login user seeds..."
	@npm run seed:login

# Seed only badge test users
seed-badge:
	@echo "Running badge user seeds..."
	@npm run seed:badge

# Clean up test data only (no seeding)
seed-clean:
	@echo "Cleaning up test data..."
	@npm run seed:clean

reseed:
	@echo "Reseeding database..."
	make seed-clean
	make seed

# Stop full local development stack
local-stop:
	@echo "Stopping local development stack..."
	@docker compose down
	@supabase stop
	@echo "Local stack stopped."

# =============================================================================
# TUI Dashboard
# =============================================================================
.PHONY: dashboard tui go-build

# Launch the worktree dashboard TUI
dashboard: go-build
	./bin/arborist

tui: dashboard

# Build the Go binary
go-build:
	@go build -o bin/arborist ./cmd/arborist

# =============================================================================
# Documentation Commands
# =============================================================================
.PHONY: move-to-wiki ctags-report

# Generate a codebase report from ctags and save to docs/ctags-report.md
# Regenerates the tags file first, then produces a compact LLM-friendly report
# Usage: make ctags-report                  (compact, ~430 lines)
#        make ctags-report VERBOSE=1        (full detail, ~5k lines)
ctags-report:
	@./scripts/ctags-gen.sh
	@python3 scripts/ctags-report.py $(if $(VERBOSE),--verbose,) -o docs/ctags-report.md

# Move markdown files from docs/ to the Jekyll wiki with date prefix and frontmatter
# Usage: make move-to-wiki
move-to-wiki:
	@echo "Moving markdown files from docs/ to wiki..."
	@DATE=$$(date +%Y-%m-%d); \
	WIKI_DIR=~/Development/sideby-wiki/_posts; \
	for file in docs/*.md; do \
		if [ -f "$$file" ]; then \
			FILENAME=$$(basename "$$file"); \
			TITLE=$$(basename "$$file" .md | sed 's/_/ /g' | sed 's/-/ /g'); \
			TARGET="$$WIKI_DIR/$$DATE-$$FILENAME"; \
			echo "Processing: $$file -> $$TARGET"; \
			{ echo "---"; \
			  echo "layout: post"; \
			  echo "title: \"$$TITLE\""; \
			  echo "date: $$DATE"; \
			  echo "---"; \
			  echo ""; \
			  cat "$$file"; \
			} > "$$TARGET"; \
			rm "$$file"; \
			echo "  ✓ Moved $$FILENAME"; \
		fi; \
	done; \
	echo "Done! Files moved to $$WIKI_DIR"

# =============================================================================
# Git Commands
# =============================================================================
.PHONY: rebase-staging pr-summary fetch-pr-comments fetch-issue worktree

# Generate PR summary using Claude or Codex from git diff
# Usage: make pr-summary [branch=<branch-name>] [model=claude|codex]
# Examples:
#   make pr-summary                        - Diff against staging (default)
#   make pr-summary branch=main            - Diff against main
#   make pr-summary branch=release/v1.1.3  - Diff against release branch
#   make pr-summary model=codex            - Use Codex for summary

pr-summary:
	@./scripts/pr-summary.sh $(if $(branch),--branch=$(branch)) $(if $(model),--model=$(model))

fetch-pr-comments:
	@./scripts/fetch-pr-comments.sh

# Fetch GitHub issue and launch Claude to analyze and implement
# ISSUE is optional, can be run with no args

fetch-issue:
	./scripts/fetch-issue.sh $(ISSUE)

# Rebase all local branches (except main and staging) onto staging
rebase-staging:
	@current_branch=$$(git branch --show-current); \
	for branch in $$(git branch --format='%(refname:short)' | grep -v -E '^(main|staging)$$'); do \
		echo "Rebasing $$branch onto staging..."; \
		git checkout $$branch && \
		git stash && \
		git pull && \
		git rebase staging || { echo "Failed to rebase $$branch"; git rebase --abort 2>/dev/null; } && \
		git stash pop; \
	done; \
	echo "Returning to original branch: $$current_branch"; \
	git checkout $$current_branch

worktree:
	./scripts/worktree.sh

wl:
	@echo "Current Git Worktrees:"
	@./scripts/worktree-list.sh


# =============================================================================
# Help
# =============================================================================
.PHONY: help

help:
	@echo "Docker Commands:"
	@echo "  make up          - Start containers (foreground)"
	@echo "  make up-d        - Start containers (detached)"
	@echo "  make stop        - Stop containers"
	@echo "  make down        - Stop and remove containers"
	@echo "  make build       - Build containers"
	@echo "  make rebuild     - Rebuild containers without cache"
	@echo "  make logs        - Follow container logs"
	@echo "  make log-server  - Start log server (browser logs → logs/app.log)"
	@echo "  make shell       - Open shell in app container"
	@echo ""
	@echo "Development Commands:"
	@echo "  make test                  - Run Vitest tests (in Docker)"
	@echo "  make test-watch            - Run tests in watch mode (in Docker)"
	@echo "  make test-local            - Run tests locally (no Docker)"
	@echo "  make test-functions        - Run Deno edge function tests"
	@echo "  make test-functions-coverage - Run Deno tests with coverage"
	@echo "  make test-all-complete     - Run all tests (Vitest + Deno)"
	@echo "  make lint        - Run linter on files changed from staging"
	@echo "  make lint-all    - Run linter on all files"
	@echo "  make typecheck   - Run TypeScript type checking"
	@echo "  make validate    - Run full validation (typecheck + lint + build)"
	@echo "  make format      - Format code with Prettier"
	@echo ""
	@echo "Supabase Remote Commands:"
	@echo "  make pull-functions  - Pull functions from staging"
	@echo "  make push-functions  - Push functions to development"
	@echo "  make sync-schema     - Sync database schema"
	@echo "  make supabase-link-prod    - Link to production Supabase project"
	@echo "  make supabase-link-staging - Link to staging Supabase project"
	@echo ""
	@echo "Supabase Local Development:"
	@echo "  make local           - Start full local stack (Supabase + Frontend)"
	@echo "  make local-from-dump - Start local stack from prod/staging dump (bypasses migrations)"
	@echo "  make local-stop      - Stop full local stack"
	@echo "  make supabase-start  - Start local Supabase services"
	@echo "  make supabase-stop   - Stop local Supabase services"
	@echo "  make supabase-migrate - Run migrations and generate types"
	@echo "  make supabase-status - Show Supabase status and credentials"
	@echo "  make sql QUERY=\"<query>\"   - Run SQL query against local database"
	@echo "  make functions-serve - Serve edge functions with hot-reload"
	@echo ""
	@echo "Database Seeding:"
	@echo "  make seed         - Seed database with all test data (TypeScript)"
	@echo "  make seed-login   - Seed only login test users"
	@echo "  make seed-badge   - Seed only badge test users"
	@echo "  make seed-clean   - Clean up test data only"
	@echo "  make reseed       - Clean and reseed database"
	@echo ""
	@echo "E2E Testing (Playwright):"
	@echo "  make e2e              - Run all e2e tests"
	@echo "  make e2e-smoke        - Run smoke tests only (fast sanity check)"
	@echo "  make e2e-smoke-ff     - Run smoke tests, stop on first failure"
	@echo "  make e2e-smoke-headed - Run smoke tests headed with visual highlights"
	@echo "  make e2e-demo         - Run single-browser demo (best for stakeholders)"
	@echo "  make e2e-login        - Run login/auth e2e tests"
	@echo "  make e2e-badge        - Run badge e2e tests"
	@echo "  make e2e-admin        - Run admin e2e tests (seeds DB first)"
	@echo "  make e2e-ui           - Open Playwright UI for interactive debugging"
	@echo "  make e2e-headed       - Run tests headed with visual highlights"
	@echo "  make e2e-report       - Show Playwright HTML report"
	@echo ""
	@echo "Full Test Suite:"
	@echo "  make test-all              	- Run complete suite, reuse today's Claude session"
	@echo "  make test-all NEW_SESSION=1	- Run complete suite, force new Claude session"
	@echo "  make test-changed 						- Run test only on changed files"
	@echo ""
	@echo "Documentation Commands:"
	@echo "  make ctags-report              - Generate codebase report to docs/ctags-report.md"
	@echo "  make ctags-report VERBOSE=1    - Generate verbose report (full detail)"
	@echo "  make move-to-wiki              - Move docs/*.md to wiki with date prefix and frontmatter"
	@echo ""
	@echo "Git Commands:"
	@echo "  make pr-summary [branch=<name>] [model=claude|codex] - Generate PR summary (vs staging by default)"
	@echo "  make fetch-pr-comments						- Fetch PR comments generated by AI"
	@echo "  make fetch-issue ISSUE=<num>			- Fetch GitHub issue and launch Claude to implement"
	@echo "  make rebase-staging  						- Rebase all local branches onto staging"
	@echo "  make worktree  									- Create a git worktree for the current branch in ../current-branch-name"
	@echo "  make wl  												- List current git worktrees"
