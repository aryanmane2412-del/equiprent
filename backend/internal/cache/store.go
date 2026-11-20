package cache

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrMiss is returned by Get when the key does not exist.
var ErrMiss = errors.New("cache: miss")

// Store is the key-value interface the application uses for ephemeral data:
// rate-limit counters, OTP throttles, live positions, short-lived tickets.
// RedisStore is used when REDIS_URL is set; MemoryStore otherwise (single
// process only — fine for development and tests).
type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// SetNX sets the key only if absent and reports whether it was set.
	SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error)
	Delete(ctx context.Context, keys ...string) error
	// Incr increments a counter. The TTL is applied when the key is created
	// and the remaining TTL is returned (fixed-window rate limiting).
	Incr(ctx context.Context, key string, ttl time.Duration) (count int64, remaining time.Duration, err error)
	// Backend names the implementation ("redis" or "memory").
	Backend() string
}

// ---- Redis -----------------------------------------------------------------------

// RedisStore implements Store on go-redis.
type RedisStore struct {
	client *redis.Client
	prefix string
}

// NewRedisStore namespaces every key with prefix (e.g. "equiprent:").
func NewRedisStore(client *redis.Client, prefix string) *RedisStore {
	return &RedisStore{client: client, prefix: prefix}
}

func (s *RedisStore) Backend() string { return "redis" }

func (s *RedisStore) Get(ctx context.Context, key string) ([]byte, error) {
	v, err := s.client.Get(ctx, s.prefix+key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrMiss
	}
	return v, err
}

func (s *RedisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return s.client.Set(ctx, s.prefix+key, value, ttl).Err()
}

func (s *RedisStore) SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	err := s.client.SetArgs(ctx, s.prefix+key, value, redis.SetArgs{Mode: "NX", TTL: ttl}).Err()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	return err == nil, err
}

func (s *RedisStore) Delete(ctx context.Context, keys ...string) error {
	full := make([]string, len(keys))
	for i, k := range keys {
		full[i] = s.prefix + k
	}
	return s.client.Del(ctx, full...).Err()
}

// incrScript increments and sets the TTL atomically on first use.
var incrScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
return {n, redis.call('PTTL', KEYS[1])}`)

func (s *RedisStore) Incr(ctx context.Context, key string, ttl time.Duration) (int64, time.Duration, error) {
	res, err := incrScript.Run(ctx, s.client, []string{s.prefix + key}, ttl.Milliseconds()).Int64Slice()
	if err != nil {
		return 0, 0, err
	}
	return res[0], time.Duration(res[1]) * time.Millisecond, nil
}

// ---- in-memory -------------------------------------------------------------------

type memEntry struct {
	value   []byte
	count   int64
	expires time.Time // zero = no expiry
}

// MemoryStore implements Store in process memory with lazy expiry and a
// periodic sweep. It is safe for concurrent use.
type MemoryStore struct {
	mu      sync.Mutex
	data    map[string]*memEntry
	now     func() time.Time
	sweeps  int
	maxKeys int
}

// NewMemoryStore creates an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: map[string]*memEntry{}, now: time.Now, maxKeys: 100_000}
}

func (s *MemoryStore) Backend() string { return "memory" }

func (s *MemoryStore) live(key string) (*memEntry, bool) {
	e, ok := s.data[key]
	if !ok {
		return nil, false
	}
	if !e.expires.IsZero() && !s.now().Before(e.expires) {
		delete(s.data, key)
		return nil, false
	}
	return e, true
}

func (s *MemoryStore) expiry(ttl time.Duration) time.Time {
	if ttl <= 0 {
		return time.Time{}
	}
	return s.now().Add(ttl)
}

// sweep drops expired keys every 1024 writes so abandoned keys don't leak.
func (s *MemoryStore) sweep() {
	s.sweeps++
	if s.sweeps%1024 != 0 && len(s.data) < s.maxKeys {
		return
	}
	now := s.now()
	for k, e := range s.data {
		if !e.expires.IsZero() && !now.Before(e.expires) {
			delete(s.data, k)
		}
	}
}

func (s *MemoryStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.live(key)
	if !ok {
		return nil, ErrMiss
	}
	return append([]byte(nil), e.value...), nil
}

func (s *MemoryStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	s.data[key] = &memEntry{value: append([]byte(nil), value...), expires: s.expiry(ttl)}
	return nil
}

func (s *MemoryStore) SetNX(_ context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.live(key); ok {
		return false, nil
	}
	s.sweep()
	s.data[key] = &memEntry{value: append([]byte(nil), value...), expires: s.expiry(ttl)}
	return true, nil
}

func (s *MemoryStore) Delete(_ context.Context, keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		delete(s.data, k)
	}
	return nil
}

func (s *MemoryStore) Incr(_ context.Context, key string, ttl time.Duration) (int64, time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.live(key)
	if !ok {
		s.sweep()
		e = &memEntry{expires: s.expiry(ttl)}
		s.data[key] = e
	}
	e.count++
	remaining := time.Duration(-1)
	if !e.expires.IsZero() {
		remaining = e.expires.Sub(s.now())
	}
	return e.count, remaining, nil
}
