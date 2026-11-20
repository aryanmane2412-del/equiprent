// Package tests holds integration tests that need real infrastructure.
//
// They run only when TEST_DATABASE_URL points at a disposable PostgreSQL
// database (the migrations are applied and rolled back):
//
//	TEST_DATABASE_URL=postgres://equiprent:equiprent@localhost:5434/equiprent_test?sslmode=disable go test ./tests/...
//
// scripts/test.ps1 -Integration sets this up for you.
package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // pgx5:// driver
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/health"
	"github.com/equiprent/backend/internal/httpserver"
	"github.com/equiprent/backend/internal/logger"
)

func databaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	return url
}

func newMigrator(t *testing.T, url string) *migrate.Migrate {
	t.Helper()
	src, err := iofs.New(os.DirFS("../migrations"), ".")
	if err != nil {
		t.Fatalf("open migrations: %v", err)
	}
	// golang-migrate's pgx v5 driver is registered under the pgx5 scheme.
	pgxURL := "pgx5://" + strings.SplitN(url, "://", 2)[1]
	m, err := migrate.NewWithSourceInstance("iofs", src, pgxURL)
	if err != nil {
		t.Fatalf("init migrate: %v", err)
	}
	t.Cleanup(func() { _, _ = m.Close() })
	return m
}

// freshDatabase creates an empty database next to the test database and drops
// it when the test ends. Needs CREATEDB (the dev/test role has it).
func freshDatabase(t *testing.T, url string) string {
	t.Helper()
	admin := newPool(t, url)
	name := fmt.Sprintf("equiprent_mig_%d", time.Now().UnixNano())
	if _, err := admin.Exec(context.Background(), "CREATE DATABASE "+name); err != nil {
		t.Skipf("cannot create a throwaway database (needs CREATEDB): %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	u, err := neturl.Parse(url)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}

func newPool(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()
	pool, err := database.NewPool(context.Background(), config.DatabaseConfig{
		URL: url, MaxConns: 4, ConnectTimeout: 5 * time.Second,
		MaxConnLifetime: time.Minute, MaxConnIdleTime: time.Minute,
	}, "equiprent-tests")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestMigrationsUpDownUp rolls every migration back and forward again on a
// throwaway database, because the shared test database holds rows (booking
// history, ledger, audit) that are append-only by design and would block the
// reference-data rollback.
func TestMigrationsUpDownUp(t *testing.T) {
	url := freshDatabase(t, databaseURL(t))
	m := newMigrator(t, url)
	pool := newPool(t, url)
	ctx := context.Background()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("up: %v", err)
	}
	assertExtensions(t, pool, true)

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("down: %v", err)
	}
	assertExtensions(t, pool, false)

	if err := m.Up(); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if v, dirty, err := m.Version(); err != nil || dirty || v < 1 {
		t.Fatalf("version=%d dirty=%v err=%v", v, dirty, err)
	}

	// The shared trigger function must exist for later migrations.
	var fn bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_proc WHERE proname = 'set_updated_at')`).Scan(&fn); err != nil || !fn {
		t.Fatalf("set_updated_at missing (err=%v)", err)
	}
}

func assertExtensions(t *testing.T, pool *pgxpool.Pool, want bool) {
	t.Helper()
	for _, ext := range []string{"citext", "pg_trgm", "btree_gist"} {
		var installed bool
		err := pool.QueryRow(context.Background(),
			`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = $1)`, ext).Scan(&installed)
		if err != nil {
			t.Fatal(err)
		}
		if installed != want {
			t.Errorf("extension %s installed=%v, want %v", ext, installed, want)
		}
	}
}

func TestSqlcGeneratedQuery(t *testing.T) {
	pool := newPool(t, databaseURL(t))
	info, err := db.New(pool).GetDatabaseInfo(context.Background())
	if err != nil {
		t.Fatalf("GetDatabaseInfo: %v", err)
	}
	if info.DatabaseName == "" || info.ServerVersion == "" {
		t.Errorf("info = %+v", info)
	}
	if time.Since(info.ServerTime).Abs() > time.Hour {
		t.Errorf("server_time looks wrong: %v", info.ServerTime)
	}
}

func TestReadinessAgainstRealDatabase(t *testing.T) {
	pool := newPool(t, databaseURL(t))
	router := httpserver.NewRouter(httpserver.Deps{
		Config: config.Config{Env: config.EnvTest, ServiceName: "equiprent-api", HTTP: config.HTTPConfig{MaxBodyBytes: 1 << 20}},
		Logger: logger.Discard(),
		Health: health.NewService(2*time.Second, true, database.HealthChecker(pool)).Disable("redis"),
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, httpserver.PathReadiness, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("readyz = %d %s", w.Code, w.Body)
	}
	var rep health.Report
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Checks["postgres"].Status != health.StatusUp {
		t.Errorf("postgres = %+v", rep.Checks["postgres"])
	}
}
