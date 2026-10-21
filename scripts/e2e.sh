#!/usr/bin/env bash
# End-to-end test: starts the whole stack on a throwaway database, sends real requests
# through the gateway, and checks the results in Postgres. Needs docker, psql, curl, openssl, python3.
#   bash scripts/e2e.sh
set -euo pipefail
cd "$(dirname "$0")/.."

# Values in .env.e2e must win over anything already exported in your shell (for example from
# `source .env`): docker compose gives shell variables priority over --env-file.
set -a
. ./.env.e2e
set +a

DC="docker compose -p remit-e2e --env-file .env.e2e -f docker-compose.prod.yml -f docker-compose.e2e.yml"
DB="postgres://remit:remit@localhost:55432/remit?sslmode=disable"
API="http://localhost:3300"
A=aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa # seeded USD wallet (1000)
B=bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb # seeded NGN wallet (0)

ok() { echo "ok   $*"; }
fail() {
  echo "FAIL $*" >&2
  $DC ps >&2 || true
  $DC logs --tail 40 ledger api-gateway fx-service >&2 || true
  exit 1
}
q() { psql "$DB" -tAc "$1"; }
expect() { # description, expected, actual
  if [ "$2" = "$3" ]; then ok "$1"; else fail "$1 (expected '$2', got '$3')"; fi
}
wait_for() { # description, seconds, command...
  local desc="$1" secs="$2"
  shift 2
  for _ in $(seq "$secs"); do
    if "$@" >/dev/null 2>&1; then ok "$desc"; return; fi
    sleep 1
  done
  fail "$desc (timed out after ${secs}s)"
}
cleanup() { $DC down -v --remove-orphans >/dev/null 2>&1 || true; }
trap cleanup EXIT

# --- start infrastructure, migrate, seed -------------------------------------------------
[ -f certs/ca.crt ] || bash scripts/gen_dev_certs.sh >/dev/null
$DC up -d postgres elasticmq
wait_for "postgres is ready" 60 psql "$DB" -c "select 1"
DATABASE_URL="$DB" bash scripts/run_migrations.sh >/dev/null
psql "$DB" -v ON_ERROR_STOP=1 -q -f sql/seed/seed.sql
ok "migrated and seeded"
API_KEY=$(DATABASE_URL="$DB" bash scripts/create_api_key.sh 2>/dev/null)
ok "created an API key"

# --- start the services --------------------------------------------------------------------
$DC up -d --build --wait || fail "the stack did not become healthy"
ok "all services healthy"

expect "/health needs no key" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$API/health")"
expect "request without an API key returns 401" 401 "$(curl -s -o /dev/null -w '%{http_code}' "$API/v1/wallets/$A")"
expect "request with a wrong API key returns 401" 401 \
  "$(curl -s -o /dev/null -w '%{http_code}' -H 'X-API-Key: rk_wrong' "$API/v1/wallets/$A")"

# Rate limit: 100 requests per minute per key. A second key gets its own counter.
KEY2=$(DATABASE_URL="$DB" bash scripts/create_api_key.sh 2>/dev/null)
ALLOWED=0
LIMITED=0
for _ in $(seq 110); do
  c=$(curl -s -o /dev/null -w '%{http_code}' -H "X-API-Key: $KEY2" "$API/v1/wallets/$A")
  case "$c" in 200) ALLOWED=$((ALLOWED + 1)) ;; 429) LIMITED=$((LIMITED + 1)) ;; esac
done
expect "100 requests are allowed in the window" 100 "$ALLOWED"
expect "the next 10 are rejected with 429" 10 "$LIMITED"
if curl -s -D - -o /dev/null -H "X-API-Key: $KEY2" "$API/v1/wallets/$A" | grep -qi '^retry-after:'; then
  ok "429 responses carry Retry-After"
else
  fail "429 response has no Retry-After header"
fi
expect "the main key is unaffected by the other key's limit" 200 \
  "$(curl -s -o /dev/null -w '%{http_code}' -H "X-API-Key: $API_KEY" "$API/v1/wallets/$A")"

post() { # idempotency key, json body -> "<body>\n<status>"
  curl -s -w '\n%{http_code}' -X POST "$API/v1/transfers" \
    -H 'content-type: application/json' -H "X-API-Key: $API_KEY" -H "Idempotency-Key: $1" -d "$2"
}
body_of() { printf '%s' "${1%$'\n'*}"; }
code_of() { printf '%s' "${1##*$'\n'}"; }

# --- a cross-currency transfer ---------------------------------------------------------------
KEY="e2e-$(date +%s)"
PAYLOAD="{\"from_wallet_id\":\"$A\",\"to_wallet_id\":\"$B\",\"amount\":\"10\",\"currency\":\"USD\"}"
out=$(post "$KEY" "$PAYLOAD")
expect "transfer returns 201" 201 "$(code_of "$out")"
TX=$(body_of "$out" | sed -E 's/.*"id":"([^"]+)".*/\1/')
[ -n "$TX" ] || fail "no transaction id in response"
body_of "$out" | grep -q '"status":"posted"' && ok "response status is posted" || fail "response status is not posted"

expect "converted amount is 15000 NGN" t "$(q "select converted_amount = 15000 and currency_to = 'NGN' from transactions where id = '$TX'")"
expect "4 ledger entries written" 4 "$(q "select count(*) from ledger_entries where transaction_id = '$TX'")"
expect "entries sum to zero in every currency" 0 \
  "$(q "select count(*) from (select 1 from ledger_entries where transaction_id = '$TX' group by currency_code having sum(amount) <> 0) x")"
