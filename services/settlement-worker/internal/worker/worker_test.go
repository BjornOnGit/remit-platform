package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"remit-platform/settlement-worker/internal/testdb"
)

func wallet(t *testing.T, pool *pgxpool.Pool, currency string) string {
	t.Helper()
	return testdb.Text(t, pool,
		`INSERT INTO wallets (owner_id, currency_code) VALUES (gen_random_uuid(), $1) RETURNING id::text`, currency)
}

func transaction(t *testing.T, pool *pgxpool.Pool, from, to, curFrom, curTo, amount, converted, corridor string) {
	t.Helper()
	testdb.Exec(t, pool,
		`INSERT INTO transactions (from_wallet_id, to_wallet_id, amount, currency_from, currency_to,
		                           converted_amount, corridor)
		 VALUES ($1, $2, $3::text::numeric, $4, $5, $6::text::numeric, NULLIF($7, ''))`,
		from, to, amount, curFrom, curTo, converted, corridor)
}

// Four cross-currency transactions in two corridors plus one same-currency transaction.
func seed(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := testdb.New(t)
	usd, usd2 := wallet(t, pool, "USD"), wallet(t, pool, "USD")
	ngn, eur := wallet(t, pool, "NGN"), wallet(t, pool, "EUR")

	transaction(t, pool, usd, ngn, "USD", "NGN", "10", "15000", "NGN-USD")
	transaction(t, pool, usd, ngn, "USD", "NGN", "20", "30000", "NGN-USD")
	transaction(t, pool, ngn, usd, "NGN", "USD", "3000", "2", "NGN-USD") // opposite direction
	transaction(t, pool, usd, eur, "USD", "EUR", "100", "92", "EUR-USD")
	transaction(t, pool, usd, usd2, "USD", "USD", "5", "5", "") // same currency: never settled
	return pool
}

func TestNetPosted_NetsPerCorridor(t *testing.T) {
	pool := seed(t)
	ctx := context.Background()

	txs, settlements, err := NetPosted(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if txs != 4 || settlements != 2 {
		t.Fatalf("netted %d transactions into %d settlements; want 4 and 2", txs, settlements)
	}

	// NGN-USD (base NGN): +3000 for the NGN->USD transaction, -15000 and -30000 for the USD->NGN ones.
	if !testdb.NumEq(t, pool, `SELECT net_amount FROM settlements WHERE corridor = 'NGN-USD'`, "-42000") {
		t.Fatal("NGN-USD net amount is not -42000")
	}
	if got := testdb.Text(t, pool, `SELECT currency_code FROM settlements WHERE corridor = 'NGN-USD'`); got != "NGN" {
		t.Fatalf("NGN-USD settlement currency = %s, want NGN", got)
	}
	if !testdb.NumEq(t, pool, `SELECT net_amount FROM settlements WHERE corridor = 'EUR-USD'`, "-92") {
		t.Fatal("EUR-USD net amount is not -92")
	}
	if n := testdb.Count(t, pool, "transactions WHERE status = 'in_settlement' AND settlement_id IS NOT NULL"); n != 4 {
		t.Fatalf("in_settlement transactions = %d, want 4", n)
	}
	if n := testdb.Count(t, pool, "transactions WHERE status = 'posted'"); n != 1 {
		t.Fatalf("posted transactions = %d, want 1 (the same-currency one)", n)
	}

	// Already-netted transactions are not netted again.
	txs, settlements, err = NetPosted(ctx, pool)
	if err != nil || txs != 0 || settlements != 0 {
		t.Fatalf("second run: %d, %d, %v; want 0, 0, nil", txs, settlements, err)
	}
}

type countingRail struct {
	failOn int // 1-based call number that fails; 0 = never
	calls  int
}

func (r *countingRail) Send(context.Context, string, string, string, string) error {
	r.calls++
	if r.calls == r.failOn {
		return errors.New("rail down")
	}
	return nil
}

func TestSettleCreated_MarksSettlementsAndTransactionsSettled(t *testing.T) {
	pool := seed(t)
	ctx := context.Background()
	if _, _, err := NetPosted(ctx, pool); err != nil {
		t.Fatal(err)
	}

	n, err := SettleCreated(ctx, pool, SimulatedRail{})
	if err != nil || n != 2 {
		t.Fatalf("settled %d, err %v; want 2, nil", n, err)
	}
	if c := testdb.Count(t, pool, "settlements WHERE status = 'settled' AND settled_at IS NOT NULL"); c != 2 {
		t.Fatalf("settled settlements = %d, want 2", c)
	}
	if c := testdb.Count(t, pool, "transactions WHERE status = 'settled'"); c != 4 {
		t.Fatalf("settled transactions = %d, want 4", c)
	}
}

func TestSettleCreated_RailFailureLeavesRestForRetry(t *testing.T) {
	pool := seed(t)
	ctx := context.Background()
	if _, _, err := NetPosted(ctx, pool); err != nil {
		t.Fatal(err)
	}

	rail := &countingRail{failOn: 2} // first settlement succeeds, second fails
	n, err := SettleCreated(ctx, pool, rail)
	if err == nil || n != 1 {
		t.Fatalf("settled %d, err %v; want 1 and a rail error", n, err)
	}
	if c := testdb.Count(t, pool, "settlements WHERE status = 'settled'"); c != 1 {
		t.Fatalf("settled settlements = %d, want 1", c)
	}
	if c := testdb.Count(t, pool, "settlements WHERE status = 'created'"); c != 1 {
		t.Fatalf("created settlements = %d, want 1", c)
	}

	// The next run (rail healthy again) finishes the remaining settlement.
	n, err = SettleCreated(ctx, pool, &countingRail{})
	if err != nil || n != 1 {
		t.Fatalf("retry settled %d, err %v; want 1, nil", n, err)
	}
	if c := testdb.Count(t, pool, "transactions WHERE status = 'settled'"); c != 4 {
		t.Fatalf("settled transactions = %d, want 4", c)
	}
}

func TestSettle_FullRunAndNoOp(t *testing.T) {
	pool := seed(t)
	ctx := context.Background()

	if err := Settle(ctx, pool, SimulatedRail{}); err != nil {
		t.Fatal(err)
	}
	if c := testdb.Count(t, pool, "transactions WHERE status = 'settled'"); c != 4 {
		t.Fatalf("settled transactions = %d, want 4", c)
	}
	// Nothing left to do: must not error or create anything.
	if err := Settle(ctx, pool, SimulatedRail{}); err != nil {
		t.Fatal(err)
	}
	if c := testdb.Count(t, pool, "settlements"); c != 2 {
		t.Fatalf("settlements = %d, want 2", c)
	}
}

func TestSettle_DatabaseErrorIsReturned(t *testing.T) {
	pool := testdb.New(t)
	pool.Close()
	if err := Settle(context.Background(), pool, SimulatedRail{}); err == nil {
		t.Fatal("expected an error from a closed pool")
	}
}

func TestRun_RunsImmediatelyAndStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	done := make(chan struct{})
	go func() {
		Run(ctx, time.Hour, func(context.Context) error {
			calls++
			cancel() // stop after the first run
			return errors.New("a failing job must not stop the loop by itself")
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after the context was cancelled")
	}
	if calls != 1 {
		t.Fatalf("job ran %d times, want 1", calls)
	}
}