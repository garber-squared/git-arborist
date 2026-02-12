#!/usr/bin/env bash
set -euo pipefail

# ┌──────────────────────────────────────────────┐
# │  sync-schema.sh                              │
# │                                              │
# │  1) Captures schema diff from staging → prod │
# │  2) Outputs non-destructive SQL via Migra    │
# │  3) Helps you apply safely to production     │
# └──────────────────────────────────────────────┘

ENV_FILE="${ENV_PATH:-$(dirname "$0")/../.env.development.local}"

# 1) Load ENV (optional .env)
if [ -f "$ENV_FILE" ]; then
  echo "📄 Loading environment from $ENV_FILE"
  set -o allexport
  source "$ENV_FILE"
  set +o allexport
else
  echo "❌ Could not find $ENV_FILE"
  exit 1
fi

# 2) Required ENV vars
: "${PROD_DB_URL:?Need PROD_DB_URL (postgres connection string for production)}"
: "${STAGING_DB_URL:?Need STAGING_DB_URL (postgres connection string for staging)}"
: "${MIGRATIONS_DIR:=supabase/migrations}"

# 3) Timestamp & output path
TIMESTAMP=$(date +%Y%m%d%H%M%S)
OUTFILE="${MIGRATIONS_DIR}/${TIMESTAMP}_migra_diff.sql"

# 4) Generate schema diff using Migra (non-destructive by default)
echo
echo "🔍 Generating schema diff with Migra"
echo "     • From:   Production"
echo "     • To:     Staging"
echo "     • Output: ${OUTFILE}"

migra "$PROD_DB_URL" "$STAGING_DB_URL" > "$OUTFILE" --unsafe

echo
echo "✅ Schema diff saved to ${OUTFILE}"
echo "   👉 Please review before applying to production."

cat <<EOF

🛠 NEXT STEPS:

  1) Review & edit: ${OUTFILE}
     - Confirm intended schema changes.
     - Consider backing up production if running destructive SQL.
     - For large operations (e.g., indexing), remove -1 and run manually.

  2) Apply to production:
       psql "\${PROD_DB_URL}" -1 -f ${OUTFILE}

  3) Commit if needed:
       git add ${OUTFILE}
       git commit -m "chore: apply schema sync from staging @ ${TIMESTAMP}"

EOF
