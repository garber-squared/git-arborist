#!/usr/bin/env bash
export PGPASSWORD='sideby2025!!!'

# 1. Dump schemas
pg_dump -h db.upffcxqiozqhdgfesmji.supabase.co -U postgres -d postgres --schema-only --no-owner --no-privileges > main_schema.sql
pg_dump -h db.uzyvbvcutbaueqiavbuj.supabase.co -U postgres -d postgres --schema-only --no-owner --no-privileges > staging_schema.sql

echo "=== SCHEMA DIFF ==="
diff -u main_schema.sql staging_schema.sql | sed 's/^/    /'

# 2. Compare row counts
echo; echo "=== ROW COUNTS ==="
tables=$(psql -h db.upffcxqiozqhdgfesmji.supabase.co -U postgres -d postgres -t -A -c "SELECT tablename FROM pg_tables WHERE schemaname='public';")
printf "Table | Main | Staging\n----- | ---- | -------\n"
for t in $tables; do
  m=$(psql -h db.upffcxqiozqhdgfesmji.supabase.co -U postgres -d postgres -t -A -c "SELECT count(*) FROM public.\"$t\";")
  s=$(psql -h db.uzyvbvcutbaueqiavbuj.supabase.co -U postgres -d postgres -t -A -c "SELECT count(*) FROM public.\"$t\";")
  printf "%s | %s | %s\n" "$t" "$m" "$s"
done
