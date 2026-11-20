// Package bootstrap holds start-up code shared by cmd/api and cmd/worker.
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/equiprent/backend/internal/cache"
	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/health"
	"github.com/equiprent/backend/internal/logger"
	"github.com/equiprent/backend/internal/version"
)

// Init loads .env (outside production), reads configuration and builds the
// logger. serviceName overrides SERVICE_NAME when non-empty.
func Init(serviceName string) (config.Config, *slog.Logger, error) {
	envFile := ""
	if os.Getenv("APP_ENV") != string(config.EnvProduction) {
		// Scripts run from backend/; the shared .env lives at the repo root.
		f, err := config.LoadDotEnv(".env", "../.env")
		if err != nil {
			return config.Config{}, nil, err
		}
		envFile = f
	}

	cfg, err := config.FromEnv()
	if err != nil {
		return config.Config{}, nil, err
	}
	if serviceName != "" {
		cfg.ServiceName = serviceName
	}

	log := logger.New(os.Stdout, cfg.Log, cfg.ServiceName, cfg.Env)
	slog.SetDefault(log)
	log.Info("starting",
		"version", version.Version, "commit", version.Commit,
		"env_file", envFile, "redis_enabled", cfg.Redis.Enabled())
	return cfg, log, nil
}

// Dependencies are the shared infrastructure clients.
type Dependencies struct {
	DB    *pgxpool.Pool
	Redis *redis.Client // nil when Redis is disabled
}

// Connect opens the PostgreSQL pool and, if configured, Redis. Connectivity
// problems are logged, not fatal: readiness reports them until they recover.
func Connect(ctx context.Context, cfg config.Config, log *slog.Logger) (*Dependencies, error) {
	pool, err := database.NewPool(ctx, cfg.Database, cfg.ServiceName)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, cfg.Database.ConnectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		log.Warn("postgres not reachable yet; readiness will report down", "error", err)
	} else {
		log.Info("postgres connected")
	}

	rdb, err := cache.NewClient(cfg.Redis)
	if err != nil {
		pool.Close()
		return nil, err
	}
	if rdb != nil {
		if err := rdb.Ping(pingCtx).Err(); err != nil {
			log.Warn("redis not reachable yet; readiness will report down", "error", err)
		} else {
			log.Info("redis connected")
		}
	}
	return &Dependencies{DB: pool, Redis: rdb}, nil
}

// Store returns the key-value store: Redis when configured, otherwise an
// in-process store (fine for a single API replica in development). Call it
// once per process and share the result.
func (d *Dependencies) Store() cache.Store {
	if d.Redis != nil {
		return cache.NewRedisStore(d.Redis, "equiprent:")
	}
	return cache.NewMemoryStore()
}

// Health builds the readiness service for the configured dependencies.
func (d *Dependencies) Health(cfg config.Config) *health.Service {
	checkers := []health.Checker{database.HealthChecker(d.DB)}
	if d.Redis != nil {
		checkers = append(checkers, cache.HealthChecker(d.Redis))
	}
	svc := health.NewService(2*time.Second, !cfg.Env.IsProduction(), checkers...)
	if d.Redis == nil {
		svc.Disable("redis")
	}
	return svc
}

// Close releases all clients.
func (d *Dependencies) Close() error {
	var err error
	if d.Redis != nil {
		if cerr := d.Redis.Close(); cerr != nil {
			err = fmt.Errorf("close redis: %w", cerr)
		}
	}
	d.DB.Close()
	return err
}
