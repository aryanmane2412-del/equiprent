package realtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type capture struct{ payloads []string }

func (c *capture) PublishRealtime(_ context.Context, p string) error {
	c.payloads = append(c.payloads, p)
	return nil
}

func TestPublishAndDispatch(t *testing.T) {
	bus := NewBus()
	a, b, other := uuid.New(), uuid.New(), uuid.New()
	sa, sb := bus.Subscribe(a), bus.Subscribe(b)
	so := bus.Subscribe(other)
	defer so.Close()
	c := &capture{}
	if err := Publish(context.Background(), c, []uuid.UUID{a, b}, ChatMessage, map[string]string{"conversation_id": "x"}); err != nil {
		t.Fatal(err)
	}
	if err := Publish(context.Background(), c, nil, ChatMessage, nil); err != nil || len(c.payloads) != 1 {
		t.Fatal("no recipients, no notify")
	}
	if err := bus.Dispatch(c.payloads[0]); err != nil {
		t.Fatal(err)
	}
	for _, s := range []*Sub{sa, sb} {
		e := <-s.C
		var d map[string]string
		_ = json.Unmarshal(e.Data, &d)
		if e.Type != ChatMessage || d["conversation_id"] != "x" || e.At.IsZero() {
			t.Fatalf("event = %+v", e)
		}
	}
	if len(so.C) != 0 {
		t.Fatal("others must not receive it")
	}
	sa.Close()
	sa.Close()
	if bus.Subscribers(a) != 0 || bus.Subscribers(b) != 1 {
		t.Fatal("subscriber bookkeeping")
	}
	if err := bus.Dispatch("not json"); err == nil {
		t.Fatal("garbage accepted")
	}
	// Payloads over the NOTIFY limit are refused rather than truncated.
	if err := Publish(context.Background(), c, []uuid.UUID{a}, ChatMessage, strings.Repeat("x", 8000)); err == nil {
		t.Fatal("oversized payload accepted")
	}
}

func TestSlowSubscriberGetsResync(t *testing.T) {
	bus := NewBus()
	u := uuid.New()
	s := bus.Subscribe(u)
	defer s.Close()
	for range 200 {
		bus.Deliver(u, Event{Type: ChatMessage})
	}
	resync := false
	for len(s.C) > 0 {
		if (<-s.C).Type == Resync {
			resync = true
		}
	}
	if !resync {
		t.Fatal("a client that fell behind must be told to resync")
	}
	bus.Broadcast(Event{Type: Resync})
	if (<-s.C).Type != Resync {
		t.Fatal("broadcast")
	}
}
