package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/equiprent/backend/internal/logger"
)

// brokenStore is declared in ratelimit_test.go.
func TestFailClosedRejectsWhenStoreDown(t *testing.T) {
	l := New(brokenStore{}, logger.Discard())
	closed := Rule{Name: "auth", Limit: 5, Window: time.Minute, FailClosed: true}
	if l.Allow(context.Background(), closed, "k").Allowed {
		t.Error("FailClosed rule must reject when the store is down")
	}
}
