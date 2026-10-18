package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"remit-platform/ledger/internal/fx"
	"remit-platform/ledger/internal/testdb"
	ledgerpb "remit-platform/ledger/proto"
)

const (
	idA       = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	idB       = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	missingID = "00000000-0000-0000-0000-000000000099"
)

type fakeRates struct {
	q   fx.Quote
	err error
}

func (f fakeRates) Rate(context.Context, string, string) (fx.Quote, error) { return f.q, f.err }

func rates(rate string) fakeRates {
	return fakeRates{q: fx.Quote{Rate: decimal.RequireFromString(rate), Source: "test", FetchedAt: time.Now()}}
}

func transferReq(key, from, to, amount, currency string) *ledgerpb.TransferReq {
	return &ledgerpb.TransferReq{
		IdempotencyKey: key, FromWalletId: from, ToWalletId: to, Amount: amount, Currency: currency,
	}
}

func TestValidateTransfer(t *testing.T) {
	invalid := map[string]func(r *ledgerpb.TransferReq){
		"missing idempotency key": func(r *ledgerpb.TransferReq) { r.IdempotencyKey = "" },
		"idempotency key too long": func(r *ledgerpb.TransferReq) {
			r.IdempotencyKey = strings.Repeat("k", 129)
		},
		"bad from wallet":   func(r *ledgerpb.TransferReq) { r.FromWalletId = "abc" },
		"bad to wallet":     func(r *ledgerpb.TransferReq) { r.ToWalletId = "abc" },
		"same wallet":       func(r *ledgerpb.TransferReq) { r.ToWalletId = r.FromWalletId },
		"zero amount":       func(r *ledgerpb.TransferReq) { r.Amount = "0" },
		"zero decimal":      func(r *ledgerpb.TransferReq) { r.Amount = "0.000000" },
		"negative amount":   func(r *ledgerpb.TransferReq) { r.Amount = "-5" },
		"exponent amount":   func(r *ledgerpb.TransferReq) { r.Amount = "1e3" },
		"too many decimals": func(r *ledgerpb.TransferReq) { r.Amount = "1.1234567" },
		"too many digits":   func(r *ledgerpb.TransferReq) { r.Amount = "123456789012345" },
		"empty amount":      func(r *ledgerpb.TransferReq) { r.Amount = "" },
		"lowercase currency": func(r *ledgerpb.TransferReq) { r.Currency = "usd" },
	}
	for name, mutate := range invalid {
		t.Run(name, func(t *testing.T) {
			r := transferReq("k", idA, idB, "10.5", "USD")
			mutate(r)
			if got := status.Code(validateTransfer(r)); got != codes.InvalidArgument {
				t.Fatalf("got %v, want InvalidArgument", got)
			}
		})
	}

	if err := validateTransfer(transferReq("k", idA, idB, "10.5", "USD")); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if err := validateTransfer(transferReq("k", idA, idB, "0.000001", "USD")); err != nil {
		t.Fatalf("smallest amount rejected: %v", err)
	}
}

func TestTransferHandler_Success(t *testing.T) {
	pool := testdb.New(t)
	testdb.Wallet(t, pool, "USD", "0", true)
	testdb.Wallet(t, pool, "NGN", "0", true)
	a := testdb.Wallet(t, pool, "USD", "100", false)
	b := testdb.Wallet(t, pool, "NGN", "0", false)
	c := testdb.Wallet(t, pool, "USD", "0", false)
	srv := &Server{DB: pool, FX: rates("1500")}
	ctx := context.Background()

	same, err := srv.Transfer(ctx, transferReq("k1", a, c, "10", "USD"))
	if err != nil || same.Transaction.Status != "posted" {
		t.Fatalf("same-currency transfer: %v %+v", err, same)
	}

	cross, err := srv.Transfer(ctx, transferReq("k2", a, b, "10", "USD"))
	if err != nil {
		t.Fatal(err)
	}
	tx := cross.Transaction
	if !decimal.RequireFromString(tx.ConvertedAmount).Equal(decimal.NewFromInt(15000)) ||
		tx.FxSource != "test" || tx.CurrencyTo != "NGN" {
		t.Fatalf("unexpected cross-currency transaction: %+v", tx)
	}

	replay, err := srv.Transfer(ctx, transferReq("k2", a, b, "10", "USD"))
	if err != nil || replay.Transaction.Id != tx.Id {
		t.Fatalf("replay: %v %+v", err, replay)
	}
	_, err = srv.Transfer(ctx, transferReq("k2", a, b, "11", "USD"))
	if got := status.Code(err); got != codes.AlreadyExists {
		t.Fatalf("conflicting replay: got %v, want AlreadyExists", got)
	}

	got, err := srv.GetTransaction(ctx, &ledgerpb.GetTransactionReq{Id: tx.Id})
	if err != nil || got.Id != tx.Id {
		t.Fatalf("get transaction: %v %+v", err, got)
	}
}

func TestTransferHandler_ErrorCodes(t *testing.T) {
	pool := testdb.New(t)
	testdb.Wallet(t, pool, "USD", "0", true)
	testdb.Wallet(t, pool, "NGN", "0", true)
	usdA := testdb.Wallet(t, pool, "USD", "100", false)
	usdB := testdb.Wallet(t, pool, "USD", "0", false)
	ngn := testdb.Wallet(t, pool, "NGN", "0", false)

	cases := []struct {
		name  string
		rates fakeRates
		req   *ledgerpb.TransferReq
		want  codes.Code
	}{
		{"invalid request", rates("1500"), transferReq("", usdA, usdB, "1", "USD"), codes.InvalidArgument},
		{"insufficient funds", rates("1500"), transferReq("e1", usdA, usdB, "500", "USD"), codes.FailedPrecondition},
		{"unknown source wallet", rates("1500"), transferReq("e2", missingID, usdB, "1", "USD"), codes.NotFound},
		{"unknown destination wallet", rates("1500"), transferReq("e3", usdA, missingID, "1", "USD"), codes.NotFound},
		{"currency differs from source wallet", rates("1500"), transferReq("e4", usdA, usdB, "1", "EUR"), codes.InvalidArgument},
		{"rate unavailable", fakeRates{err: fx.ErrRateUnavailable}, transferReq("e5", usdA, ngn, "1", "USD"), codes.FailedPrecondition},
		{"rate stale", fakeRates{err: fx.ErrRateStale}, transferReq("e6", usdA, ngn, "1", "USD"), codes.FailedPrecondition},
		{"fx service down", fakeRates{err: fx.ErrRateServiceDown}, transferReq("e7", usdA, ngn, "1", "USD"), codes.Unavailable},
		{"amount too small", rates("0.5"), transferReq("e8", usdA, ngn, "0.001", "USD"), codes.InvalidArgument},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := &Server{DB: pool, FX: c.rates}
			_, err := srv.Transfer(context.Background(), c.req)
			if got := status.Code(err); got != c.want {
				t.Fatalf("got %v (%v), want %v", got, err, c.want)
			}
		})
	}

	// None of the rejected transfers may have changed anything.
	if n := testdb.Count(t, pool, "transactions"); n != 0 {
		t.Fatalf("transactions = %d, want 0", n)
	}
	if !testdb.Balance(t, pool, usdA).Equal(decimal.NewFromInt(100)) {
		t.Fatal("source balance changed")
	}
}