// Package database owns the PostgreSQL connection pool (pgx v5).
// Generated sqlc query code lives in internal/db.
package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/health"
)

// NewPool creates a pgx pool. It does not block on connecting: pgxpool dials
// lazily, so the API can start (and report not-ready) while the database is
// still coming up. Call Ping to verify connectivity.
func NewPool(ctx context.Context, cfg config.DatabaseConfig, applicationName string) (*pgxpool.Pool, error) {
	pcfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		// Never include the URL: it contains the password.
		return nil, fmt.Errorf("parse DATABASE_URL: invalid connection string")
	}
	pcfg.MaxConns = cfg.MaxConns
	pcfg.MinConns = cfg.MinConns
	pcfg.MaxConnLifetime = cfg.MaxConnLifetime
	pcfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	pcfg.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	pcfg.ConnConfig.RuntimeParams["application_name"] = applicationName
	// Store and compare in UTC; clients convert to IST for display.
	pcfg.ConnConfig.RuntimeParams["timezone"] = "UTC"

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	return pool, nil
}

// HealthChecker reports whether the pool can reach PostgreSQL.
func HealthChecker(pool *pgxpool.Pool) health.Checker {
	return health.CheckFunc{CheckName: "postgres", Fn: pool.Ping}
}
