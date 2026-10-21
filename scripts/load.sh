#!/usr/bin/env bash
# Load test: starts the whole stack on a throwaway database, seeds 300 wallets, runs k6
# (in Docker) against the gateway, then runs reconciliation to prove the ledger is still
# consistent. Results are written to docs/load-test-results/summary.json.
#   bash scripts/load.sh                       # 100 rps for 30s per scenario
#   RATE=200 DURATION=60s bash scripts/load.sh
set -euo pipefail
cd "$(dirname "$0")/.."

# Values in .env.e2e must win over anything already exported in your shell (for example from
# `source .env`): docker compose gives shell variables priority over --env-file.
set -a
. ./.env.e2e
set +a

DC="docker compose -p remit-load --env-file .env.e2e -f docker-compose.prod.yml -f docker-compose.e2e.yml"
DB="postgres://remit:remit@localhost:55432/remit?sslmode=disable"
RATE="${RATE:-100}"
DURATION="${DURATION:-30s}"

cleanup() { $DC down -v --remove-orphans >/dev/null 2>&1 || true; }
trap cleanup EXIT

[ -f certs/ca.crt ] || bash scripts/gen_dev_certs.sh >/dev/null
mkdir -p docs/load-test-results

# Pull the k6 image first (with retries) so a flaky registry connection fails fast,
# before the stack is built and started.
for attempt in 1 2 3 4 5; do
  docker pull grafana/k6 && break
  [ "$attempt" = 5 ] && { echo "could not pull grafana/k6" >&2; exit 1; }
  sleep 3
done

$DC up -d postgres elasticmq
for _ in $(seq 60); do psql "$DB" -c "select 1" >/dev/null 2>&1 && break; sleep 1; done
DATABASE_URL="$DB" bash scripts/run_migrations.sh >/dev/null
psql "$DB" -v ON_ERROR_STOP=1 -q -f sql/seed/seed.sql
psql "$DB" -v ON_ERROR_STOP=1 -q -f sql/seed/load.sql
# The default limit (100 requests/minute per key) would reject the load test.
export RATE_LIMIT_MAX=1000000
$DC up -d --build --wait
API_KEY=$(DATABASE_URL="$DB" bash scripts/create_api_key.sh 2>/dev/null)
echo "stack is up; running k6 (RATE=$RATE DURATION=$DURATION)"

set +e
docker run --rm --network host --user "$(id -u):$(id -g)" \
  -v "$PWD/scripts/load:/scripts:ro,z" -v "$PWD/docs/load-test-results:/out:z" \
  -e BASE_URL=http://localhost:3300 -e API_KEY="$API_KEY" -e RATE="$RATE" -e DURATION="$DURATION" \
  grafana/k6 run --summary-export=/out/summary.json /scripts/transfer.js
K6_STATUS=$?
set -e

echo
echo "transfers created:     $(psql "$DB" -tAc "select count(*) from transactions where from_wallet_id::text like '10000000-%'")"
echo "settled / in flight:   $(psql "$DB" -tAc "select status || '=' || count(*) from transactions group by status order by status" | tr '\n' ' ')"

echo "waiting for settlement to catch up, then reconciling..."
sleep 15
if $DC run --rm --no-deps reconciliation -once >/dev/null 2>&1; then
  echo "reconciliation: OK (no mismatches after the load test)"
else
  echo "reconciliation: MISMATCHES FOUND" >&2
  exit 1
fi

exit "$K6_STATUS"