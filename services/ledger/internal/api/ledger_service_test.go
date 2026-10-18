package api

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"remit-platform/ledger/internal/testdb"
	ledgerpb "remit-platform/ledger/proto"
)

func TestWalletHandlers(t *testing.T) {
	srv := &Server{DB: testdb.New(t)}
	ctx := context.Background()
	owner := "22222222-2222-2222-2222-222222222222"

	invalid := []*ledgerpb.CreateWalletReq{
		{OwnerId: "abc", CurrencyCode: "USD"},
		{OwnerId: owner, CurrencyCode: "usd"},
	}
	for _, req := range invalid {
		if _, err := srv.CreateWallet(ctx, req); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("CreateWallet(%v): got %v, want InvalidArgument", req, err)
		}
	}

	w, err := srv.CreateWallet(ctx, &ledgerpb.CreateWalletReq{OwnerId: owner, CurrencyCode: "USD"})
	if err != nil || w.Id == "" || w.CurrencyCode != "USD" {
		t.Fatalf("create wallet: %v %+v", err, w)
	}
	if _, err := srv.CreateWallet(ctx, &ledgerpb.CreateWalletReq{OwnerId: owner, CurrencyCode: "USD"}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("duplicate: got %v, want AlreadyExists", err)
	}

	got, err := srv.GetWallet(ctx, &ledgerpb.GetWalletReq{Id: w.Id})
	if err != nil || got.Id != w.Id {
		t.Fatalf("get wallet: %v %+v", err, got)
	}
	if _, err := srv.GetWallet(ctx, &ledgerpb.GetWalletReq{Id: missingID}); status.Code(err) != codes.NotFound {
		t.Fatalf("missing wallet: got %v, want NotFound", err)
	}
	if _, err := srv.GetWallet(ctx, &ledgerpb.GetWalletReq{Id: "abc"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("bad id: got %v, want InvalidArgument", err)
	}
}

func TestGetTransactionHandler_Errors(t *testing.T) {
	srv := &Server{DB: testdb.New(t)}
	ctx := context.Background()

	if _, err := srv.GetTransaction(ctx, &ledgerpb.GetTransactionReq{Id: "abc"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("bad id: got %v, want InvalidArgument", err)
	}
	if _, err := srv.GetTransaction(ctx, &ledgerpb.GetTransactionReq{Id: missingID}); status.Code(err) != codes.NotFound {
		t.Fatalf("missing: got %v, want NotFound", err)
	}
}

func TestHandlers_InternalErrorWhenDatabaseUnavailable(t *testing.T) {
	pool := testdb.New(t)
	srv := &Server{DB: pool, FX: rates("1500")}
	pool.Close()
	ctx := context.Background()

	checks := map[string]error{}
	_, checks["CreateWallet"] = srv.CreateWallet(ctx, &ledgerpb.CreateWalletReq{OwnerId: idA, CurrencyCode: "USD"})
	_, checks["GetWallet"] = srv.GetWallet(ctx, &ledgerpb.GetWalletReq{Id: idA})
	_, checks["GetTransaction"] = srv.GetTransaction(ctx, &ledgerpb.GetTransactionReq{Id: idA})
	_, checks["Transfer"] = srv.Transfer(ctx, transferReq("k", idA, idB, "1", "USD"))

	for name, err := range checks {
		if got := status.Code(err); got != codes.Internal {
			t.Errorf("%s: got %v, want Internal", name, got)
		}
	}
}