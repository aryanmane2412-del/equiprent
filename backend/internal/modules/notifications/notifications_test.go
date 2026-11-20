package notifications

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/events"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/logger"
)

type memRepo struct {
	orgUsers map[uuid.UUID][]uuid.UUID
	sent     map[uuid.UUID][]Message
	muted    map[uuid.UUID][]string
	prefs    map[uuid.UUID]map[string]bool
	seen     map[string]bool
	drivers  []uuid.UUID
	admins   []uuid.UUID
}

func newMem() *memRepo {
	return &memRepo{orgUsers: map[uuid.UUID][]uuid.UUID{}, sent: map[uuid.UUID][]Message{},
		muted: map[uuid.UUID][]string{}, prefs: map[uuid.UUID]map[string]bool{}}
}

func (m *memRepo) CreateMany(_ context.Context, users []uuid.UUID, msg Message) error {
	for _, u := range users {
		m.sent[u] = append(m.sent[u], msg)
	}
	return nil
}
func (m *memRepo) List(context.Context, uuid.UUID, bool, httpx.Keyset) ([]Notification, error) {
	return nil, nil
}
func (m *memRepo) Unread(context.Context, uuid.UUID) (int32, error)             { return 0, nil }
func (m *memRepo) MarkRead(context.Context, uuid.UUID, uuid.UUID) (bool, error) { return true, nil }
func (m *memRepo) MarkAllRead(context.Context, uuid.UUID) (int64, error)        { return 0, nil }
func (m *memRepo) UpsertDevice(context.Context, db.UpsertDeviceParams) (uuid.UUID, error) {
	return uuid.New(), nil
}
func (m *memRepo) DeleteDevice(context.Context, uuid.UUID, string) (bool, error) { return true, nil }
func (m *memRepo) Devices(context.Context, uuid.UUID) ([]Device, error)          { return nil, nil }
func (m *memRepo) OrgUsers(_ context.Context, org uuid.UUID, _ []string) ([]uuid.UUID, error) {
	return m.orgUsers[org], nil
}
func (m *memRepo) OwnerRecipients(_ context.Context, org uuid.UUID, _, _ uuid.NullUUID) ([]uuid.UUID, error) {
	return m.orgUsers[org], nil
}
func (m *memRepo) Muted(_ context.Context, u uuid.UUID) ([]string, error) { return m.muted[u], nil }
func (m *memRepo) Preferences(_ context.Context, u uuid.UUID) (map[string]bool, error) {
	return m.prefs[u], nil
}
func (m *memRepo) SetPreferences(_ context.Context, u uuid.UUID, prefs []Preference) error {
	if m.prefs[u] == nil {
		m.prefs[u] = map[string]bool{}
	}
	for _, p := range prefs {
		m.prefs[u][p.Category] = p.Push
	}
	return nil
}

// recordPusher remembers pushes per user.
type recordPusher struct {
	mu     sync.Mutex
	pushed map[uuid.UUID][]Message
}

func (r *recordPusher) Push(_ context.Context, u uuid.UUID, m Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pushed == nil {
		r.pushed = map[uuid.UUID][]Message{}
	}
	r.pushed[u] = append(r.pushed[u], m)
	return nil
}

