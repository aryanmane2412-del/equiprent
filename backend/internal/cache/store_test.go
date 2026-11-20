package cache

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryStoreGetSetExpiry(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)
	s := NewMemoryStore()
	s.now = func() time.Time { return now }

	if _, err := s.Get(ctx, "k"); !errors.Is(err, ErrMiss) {
		t.Fatalf("want miss, got %v", err)
	}
	_ = s.Set(ctx, "k", []byte("v"), time.Minute)
	if v, err := s.Get(ctx, "k"); err != nil || string(v) != "v" {
		t.Fatalf("get = %q, %v", v, err)
	}
	now = now.Add(time.Minute)
	if _, err := s.Get(ctx, "k"); !errors.Is(err, ErrMiss) {
		t.Fatal("entry should have expired")
	}
}

func TestMemoryStoreSetNXAndDelete(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	if ok, _ := s.SetNX(ctx, "lock", []byte("a"), time.Minute); !ok {
		t.Fatal("first SetNX should win")
	}
	if ok, _ := s.SetNX(ctx, "lock", []byte("b"), time.Minute); ok {
		t.Fatal("second SetNX should lose")
	}
	_ = s.Delete(ctx, "lock")
	if ok, _ := s.SetNX(ctx, "lock", []byte("c"), time.Minute); !ok {
		t.Fatal("SetNX after delete should win")
	}
}

func TestMemoryStoreIncrFixedWindow(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)
	s := NewMemoryStore()
	s.now = func() time.Time { return now }

	for i := int64(1); i <= 3; i++ {
		n, ttl, _ := s.Incr(ctx, "rl", 10*time.Second)
		if n != i || ttl != 10*time.Second {
			t.Fatalf("incr %d: n=%d ttl=%v", i, n, ttl)
		}
	}
	now = now.Add(4 * time.Second)
	if _, ttl, _ := s.Incr(ctx, "rl", 10*time.Second); ttl != 6*time.Second {
		t.Fatalf("window should not be extended, ttl=%v", ttl)
	}
	now = now.Add(6 * time.Second)
	if n, _, _ := s.Incr(ctx, "rl", 10*time.Second); n != 1 {
		t.Fatalf("new window should restart at 1, got %d", n)
	}
}
