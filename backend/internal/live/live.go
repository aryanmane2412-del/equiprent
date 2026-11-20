// Package live fans location updates out to WebSocket subscribers and
// signs the short-lived tickets that open those sockets.
//
// The hub is in-process: with several API replicas, put a shared bus
// (e.g. Redis pub/sub) behind Publish. Each subscriber has a bounded
// buffer; a slow client loses old updates rather than blocking ingestion.
package live

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Message is one update sent to subscribers (JSON).
type Message struct {
	Type        string    `json:"type" doc:"position, session_ended or snapshot"`
	EquipmentID uuid.UUID `json:"equipment_id"`
	BookingID   uuid.UUID `json:"booking_id,omitzero"`
	Lat         float64   `json:"lat,omitempty"`
	Lng         float64   `json:"lng,omitempty"`
	AccuracyM   *float32  `json:"accuracy_m,omitempty"`
	RecordedAt  time.Time `json:"recorded_at,omitzero"`
	Source      string    `json:"source,omitempty" doc:"app (a phone travelling with the machine), telematics (a tracker on the machine) or simulated"`
}

// Hub is a topic → subscribers fan-out keyed by equipment id.
type Hub struct {
	mu   sync.RWMutex
	subs map[uuid.UUID]map[*Sub]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[uuid.UUID]map[*Sub]struct{}{}} }

// Sub is one subscriber; read C until it closes.
type Sub struct {
	C       chan Message
	hub     *Hub
	topics  []uuid.UUID
	closeMu sync.Once
}

// Subscribe listens to the machines (buffer of 32 messages).
func (h *Hub) Subscribe(equipment ...uuid.UUID) *Sub {
	s := &Sub{C: make(chan Message, 32), hub: h, topics: equipment}
	h.mu.Lock()
	for _, id := range equipment {
		if h.subs[id] == nil {
			h.subs[id] = map[*Sub]struct{}{}
		}
		h.subs[id][s] = struct{}{}
	}
	h.mu.Unlock()
	return s
}

// Close unsubscribes.
func (s *Sub) Close() {
	s.closeMu.Do(func() {
		s.hub.mu.Lock()
		for _, id := range s.topics {
			delete(s.hub.subs[id], s)
			if len(s.hub.subs[id]) == 0 {
				delete(s.hub.subs, id)
			}
		}
		s.hub.mu.Unlock()
		close(s.C)
	})
}

// Publish delivers to every subscriber of the machine without blocking:
// a full buffer drops its oldest message.
func (h *Hub) Publish(m Message) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for s := range h.subs[m.EquipmentID] {
		select {
		case s.C <- m:
		default:
			select { // drop the oldest, keep the newest
			case <-s.C:
			default:
			}
			select {
			case s.C <- m:
			default:
			}
		}
		n++
	}
	return n
}

// Subscribers counts current subscribers of a machine (tests, metrics).
func (h *Hub) Subscribers(equipmentID uuid.UUID) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs[equipmentID])
}

// ---- tickets ----------------------------------------------------------------------------

// Ticket authorises one WebSocket for one user and subject until Exp.
// Browsers cannot set headers on a WebSocket, so the app first asks for a
// ticket over the authenticated API.
type Ticket struct {
	UserID  uuid.UUID `json:"u"`
	Kind    string    `json:"k"` // booking | fleet
	Subject uuid.UUID `json:"s"`
	Exp     int64     `json:"e"`
}

// Tickets signs and verifies tickets with HMAC-SHA256.
type Tickets struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

func NewTickets(secret string, ttl time.Duration) *Tickets {
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &Tickets{secret: []byte("ws-ticket\n" + secret), ttl: ttl, now: time.Now}
}

// ErrBadTicket is returned for forged, malformed or expired tickets.
var ErrBadTicket = errors.New("invalid or expired ticket")

func (t *Tickets) mac(payload string) string {
	m := hmac.New(sha256.New, t.secret)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Issue returns the ticket string and when it expires.
func (t *Tickets) Issue(user uuid.UUID, kind string, subject uuid.UUID) (string, time.Time) {
	exp := t.now().Add(t.ttl)
	raw, _ := json.Marshal(Ticket{UserID: user, Kind: kind, Subject: subject, Exp: exp.Unix()})
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + t.mac(payload), exp
}

// Verify checks the signature and expiry.
func (t *Tickets) Verify(s string) (Ticket, error) {
	payload, sig, ok := strings.Cut(s, ".")
	if !ok || !hmac.Equal([]byte(t.mac(payload)), []byte(sig)) {
		return Ticket{}, ErrBadTicket
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Ticket{}, ErrBadTicket
	}
	var tk Ticket
	if json.Unmarshal(raw, &tk) != nil || tk.UserID == uuid.Nil || t.now().Unix() > tk.Exp {
		return Ticket{}, ErrBadTicket
	}
	return tk, nil
}

// TTLSeconds is for API docs.
func (t *Tickets) TTLSeconds() string { return strconv.Itoa(int(t.ttl.Seconds())) }
