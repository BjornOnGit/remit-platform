package events

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Publisher interface {
	Publish(ctx context.Context, eventType, body string) error
}

type outboxRow struct {
	id        int64
	eventType string
	payload   string
}

// RunRelay polls the outbox and publishes unpublished events until ctx is cancelled.
// Delivery is at-least-once: a crash between publish and commit re-sends the event,
// so consumers must be idempotent.
func RunRelay(ctx context.Context, pool *pgxpool.Pool, pub Publisher, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := relayOnce(ctx, pool, pub)
			if n > 0 {
				log.Printf("outbox relay: published %d events", n)
			}
			if err != nil {
				log.Printf("outbox relay: %v", err)
			}
		}
	}
}

func relayOnce(ctx context.Context, pool *pgxpool.Pool, pub Publisher) (int, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx,
		`SELECT id, event_type, payload::text FROM outbox_events
		 WHERE published_at IS NULL ORDER BY id LIMIT 10 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return 0, err
	}
	var batch []outboxRow
	for rows.Next() {
		var r outboxRow
		if err := rows.Scan(&r.id, &r.eventType, &r.payload); err != nil {
			rows.Close()
			return 0, err
		}
		batch = append(batch, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	n := 0
	var pubErr error
	for _, r := range batch {
		if err := pub.Publish(ctx, r.eventType, r.payload); err != nil {
			pubErr = err
			break
		}
		if _, err := tx.Exec(ctx, `UPDATE outbox_events SET published_at = now() WHERE id = $1`, r.id); err != nil {
			return 0, err
		}
		n++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return n, pubErr
}