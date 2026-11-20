// Package realtime pushes small per-user events (new chat message, read
// receipt, new notification) to connected clients.
//
// Events are published with pg_notify inside the writer's transaction, so
// they are sent only if the change commits, and they reach every API
// instance (and come from the worker process too). Each instance runs a
// Listener that feeds its in-process Bus; WebSocket connections subscribe
// to their user's topic.
//
// Events are hints, never the record: clients fetch the data through the
// normal access-checked APIs (e.g. GET /conversations/{id}/messages?after=).
// After a reconnect a client resynchronises the same way, so a missed event
// costs nothing but latency.
package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Channel is the Postgres NOTIFY channel.
const Channel = "equiprent_realtime"

// maxPayload stays under Postgres's 8000-byte NOTIFY limit.
const maxPayload = 7500

// Event types.
const (
	ChatMessage  = "chat.message"
	ChatRead     = "chat.read"
	ChatDeleted  = "chat.deleted"
	Notification = "notification"
	// Resync tells clients to refetch: sent when this instance's listener
	// reconnected and may have missed events.
	Resync = "resync"
	Hello  = "hello"
)

// Event is what a client receives.
type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
	At   time.Time       `json:"at"`
}

// envelope is the NOTIFY payload: the event plus its recipients.
type envelope struct {
	Users []uuid.UUID `json:"u"`
	Event
}

// Publisher runs pg_notify (db.Queries.PublishRealtime).
type Publisher interface {
	PublishRealtime(ctx context.Context, payload string) error
}

// Publish queues an event for users; it is sent when the surrounding
// transaction commits (immediately outside a transaction).
func Publish(ctx context.Context, q Publisher, users []uuid.UUID, typ string, data any) error {
	if len(users) == 0 {
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	body, err := json.Marshal(envelope{Users: users, Event: Event{Type: typ, Data: raw, At: time.Now().UTC()}})
	if err != nil {
		return err
	}
	if len(body) > maxPayload {
		return fmt.Errorf("realtime event %s too large (%d bytes)", typ, len(body))
	}
	return q.PublishRealtime(ctx, string(body))
}

// ---- bus -------------------------------------------------------------------------------------

// Bus fans events out to the subscribers of each user on this instance.
type Bus struct {
	mu   sync.RWMutex
	subs map[uuid.UUID]map[*Sub]struct{}
}

func NewBus() *Bus { return &Bus{subs: map[uuid.UUID]map[*Sub]struct{}{}} }

// Sub is one connection's subscription.
type Sub struct {
	C    chan Event
	bus  *Bus
	user uuid.UUID
	once sync.Once
}

// Subscribe listens to one user's events (buffer of 64).
func (b *Bus) Subscribe(user uuid.UUID) *Sub {
	s := &Sub{C: make(chan Event, 64), bus: b, user: user}
	b.mu.Lock()
	if b.subs[user] == nil {
		b.subs[user] = map[*Sub]struct{}{}
	}
	b.subs[user][s] = struct{}{}
	b.mu.Unlock()
	return s
}

func (s *Sub) Close() {
	s.once.Do(func() {
		s.bus.mu.Lock()
		delete(s.bus.subs[s.user], s)
		if len(s.bus.subs[s.user]) == 0 {
			delete(s.bus.subs, s.user)
		}
		s.bus.mu.Unlock()
		close(s.C)
	})
}

// Deliver hands an event to the user's subscribers without blocking; a
// full buffer drops its oldest event and the client is told to resync.
func (b *Bus) Deliver(user uuid.UUID, e Event) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	n := 0
	for s := range b.subs[user] {
		select {
		case s.C <- e:
		default:
			select { // drop the oldest, ask for a resync
			case <-s.C:
			default:
			}
			select {
			case s.C <- Event{Type: Resync, At: e.At}:
			default:
			}
		}
		n++
	}
	return n
}

// Broadcast sends an event to every subscriber (used for Resync).
func (b *Bus) Broadcast(e Event) {
	b.mu.RLock()
	users := make([]uuid.UUID, 0, len(b.subs))
	for u := range b.subs {
		users = append(users, u)
	}
	b.mu.RUnlock()
	for _, u := range users {
		b.Deliver(u, e)
	}
}

// Subscribers counts a user's connections on this instance.
func (b *Bus) Subscribers(user uuid.UUID) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs[user])
}

// Dispatch parses one NOTIFY payload and delivers it.
func (b *Bus) Dispatch(payload string) error {
	var env envelope
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		return err
	}
	for _, u := range env.Users {
		b.Deliver(u, env.Event)
	}
	return nil
}

// ---- listener ---------------------------------------------------------------------------------

// Listen holds one pooled connection on LISTEN and feeds the bus until ctx
// ends, reconnecting with backoff. After every reconnect (events may have
// been missed meanwhile) all subscribers are told to resync.
func Listen(ctx context.Context, pool *pgxpool.Pool, bus *Bus, log *slog.Logger) {
	backoff := time.Second
	first := true
	for ctx.Err() == nil {
		err := listenOnce(ctx, pool, bus, log, func() {
			backoff = time.Second
			if !first {
				bus.Broadcast(Event{Type: Resync, At: time.Now().UTC()})
			}
			first = false
		})
		if ctx.Err() != nil {
			return
		}
		log.Warn("realtime listener disconnected; retrying", "error", err, "in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func listenOnce(ctx context.Context, pool *pgxpool.Pool, bus *Bus, log *slog.Logger, ready func()) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+Channel); err != nil {
		return err
	}
	ready()
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				// Leave the connection clean for the pool.
				_, _ = conn.Exec(context.Background(), "UNLISTEN *")
			}
			return err
		}
		if err := bus.Dispatch(n.Payload); err != nil {
			log.Warn("bad realtime payload", "error", err)
		}
	}
}
