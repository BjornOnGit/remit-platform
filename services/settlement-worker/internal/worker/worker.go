package worker

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Run executes job immediately, then every interval, until ctx is cancelled.
// A failing job is logged and retried on the next tick.
func Run(ctx context.Context, interval time.Duration, job func(context.Context) error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := job(ctx); err != nil && ctx.Err() == nil {
			log.Printf("settlement run failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// netSQL nets all posted, not-yet-settled cross-currency transactions in one statement:
//   - picked: lock the rows (SKIP LOCKED, so concurrent workers never double-settle)
//   - net:    per corridor (sorted currency pair, e.g. NGN-USD) sum in the pair's first
//     currency. A->B transfers add their amount; B->A transfers subtract their converted
//     amount (already in A). Using the stored amounts ties the settlement exactly to
//     what the ledger posted. Positive = net flow from the base currency side.
//   - ins:    one settlement per corridor
//   - UPDATE: link the transactions to it and mark them in_settlement
const netSQL = `
WITH picked AS (
  SELECT id, corridor, currency_from, amount, converted_amount
  FROM transactions
  WHERE status = 'posted' AND corridor IS NOT NULL AND settlement_id IS NULL
  FOR UPDATE SKIP LOCKED
),
net AS (
  SELECT corridor,
         split_part(corridor, '-', 1) AS base,
         SUM(CASE WHEN currency_from = split_part(corridor, '-', 1)
                  THEN amount ELSE -converted_amount END) AS net_amount
  FROM picked
  GROUP BY corridor
),
ins AS (
  INSERT INTO settlements (corridor, currency_code, net_amount)
  SELECT corridor, base, net_amount FROM net
  RETURNING id, corridor
)
UPDATE transactions t
SET settlement_id = ins.id, status = 'in_settlement'
FROM picked p JOIN ins ON ins.corridor = p.corridor
WHERE t.id = p.id
RETURNING t.settlement_id::text`

// NetPosted creates settlements for all unsettled transactions.
// It returns the number of transactions included and the number of settlements created.
func NetPosted(ctx context.Context, pool *pgxpool.Pool) (int, int, error) {
	rows, err := pool.Query(ctx, netSQL)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	txCount := 0
	settlements := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, 0, err
		}
		txCount++
		settlements[id] = struct{}{}
	}
	return txCount, len(settlements), rows.Err()
}

// Rail sends a settlement to the external payment rail.
type Rail interface {
	Send(ctx context.Context, id, corridor, currency, netAmount string) error
}

// SimulatedRail stands in for a real rail: it just logs and succeeds.
type SimulatedRail struct{}

func (SimulatedRail) Send(_ context.Context, id, corridor, currency, netAmount string) error {
	log.Printf("rail: sent settlement %s corridor=%s net=%s %s", id, corridor, netAmount, currency)
	return nil
}

type created struct{ id, corridor, currency, net string }

// SettleCreated sends every `created` settlement to the rail and marks it and its
// transactions settled. A rail failure stops the run; unsent settlements stay `created`
// and are retried on the next run. Returns the number of settlements completed.
func SettleCreated(ctx context.Context, pool *pgxpool.Pool, rail Rail) (int, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx,
		`SELECT id::text, corridor, currency_code, net_amount::text FROM settlements
		 WHERE status = 'created' ORDER BY created_at FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return 0, err
	}
	var batch []created
	for rows.Next() {
		var c created
		if err := rows.Scan(&c.id, &c.corridor, &c.currency, &c.net); err != nil {
			rows.Close()
			return 0, err
		}
		batch = append(batch, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	n := 0
	var railErr error
	for _, c := range batch {
		if err := rail.Send(ctx, c.id, c.corridor, c.currency, c.net); err != nil {
			railErr = err
			break
		}
		if _, err := tx.Exec(ctx,
			`UPDATE settlements SET status = 'settled', settled_at = now() WHERE id = $1`, c.id); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE transactions SET status = 'settled' WHERE settlement_id = $1 AND status = 'in_settlement'`, c.id); err != nil {
			return 0, err
		}
		n++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return n, railErr
}

// Settle is one settlement run: net new transactions, then send created settlements.
func Settle(ctx context.Context, pool *pgxpool.Pool, rail Rail) error {
	txs, settlements, err := NetPosted(ctx, pool)
	if err != nil {
		return err
	}
	if txs > 0 {
		log.Printf("netted %d transactions into %d settlements", txs, settlements)
	}

	sent, err := SettleCreated(ctx, pool, rail)
	if sent > 0 {
		log.Printf("settled %d settlements", sent)
	}
	if err != nil {
		return err
	}
	if txs == 0 && sent == 0 {
		log.Println("no-op: nothing to settle")
	}
	return nil
}