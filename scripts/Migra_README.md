
# Schema Sync with Migra

This project uses [Migra](https://github.com/djrobstep/migra) to sync schema changes from **staging** to **production** in a Supabase Postgres setup — without relying on migration history.

## 🚀 What is Migra?

**Migra** is a powerful PostgreSQL schema diff tool that compares two databases and outputs the SQL needed to make one match the other. It’s ideal for applying schema changes from staging to production when you don’t need or want traditional migration tracking.

---

## ✅ Setup (macOS with pyenv)

1. **Activate a Python version with `pip`**
   ```bash
   pyenv global 3.12.6
   ```

2. **Install dependencies**
   ```bash
   pip install migra psycopg2-binary
   ```

---

## 🛠 How to Use

### 1. Set your connection strings

Replace these with your actual Supabase credentials:

```bash
export PROD_URL='postgres://postgres:<password>@<prod-host>:5432/postgres'
export STAGE_URL='postgres://postgres:<password>@<stage-host>:5432/postgres'
```

Use single quotes to avoid issues with special characters in your password.

---

### 2. Generate the schema diff

```bash
migra "$PROD_URL" "$STAGE_URL" > schema_diff.sql
```

- Compares **production** (left) with **staging** (right)
- Outputs SQL to bring prod up to date with staging
- Default behavior is **non-destructive** (won’t drop anything)

---

### 3. Review and apply the SQL

**Inspect `schema_diff.sql` carefully.** If safe:

```bash
psql "$PROD_URL" -1 -f schema_diff.sql
```

- The `-1` flag wraps everything in a single transaction.
- If anything fails, no changes are applied.

---

## ⚠️ Safety Tips

- **Back up production** before applying changes.
- Migra may suggest `DROP` statements if used with `--unsafe` — avoid unless intentional.
- Be cautious with:
  - `DROP TABLE`
  - `NOT NULL` columns on non-empty tables
  - New constraints that might conflict with existing data

---

## ✅ Good for

- Keeping staging and production schemas aligned
- Lightweight environments that don’t need formal migration history
- Automating schema updates in CI/CD

---

## 📚 References

- https://github.com/djrobstep/migra
- https://supabase.com/docs/guides/cli/db/migration-diff
