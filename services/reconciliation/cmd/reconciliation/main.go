package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"remit-platform/reconciliation/internal/recon"
)

func main() {
	once := flag.Bool("once", false, "run once and exit (exit code 1 if any mismatch)")
	flag.Parse()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	interval := 24 * time.Hour
	if v := os.Getenv("RECON_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			log.Fatalf("RECON_INTERVAL must be a positive duration like 24h: %q", v)
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

	if *once {
		if run(ctx, pool) {
			pool.Close()
			os.Exit(1)
		}
		return
	}

	log.Printf("reconciliation started (interval %s)", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		run(ctx, pool)
		select {
		case <-ctx.Done():
			log.Println("reconciliation stopped")
			return
		case <-ticker.C:
		}
	}
}

// run executes all checks and reports whether any mismatch (or error) occurred.
func run(ctx context.Context, pool *pgxpool.Pool) bool {
	results, err := recon.Run(ctx, pool)
	bad := false
	for _, r := range results {
		if r.Mismatches > 0 {
			log.Printf("MISMATCH %s: %d problems", r.Check, r.Mismatches)
			bad = true
		} else {
			log.Printf("ok       %s", r.Check)
		}
	}
	if err != nil {
		log.Printf("reconciliation failed: %v", err)
		bad = true
	}
	return bad
}