func mustPayload(t *testing.T, p events.Payload) []byte {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDispatchRoutesToTheOtherSideOnly(t *testing.T) {
	ownerOrg, customerOrg := uuid.New(), uuid.New()
	owner1, owner2, cust := uuid.New(), uuid.New(), uuid.New()
	repo := newMem()
	repo.orgUsers = map[uuid.UUID][]uuid.UUID{ownerOrg: {owner1, owner2}, customerOrg: {cust}}
	svc := NewService(repo, LogPusher{Log: logger.Discard()}, logger.Discard())
	ctx := context.Background()

	n, err := svc.Dispatch(ctx, events.BookingRequested, uuid.New(), mustPayload(t, events.Payload{
		Reference: "ER-2611-AAAA", OwnerOrgID: ownerOrg, CustomerOrgID: customerOrg, ActorUserID: cust}))
	if err != nil || n != 3 || len(repo.sent[cust]) != 1 || repo.sent[cust][0].Title != "Booking submitted" {
		t.Fatalf("requested: n=%d err=%v sent=%v", n, err, repo.sent)
	}
	if msg := repo.sent[owner1][0]; msg.Body != "Acme requested Excavator (ER-2611-AAAA) at Pune. Respond before it expires." || msg.Type != events.BookingRequested {
		t.Fatalf("message = %+v", msg)
	}
	if repo.sent[owner1][0].Data["booking_id"] == nil {
		t.Fatal("booking events carry booking_id for deep links")
	}

	// Both sides, minus the actor (owner1), no duplicates.
	n, _ = svc.Dispatch(ctx, events.BookingConfirmed, uuid.New(), mustPayload(t, events.Payload{Reference: "ER-2611-AAAA",
		OwnerOrgID: ownerOrg, CustomerOrgID: customerOrg, ActorUserID: owner1, UserIDs: []uuid.UUID{cust}}))
	if n != 2 || len(repo.sent[owner1]) != 1 || len(repo.sent[cust]) != 2 {
		t.Fatalf("confirmed: n=%d sent=%v", n, repo.sent)
	}

	if n, err := svc.Dispatch(ctx, "listing.unknown", uuid.New(), []byte(`{}`)); n != 0 || err != nil {
		t.Fatalf("unrouted event: %d %v", n, err)
	}
	if _, err := svc.Dispatch(ctx, events.BookingRequested, uuid.New(), []byte(`not json`)); err == nil {
		t.Fatal("malformed payload must error (the worker skips it)")
	}
}

func TestDispatchWordingPerSide(t *testing.T) {
	ownerOrg, customerOrg := uuid.New(), uuid.New()
	owner, cust := uuid.New(), uuid.New()
	repo := newMem()
	repo.orgUsers = map[uuid.UUID][]uuid.UUID{ownerOrg: {owner}, customerOrg: {cust}}
	svc := NewService(repo, LogPusher{Log: logger.Discard()}, logger.Discard())
	ctx := context.Background()

	pl := events.Payload{Reference: "ER-2611-BBBB", OwnerOrgID: ownerOrg, CustomerOrgID: customerOrg,
		Extra: map[string]any{"amount_paise": 12345650, "purpose": "advance", "booking_id": uuid.New()}}
	if n, err := svc.Dispatch(ctx, events.PaymentCaptured, uuid.New(), mustPayload(t, pl)); err != nil || n != 2 {
		t.Fatalf("payment: %d %v", n, err)
	}
	if got := repo.sent[cust][0]; got.Title != "Payment confirmed" || got.Body != "We received ₹1,23,456.50 for ER-2611-BBBB. Thank you!" {
		t.Fatalf("customer wording = %+v", got)
	}
	if got := repo.sent[owner][0]; got.Title != "Payment received" || !strings.Contains(got.Body, "₹1,23,456.50") {
		t.Fatalf("owner wording = %+v", got)
	}

	// Agreement signed by the customer: the owner hears the signer's name.
	pl = events.Payload{Reference: "ER-2611-BBBB", OwnerOrgID: ownerOrg, CustomerOrgID: customerOrg, ActorUserID: cust,
		Extra: map[string]any{"actor_name": "Asha Rao"}}
	_, _ = svc.Dispatch(ctx, events.AgreementSigned, uuid.New(), mustPayload(t, pl))
	if got := repo.sent[owner][1]; got.Body != "Asha Rao signed the rental agreement for ER-2611-BBBB." {
		t.Fatalf("agreement wording = %+v", got)
	}
	if _, leaked := repo.sent[owner][1].Data["actor_name"]; leaked {
		t.Fatal("actor_name is for wording only")
	}
}

func TestChatIsPushOnlyAndRespectsMutes(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	repo := newMem()
	repo.muted[c] = []string{CatChat}
	push := &recordPusher{}
	svc := NewService(repo, push, logger.Discard())
	pl := events.Payload{ActorUserID: a, UserIDs: []uuid.UUID{a, b, c}, Title: "Please start at the north corner.",
		Extra: map[string]any{"actor_name": "Ravi", "conversation_id": uuid.New()}}
	n, err := svc.Dispatch(context.Background(), events.ChatMessage, uuid.New(), mustPayload(t, pl))
	if err != nil || n != 2 {
		t.Fatalf("chat: %d %v", n, err)
	}
	if len(repo.sent) != 0 {
		t.Fatalf("chat messages must not create in-app notifications: %v", repo.sent)
	}
	if len(push.pushed[b]) != 1 || len(push.pushed[c]) != 0 || len(push.pushed[a]) != 0 {
		t.Fatalf("pushed = %v", push.pushed)
	}
	if got := push.pushed[b][0]; got.Title != "Ravi" || got.Body != "Please start at the north corner." {
		t.Fatalf("chat push = %+v", got)
	}
}

func TestPreferencesDefaultToOn(t *testing.T) {
	repo := newMem()
	svc := NewService(repo, LogPusher{Log: logger.Discard()}, logger.Discard())
	p := &httpx.Principal{UserID: uuid.New()}
	list, _ := svc.SetPreferences(context.Background(), p, PreferencesRequest{Preferences: []Preference{{Category: CatChat, Push: false}}})
	if len(list.Data) != len(AllCategories) {
		t.Fatalf("prefs = %+v", list)
	}
	for _, pr := range list.Data {
		if pr.Push != (pr.Category != CatChat) {
			t.Fatalf("pref %+v", pr)
		}
	}
}

func TestCategoryAndRoutes(t *testing.T) {
	cases := map[string]string{
		events.BookingRequested: CatBookings, events.PaymentCaptured: CatPayments, events.BookingFinalCharges: CatPayments,
		events.PayoutPaid: CatPayments, events.AgreementExecuted: CatAgreements, events.ChatMessage: CatChat,
		events.MaintenanceReminder: CatMaintenance, events.DocumentExpiring: CatMaintenance, events.ReviewReceived: CatReviews,
		events.VerificationApproved: CatAccount, events.DisputeOpened: CatBookings,
	}
	for ev, want := range cases {
		if got := Category(ev); got != want {
			t.Errorf("Category(%s) = %s, want %s", ev, got, want)
		}
	}
	// Everything the task lists is routed.
	for _, ev := range []string{events.BookingRequested, events.BookingAccepted, events.BookingRejected, events.PaymentCaptured,
		events.BookingOperatorAssigned, events.BookingActive, events.BookingCompleted, events.BookingFinalCharges, events.PayoutPaid,
		events.MaintenanceReminder, events.MaintenanceDue, events.MaintenanceOverdue,
		events.BookingCancellationRequested, events.BookingCancellationDeclined, events.BookingExtensionRequested,
		events.BookingExtensionApproved, events.BookingExtensionDeclined, events.BookingExtensionExpired, events.BookingPaymentExpired,
		events.BookingRefundPending, events.BookingRefunded, events.RefundProcessed, events.PaymentFailed} {
		rt, ok := routes[ev]
		if !ok || rt.title == "" || (!rt.toOwner && !rt.toCustomer && !rt.toUsers) {
			t.Errorf("bad route %s: %+v", ev, rt)
		}
	}
	for ev := range routes {
		if !strings.Contains(ev, ".") || !slices.Contains(AllCategories, Category(ev)) {
			t.Errorf("route %s has no category", ev)
		}
	}
}

func TestINR(t *testing.T) {
	for paise, want := range map[int64]string{0: "₹0.00", 5: "₹0.05", 99900: "₹999.00", 100000: "₹1,000.00",
		12345650: "₹1,23,456.50", 1000000000: "₹1,00,00,000.00", -250: "-₹2.50"} {
		if got := INR(paise); got != want {
			t.Errorf("INR(%d) = %s, want %s", paise, got, want)
		}
	}
}

// ---- FCM ----------------------------------------------------------------------------------

type memDevices struct {
	tokens  map[uuid.UUID][]string
	pushed  []string
	failed  map[string]string
	dropped []string
}

func (m *memDevices) PushTokens(_ context.Context, u uuid.UUID) ([]string, error) {
	return m.tokens[u], nil
}
func (m *memDevices) MarkPushed(_ context.Context, tok string) error {
	m.pushed = append(m.pushed, tok)
	return nil
}
func (m *memDevices) MarkFailed(_ context.Context, tok, reason string) error {
	m.failed[tok] = reason
	return nil
}
func (m *memDevices) DropToken(_ context.Context, tok string) error {
	m.dropped = append(m.dropped, tok)
	return nil
}

func testKeyPEM(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), k
}

