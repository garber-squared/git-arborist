#!/usr/bin/env bash
set -euo pipefail

# -- Load environment variables from .env.development.local --
ENV_FILE="${ENV_PATH:-$(dirname "$0")/../.env.development.local}"

# resolve this script’s directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"



if [ -f "$ENV_FILE" ]; then
  echo "📄 Loading environment from $ENV_FILE"
  set -a
  source "$ENV_FILE"
  set +a
else
  echo "❌ Could not find $ENV_FILE"
  exit 1
fi

# -- Required env vars --
: "${STAGING_PROJECT_ID:?Missing STAGING_PROJECT_ID in $ENV_FILE}"
: "${PROD_PROJECT_ID:?Missing PROD_PROJECT_ID in $ENV_FILE}"
: "${SUPABASE_PASSWORD:?Missing SUPABASE_PASSWORD in $ENV_FILE}"

# -- Ask user which environment to sync from --
echo "🧠 Which environment would you like to sync from?"
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

# -- Paths --
FUNCTIONS_DIR="$SCRIPT_DIR/../supabase/functions"
mkdir -p "$FUNCTIONS_DIR"


# -- Link to selected project --
echo "🔗 Linking to $ENVIRONMENT project..."
supabase link --project-ref "$PROJECT_ID" -p "$SUPABASE_PASSWORD" >/dev/null

# -- Get list of deployed functions (parse from CLI table output) --
echo "📡 Fetching deployed functions from $ENVIRONMENT..."
DEPLOYED_FUNCTIONS=$(
  supabase functions list |
  awk '{ print $3 }' |
  grep -E '^[a-zA-Z0-9_-]+$' |
  grep -v -E '^(SLUG|NAME)$'
)

# -- Get list of local functions --
echo "📁 Checking local functions..."
LOCAL_FUNCTIONS=()
for fn_path in "$FUNCTIONS_DIR"/*/; do
  [ -d "$fn_path" ] || continue
  LOCAL_FUNCTIONS+=("$(basename "$fn_path")")
done

if [ ${#LOCAL_FUNCTIONS[@]:-0} -eq 0 ]; then
  echo "⚠️ No local functions found in $FUNCTIONS_DIR"
fi

# -- Compare and find missing ones --
echo "🔍 Comparing $ENVIRONMENT and local functions..."
MISSING_FUNCTIONS=()
for fn in $DEPLOYED_FUNCTIONS; do
  if [[ ! " ${LOCAL_FUNCTIONS[*]:-} " =~ " $fn " ]]; then
    MISSING_FUNCTIONS+=("$fn")
  fi
done

# -- Download any missing functions --
if [ ${#MISSING_FUNCTIONS[@]} -eq 0 ]; then
  echo "✅ All $ENVIRONMENT functions are present locally."
else
  echo "⬇️ Downloading missing functions to supabase/functions/"
  for fn in "${MISSING_FUNCTIONS[@]}"; do
    echo "📥 Downloading: $fn"
    supabase functions download "$fn"
    find "$FUNCTIONS_DIR" -type d -name "file:" -exec rm -rf {} +

  done
  echo "✅ Finished downloading all missing functions."
fi
