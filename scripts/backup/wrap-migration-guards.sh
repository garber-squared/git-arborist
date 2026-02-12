#!/bin/bash

file="$1"
tmpfile="${file}.tmp"

inside_enum=0
enum_lines=""
enum_type=""
schema=""
line_number=0
start_line=0

{
while IFS= read -r line; do
  ((line_number++))

  # ─────────────────────────────────────────────────────────
  # ENUMS
  if [[ $inside_enum -eq 0 && "$line" =~ ^CREATE\ TYPE\ \"([a-zA-Z0-9_]+)\"\.\"([a-zA-Z0-9_]+)\"\ AS\ ENUM ]]; then
    schema="${BASH_REMATCH[1]}"
    enum_type="${BASH_REMATCH[2]}"
    inside_enum=1
    enum_lines="$line"
    start_line=$line_number

    if [[ "$line" =~ \)\;[[:space:]]*$ ]]; then
      inside_enum=0
      echo "-- 🛡️ Wrapped enum '${enum_type}' with IF NOT EXISTS guard (line $start_line)"
      echo "DO \$\$"
      echo "BEGIN"
      echo "  IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = '${enum_type}') THEN"
      echo "    $enum_lines"
      echo "  END IF;"
      echo "END\$\$;"
    fi
    continue
  elif [[ $inside_enum -eq 1 ]]; then
    enum_lines="$enum_lines"$'\n'"$line"

    if [[ "$line" =~ \)\;[[:space:]]*$ ]]; then
      inside_enum=0
      echo "-- 🛡️ Wrapped enum '${enum_type}' with IF NOT EXISTS guard (starting at line $start_line)"
      echo "DO \$\$"
      echo "BEGIN"
      echo "  IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = '${enum_type}') THEN"
      echo "$enum_lines"
      echo "  END IF;"
      echo "END\$\$;"
    fi
    continue
  fi

  # ─────────────────────────────────────────────────────────
  # PRIMARY KEY constraints
  if [[ "$line" =~ ^ALTER\ TABLE\ ONLY\ \"([a-zA-Z0-9_]+)\"\.\"([a-zA-Z0-9_]+)\".*ADD\ CONSTRAINT\ \"([^\"]+)\"\ PRIMARY\ KEY ]]; then
    schema="${BASH_REMATCH[1]}"
    table="${BASH_REMATCH[2]}"
    constraint="${BASH_REMATCH[3]}"
    echo "-- 🛡️ Wrapped primary key constraint '$constraint' on ${schema}.${table} with IF NOT EXISTS (line $line_number)"
    echo "DO \$\$"
    echo "BEGIN"
    echo "  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = '${constraint}') THEN"
    echo "    $line"
    echo "  END IF;"
    echo "END\$\$;"
    continue
  fi

  # ─────────────────────────────────────────────────────────
  # FOREIGN KEY constraints
  if [[ "$line" =~ ^ALTER\ TABLE\ ONLY\ \"([a-zA-Z0-9_]+)\"\.\"([a-zA-Z0-9_]+)\".*ADD\ CONSTRAINT\ \"([^\"]+)\"\ FOREIGN\ KEY ]]; then
    schema="${BASH_REMATCH[1]}"
    table="${BASH_REMATCH[2]}"
    constraint="${BASH_REMATCH[3]}"
    echo "-- 🛡️ Wrapped foreign key constraint '$constraint' on ${schema}.${table} with IF NOT EXISTS (line $line_number)"
    echo "DO \$\$"
    echo "BEGIN"
    echo "  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = '${constraint}') THEN"
    echo "    $line"
    echo "  END IF;"
    echo "END\$\$;"
    continue
  fi

  # ─────────────────────────────────────────────────────────
  # Default: print line unchanged
  echo "$line"
done
} < "$file" > "$tmpfile" && mv "$tmpfile" "$file"

echo "✅ Finished processing $file"
