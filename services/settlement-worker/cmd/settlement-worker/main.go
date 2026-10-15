package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"remit-platform/settlement-worker/internal/worker"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	interval := time.Minute
	if v := os.Getenv("SETTLEMENT_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			log.Fatalf("SETTLEMENT_INTERVAL must be a positive duration like 1m: %q", v)
		}
		interval = d
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("database ping failed: %v", err)
	}

	log.Printf("settlement worker started (interval %s)", interval)
	worker.Run(ctx, interval, func(ctx context.Context) error {
		return worker.Settle(ctx, pool, worker.SimulatedRail{})
	})
	log.Println("settlement worker stopped")
}