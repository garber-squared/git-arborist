#!/usr/bin/env bash
set -euo pipefail

# --- Load environment variables from .env in scripts directory ---
SCRIPT_DIR="$(dirname "$0")"
ENV_FILE="$SCRIPT_DIR/.env"

if [ -f "$ENV_FILE" ]; then
  echo "📄 Loading environment from $ENV_FILE"
  set -a  # automatically export all variables
  source "$ENV_FILE"
  set +a  # stop auto-exporting
else
  echo "❌ Could not find $ENV_FILE"
  echo "💡 Please create scripts/.env with SUPABASE_PW=your_password"
  exit 1
fi

# Check if password is set
if [ -z "${SUPABASE_PW:-}" ]; then
  echo "❌ SUPABASE_PW not found in environment"
  echo "💡 Please add SUPABASE_PW=your_password to scripts/.env"
  exit 1
fi

DRY_RUN=${1:-false}
SUPABASE_PROD_PROJECT_ID="upffcxqiozqhdgfesmji"

if [ "$DRY_RUN" = "true" ]; then
  echo "🔍 Running in dry run mode. No changes will be made."
fi

# ✅ Explicitly link to production project
echo "🔗 Linking to production project: $SUPABASE_PROD_PROJECT_ID"
supabase link --project-ref "$SUPABASE_PROD_PROJECT_ID" -p "$SUPABASE_PW" >/dev/null


FUNCTIONS_DIR="../supabase/functions"
VALID_NAME_REGEX="^[A-Za-z][A-Za-z0-9_-]*$"

if [ ! -d "$FUNCTIONS_DIR" ]; then
  echo "❌ No functions directory found at $FUNCTIONS_DIR"
  exit 1
fi

echo "🚀 Deploying all valid Supabase edge functions in $FUNCTIONS_DIR"

shopt -s nullglob  # Avoid issues if no dirs exist

for fn_path in "$FUNCTIONS_DIR"/*/; do
  fn=$(basename "$fn_path")

  # Skip invalid or helper folders
  if [[ ! "$fn" =~ $VALID_NAME_REGEX ]]; then
    echo "⚠️ Skipping invalid or helper folder: $fn"
    continue
  fi

  if [ ! -f "$fn_path/index.ts" ] && [ ! -f "$fn_path/index.js" ]; then
    echo "⚠️ Skipping $fn (no index.ts or index.js file found)"
    continue
  fi

  echo "📤 Deploying function: $fn"
  if [[ "$DRY_RUN" == "true" ]]; then
    echo "🧪 Would deploy: $fn"
  else
    supabase functions deploy "$fn"
  fi

done

echo "✅ All valid functions deployed!"
