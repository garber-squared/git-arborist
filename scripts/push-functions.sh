#!/usr/bin/env bash
set -euo pipefail

# --- Load environment variables ---
ENV_FILE="${ENV_PATH:-$(dirname "$0")/../.env.development.local}"

if [ -f "$ENV_FILE" ]; then
  echo "📄 Loading environment from $ENV_FILE"
  set -a
  source "$ENV_FILE"
  set +a
else
  echo "❌ Could not find $ENV_FILE"
  exit 1
fi

# --- Required env vars ---
: "${STAGING_PROJECT_ID:?Missing STAGING_PROJECT_ID in $ENV_FILE}"
: "${PROD_PROJECT_ID:?Missing PROD_PROJECT_ID in $ENV_FILE}"
: "${SUPABASE_PASSWORD:?Missing SUPABASE_PASSWORD in $ENV_FILE}"

# --- Choose environment ---
echo "🧠 Where do you want to deploy your local functions?"
select ENVIRONMENT in "staging" "production"; do
  case $ENVIRONMENT in
    staging)
      PROJECT_ID="$STAGING_PROJECT_ID"
      break
      ;;
    production)
      PROJECT_ID="$PROD_PROJECT_ID"
      break
      ;;
    *)
      echo "❌ Invalid selection. Please choose 1 or 2."
      ;;
  esac
done

echo "🌐 Selected environment: $ENVIRONMENT"
echo "🔗 Project ID: $PROJECT_ID"

# --- Link to Supabase project ---
echo "🔗 Linking to $ENVIRONMENT project..."
supabase link --project-ref "$PROJECT_ID" -p "$SUPABASE_PASSWORD" >/dev/null

# --- Deploy functions ---
FUNCTIONS_DIR="../supabase/functions"

echo "🚀 Deploying local functions..."
for fn_path in "$FUNCTIONS_DIR"/*/; do
  [ -d "$fn_path" ] || continue
  FN_NAME=$(basename "$fn_path")
  echo "📤 Deploying: $FN_NAME"
  supabase functions deploy "$FN_NAME"
done

echo "✅ All functions deployed to $ENVIRONMENT."
