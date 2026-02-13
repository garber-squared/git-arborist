#!/bin/bash

file="$1"
tmpfile="${file}.tmp"

inside_enum=0
enum_lines=""
enum_type=""
schema=""
line_number=0
start_line=0

while IFS= read -r line; do
  ((line_number++))
  if [[ $inside_enum -eq 0 && "$line" =~ ^CREATE\ TYPE\ \"([a-zA-Z0-9_]+)\"\.\"([a-zA-Z0-9_]+)\"\ AS\ ENUM ]]; then
    schema="${BASH_REMATCH[1]}"
    enum_type="${BASH_REMATCH[2]}"
    inside_enum=1
    enum_lines="$line"
    start_line=$line_number
    [[ "$line" =~ \)\;[[:space:]]*$ ]] && inside_enum=0
    continue
  elif [[ $inside_enum -eq 1 ]]; then
    enum_lines="$enum_lines"$'\n'"$line"
    [[ "$line" =~ \)\;[[:space:]]*$ ]] && inside_enum=0
    if [[ $inside_enum -eq 0 ]]; then
      echo "⚠️  Wrapped enum '$enum_type' starting at line $start_line"
      echo "DO \$\$"
      echo "BEGIN"
      echo "  IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = '${enum_type}') THEN"
      echo "    ${enum_lines}"
      echo "  END IF;"
      echo "END\$\$;"
    fi
    continue
  fi

  echo "$line"
done < "$file" > "$tmpfile" && mv "$tmpfile" "$file"

echo "✅ Finished processing $file"
