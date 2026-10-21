#!/usr/bin/env bash
# Creates an API key and stores only its SHA-256 hash in api_keys.
# The key is printed to stdout once and cannot be recovered afterwards.
#   DATABASE_URL=... bash scripts/create_api_key.sh [owner-uuid]
set -euo pipefail

: "${DATABASE_URL:?DATABASE_URL is required}"
OWNER="${1:-}"

KEY="rk_$(openssl rand -hex 24)"
HASH="$(printf '%s' "$KEY" | sha256sum | cut -d' ' -f1)"

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -q -v hash="$HASH" -v owner="$OWNER" <<'SQL'
INSERT INTO api_keys (owner_id, key_hash)
VALUES (COALESCE(NULLIF(:'owner', '')::uuid, gen_random_uuid()), :'hash');
SQL

echo "created API key (store it now; it cannot be shown again)" >&2
echo "$KEY"