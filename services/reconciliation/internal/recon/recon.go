package recon

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Each check selects one row per mismatch as (check_name, expected, actual, 'mismatch', details).
// "expected" is the value derived from source records; "actual" is the stored value.
type check struct {
	name  string
	query string
}

const insertPrefix = `INSERT INTO reconciliation_logs (check_name, expected, actual, status, details) `

var checks = []check{
	{
		// wallets.balance must equal the sum of the wallet's ledger entries.
		name: "wallet_balance_matches_entries",
		query: `SELECT $1::text, COALESCE(SUM(e.amount), 0), w.balance, 'mismatch',
		               jsonb_build_object('wallet_id', w.id, 'currency', w.currency_code)
		        FROM wallets w LEFT JOIN ledger_entries e ON e.wallet_id = w.id
		        GROUP BY w.id
		        HAVING w.balance <> COALESCE(SUM(e.amount), 0)`,
	},
	{
		// every transaction's entries must sum to zero within each currency.
		name: "transaction_entries_balance",
		query: `SELECT $1::text, 0, SUM(amount), 'mismatch',
		               jsonb_build_object('transaction_id', transaction_id, 'currency', currency_code)
		        FROM ledger_entries
		        GROUP BY transaction_id, currency_code
		        HAVING SUM(amount) <> 0`,
	},
	{
		// a settlement's net amount must equal the net recomputed from its transactions
		// (same formula the settlement worker uses).
		name: "settlement_net_matches_transactions",
		query: `SELECT $1::text,
		               COALESCE(SUM(CASE WHEN t.currency_from = split_part(s.corridor, '-', 1)
		                                 THEN t.amount ELSE -t.converted_amount END), 0),
		               s.net_amount, 'mismatch',
		               jsonb_build_object('settlement_id', s.id, 'corridor', s.corridor)
		        FROM settlements s LEFT JOIN transactions t ON t.settlement_id = s.id
		        GROUP BY s.id
		        HAVING s.net_amount <> COALESCE(SUM(CASE WHEN t.currency_from = split_part(s.corridor, '-', 1)
		                                                 THEN t.amount ELSE -t.converted_amount END), 0)`,
	},
}

type Result struct {
	Check      string
	Mismatches int64
}

// Run executes every check and writes the outcome to reconciliation_logs:
// one 'mismatch' row per problem found, or a single 'ok' row for a clean check.
func Run(ctx context.Context, pool *pgxpool.Pool) ([]Result, error) {
	results := make([]Result, 0, len(checks))
	for _, c := range checks {
		tag, err := pool.Exec(ctx, insertPrefix+c.query, c.name)
		if err != nil {
			return results, err
		}
		if tag.RowsAffected() == 0 {
			if _, err := pool.Exec(ctx,
				`INSERT INTO reconciliation_logs (check_name, status) VALUES ($1, 'ok')`, c.name); err != nil {
				return results, err
			}
		}
		results = append(results, Result{Check: c.name, Mismatches: tag.RowsAffected()})
	}
	return results, nil
}