package recon

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"remit-platform/reconciliation/internal/testdb"
)

const (
	checkBalance    = "wallet_balance_matches_entries"
	checkEntries    = "transaction_entries_balance"
	checkSettlement = "settlement_net_matches_transactions"
)

func wallet(t *testing.T, pool *pgxpool.Pool, currency string, system bool) string {
	t.Helper()
	return testdb.Text(t, pool,
		`INSERT INTO wallets (owner_id, currency_code, is_system) VALUES (gen_random_uuid(), $1, $2) RETURNING id::text`,
		currency, system)
}

// A consistent ledger: a transfer of 10 USD from a system wallet to a user wallet,
// with matching entries and balances. Returns the user wallet and the transaction id.
func consistentLedger(t *testing.T) (pool *pgxpool.Pool, user, txID string) {
	t.Helper()
	pool = testdb.New(t)
	system := wallet(t, pool, "USD", true)
	user = wallet(t, pool, "USD", false)
	txID = testdb.Text(t, pool,
		`INSERT INTO transactions (from_wallet_id, to_wallet_id, amount, currency_from, currency_to, converted_amount)
		 VALUES ($1, $2, 10, 'USD', 'USD', 10) RETURNING id::text`, system, user)
	testdb.Exec(t, pool,
		`INSERT INTO ledger_entries (wallet_id, transaction_id, amount, currency_code, entry_type)
		 VALUES ($1, $3, -10, 'USD', 'transfer_debit'), ($2, $3, 10, 'USD', 'transfer_credit')`,
		system, user, txID)
	testdb.Exec(t, pool, `UPDATE wallets SET balance = -10 WHERE id = $1`, system)
	testdb.Exec(t, pool, `UPDATE wallets SET balance = 10 WHERE id = $1`, user)
	return pool, user, txID
}

func run(t *testing.T, pool *pgxpool.Pool) map[string]int64 {
	t.Helper()
	results, err := Run(context.Background(), pool)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	out := map[string]int64{}
	for _, r := range results {
		out[r.Check] = r.Mismatches
	}
	return out
}

func expectMismatches(t *testing.T, got map[string]int64, balance, entries, settlement int64) {
	t.Helper()
	if got[checkBalance] != balance || got[checkEntries] != entries || got[checkSettlement] != settlement {
		t.Fatalf("mismatches = %v; want balance=%d entries=%d settlement=%d", got, balance, entries, settlement)
	}
}

func TestRun_CleanLedger(t *testing.T) {
	pool, _, _ := consistentLedger(t)

	expectMismatches(t, run(t, pool), 0, 0, 0)
	if n := testdb.Count(t, pool, "reconciliation_logs WHERE status = 'ok'"); n != 3 {
		t.Fatalf("ok rows = %d, want 3", n)
	}
	if n := testdb.Count(t, pool, "reconciliation_logs WHERE status = 'mismatch'"); n != 0 {
		t.Fatalf("mismatch rows = %d, want 0", n)
	}
}

func TestRun_DetectsWalletBalanceMismatch(t *testing.T) {
	pool, user, _ := consistentLedger(t)
	testdb.Exec(t, pool, `UPDATE wallets SET balance = balance + 5 WHERE id = $1`, user)

	expectMismatches(t, run(t, pool), 1, 0, 0)

	if got := testdb.Text(t, pool, `SELECT details->>'wallet_id' FROM reconciliation_logs WHERE status = 'mismatch'`); got != user {
		t.Fatalf("mismatch is for wallet %s, want %s", got, user)
	}
	if !testdb.NumEq(t, pool, "SELECT expected FROM reconciliation_logs WHERE status = 'mismatch'", "10") ||
		!testdb.NumEq(t, pool, "SELECT actual FROM reconciliation_logs WHERE status = 'mismatch'", "15") {
		t.Fatal("expected/actual not logged as 10/15")
	}
}

func TestRun_DetectsUnbalancedTransaction(t *testing.T) {
	pool, user, txID := consistentLedger(t)
	// An extra credit with no matching debit: the transaction no longer sums to zero,
	// and the user's entries (15) no longer match the stored balance (10).
	testdb.Exec(t, pool,
		`INSERT INTO ledger_entries (wallet_id, transaction_id, amount, currency_code, entry_type)
		 VALUES ($1, $2, 5, 'USD', 'manual_bad')`, user, txID)

	expectMismatches(t, run(t, pool), 1, 1, 0)

	if got := testdb.Text(t, pool,
		`SELECT details->>'transaction_id' FROM reconciliation_logs WHERE check_name = $1 AND status = 'mismatch'`,
		checkEntries); got != txID {
		t.Fatalf("mismatch is for transaction %s, want %s", got, txID)
	}
}

func TestRun_DetectsSettlementMismatch(t *testing.T) {
	pool, user, _ := consistentLedger(t)
	other := wallet(t, pool, "NGN", false)

	// USD -> NGN, 10 USD became 15000 NGN: the NGN-USD settlement (base NGN) should net to -15000.
	settlementID := testdb.Text(t, pool,
		`INSERT INTO settlements (corridor, currency_code, net_amount) VALUES ('NGN-USD', 'NGN', -1) RETURNING id::text`)
	testdb.Exec(t, pool,
		`INSERT INTO transactions (from_wallet_id, to_wallet_id, amount, currency_from, currency_to,
		                           converted_amount, corridor, status, settlement_id)
		 VALUES ($1, $2, 10, 'USD', 'NGN', 15000, 'NGN-USD', 'in_settlement', $3)`, user, other, settlementID)

	expectMismatches(t, run(t, pool), 0, 0, 1)

	testdb.Exec(t, pool, `UPDATE settlements SET net_amount = -15000 WHERE id = $1`, settlementID)
	expectMismatches(t, run(t, pool), 0, 0, 0)
}

func TestRun_DatabaseErrorIsReturned(t *testing.T) {
	pool := testdb.New(t)
	pool.Close()
	if _, err := Run(context.Background(), pool); err == nil {
		t.Fatal("expected an error from a closed pool")
	}
}