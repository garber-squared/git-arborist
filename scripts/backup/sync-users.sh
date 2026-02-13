
#!/usr/bin/env bash
set -euo pipefail

# load your .env files for PROD_DB_URL and STAGING_DB_URL

[ -f .env.development.local ]  && source .env.development.local

: "${PROD_DB_URL:?Need PROD_DB_URL in .env.development.local}"
: "${STAGING_DB_URL:?Need STAGING_DB_URL in .env.development.local}"

BACKUP_DIR="supabase/backup"
SCHEMA_DUMP="$BACKUP_DIR/public_schema.sql"
DATA_DUMP="$BACKUP_DIR/public_data.sql"

mkdir -p "$BACKUP_DIR"

echo "📥 Dumping production auth data"
supabase db dump --db-url "$PROD_DB_URL" --data-only --schema auth --file "$BACKUP_DIR/auth_data.sql"


echo "🚀 Restoring auth data to staging"
psql "$STAGING_DB_URL" --set ON_ERROR_STOP=on -f "$BACKUP_DIR/auth_data.sql"

# Recreate handle_new_user trigger
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

echo "✅ Done! Staging is now in sync with Production's public schema and data."
