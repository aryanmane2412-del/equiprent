// Package ratelimit implements fixed-window rate limiting on the cache Store
// (Redis in deployed environments, so limits are shared by all API pods).
package ratelimit

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/cache"
	"github.com/equiprent/backend/internal/httpx"
)

// Rule is "Limit requests per Window" for one named bucket.
type Rule struct {
	Name   string
	Limit  int
	Window time.Duration
	// FailClosed rejects requests when the store is unavailable. Use it for
	// credential endpoints, where an outage must not lift the brute-force
	// and SMS-cost protection.
	FailClosed bool
}

// Result describes one decision.
type Result struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
}

// Limiter counts requests in a cache Store.
type Limiter struct {
	store cache.Store
	log   *slog.Logger
	now   func() time.Time
}

// New returns a limiter on store.
func New(store cache.Store, log *slog.Logger) *Limiter {
	return &Limiter{store: store, log: log, now: time.Now}
}

// Allow records one hit for key under rule. If the store fails the request
// is allowed (fail open) and a warning is logged: an outage of the cache must
// not take the API down.
func (l *Limiter) Allow(ctx context.Context, rule Rule, key string) Result {
	window := l.now().UnixNano() / int64(rule.Window)
	bucket := "rl:" + rule.Name + ":" + key + ":" + strconv.FormatInt(window, 10)
	n, ttl, err := l.store.Incr(ctx, bucket, rule.Window)
	if err != nil {
		if rule.FailClosed {
			l.log.ErrorContext(ctx, "rate limiter unavailable, rejecting request", "rule", rule.Name, "error", err)
			return Result{Allowed: false, Limit: rule.Limit, RetryAfter: 30 * time.Second}
		}
		l.log.WarnContext(ctx, "rate limiter unavailable, allowing request", "rule", rule.Name, "error", err)
		return Result{Allowed: true, Limit: rule.Limit, Remaining: rule.Limit}
	}
	if ttl < 0 {
		ttl = rule.Window
	}
	remaining := max(rule.Limit-int(n), 0)
	return Result{Allowed: int(n) <= rule.Limit, Limit: rule.Limit, Remaining: remaining, RetryAfter: ttl}
}

// KeyFunc extracts the bucket key from a request.
type KeyFunc func(c *gin.Context) string

// ByIP keys on the client IP (honouring HTTP_TRUSTED_PROXIES).
func ByIP(c *gin.Context) string { return "ip:" + c.ClientIP() }

// ByUserOrIP keys on the authenticated user, falling back to the IP.
func ByUserOrIP(c *gin.Context) string {
	if p, ok := httpx.CurrentPrincipal(c); ok {
		return "u:" + p.UserID.String()
	}
	return ByIP(c)
}

// Middleware enforces rule per key and sets RateLimit-* headers.
func (l *Limiter) Middleware(rule Rule, key KeyFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		res := l.Allow(c.Request.Context(), rule, key(c))
		h := c.Writer.Header()
		h.Set("RateLimit-Limit", strconv.Itoa(res.Limit))
		h.Set("RateLimit-Remaining", strconv.Itoa(res.Remaining))
		if !res.Allowed {
			httpx.Fail(c, apperror.ErrRateLimited.WithRetryAfter(res.RetryAfter))
			return
		}
		c.Next()
	}
}
