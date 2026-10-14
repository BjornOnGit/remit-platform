package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"remit-platform/ledger/internal/fx"
	"remit-platform/ledger/internal/store"
	ledgerpb "remit-platform/ledger/proto"
)

// numeric(20,6): up to 14 integer digits and 6 decimals.
var amountRe = regexp.MustCompile(`^\d{1,14}(\.\d{1,6})?$`)

// validateTransfer checks request fields only (no DB access).
func validateTransfer(req *ledgerpb.TransferReq) error {
	if k := req.GetIdempotencyKey(); k == "" || len(k) > 128 {
		return status.Error(codes.InvalidArgument, "idempotency_key is required (max 128 chars)")
	}
	if _, err := uuid.Parse(req.GetFromWalletId()); err != nil {
		return status.Error(codes.InvalidArgument, "from_wallet_id must be a valid UUID")
	}
	if _, err := uuid.Parse(req.GetToWalletId()); err != nil {
		return status.Error(codes.InvalidArgument, "to_wallet_id must be a valid UUID")
	}
	if req.GetFromWalletId() == req.GetToWalletId() {
		return status.Error(codes.InvalidArgument, "from_wallet_id and to_wallet_id must differ")
	}
	amt := req.GetAmount()
	if !amountRe.MatchString(amt) || strings.Trim(amt, "0.") == "" {
		return status.Error(codes.InvalidArgument, "amount must be a positive decimal with up to 6 decimal places")
	}
	if !currencyRe.MatchString(req.GetCurrency()) {
		return status.Error(codes.InvalidArgument, "currency must be a 3-letter uppercase code")
	}
	return nil
}

func (s *Server) Transfer(ctx context.Context, req *ledgerpb.TransferReq) (*ledgerpb.TransferResp, error) {
	if err := validateTransfer(req); err != nil {
		return nil, err
	}

	from, err := store.GetWallet(ctx, s.DB, req.GetFromWalletId())
	if errors.Is(err, store.ErrWalletNotFound) {
		return nil, status.Error(codes.NotFound, "from wallet not found")
	}
	if err != nil {
		log.Printf("transfer: get source wallet: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	if from.CurrencyCode != req.GetCurrency() {
		return nil, status.Error(codes.InvalidArgument, "currency does not match the source wallet")
	}

	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.ToLower(req.GetFromWalletId()), strings.ToLower(req.GetToWalletId()),
		req.GetAmount(), req.GetCurrency(),
	}, "|")))
	t, err := store.Transfer(ctx, s.DB, req.GetFromWalletId(), req.GetToWalletId(),
		req.GetAmount(), req.GetCurrency(), req.GetIdempotencyKey(), hex.EncodeToString(sum[:]))
	switch {
	case errors.Is(err, store.ErrIdempotencyConflict):
		return nil, status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, store.ErrWalletNotFound):
		return nil, status.Error(codes.NotFound, "wallet not found")
	case errors.Is(err, store.ErrInsufficientFunds):
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, store.ErrCurrencyMismatch), errors.Is(err, store.ErrAmountTooSmall):
		return nil, status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, fx.ErrRateUnavailable):
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	case err != nil:
		log.Printf("transfer: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &ledgerpb.TransferResp{Transaction: transactionResp(t)}, nil
}

func (s *Server) GetTransaction(ctx context.Context, req *ledgerpb.GetTransactionReq) (*ledgerpb.TransactionResp, error) {
	if _, err := uuid.Parse(req.GetId()); err != nil {
		return nil, status.Error(codes.InvalidArgument, "id must be a valid UUID")
	}

	t, err := store.GetTransaction(ctx, s.DB, req.GetId())
	if errors.Is(err, store.ErrTransactionNotFound) {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	if err != nil {
		log.Printf("get transaction: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}
	return transactionResp(t), nil
}

func transactionResp(t store.Transaction) *ledgerpb.TransactionResp {
	return &ledgerpb.TransactionResp{
		Id:              t.ID,
		FromWalletId:    t.FromWalletID,
		ToWalletId:      t.ToWalletID,
		Amount:          t.Amount,
		CurrencyFrom:    t.CurrencyFrom,
		CurrencyTo:      t.CurrencyTo,
		ConvertedAmount: t.ConvertedAmount,
		FxRate:          t.FxRate,
		FxSource:        t.FxSource,
		FxTimestamp:     t.FxTimestamp,
		Status:          t.Status,
		CreatedAt:       t.CreatedAt.Format(time.RFC3339),
	}
}