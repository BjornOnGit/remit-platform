package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"remit-platform/ledger/internal/corr"
	"remit-platform/ledger/internal/fx"
)

var (
	ErrIdempotencyConflict = errors.New("idempotency key already used with a different request")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrCurrencyMismatch    = errors.New("transfer currency must match the source wallet currency")
	ErrAmountTooSmall      = errors.New("amount is too small after conversion")
	ErrTransactionNotFound = errors.New("transaction not found")
)

type Transaction struct {
	ID              string
	FromWalletID    string
	ToWalletID      string
	Amount          string
	CurrencyFrom    string
	CurrencyTo      string
	ConvertedAmount string
	FxRate          string
	FxSource        string
	FxTimestamp     string
	Status          string
	CreatedAt       time.Time
}

type entry struct {
	walletID string
	amount   decimal.Decimal // negative = debit, positive = credit
	currency string
	kind     string
}

// Transfer moves money between two wallets in one DB transaction.
// Same-currency: 2 ledger entries. Cross-currency: 4 entries via the system FX wallets.
// quote is required when the two wallets use different currencies.
func Transfer(ctx context.Context, pool *pgxpool.Pool, fromID, toID, amount, currency, idemKey, reqHash string, quote *fx.Quote) (Transaction, error) {
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

	// Lock both user wallets ordered by id to avoid deadlocks.
	currencies, err := lockWallets(ctx, tx,
		`SELECT id::text, currency_code FROM wallets WHERE id = ANY($1::uuid[]) ORDER BY id FOR UPDATE`,
		[]string{fromID, toID})
	if err != nil {
		return Transaction{}, err
	}
	fromCur, okFrom := currencies[fromID]
	toCur, okTo := currencies[toID]
	if !okFrom || !okTo {
		return Transaction{}, ErrWalletNotFound
	}
	if fromCur != currency {
		return Transaction{}, ErrCurrencyMismatch
	}

	amt, err := decimal.NewFromString(amount)
	if err != nil {
		return Transaction{}, err
	}

	entries := []entry{{fromID, amt.Neg(), fromCur, "transfer_debit"}}
	converted := amt
	var rate, source, corridor string
	var fxTime *time.Time

	if toCur == fromCur {
		entries = append(entries, entry{toID, amt, toCur, "transfer_credit"})
	} else {
		if quote == nil {
			return Transaction{}, errors.New("fx quote required for cross-currency transfer")
		}
		converted = amt.Mul(quote.Rate).RoundBank(2)
		if !converted.IsPositive() {
			return Transaction{}, ErrAmountTooSmall
		}

		// Lock system FX wallets (always after user wallets, ordered by id).
		sys, err := lockWallets(ctx, tx,
			`SELECT currency_code, id::text FROM wallets
			 WHERE is_system AND currency_code = ANY($1::text[]) ORDER BY id FOR UPDATE`,
			[]string{fromCur, toCur})
		if err != nil {
			return Transaction{}, err
		}
		if sys[fromCur] == "" || sys[toCur] == "" {
			return Transaction{}, errors.New("system FX wallet missing")
		}

		entries = append(entries,
			entry{sys[fromCur], amt, fromCur, "fx_credit"},
			entry{sys[toCur], converted.Neg(), toCur, "fx_debit"},
			entry{toID, converted, toCur, "transfer_credit"},
		)

		ts := quote.FetchedAt.UTC()
		fxTime = &ts
		rate, source = quote.Rate.String(), quote.Source
		corridor = corridorOf(fromCur, toCur)
	}

	for _, e := range entries {
		if err := applyBalance(ctx, tx, e); err != nil {
			return Transaction{}, err
		}
	}

	var txID string
	err = tx.QueryRow(ctx,
		`INSERT INTO transactions
		   (from_wallet_id, to_wallet_id, amount, currency_from, currency_to, converted_amount,
		    fx_rate, fx_source, fx_timestamp, corridor, correlation_id)
		 VALUES ($1, $2, $3::text::numeric, $4, $5, $6::text::numeric,
		         NULLIF($7::text, '')::numeric, NULLIF($8::text, ''), $9::timestamptz, NULLIF($10::text, ''), NULLIF($11::text, ''))
		 RETURNING id::text`,
		fromID, toID, amt.String(), fromCur, toCur, converted.String(),
		rate, source, fxTime, corridor, corr.From(ctx),
	).Scan(&txID)
	if err != nil {
		return Transaction{}, err
	}

	for _, e := range entries {
		if _, err := tx.Exec(ctx,
			`INSERT INTO ledger_entries (wallet_id, transaction_id, amount, currency_code, entry_type)
			 VALUES ($1, $2, $3::text::numeric, $4, $5)`,
			e.walletID, txID, e.amount.String(), e.currency, e.kind); err != nil {
			return Transaction{}, err
		}
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO idempotency_keys (key, request_hash, transaction_id) VALUES ($1, $2, $3)`,
		idemKey, reqHash, txID); err != nil {
		return Transaction{}, err
	}

	t, err := getTransaction(ctx, tx, txID)
	if err != nil {
		return Transaction{}, err
	}

	// Transactional outbox: the event commits (or rolls back) together with the transfer.
	payload, err := json.Marshal(map[string]string{
		"transaction_id":   t.ID,
		"from_wallet_id":   t.FromWalletID,
		"to_wallet_id":     t.ToWalletID,
		"amount":           t.Amount,
		"currency_from":    t.CurrencyFrom,
		"currency_to":      t.CurrencyTo,
		"converted_amount": t.ConvertedAmount,
		"status":           t.Status,
		"created_at":       t.CreatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return Transaction{}, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO outbox_events (event_type, payload) VALUES ('transaction.posted', $1::text::jsonb)`,
		string(payload)); err != nil {
		return Transaction{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Transaction{}, err
	}
	return t, nil
}

// lockWallets runs a locking query returning (key, value) pairs of text columns.
func lockWallets(ctx context.Context, tx pgx.Tx, sql string, arg []string) (map[string]string, error) {
	rows, err := tx.Query(ctx, sql, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var a, b string
		if err := rows.Scan(&a, &b); err != nil {
			return nil, err
		}
		out[a] = b
	}
	return out, rows.Err()
}

func applyBalance(ctx context.Context, tx pgx.Tx, e entry) error {
	if e.amount.IsNegative() {
		tag, err := tx.Exec(ctx,
			`UPDATE wallets SET balance = balance - $2::text::numeric
			 WHERE id = $1 AND (is_system OR balance >= $2::text::numeric)`,
			e.walletID, e.amount.Neg().String())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrInsufficientFunds
		}
		return nil
	}
	_, err := tx.Exec(ctx,
		`UPDATE wallets SET balance = balance + $2::text::numeric WHERE id = $1`,
		e.walletID, e.amount.String())
	return err
}

func corridorOf(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "-" + b
}

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
		        currency_from, currency_to, converted_amount::text,
		        COALESCE(fx_rate::text, ''), COALESCE(fx_source, ''),
		        COALESCE(to_char(fx_timestamp AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
		        status, created_at
		 FROM transactions WHERE id = $1`, id,
	).Scan(&t.ID, &t.FromWalletID, &t.ToWalletID, &t.Amount,
		&t.CurrencyFrom, &t.CurrencyTo, &t.ConvertedAmount,
		&t.FxRate, &t.FxSource, &t.FxTimestamp, &t.Status, &t.CreatedAt)
	return t, err
}