expect "sender balance is 990" t "$(q "select balance = 990 from wallets where id = '$A'")"
expect "receiver balance is 15000" t "$(q "select balance = 15000 from wallets where id = '$B'")"

# --- idempotency and errors ----------------------------------------------------------------------
out=$(post "$KEY" "$PAYLOAD")
expect "replay returns 201" 201 "$(code_of "$out")"
expect "replay returns the same transaction" "$TX" "$(body_of "$out" | sed -E 's/.*"id":"([^"]+)".*/\1/')"
expect "replay created no second transaction" 1 "$(q "select count(*) from transactions where corridor is not null")"

out=$(post "$KEY" "{\"from_wallet_id\":\"$A\",\"to_wallet_id\":\"$B\",\"amount\":\"11\",\"currency\":\"USD\"}")
expect "same key with a different payload returns 409" 409 "$(code_of "$out")"
out=$(post "e2e-bad" '{"from_wallet_id":"abc","amount":"-5","currency":"usd"}')
expect "invalid body returns 400" 400 "$(code_of "$out")"
out=$(post "e2e-overdraw" "{\"from_wallet_id\":\"$A\",\"to_wallet_id\":\"$B\",\"amount\":\"999999\",\"currency\":\"USD\"}")
expect "insufficient funds returns 422" 422 "$(code_of "$out")"
out=$(post "e2e-missing" "{\"from_wallet_id\":\"$A\",\"to_wallet_id\":\"99999999-9999-9999-9999-999999999999\",\"amount\":\"1\",\"currency\":\"USD\"}")
expect "unknown wallet returns 404" 404 "$(code_of "$out")"
expect "rejected transfers changed nothing" t "$(q "select balance = 990 from wallets where id = '$A'")"

expect "GET transaction returns 200" 200 "$(curl -s -o /dev/null -w '%{http_code}' -H "X-API-Key: $API_KEY" "$API/v1/transactions/$TX")"
expect "GET wallet returns 200" 200 "$(curl -s -o /dev/null -w '%{http_code}' -H "X-API-Key: $API_KEY" "$API/v1/wallets/$A")"
expect "GET fx returns 200" 200 "$(curl -s -o /dev/null -w '%{http_code}' -H "X-API-Key: $API_KEY" "$API/v1/fx?from=USD&to=NGN")"

# --- asynchronous parts: outbox -> SQS, then settlement --------------------------------------------
outbox_published() { [ "$(q "select count(*) from outbox_events where published_at is not null")" -ge 1 ]; }
wait_for "outbox event published to the queue" 30 outbox_published

tx_settled() { [ "$(q "select status from transactions where id = '$TX'")" = settled ]; }
wait_for "transaction netted and settled by the worker" 60 tx_settled
expect "settlement is settled" settled "$(q "select s.status from settlements s join transactions t on t.settlement_id = s.id where t.id = '$TX'")"

# --- audit export ---------------------------------------------------------------------------------------
EXPORT_COLUMNS="entry_id,entry_created_at,transaction_id,transaction_created_at,wallet_id,entry_currency,amount,entry_type,transaction_status,from_wallet_id,to_wallet_id,currency_from,currency_to,transaction_amount,converted_amount,fx_rate,fx_source,fx_timestamp,corridor,settlement_id,correlation_id"
ENTRY_COUNT=$(q "select count(*) from ledger_entries")

csv=$(DATABASE_URL="$DB" bash scripts/audit_export.sh --from 2000-01-01 --to 2999-12-31 --format csv)
expect "CSV export has the expected columns" "$EXPORT_COLUMNS" "$(printf '%s\n' "$csv" | head -1)"
expect "CSV export has one row per ledger entry" "$ENTRY_COUNT" "$(( $(printf '%s\n' "$csv" | wc -l) - 1 ))"

JSON_FILE=$(mktemp)
DATABASE_URL="$DB" bash scripts/audit_export.sh --from 2000-01-01 --to 2999-12-31 --format json --out "$JSON_FILE" 2>/dev/null
if python3 - "$JSON_FILE" "$TX" "$ENTRY_COUNT" "$EXPORT_COLUMNS" <<'PY'
import json, sys
path, tx, count, columns = sys.argv[1], sys.argv[2], int(sys.argv[3]), sys.argv[4].split(",")
rows = json.load(open(path))
assert len(rows) == count, f"{len(rows)} rows, expected {count}"
assert all(list(r.keys()) == columns for r in rows), "unexpected columns"
mine = [r for r in rows if r["transaction_id"] == tx]
assert len(mine) == 4, f"{len(mine)} entries for the transfer, expected 4"
assert all(float(r["fx_rate"]) == 1500 and r["fx_source"] == "static" and r["corridor"] == "NGN-USD" for r in mine), "FX details missing"
assert abs(sum(float(r["amount"]) for r in mine if r["entry_currency"].strip() == "NGN")) < 1e-9, "NGN entries do not net to zero"
PY
then ok "JSON export has the expected schema and FX details"; else fail "JSON export failed validation"; fi
rm -f "$JSON_FILE"
expect "an empty date range exports only the header" 1 "$(DATABASE_URL="$DB" bash scripts/audit_export.sh --from 2000-01-01 --to 2000-01-02 | wc -l)"

# --- reconciliation ------------------------------------------------------------------------------------
if $DC run --rm --no-deps reconciliation -once >/dev/null 2>&1; then ok "reconciliation found no mismatches"; else fail "reconciliation reported mismatches"; fi
expect "no mismatch rows logged" 0 "$(q "select count(*) from reconciliation_logs where status = 'mismatch'")"

echo
echo "E2E PASSED"