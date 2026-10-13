package api

import (
	"context"
	"errors"
	"log"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	ledgerpb "remit-platform/ledger/proto"
	"remit-platform/ledger/internal/store"
)

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

type Server struct {
	ledgerpb.UnimplementedLedgerServer
	DB *pgxpool.Pool
}

func (s *Server) CreateWallet(ctx context.Context, req *ledgerpb.CreateWalletReq) (*ledgerpb.WalletResp, error) {
	if _, err := uuid.Parse(req.GetOwnerId()); err != nil {
		return nil, status.Error(codes.InvalidArgument, "owner_id must be a valid UUID")
	}
	if !currencyRe.MatchString(req.GetCurrencyCode()) {
		return nil, status.Error(codes.InvalidArgument, "currency_code must be a 3-letter uppercase code")
	}

	w, err := store.CreateWallet(ctx, s.DB, req.GetOwnerId(), req.GetCurrencyCode())
	if errors.Is(err, store.ErrWalletExists) {
		return nil, status.Error(codes.AlreadyExists, err.Error())
	}
	if err != nil {
		log.Printf("create wallet: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	return walletResp(w), nil
}

func (s *Server) GetWallet(ctx context.Context, req *ledgerpb.GetWalletReq) (*ledgerpb.WalletResp, error) {
	if _, err := uuid.Parse(req.GetId()); err != nil {
		return nil, status.Error(codes.InvalidArgument, "id must be a valid UUID")
	}

	w, err := store.GetWallet(ctx, s.DB, req.GetId())
	if errors.Is(err, store.ErrWalletNotFound) {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	if err != nil {
		log.Printf("get wallet: %v", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	return walletResp(w), nil
}

func walletResp(w store.Wallet) *ledgerpb.WalletResp {
	return &ledgerpb.WalletResp{
		Id:           w.ID,
		OwnerId:      w.OwnerID,
		CurrencyCode: w.CurrencyCode,
		Balance:      w.Balance,
		CreatedAt:    w.CreatedAt.Format(time.RFC3339),
	}
}