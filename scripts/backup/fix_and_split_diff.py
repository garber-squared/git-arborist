#!/usr/bin/env python3
import re
from pathlib import Path

# ────────────────────────────────────────────────
# CONFIGURATION
# ────────────────────────────────────────────────
MIG_DIR = Path("supabase/migrations")
ORIG = MIG_DIR / "20250616141256_staging_diff.sql"
FIXED = MIG_DIR / "20250616141256_staging_diff_fixed.sql"
CHUNK_SIZE = 1000  # lines per chunk

# ────────────────────────────────────────────────
# STEP 1: Read original
# ────────────────────────────────────────────────
text = ORIG.read_text(encoding="utf-8")

# ────────────────────────────────────────────────
# STEP 2: Inject guards via regex
# ────────────────────────────────────────────────
patterns = [
    # CREATE EXTENSION, TYPE, SCHEMA, TABLE
    (r"^CREATE EXTENSION ", "CREATE EXTENSION IF NOT EXISTS "),
    (r"^CREATE TYPE ", "CREATE TYPE IF NOT EXISTS "),
    (r"^CREATE SCHEMA ", "CREATE SCHEMA IF NOT EXISTS "),
    (r"^CREATE TABLE (ONLY )?", "CREATE TABLE IF NOT EXISTS "),
    # ALTER TABLE ... ADD COLUMN
    (r"^(ALTER TABLE\s+\S+\s+)ADD COLUMN ", r"\1ADD COLUMN IF NOT EXISTS "),
    # CREATE INDEX / UNIQUE INDEX
    (r"^CREATE INDEX ", "CREATE INDEX IF NOT EXISTS "),
    (r"^CREATE UNIQUE INDEX ", "CREATE UNIQUE INDEX IF NOT EXISTS "),
    # primary-key guards: drop if exists, then add
    (
        r'^(ALTER TABLE\s+\S+\s+)ADD CONSTRAINT "([^"]+)_pkey"',
        r'\1DROP CONSTRAINT IF EXISTS "\2_pkey";\n\1ADD CONSTRAINT "\2_pkey"',
    ),
]

for pat, repl in patterns:
    text = re.sub(pat, repl, text, flags=re.MULTILINE)

# ────────────────────────────────────────────────
# STEP 3: Write the fixed file
# ────────────────────────────────────────────────
FIXED.write_text(text, encoding="utf-8")
print(f"✅ Wrote fixed migration to {FIXED}")

# ────────────────────────────────────────────────
# STEP 4: Split into chunks
# ────────────────────────────────────────────────
lines = text.splitlines(keepends=True)
for i in range(0, len(lines), CHUNK_SIZE):
    chunk_lines = lines[i : i + CHUNK_SIZE]
    chunk_file = FIXED.with_name(f"{FIXED.stem}_chunk_{i//CHUNK_SIZE:02d}.sql")
    chunk_file.write_text("".join(chunk_lines), encoding="utf-8")
    print(f"   • chunk {i//CHUNK_SIZE:02d} → {chunk_file}")

print("\n🎯 All done! You now have:")
print(f"    • Full fixed file:        {FIXED}")
print(f"    • {len(lines)//CHUNK_SIZE + 1} chunk files in {MIG_DIR}")
