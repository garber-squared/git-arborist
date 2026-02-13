
#!/usr/bin/env bash
set -euo pipefail

# load your .env files for PROD_DB_URL and STAGING_DB_URL

[ -f .env.development.local ]  && source .env.development.local

: "${PROD_DB_URL:?Need PROD_DB_URL in .env.development.local}"
: "${STAGING_DB_URL:?Need STAGING_DB_URL in .env.development.local}"

BACKUP_DIR="supabase/backup"
NOW=$(date +%Y%m%d_%H%M%S)
FULL_DUMP="$BACKUP_DIR/public_full_$NOW.sql"



mkdir -p "$BACKUP_DIR"

# 1. Clean staging public schema
echo "🔴 Cleaning OUT staging public schema"
psql "$STAGING_DB_URL" \
  --set ON_ERROR_STOP=on \
  -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public AUTHORIZATION postgres;"

# 2. Recreate extensions
echo "📦 (Re)creating extensions in public"
psql "$STAGING_DB_URL" --set ON_ERROR_STOP=on <<'SQL'
    CREATE EXTENSION IF NOT EXISTS plpgsql WITH SCHEMA public;
    CREATE EXTENSION IF NOT EXISTS pg_stat_statements WITH SCHEMA public;
    CREATE EXTENSION IF NOT EXISTS "uuid-ossp" WITH SCHEMA public;
    CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;
    CREATE EXTENSION IF NOT EXISTS pgjwt WITH SCHEMA public;
    CREATE EXTENSION IF NOT EXISTS pg_cron;
    CREATE EXTENSION IF NOT EXISTS pgmq;
    CREATE EXTENSION IF NOT EXISTS pgsodium;
    CREATE EXTENSION IF NOT EXISTS supabase_vault;
    CREATE EXTENSION IF NOT EXISTS pg_graphql;
    CREATE EXTENSION IF NOT EXISTS vector;
    CREATE EXTENSION IF NOT EXISTS pg_net;
    CREATE EXTENSION IF NOT EXISTS http;
SQL

# 3. Dump and restore public schema and data
echo "📥 Dumping full PROD public schema + data → $FULL_DUMP"
pg_dump "$PROD_DB_URL" \
  --schema=public \
  --no-owner \
  --no-privileges \
  --format=plain \
  | sed '/^CREATE SCHEMA public;/d' > "$FULL_DUMP"


echo "🚀 Restoring full dump to STAGING"
psql "$STAGING_DB_URL" \
  --set ON_ERROR_STOP=on \
  -f "$FULL_DUMP"

# 4. Dump and restore auth  data

echo "📥 Dumping production auth data"
supabase db dump --db-url "$PROD_DB_URL" --data-only --schema auth --file "$BACKUP_DIR/auth_data.sql"

echo "🚀 Restoring auth data to staging"
psql "$STAGING_DB_URL" --set ON_ERROR_STOP=on -f "$BACKUP_DIR/auth_data.sql"

# 5. Recreate handle_new_user trigger
echo "🔄 Creating handle_new_user trigger for auth-profile sync"
psql "$STAGING_DB_URL" \
  --set ON_ERROR_STOP=on \
  -c "
    -- Check if trigger exists and drop if it does
    DO \$\$
    BEGIN
      IF EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'on_auth_user_created') THEN
        DROP TRIGGER on_auth_user_created ON auth.users;
      END IF;
    END \$\$;

    -- Create or replace the trigger function
    CREATE OR REPLACE FUNCTION public.handle_new_user()
    RETURNS trigger
    LANGUAGE plpgsql
    SECURITY DEFINER
    SET search_path = 'public'
    AS \$\$
    BEGIN
      -- Insert profile with metadata from auth.users
      INSERT INTO public.profiles (
        id, 
        email, 
        first_name, 
        last_name, 
        onboarding_completed
      )
      VALUES (
        NEW.id,
        NEW.email,
        NEW.raw_user_meta_data->>'first_name',
        NEW.raw_user_meta_data->>'last_name',
        false
      )
      ON CONFLICT (id) DO NOTHING;
      
      RETURN NEW;
    END;
    \$\$;

    -- Add trigger to auth.users
    CREATE TRIGGER on_auth_user_created
      AFTER INSERT ON auth.users
      FOR EACH ROW EXECUTE PROCEDURE public.handle_new_user();
  "

# 6. Sync edge functions
echo "🔁 Syncing edge functions from production to staging"
for fn in $(supabase functions list --project-ref "$PROD_PROJECT_REF" | tail -n +2 | awk '{print $1}'); do
  echo "📥 Downloading function: $fn"
  supabase functions download "$fn" --project-ref "$PROD_PROJECT_REF" --output "supabase/functions/$fn"
  echo "🚀 Deploying function: $fn to staging"
  supabase functions deploy "$fn" --project-ref "$STAGING_PROJECT_REF"
done


echo "✅ Done! Staging is now in sync with Production's public schema and data."
