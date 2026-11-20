package live

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHubFanOutAndClose(t *testing.T) {
	h := NewHub()
	a, b := uuid.New(), uuid.New()
	s1 := h.Subscribe(a)
	s2 := h.Subscribe(a, b)
	if n := h.Publish(Message{Type: "position", EquipmentID: a, Lat: 1}); n != 2 {
		t.Fatalf("delivered to %d", n)
	}
	if n := h.Publish(Message{Type: "position", EquipmentID: b}); n != 1 {
		t.Fatalf("b delivered to %d", n)
	}
	if m := <-s1.C; m.Lat != 1 {
		t.Fatalf("s1 got %+v", m)
	}
	if len(s2.C) != 2 {
		t.Fatalf("s2 buffered %d", len(s2.C))
	}
	s1.Close()
	s1.Close() // idempotent
	if h.Subscribers(a) != 1 {
		t.Fatalf("subscribers after close = %d", h.Subscribers(a))
	}
	if _, ok := <-s1.C; ok {
		t.Fatal("closed subscription must close its channel")
	}
	s2.Close()
	if h.Subscribers(a) != 0 || h.Subscribers(b) != 0 {
		t.Fatal("topics are cleaned up")
	}
}

// A slow subscriber never blocks publishing; it keeps the newest updates.
func TestHubDropsOldestForSlowSubscribers(t *testing.T) {
	h := NewHub()
	a := uuid.New()
	s := h.Subscribe(a)
	defer s.Close()
	for i := range 100 {
		h.Publish(Message{EquipmentID: a, Lat: float64(i)})
	}
	var last Message
	for len(s.C) > 0 {
		last = <-s.C
	}
	if last.Lat != 99 {
		t.Fatalf("newest update lost: %+v", last)
	}
}

func TestTickets(t *testing.T) {
	tk := NewTickets("secret", time.Minute)
	user, booking := uuid.New(), uuid.New()
	s, exp := tk.Issue(user, "booking", booking)
	if exp.Before(time.Now()) {
		t.Fatal("expiry in the past")
	}
	got, err := tk.Verify(s)
	if err != nil || got.UserID != user || got.Subject != booking || got.Kind != "booking" {
		t.Fatalf("verify = %+v %v", got, err)
	}
	for name, bad := range map[string]string{
		"tampered":  s[:len(s)-2] + "xx",
		"no sig":    s[:len(s)-44],
		"garbage":   "abc",
		"other key": func() string { x, _ := NewTickets("other", time.Minute).Issue(user, "booking", booking); return x }(),
		"empty":     "",
		"swapped":   func() string { x, _ := tk.Issue(uuid.New(), "fleet", booking); return s[:len(s)-43] + x[len(x)-43:] }(),
	} {
		if _, err := tk.Verify(bad); err != ErrBadTicket {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
	tk.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if _, err := tk.Verify(s); err != ErrBadTicket {
		t.Fatal("expired ticket accepted")
	}
}
