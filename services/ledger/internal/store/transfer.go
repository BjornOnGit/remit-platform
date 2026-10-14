package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInsufficientFunds = errors.New("insufficient funds")
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
func Transfer(ctx context.Context, pool *pgxpool.Pool, fromID, toID, amount, currency string) (Transaction, error) {
	fromID, toID = strings.ToLower(fromID), strings.ToLower(toID)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return Transaction{}, err
	}
	defer tx.Rollback(ctx)

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

	if err := tx.Commit(ctx); err != nil {
		return Transaction{}, err
	}
	return t, nil
}