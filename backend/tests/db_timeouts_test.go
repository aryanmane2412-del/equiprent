package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/database"
)

func timeoutPool(t *testing.T, mutate func(*config.DatabaseConfig)) interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Close()
} {
	t.Helper()
	cfg := config.DatabaseConfig{URL: databaseURL(t), MaxConns: 4, ConnectTimeout: 5 * time.Second}
	mutate(&cfg)
	pool, err := database.NewPool(context.Background(), cfg, "timeout-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// A runaway query is cancelled by the database itself, with the standard "query_canceled" code,
// instead of holding a request or a worker job.
func TestStatementTimeoutCancelsARunawayQuery(t *testing.T) {
	pool := timeoutPool(t, func(c *config.DatabaseConfig) { c.StatementTimeout = 300 * time.Millisecond })
	start := time.Now()
	_, err := pool.Exec(context.Background(), `SELECT pg_sleep(5)`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "57014" {
		t.Fatalf("want query_canceled (57014), got %v", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("the query ran %s instead of being cancelled at 300ms", took)
	}
}

// Waiting for a lock another session holds gives up quickly (55P03 lock_not_available).
func TestLockTimeoutStopsALongWait(t *testing.T) {
	holder := timeoutPool(t, func(c *config.DatabaseConfig) {})
	waiter := timeoutPool(t, func(c *config.DatabaseConfig) { c.LockTimeout = 300 * time.Millisecond })
	ctx := context.Background()
	if _, err := holder.Exec(ctx, `CREATE TABLE IF NOT EXISTS zz_lock_probe (id int)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = holder.Exec(ctx, `DROP TABLE IF EXISTS zz_lock_probe`) })

	// One session holds an exclusive table lock inside an open transaction...
	tx, err := database.NewPool(ctx, config.DatabaseConfig{URL: databaseURL(t), MaxConns: 1, ConnectTimeout: 5 * time.Second}, "lock-holder")
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	conn, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Rollback(ctx) }()
	if _, err := conn.Exec(ctx, `LOCK TABLE zz_lock_probe IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	// ...and another needs it: it must give up, not wait forever.
	start := time.Now()
	_, err = waiter.Exec(ctx, `SELECT count(*) FROM zz_lock_probe`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("want lock_not_available (55P03), got %v", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("waited %s for the lock instead of giving up at 300ms", took)
	}
}

// A guard set to 0 is off, and the defaults are on.
func TestTimeoutDefaultsAreOnAndZeroDisables(t *testing.T) {
	cfg, err := config.Load(func(k string) (string, bool) {
		switch k {
		case "DATABASE_URL":
			return databaseURL(t), true
		case "APP_ENV":
			return "test", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.StatementTimeout != 25*time.Second || cfg.Database.LockTimeout != 10*time.Second || cfg.Database.IdleInTxTimeout != 60*time.Second {
		t.Fatalf("defaults: %+v", cfg.Database)
	}
	off := timeoutPool(t, func(c *config.DatabaseConfig) {})
	if _, err := off.Exec(context.Background(), `SELECT pg_sleep(0.4)`); err != nil {
		t.Fatalf("with no guard a 0.4 s query must simply run: %v", err)
	}
}
