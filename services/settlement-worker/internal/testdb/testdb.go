// Package testdb gives tests an isolated Postgres schema with the project migrations applied.
// Tests are skipped when TEST_DATABASE_URL is not set.
// (Each Go module keeps its own small copy of this helper.)
package testdb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var seq atomic.Int64

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
	schema := fmt.Sprintf("t_%d_%d_%d", os.Getpid(), time.Now().UnixNano(), seq.Add(1))

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

// Exec runs a statement.
func Exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	_, err := pool.Exec(context.Background(), sql, args...)
	must(t, err)
}

// Text runs a query that returns a single text value.
func Text(t *testing.T, pool *pgxpool.Pool, query string, args ...any) string {
	t.Helper()
	var s string
	must(t, pool.QueryRow(context.Background(), query, args...).Scan(&s))
	return s
}

// Count counts rows; `from` may include a WHERE clause, e.g. "transactions WHERE status = 'settled'".
func Count(t *testing.T, pool *pgxpool.Pool, from string) int {
	t.Helper()
	var n int
	must(t, pool.QueryRow(context.Background(), "SELECT count(*) FROM "+from).Scan(&n))
	return n
}

// NumEq reports whether the single numeric value returned by query equals want.
func NumEq(t *testing.T, pool *pgxpool.Pool, query, want string) bool {
	t.Helper()
	var eq bool
	must(t, pool.QueryRow(context.Background(), "SELECT (("+query+") = $1::text::numeric)", want).Scan(&eq))
	return eq
}