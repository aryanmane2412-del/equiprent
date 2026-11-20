package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/equiprent/backend/internal/logger"
)

type fakeLocker struct{ held bool }

func (f *fakeLocker) WithLock(ctx context.Context, _ int64, fn func(context.Context) error) (bool, error) {
	if f.held {
		return false, nil
	}
	return true, fn(ctx)
}

func TestRunOnceCountsSuccessOnly(t *testing.T) {
	s := NewScheduler(&fakeLocker{}, logger.Discard())
	ok := Job{Name: "ok", Interval: time.Second, Run: func(context.Context) error { return nil }}
	bad := Job{Name: "bad", Interval: time.Second, Run: func(context.Context) error { return errors.New("boom") }}
	s.RunOnce(context.Background(), ok)
	s.RunOnce(context.Background(), bad)
	if s.Runs("ok") != 1 || s.Runs("bad") != 0 {
		t.Fatalf("runs ok=%d bad=%d", s.Runs("ok"), s.Runs("bad"))
	}
}

func TestPanicsAreRecovered(t *testing.T) {
	s := NewScheduler(nil, logger.Discard())
	s.RunOnce(context.Background(), Job{Name: "panics", Interval: time.Second, Run: func(context.Context) error { panic("x") }})
	if s.Runs("panics") != 0 {
		t.Fatal("a panicking run must not count as done")
	}
}

func TestLockedElsewhereIsSkipped(t *testing.T) {
	var calls atomic.Int32
	s := NewScheduler(&fakeLocker{held: true}, logger.Discard())
	s.RunOnce(context.Background(), Job{Name: "j", Interval: time.Second, Run: func(context.Context) error { calls.Add(1); return nil }})
	if calls.Load() != 0 || s.Runs("j") != 0 {
		t.Fatal("job ran without the lock")
	}
}

func TestTimeoutIsApplied(t *testing.T) {
	s := NewScheduler(nil, logger.Discard())
	var deadline time.Time
	s.RunOnce(context.Background(), Job{Name: "t", Interval: time.Hour, Timeout: 50 * time.Millisecond, Run: func(ctx context.Context) error {
		deadline, _ = ctx.Deadline()
		return nil
	}})
	if d := time.Until(deadline); d > 50*time.Millisecond || d < -time.Second {
		t.Fatalf("deadline in %v", d)
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	var calls atomic.Int32
	s := NewScheduler(nil, logger.Discard(), Job{Name: "tick", Interval: 10 * time.Millisecond, Run: func(context.Context) error {
		calls.Add(1)
		return nil
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop")
	}
	if calls.Load() < 2 {
		t.Fatalf("job ran %d times", calls.Load())
	}
}

func TestLockKeyIsStableAndDistinct(t *testing.T) {
	first, again := LockKey("a"), LockKey("a")
	if first != again || first == LockKey("b") || first < 0 {
		t.Fatal("lock keys must be stable, distinct and non-negative")
	}
}