func TestFCMPusher(t *testing.T) {
	keyPEM, key := testKeyPEM(t)
	var tokenCalls int
	var sent []fcmMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			tokenCalls++
			body, _ := io.ReadAll(r.Body)
			form, _ := url.ParseQuery(string(body))
			if form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || strings.Count(form.Get("assertion"), ".") != 2 {
				http.Error(w, `{"error":"invalid_grant"}`, 400)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"ya29.test","expires_in":3600}`))
		case r.URL.Path == "/v1/projects/equiprent-test/messages:send":
			if r.Header.Get("Authorization") != "Bearer ya29.test" {
				w.WriteHeader(401)
				return
			}
			var m fcmMessage
			_ = json.NewDecoder(r.Body).Decode(&m)
			sent = append(sent, m)
			switch m.Message.Token {
			case "token-gone-0000":
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`{"error":{"code":404,"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`))
			case "token-busy-0000":
				w.WriteHeader(503)
				_, _ = w.Write([]byte(`{"error":{"code":503,"status":"UNAVAILABLE"}}`))
			default:
				_, _ = w.Write([]byte(`{"name":"projects/equiprent-test/messages/1"}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	sa := ServiceAccount{ClientEmail: "push@equiprent-test.iam.gserviceaccount.com", PrivateKey: keyPEM, TokenURI: srv.URL + "/token"}
	user := uuid.New()
	devs := &memDevices{tokens: map[uuid.UUID][]string{user: {"token-good-0000", "token-gone-0000", "token-busy-0000"}}, failed: map[string]string{}}
	f, err := NewFCMPusher(sa, "equiprent-test", devs, logger.Discard())
	if err != nil {
		t.Fatal(err)
	}
	f.Endpoint = srv.URL
	f.AppURL = "https://app.example.test"

	msg := Message{Type: events.ChatMessage, Title: "Ravi", Body: "Hello", Data: map[string]any{"conversation_id": "c-1", "count": 2}}
	err = f.Push(context.Background(), user, msg)
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("expected the unavailable device to be reported, got %v", err)
	}
	if len(sent) != 3 || !slices.Equal(devs.pushed, []string{"token-good-0000"}) || !slices.Equal(devs.dropped, []string{"token-gone-0000"}) {
		t.Fatalf("sent=%d pushed=%v dropped=%v", len(sent), devs.pushed, devs.dropped)
	}
	if devs.failed["token-busy-0000"] == "" {
		t.Fatal("transient failure not recorded")
	}
	m := sent[0].Message
	if m.Notification["title"] != "Ravi" || m.Data["type"] != events.ChatMessage || m.Data["conversation_id"] != "c-1" || m.Data["count"] != "2" {
		t.Fatalf("message = %+v", m)
	}
	if m.Android["collapse_key"] != "c-1" || m.Webpush == nil {
		t.Fatalf("android/web options = %+v / %+v", m.Android, m.Webpush)
	}
	// The OAuth token is cached between pushes.
	devs.tokens[user] = []string{"token-good-0000"}
	if err := f.Push(context.Background(), user, msg); err != nil {
		t.Fatal(err)
	}
	if tokenCalls != 1 {
		t.Fatalf("token fetched %d times", tokenCalls)
	}
	// The signed assertion verifies with the service account key.
	assertion, _ := f.signJWT(f.now())
	if !verifyRS256(t, assertion, &key.PublicKey) {
		t.Fatal("JWT signature does not verify")
	}
	// No devices: nothing to do.
	if err := f.Push(context.Background(), uuid.New(), msg); err != nil {
		t.Fatal(err)
	}
}

func TestLoadServiceAccount(t *testing.T) {
	keyPEM, _ := testKeyPEM(t)
	raw, _ := json.Marshal(map[string]string{"project_id": "p1", "client_email": "a@b.c", "private_key": keyPEM})
	sa, err := LoadServiceAccount(string(raw))
	if err != nil || sa.ProjectID != "p1" || sa.TokenURI == "" {
		t.Fatalf("sa=%+v err=%v", sa, err)
	}
	if _, err := LoadServiceAccount(`{"project_id":"p1"}`); err == nil {
		t.Fatal("incomplete key accepted")
	}
	if _, err := NewFCMPusher(ServiceAccount{ClientEmail: "a", PrivateKey: "nope"}, "p", nil, logger.Discard()); err == nil {
		t.Fatal("bad PEM accepted")
	}
}

func (m *memRepo) CreateForEvent(ctx context.Context, users []uuid.UUID, msg Message, ev uuid.UUID) ([]uuid.UUID, error) {
	if m.seen == nil {
		m.seen = map[string]bool{}
	}
	var fresh []uuid.UUID
	for _, u := range users {
		k := ev.String() + u.String()
		if !m.seen[k] {
			m.seen[k] = true
			fresh = append(fresh, u)
		}
	}
	return fresh, m.CreateMany(ctx, fresh, msg)
}
func (m *memRepo) BookingContext(context.Context, uuid.UUID) (BookingFacts, error) {
	return BookingFacts{Equipment: "Excavator", Customer: "Acme", Site: "Pune"}, nil
}
func (m *memRepo) BookingOperators(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return m.drivers, nil
}
func (m *memRepo) PlatformUsers(context.Context, []string) ([]uuid.UUID, error) { return m.admins, nil }

func TestReplayedEventNotifiesOnce(t *testing.T) {
	repo := newMem()
	u := uuid.New()
	svc := NewService(repo, LogPusher{Log: logger.Discard()}, logger.Discard())
	ctx := events.WithEventID(context.Background(), uuid.New())
	msg := Message{Type: "booking.requested", Title: "t", Body: "b"}
	for i := 0; i < 2; i++ {
		if err := svc.Notify(ctx, []uuid.UUID{u}, msg); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(repo.sent[u]); n != 1 {
		t.Fatalf("replayed event stored %d notifications, want 1", n)
	}
}
