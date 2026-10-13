#!/usr/bin/env bash
set -euo pipefail

: "${DATABASE_URL:?DATABASE_URL is required}"

DIR="$(cd "$(dirname "$0")/../sql/migrations" && pwd)"

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -q -c \
  "CREATE TABLE IF NOT EXISTS schema_migrations (
     filename text PRIMARY KEY,
     applied_at timestamptz NOT NULL DEFAULT now()
   )"

for f in "$DIR"/*.sql; do
  name="$(basename "$f")"
  applied="$(psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -tAc \
    "SELECT 1 FROM schema_migrations WHERE filename = '$name'")"
  if [ -n "$applied" ]; then
    echo "skip   $name"
    continue
  fi
  echo "apply  $name"
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -q -1 \
    -f "$f" \
    -c "INSERT INTO schema_migrations (filename) VALUES ('$name')"
done

echo "migrations up to date"