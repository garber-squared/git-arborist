#!/usr/bin/env bash

set -euo pipefail

# Load environment variables
if [[ -z "${ENV_PATH:-}" ]]; then
  echo "ENV_PATH not set. Usage: ENV_PATH=.env.development.local ./scripts/update-production-schema.sh"
  exit 1
fi

# Load DB URLs from .env file
export $(grep -E '^PROD_DB_URL=' "$ENV_PATH")
export $(grep -E '^STAGING_DB_URL=' "$ENV_PATH")

# Check for required env vars
if [[ -z "${PROD_DB_URL:-}" || -z "${STAGING_DB_URL:-}" ]]; then
  echo "Error: PROD_DB_URL and STAGING_DB_URL must be set in $ENV_PATH"
  exit 1
fi

# Create temporary file for the schema diff
SCHEMA_DIFF_FILE="$(mktemp -t schema_diff.XXXXXX.sql)"

# Run Migra to diff staging (target) vs production (source)
echo "Generating schema diff from staging to production..."
migra "$PROD_DB_URL" "$STAGING_DB_URL" > "$SCHEMA_DIFF_FILE"

# If the diff is empty, exit
if [[ ! -s "$SCHEMA_DIFF_FILE" ]]; then
  echo "✅ Schemas are already in sync. No changes needed."
  rm "$SCHEMA_DIFF_FILE"
  exit 0
fi

# Preview the diff
echo "🔍 Review schema diff:"
cat "$SCHEMA_DIFF_FILE"

echo ""
read -p "⚠️ Apply these changes to production? (y/N): " confirm
if [[ "$confirm" != "y" && "$confirm" != "Y" ]]; then
  echo "❌ Aborted."
  rm "$SCHEMA_DIFF_FILE"
  exit 1
fi

# Apply the diff with psql in a transaction
echo "🚀 Applying schema diff to production..."
psql "$PROD_DB_URL" -1 -f "$SCHEMA_DIFF_FILE"

echo "✅ Production schema updated successfully."
rm "$SCHEMA_DIFF_FILE"
