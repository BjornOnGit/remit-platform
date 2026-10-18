package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"remit-platform/ledger/internal/fx"
	"remit-platform/ledger/internal/store"
	"remit-platform/ledger/internal/testdb"
)

const missingWallet = "00000000-0000-0000-0000-000000000099"

// request_hash is char(64) (a SHA-256 hex digest), so test hashes must be 64 characters too.
func hash(c string) string { return strings.Repeat(c, 64) }

func quote(rate string) *fx.Quote {
	return &fx.Quote{Rate: decimal.RequireFromString(rate), Source: "test", FetchedAt: time.Now()}
}

func assertDec(t *testing.T, got decimal.Decimal, want string) {
	t.Helper()
	if !got.Equal(decimal.RequireFromString(want)) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestTransfer_SameCurrency(t *testing.T) {
	pool := testdb.New(t)
	a := testdb.Wallet(t, pool, "USD", "100", false)
	b := testdb.Wallet(t, pool, "USD", "0", false)

	tx, err := store.Transfer(context.Background(), pool, a, b, "25.5", "USD", "k1", "h1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if tx.Status != "posted" || tx.CurrencyFrom != "USD" || tx.CurrencyTo != "USD" {
		t.Fatalf("unexpected transaction: %+v", tx)
	}
	assertDec(t, decimal.RequireFromString(tx.ConvertedAmount), "25.5")
	assertDec(t, testdb.Balance(t, pool, a), "74.5")
	assertDec(t, testdb.Balance(t, pool, b), "25.5")

	if n := testdb.Count(t, pool, "ledger_entries"); n != 2 {
		t.Fatalf("ledger entries = %d, want 2", n)
	}
	if n := testdb.Count(t, pool, "outbox_events"); n != 1 {
		t.Fatalf("outbox events = %d, want 1", n)
	}
	assertDec(t, decimal.RequireFromString(
		testdb.Text(t, pool, `SELECT COALESCE(sum(amount), 0)::text FROM ledger_entries`)), "0")
}

func TestTransfer_InsufficientFunds(t *testing.T) {
	pool := testdb.New(t)
	a := testdb.Wallet(t, pool, "USD", "10", false)
	b := testdb.Wallet(t, pool, "USD", "0", false)

	_, err := store.Transfer(context.Background(), pool, a, b, "20", "USD", "k1", "h1", nil)
	if !errors.Is(err, store.ErrInsufficientFunds) {
		t.Fatalf("got %v, want ErrInsufficientFunds", err)
	}
	assertDec(t, testdb.Balance(t, pool, a), "10")
	assertDec(t, testdb.Balance(t, pool, b), "0")
	for _, table := range []string{"transactions", "ledger_entries", "outbox_events", "idempotency_keys"} {
		if n := testdb.Count(t, pool, table); n != 0 {
			t.Fatalf("%s has %d rows after a rejected transfer", table, n)
		}
	}
}

func TestTransfer_IdempotentReplay(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	a := testdb.Wallet(t, pool, "USD", "100", false)
	b := testdb.Wallet(t, pool, "USD", "0", false)

	first, err := store.Transfer(ctx, pool, a, b, "10", "USD", "same-key", hash("1"), nil)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.Transfer(ctx, pool, a, b, "10", "USD", "same-key", hash("1"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != again.ID {
		t.Fatalf("replay returned a different transaction: %s vs %s", first.ID, again.ID)
	}
	if n := testdb.Count(t, pool, "transactions"); n != 1 {
		t.Fatalf("transactions = %d, want 1", n)
	}
	if n := testdb.Count(t, pool, "outbox_events"); n != 1 {
		t.Fatalf("outbox events = %d, want 1", n)
	}
	assertDec(t, testdb.Balance(t, pool, a), "90")

	_, err = store.Transfer(ctx, pool, a, b, "10", "USD", "same-key", hash("2"), nil)
	if !errors.Is(err, store.ErrIdempotencyConflict) {
		t.Fatalf("got %v, want ErrIdempotencyConflict", err)
	}
}

func TestTransfer_CrossCurrency(t *testing.T) {
	pool := testdb.New(t)
	sysUSD := testdb.Wallet(t, pool, "USD", "0", true)
	sysNGN := testdb.Wallet(t, pool, "NGN", "0", true)
	a := testdb.Wallet(t, pool, "USD", "100", false)
	b := testdb.Wallet(t, pool, "NGN", "0", false)

	tx, err := store.Transfer(context.Background(), pool, a, b, "10", "USD", "k1", "h1", quote("1500"))
	if err != nil {
		t.Fatal(err)
	}
	if tx.CurrencyFrom != "USD" || tx.CurrencyTo != "NGN" || tx.FxSource != "test" || tx.FxTimestamp == "" {
		t.Fatalf("unexpected transaction: %+v", tx)
	}
	assertDec(t, decimal.RequireFromString(tx.ConvertedAmount), "15000")
	assertDec(t, decimal.RequireFromString(tx.FxRate), "1500")

	assertDec(t, testdb.Balance(t, pool, a), "90")
	assertDec(t, testdb.Balance(t, pool, b), "15000")
	assertDec(t, testdb.Balance(t, pool, sysUSD), "10")
	assertDec(t, testdb.Balance(t, pool, sysNGN), "-15000")

	if n := testdb.Count(t, pool, "ledger_entries"); n != 4 {
		t.Fatalf("ledger entries = %d, want 4", n)
	}
	for _, cur := range []string{"USD", "NGN"} {
		sum := testdb.Text(t, pool,
			`SELECT COALESCE(sum(amount), 0)::text FROM ledger_entries WHERE currency_code = $1`, cur)
		assertDec(t, decimal.RequireFromString(sum), "0")
	}
	if got := testdb.Text(t, pool, `SELECT corridor FROM transactions WHERE id = $1`, tx.ID); got != "NGN-USD" {
		t.Fatalf("corridor = %s, want NGN-USD", got)
	}
}

func TestTransfer_RoundingHalfEven(t *testing.T) {
	cases := []struct{ amount, rate, want string }{
		{"0.25", "0.5", "0.12"}, // 0.125 -> round half to even
		{"0.35", "0.5", "0.18"}, // 0.175 -> round half to even
		{"2.50", "0.92", "2.30"},
		{"100", "1500", "150000"},
	}
	for _, c := range cases {
		t.Run(c.amount+"x"+c.rate, func(t *testing.T) {
			pool := testdb.New(t)
			testdb.Wallet(t, pool, "USD", "0", true)
			testdb.Wallet(t, pool, "EUR", "0", true)
			a := testdb.Wallet(t, pool, "USD", "1000", false)
			b := testdb.Wallet(t, pool, "EUR", "0", false)

			tx, err := store.Transfer(context.Background(), pool, a, b, c.amount, "USD", "k", "h", quote(c.rate))
			if err != nil {
				t.Fatal(err)
			}
			assertDec(t, decimal.RequireFromString(tx.ConvertedAmount), c.want)
		})
	}
}

func TestTransfer_AmountTooSmallAfterConversion(t *testing.T) {
	pool := testdb.New(t)
	testdb.Wallet(t, pool, "USD", "0", true)
	testdb.Wallet(t, pool, "EUR", "0", true)
	a := testdb.Wallet(t, pool, "USD", "10", false)
	b := testdb.Wallet(t, pool, "EUR", "0", false)

	_, err := store.Transfer(context.Background(), pool, a, b, "0.001", "USD", "k", "h", quote("0.5"))
	if !errors.Is(err, store.ErrAmountTooSmall) {
		t.Fatalf("got %v, want ErrAmountTooSmall", err)
	}
	if n := testdb.Count(t, pool, "transactions"); n != 0 {
		t.Fatalf("transactions = %d, want 0", n)
	}
}

func TestTransfer_Errors(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	a := testdb.Wallet(t, pool, "USD", "100", false)
	usd2 := testdb.Wallet(t, pool, "USD", "0", false)
	eur := testdb.Wallet(t, pool, "EUR", "0", false)

	if _, err := store.Transfer(ctx, pool, a, missingWallet, "1", "USD", "k1", "h", nil); !errors.Is(err, store.ErrWalletNotFound) {
		t.Fatalf("missing wallet: got %v", err)
	}
	if _, err := store.Transfer(ctx, pool, a, usd2, "1", "EUR", "k2", "h", nil); !errors.Is(err, store.ErrCurrencyMismatch) {
		t.Fatalf("currency mismatch: got %v", err)
	}
	if _, err := store.Transfer(ctx, pool, a, eur, "1", "USD", "k3", "h", nil); err == nil {
		t.Fatal("cross-currency transfer without a quote must fail")
	}
	// Quote supplied but the system FX wallets do not exist.
	if _, err := store.Transfer(ctx, pool, a, eur, "1", "USD", "k4", "h", quote("0.9")); err == nil {
		t.Fatal("cross-currency transfer without system wallets must fail")
	}

	assertDec(t, testdb.Balance(t, pool, a), "100")
	if n := testdb.Count(t, pool, "transactions"); n != 0 {
		t.Fatalf("transactions = %d, want 0", n)
	}
}

func TestGetTransaction(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	a := testdb.Wallet(t, pool, "USD", "100", false)
	b := testdb.Wallet(t, pool, "USD", "0", false)

	created, err := store.Transfer(ctx, pool, a, b, "5", "USD", "k1", "h1", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetTransaction(ctx, pool, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || got.Status != "posted" {
		t.Fatalf("unexpected transaction: %+v", got)
	}
	if _, err := store.GetTransaction(ctx, pool, missingWallet); !errors.Is(err, store.ErrTransactionNotFound) {
		t.Fatalf("got %v, want ErrTransactionNotFound", err)
	}
}

func TestWallets(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	owner := "11111111-1111-1111-1111-111111111111"

	w, err := store.CreateWallet(ctx, pool, owner, "USD")
	if err != nil {
		t.Fatal(err)
	}
	assertDec(t, decimal.RequireFromString(w.Balance), "0")

	if _, err := store.CreateWallet(ctx, pool, owner, "USD"); !errors.Is(err, store.ErrWalletExists) {
		t.Fatalf("duplicate: got %v, want ErrWalletExists", err)
	}
	got, err := store.GetWallet(ctx, pool, w.ID)
	if err != nil || got.ID != w.ID {
		t.Fatalf("get wallet: %v %+v", err, got)
	}
	if _, err := store.GetWallet(ctx, pool, missingWallet); !errors.Is(err, store.ErrWalletNotFound) {
		t.Fatalf("got %v, want ErrWalletNotFound", err)
	}
}

func TestStore_DatabaseUnavailable(t *testing.T) {
	pool := testdb.New(t)
	pool.Close()
	ctx := context.Background()

	if _, err := store.Transfer(ctx, pool, missingWallet, missingWallet, "1", "USD", "k", hash("1"), nil); err == nil {
		t.Fatal("Transfer: expected an error")
	}
	if _, err := store.CreateWallet(ctx, pool, "11111111-1111-1111-1111-111111111111", "USD"); err == nil {
		t.Fatal("CreateWallet: expected an error")
	}
	if _, err := store.GetWallet(ctx, pool, missingWallet); err == nil || errors.Is(err, store.ErrWalletNotFound) {
		t.Fatalf("GetWallet: got %v, want a database error", err)
	}
	if _, err := store.GetTransaction(ctx, pool, missingWallet); err == nil || errors.Is(err, store.ErrTransactionNotFound) {
		t.Fatalf("GetTransaction: got %v, want a database error", err)
	}
}