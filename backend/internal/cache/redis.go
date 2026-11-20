// Package cache wraps the optional Redis client. Redis holds only ephemeral
// data (cache, rate limits, pub/sub, job queues); PostgreSQL stays the source
// of truth, so the platform must keep working without business-data loss if
// Redis is flushed.
package cache

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/health"
)

// NewClient returns a Redis client, or (nil, nil) when Redis is not configured.
func NewClient(cfg config.RedisConfig) (*redis.Client, error) {
	if !cfg.Enabled() {
		return nil, nil
	}
	opts, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: invalid connection string")
	}
	return redis.NewClient(opts), nil
}

// HealthChecker reports whether Redis answers PING.
func HealthChecker(client *redis.Client) health.Checker {
	return health.CheckFunc{CheckName: "redis", Fn: func(ctx context.Context) error {
		return client.Ping(ctx).Err()
	}}
}
