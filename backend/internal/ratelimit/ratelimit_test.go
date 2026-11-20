package ratelimit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/equiprent/backend/internal/cache"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/logger"
)

func TestAllowCountsPerWindow(t *testing.T) {
	l := New(cache.NewMemoryStore(), logger.Discard())
	now := time.Unix(1_700_000_000, 0)
	l.now = func() time.Time { return now }
	rule := Rule{Name: "t", Limit: 2, Window: time.Minute}

	for i, want := range []bool{true, true, false} {
		if got := l.Allow(context.Background(), rule, "k"); got.Allowed != want {
			t.Fatalf("hit %d allowed=%v want %v", i, got.Allowed, want)
		}
	}
	if got := l.Allow(context.Background(), rule, "other"); !got.Allowed {
		t.Fatal("keys must be independent")
	}
	now = now.Add(time.Minute)
	if got := l.Allow(context.Background(), rule, "k"); !got.Allowed || got.Remaining != 1 {
		t.Fatalf("next window: %+v", got)
	}
}

type brokenStore struct{ cache.Store }

func (brokenStore) Incr(context.Context, string, time.Duration) (int64, time.Duration, error) {
	return 0, 0, errors.New("redis down")
}

func TestAllowFailsOpen(t *testing.T) {
	l := New(brokenStore{}, logger.Discard())
	if !l.Allow(context.Background(), Rule{Name: "t", Limit: 1, Window: time.Second}, "k").Allowed {
		t.Fatal("limiter must fail open")
	}
}

func TestMiddlewareReturns429WithRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	l := New(cache.NewMemoryStore(), logger.Discard())
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Next()
		if len(c.Errors) > 0 {
			httpx.WriteProblem(c, logger.Discard(), c.Errors.Last().Err)
		}
	})
	r.GET("/x", l.Middleware(Rule{Name: "x", Limit: 1, Window: time.Minute}, ByIP), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	do := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
		return w
	}
	if w := do(); w.Code != http.StatusNoContent || w.Header().Get("RateLimit-Remaining") != "0" {
		t.Fatalf("first: %d %v", w.Code, w.Header())
	}
	w := do()
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("second: %d retry-after=%q", w.Code, w.Header().Get("Retry-After"))
	}
}
