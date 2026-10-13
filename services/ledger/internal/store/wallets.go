package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrWalletExists   = errors.New("wallet already exists for owner and currency")
	ErrWalletNotFound = errors.New("wallet not found")
)

type Wallet struct {
	ID           string
	OwnerID      string
	CurrencyCode string
	Balance      string
	CreatedAt    time.Time
}

func CreateWallet(ctx context.Context, pool *pgxpool.Pool, ownerID, currency string) (Wallet, error) {
	var w Wallet
	err := pool.QueryRow(ctx,
		`INSERT INTO wallets (owner_id, currency_code) VALUES ($1, $2)
		 RETURNING id::text, owner_id::text, currency_code, balance::text, created_at`,
		ownerID, currency,
	).Scan(&w.ID, &w.OwnerID, &w.CurrencyCode, &w.Balance, &w.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Wallet{}, ErrWalletExists
		}
		return Wallet{}, err
	}
	return w, nil
}

func GetWallet(ctx context.Context, pool *pgxpool.Pool, id string) (Wallet, error) {
	var w Wallet
	err := pool.QueryRow(ctx,
		`SELECT id::text, owner_id::text, currency_code, balance::text, created_at
		 FROM wallets WHERE id = $1`,
		id,
	).Scan(&w.ID, &w.OwnerID, &w.CurrencyCode, &w.Balance, &w.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Wallet{}, ErrWalletNotFound
	}
	if err != nil {
		return Wallet{}, err
	}
	return w, nil
}