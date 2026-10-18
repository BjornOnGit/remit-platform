package store_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"remit-platform/ledger/internal/store"
	"remit-platform/ledger/internal/testdb"
)

// runParallel runs fn(0..n-1) concurrently and counts successes and insufficient-funds
// rejections. Any other error fails the test (a deadlock would show up here).
func runParallel(t *testing.T, n int, fn func(i int) error) (ok, insufficient int) {
	t.Helper()
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := fn(i)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, store.ErrInsufficientFunds):
				insufficient++
			default:
				t.Errorf("transfer %d: unexpected error: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	return ok, insufficient
}

func entriesSum(t *testing.T, pool *pgxpool.Pool, walletID string) decimal.Decimal {
	t.Helper()
	return decimal.RequireFromString(testdb.Text(t, pool,
		`SELECT COALESCE(sum(amount), 0)::text FROM ledger_entries WHERE wallet_id = $1`, walletID))
}

// 50 parallel transfers of 3 from a wallet holding 100: exactly 33 can succeed,
// the balance never goes negative, and the balance equals the sum of entries.
func TestTransfer_ConcurrentDebitsNeverOverdraw(t *testing.T) {
	pool := testdb.New(t)
	a := testdb.Wallet(t, pool, "USD", "100", false)
	b := testdb.Wallet(t, pool, "USD", "0", false)

	ok, insufficient := runParallel(t, 50, func(i int) error {
		_, err := store.Transfer(context.Background(), pool, a, b, "3", "USD", fmt.Sprintf("k-%d", i), hash("1"), nil)
		return err
	})

	if ok != 33 || insufficient != 17 {
		t.Fatalf("successes = %d, insufficient = %d; want 33 and 17", ok, insufficient)
	}
	assertDec(t, testdb.Balance(t, pool, a), "1")
	assertDec(t, testdb.Balance(t, pool, b), "99")
	// The wallets were created with a starting balance (no funding entries), so entries net to -99 / +99.
	assertDec(t, entriesSum(t, pool, a), "-99")
	assertDec(t, entriesSum(t, pool, b), "99")
	if n := testdb.Count(t, pool, "transactions"); n != 33 {
		t.Fatalf("transactions = %d, want 33", n)
	}
}

// Transfers in opposite directions between the same two wallets must not deadlock
// (wallet rows are always locked in id order) and must conserve money.
func TestTransfer_OppositeDirectionsDoNotDeadlock(t *testing.T) {
	pool := testdb.New(t)
	a := testdb.Wallet(t, pool, "USD", "1000", false)
	b := testdb.Wallet(t, pool, "USD", "1000", false)

	ok, insufficient := runParallel(t, 60, func(i int) error {
		from, to := a, b
		if i%2 == 1 {
			from, to = b, a
		}
		_, err := store.Transfer(context.Background(), pool, from, to, "1", "USD", fmt.Sprintf("k-%d", i), hash("1"), nil)
		return err
	})

	if ok != 60 || insufficient != 0 {
		t.Fatalf("successes = %d, insufficient = %d; want 60 and 0", ok, insufficient)
	}
	assertDec(t, testdb.Balance(t, pool, a), "1000")
	assertDec(t, testdb.Balance(t, pool, b), "1000")
	assertDec(t, entriesSum(t, pool, a), "0")
	assertDec(t, entriesSum(t, pool, b), "0")
}

// Parallel requests carrying the same idempotency key produce exactly one transfer.
func TestTransfer_ParallelSameKeyCreatesOneTransaction(t *testing.T) {
	pool := testdb.New(t)
	a := testdb.Wallet(t, pool, "USD", "100", false)
	b := testdb.Wallet(t, pool, "USD", "0", false)

	ids := make(chan string, 20)
	ok, _ := runParallel(t, 20, func(int) error {
		tx, err := store.Transfer(context.Background(), pool, a, b, "10", "USD", "one-key", hash("1"), nil)
		if err == nil {
			ids <- tx.ID
		}
		return err
	})
	close(ids)

	if ok != 20 {
		t.Fatalf("successes = %d, want 20 (all replays succeed)", ok)
	}
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		} else if id != first {
			t.Fatalf("different transaction ids returned: %s and %s", first, id)
		}
	}
	if n := testdb.Count(t, pool, "transactions"); n != 1 {
		t.Fatalf("transactions = %d, want 1", n)
	}
	assertDec(t, testdb.Balance(t, pool, a), "90")
}