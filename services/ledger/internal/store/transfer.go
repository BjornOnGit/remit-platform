package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrIdempotencyConflict = errors.New("idempotency key already used with a different request")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrCurrencyMismatch  = errors.New("destination wallet currency differs from transfer currency (cross-currency not supported yet)")
)

type Transaction struct {
	ID              string
	FromWalletID    string
	ToWalletID      string
	Amount          string
	CurrencyFrom    string
	CurrencyTo      string
	ConvertedAmount string
	Status          string
	CreatedAt       time.Time
}

// Transfer moves amount between two wallets of the same currency in one DB transaction.
func Transfer(ctx context.Context, pool *pgxpool.Pool, fromID, toID, amount, currency, idemKey, reqHash string) (Transaction, error) {
	fromID, toID = strings.ToLower(fromID), strings.ToLower(toID)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return Transaction{}, err
	}
	defer tx.Rollback(ctx)

	// Serialize requests sharing an idempotency key, then replay if already processed.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, idemKey); err != nil {
		return Transaction{}, err
	}
	var storedHash, storedTxID string
	err = tx.QueryRow(ctx,
		`SELECT request_hash, transaction_id::text FROM idempotency_keys WHERE key = $1`,
		idemKey).Scan(&storedHash, &storedTxID)
	switch {
	case err == nil:
		if storedHash != reqHash {
			return Transaction{}, ErrIdempotencyConflict
		}
		return getTransaction(ctx, tx, storedTxID)
	case !errors.Is(err, pgx.ErrNoRows):
		return Transaction{}, err
	}

	// Lock both wallets ordered by id to avoid deadlocks.
	rows, err := tx.Query(ctx,
		`SELECT id::text, currency_code FROM wallets
		 WHERE id = ANY($1::uuid[]) ORDER BY id FOR UPDATE`,
		[]string{fromID, toID})
	if err != nil {
		return Transaction{}, err
	}
	currencies := map[string]string{}
	for rows.Next() {
		var id, cur string
		if err := rows.Scan(&id, &cur); err != nil {
			rows.Close()
			return Transaction{}, err
		}
		currencies[id] = cur
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Transaction{}, err
	}

	fromCur, okFrom := currencies[fromID]
	toCur, okTo := currencies[toID]
	if !okFrom || !okTo {
		return Transaction{}, ErrWalletNotFound
	}
	if fromCur != currency || toCur != currency {
		return Transaction{}, ErrCurrencyMismatch
	}

	tag, err := tx.Exec(ctx,
		`UPDATE wallets SET balance = balance - $2::text::numeric
		 WHERE id = $1 AND (is_system OR balance >= $2::text::numeric)`,
		fromID, amount)
	if err != nil {
		return Transaction{}, err
	}
	if tag.RowsAffected() == 0 {
		return Transaction{}, ErrInsufficientFunds
	}

	if _, err := tx.Exec(ctx,
		`UPDATE wallets SET balance = balance + $2::text::numeric WHERE id = $1`,
		toID, amount); err != nil {
		return Transaction{}, err
	}

	t := Transaction{CurrencyFrom: currency, CurrencyTo: currency}
	err = tx.QueryRow(ctx,
		`INSERT INTO transactions
		   (from_wallet_id, to_wallet_id, amount, currency_from, currency_to, converted_amount)
		 VALUES ($1, $2, $3::text::numeric, $4, $4, $3::text::numeric)
		 RETURNING id::text, from_wallet_id::text, to_wallet_id::text,
		           amount::text, converted_amount::text, status, created_at`,
		fromID, toID, amount, currency,
	).Scan(&t.ID, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.ConvertedAmount, &t.Status, &t.CreatedAt)
	if err != nil {
		return Transaction{}, err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO ledger_entries (wallet_id, transaction_id, amount, currency_code, entry_type)
		 VALUES ($1, $3, (-($4::text::numeric)), $5, 'transfer_debit'),
		        ($2, $3, ($4::text::numeric),    $5, 'transfer_credit')`,
		fromID, toID, t.ID, amount, currency); err != nil {
		return Transaction{}, err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO idempotency_keys (key, request_hash, transaction_id) VALUES ($1, $2, $3)`,
		idemKey, reqHash, t.ID); err != nil {
		return Transaction{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Transaction{}, err
	}
	return t, nil
}

var ErrTransactionNotFound = errors.New("transaction not found")

// GetTransaction reads a transaction by id.
func GetTransaction(ctx context.Context, pool *pgxpool.Pool, id string) (Transaction, error) {
	t, err := getTransaction(ctx, pool, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Transaction{}, ErrTransactionNotFound
	}
	return t, err
}

// rowQuerier is satisfied by both *pgxpool.Pool and pgx.Tx.
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func getTransaction(ctx context.Context, q rowQuerier, id string) (Transaction, error) {
	var t Transaction
	err := q.QueryRow(ctx,
		`SELECT id::text, from_wallet_id::text, to_wallet_id::text, amount::text,
		        currency_from, currency_to, converted_amount::text, status, created_at
		 FROM transactions WHERE id = $1`, id,
	).Scan(&t.ID, &t.FromWalletID, &t.ToWalletID, &t.Amount,
		&t.CurrencyFrom, &t.CurrencyTo, &t.ConvertedAmount, &t.Status, &t.CreatedAt)
	return t, err
}