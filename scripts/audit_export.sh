#!/usr/bin/env bash
# Exports every ledger entry created in a date range (UTC, both ends inclusive), together with
# its transaction and the FX rate that was used (rate, source, timestamp, corridor).
#   DATABASE_URL=... bash scripts/audit_export.sh --from 2026-10-01 --to 2026-10-31 [--format csv|json] [--out file]
# Output goes to stdout unless --out is given.
set -euo pipefail

: "${DATABASE_URL:?DATABASE_URL is required}"
FROM="" TO="" FORMAT="csv" OUT="/dev/stdout"

usage() {
  echo "usage: audit_export.sh --from YYYY-MM-DD --to YYYY-MM-DD [--format csv|json] [--out file]" >&2
  exit 2
}
while [ $# -gt 0 ]; do
  case "$1" in
    --from) FROM="${2:-}"; shift 2 ;;
    --to) TO="${2:-}"; shift 2 ;;
    --format) FORMAT="${2:-}"; shift 2 ;;
    --out) OUT="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done

date_re='^[0-9]{4}-[0-9]{2}-[0-9]{2}$'
[[ "$FROM" =~ $date_re && "$TO" =~ $date_re ]] || usage
[[ "$FORMAT" == csv || "$FORMAT" == json ]] || usage
[[ ! "$FROM" > "$TO" ]] || { echo "--from must not be after --to" >&2; exit 2; }
[ -n "$OUT" ] || usage

QUERY=$(cat <<'SQL'
SELECT e.id                 AS entry_id,
       e.created_at         AS entry_created_at,
       e.transaction_id,
       t.created_at         AS transaction_created_at,
       e.wallet_id,
       e.currency_code      AS entry_currency,
       e.amount,
       e.entry_type,
       t.status             AS transaction_status,
       t.from_wallet_id,
       t.to_wallet_id,
       t.currency_from,
       t.currency_to,
       t.amount             AS transaction_amount,
       t.converted_amount,
       t.fx_rate,
       t.fx_source,
       t.fx_timestamp,
       t.corridor,
       t.settlement_id,
       t.correlation_id
FROM ledger_entries e
JOIN transactions t ON t.id = e.transaction_id
WHERE e.created_at >= :'from'::date
  AND e.created_at <  (:'to'::date + 1)
ORDER BY e.created_at, e.id
SQL
)

{
  case "$FORMAT" in
    csv) printf 'COPY (\n%s\n) TO STDOUT WITH CSV HEADER;\n' "$QUERY" ;;
    json) printf "SELECT COALESCE(json_agg(row_to_json(q)), '[]'::json) FROM (\n%s\n) q;\n" "$QUERY" ;;
  esac
} | PGOPTIONS='-c timezone=UTC' psql "$DATABASE_URL" -X -q -tA -v ON_ERROR_STOP=1 \
  -v from="$FROM" -v to="$TO" > "$OUT"

[ "$OUT" = /dev/stdout ] || echo "wrote $OUT" >&2