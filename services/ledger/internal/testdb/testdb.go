// Package testdb gives tests an isolated Postgres schema with the project migrations applied.
// Tests are skipped when TEST_DATABASE_URL is not set.
package testdb

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// New creates a throwaway schema, applies sql/migrations/*.sql to it, and returns a pool
// bound to that schema. The schema is dropped when the test ends.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping database tests")
	}
	ctx := context.Background()
	schema := "t_" + strings.ReplaceAll(uuid.NewString(), "-", "")

	admin, err := pgxpool.New(ctx, url)
	must(t, err)
	_, err = admin.Exec(ctx, "CREATE SCHEMA "+schema)
	must(t, err)

	cfg, err := pgxpool.ParseConfig(url)
	must(t, err)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	must(t, err)

	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})

	_, file, _, _ := runtime.Caller(0)
	files, err := filepath.Glob(filepath.Join(filepath.Dir(file), "../../../../sql/migrations/*.sql"))
	must(t, err)
	if len(files) == 0 {
		t.Fatal("no migrations found")
	}
	sort.Strings(files)
	for _, f := range files {
		sqlText, err := os.ReadFile(f)
		must(t, err)
		_, err = pool.Exec(ctx, string(sqlText))
		must(t, err)
	}
	return pool
}

// Wallet inserts a wallet with the given balance and returns its id.
func Wallet(t *testing.T, pool *pgxpool.Pool, currency, balance string, system bool) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(),
		`INSERT INTO wallets (owner_id, currency_code, balance, is_system)
		 VALUES ($1, $2, $3::text::numeric, $4) RETURNING id::text`,
		uuid.NewString(), currency, balance, system).Scan(&id)
	must(t, err)
	return id
}

// Text runs a query that returns a single text value.
func Text(t *testing.T, pool *pgxpool.Pool, query string, args ...any) string {
	t.Helper()
	var s string
	must(t, pool.QueryRow(context.Background(), query, args...).Scan(&s))
	return s
}

func Balance(t *testing.T, pool *pgxpool.Pool, walletID string) decimal.Decimal {
	t.Helper()
	return decimal.RequireFromString(Text(t, pool, `SELECT balance::text FROM wallets WHERE id = $1`, walletID))
}

// Count returns the number of rows in a table.
func Count(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	must(t, pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n))
	return n
}