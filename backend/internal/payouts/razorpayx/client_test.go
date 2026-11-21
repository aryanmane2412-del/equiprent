package razorpayx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/modules/settlements"
	"github.com/equiprent/backend/internal/modules/verification"
)

// fakeAPI mimics the parts of RazorpayX the client uses.
type fakeAPI struct {
	mu          sync.Mutex
	srv         *httptest.Server
	calls       []string
	lastPayout  map[string]any
	lastHeaders http.Header
	fundStatus  int // non-zero: fail POST /fund_accounts with this status
	valStatuses []string
	valResult   map[string]any
	payoutCode  int
	payoutBody  string
	statusBody  string
	authOK      bool
}

func newFake(t *testing.T) (*fakeAPI, *Client) {
	t.Helper()
	f := &fakeAPI{valStatuses: []string{"created", "completed"}, valResult: map[string]any{"account_status": "active", "registered_name": "SHREE GANESH INFRA"}, authOK: true}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	c := New("rzp_test_key", "secret", "2323230012345678", "IMPS")
	c.BaseURL = f.srv.URL
	c.PollEvery, c.PollFor = 5*time.Millisecond, 400*time.Millisecond
	return f, c
}

func (f *fakeAPI) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	if u, p, ok := r.BasicAuth(); !ok || u != "rzp_test_key" || p != "secret" {
		f.authOK = false
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"code":"BAD_REQUEST_ERROR","description":"Authentication failed"}}`)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var in map[string]any
	_ = json.Unmarshal(body, &in)
	w.Header().Set("Content-Type", "application/json")
	reply := func(code int, v any) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/contacts":
		reply(200, map[string]any{"id": "cont_1", "name": in["name"]})
	case r.Method == http.MethodPost && r.URL.Path == "/fund_accounts":
		if f.fundStatus != 0 {
			reply(f.fundStatus, map[string]any{"error": map[string]any{"code": "BAD_REQUEST_ERROR", "description": "The IFSC code is invalid"}})
			return
		}
		reply(200, map[string]any{"id": "fa_1"})
	case r.Method == http.MethodPost && r.URL.Path == "/fund_accounts/validations":
		reply(200, map[string]any{"id": "fav_1", "status": "created"})
	case r.Method == http.MethodGet && r.URL.Path == "/fund_accounts/validations/fav_1":
		st := f.valStatuses[0]
		if len(f.valStatuses) > 1 {
			f.valStatuses = f.valStatuses[1:]
		}
		out := map[string]any{"id": "fav_1", "status": st}
		if st == "completed" {
			out["results"] = f.valResult
		}
		reply(200, out)
	case r.Method == http.MethodPost && r.URL.Path == "/payouts":
		f.lastPayout, f.lastHeaders = in, r.Header.Clone()
		code := f.payoutCode
		if code == 0 {
			code = 200
		}
		w.WriteHeader(code)
		body := f.payoutBody
		if body == "" {
			body = `{"id":"pout_1","status":"processing"}`
		}
		_, _ = io.WriteString(w, body)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/payouts/"):
		_, _ = io.WriteString(w, f.statusBody)
	default:
		reply(404, map[string]any{"error": map[string]any{"code": "NOT_FOUND", "description": "no route"}})
	}
}

var account = verification.PayoutAccount{HolderName: "Shree Ganesh Infra", AccountNumber: "123456789012", IFSC: "HDFC0001234"}

func TestVerifyAccountSucceedsAfterPolling(t *testing.T) {
	f, c := newFake(t)
	res, err := c.VerifyAccount(context.Background(), account)
	if err != nil || !res.Verified || res.Reference != "fa_1" || res.Provider != "razorpay" || res.Mode != "test" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if !f.authOK {
		t.Error("requests must use HTTP basic auth with the key id and secret")
	}
	want := []string{"POST /contacts", "POST /fund_accounts", "POST /fund_accounts/validations", "GET /fund_accounts/validations/fav_1"}
	if len(f.calls) < len(want) {
		t.Fatalf("calls = %v", f.calls)
	}
	for i, w := range want {
		if f.calls[i] != w {
			t.Fatalf("call %d = %q, want %q", i, f.calls[i], w)
		}
	}
}

func TestVerifyAccountRefusals(t *testing.T) {
	cases := map[string]func(f *fakeAPI){
		"closed account": func(f *fakeAPI) { f.valResult = map[string]any{"account_status": "invalid"} },
		"name of someone else": func(f *fakeAPI) {
			f.valResult = map[string]any{"account_status": "active", "registered_name": "RAMESH KUMAR GUPTA"}
		},
		"validation failed": func(f *fakeAPI) { f.valStatuses = []string{"failed"} },
	}
	for name, setup := range cases {
		f, c := newFake(t)
		setup(f)
		res, err := c.VerifyAccount(context.Background(), account)
		if err != nil || res.Verified || res.Reason == "" {
			t.Errorf("%s: must be a clear refusal, got res=%+v err=%v", name, res, err)
		}
	}
}

func TestVerifyAccountBadDetailsAreTheOwnersToFix(t *testing.T) {
	f, c := newFake(t)
	f.fundStatus = 400
	res, err := c.VerifyAccount(context.Background(), account)
	if err != nil || res.Verified || !strings.Contains(res.Reason, "IFSC") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestVerifyAccountProviderProblemsAreUnavailableNotVerified(t *testing.T) {
	f, c := newFake(t)
	f.valStatuses = []string{"created"} // the bank never answers
	if _, err := c.VerifyAccount(context.Background(), account); !errors.Is(err, apperror.ErrServiceUnavailable) {
		t.Fatalf("a bank that does not answer must not verify: %v", err)
	}
	_, c2 := newFake(t)
	c2.KeySecret = "wrong"
	if res, err := c2.VerifyAccount(context.Background(), account); err == nil || res.Verified {
		t.Fatalf("bad credentials must not verify: res=%+v err=%v", res, err)
	}
}

func TestTransferSendsIdempotencyKeyAndAmount(t *testing.T) {
	f, c := newFake(t)
	id, err := c.Transfer(context.Background(), "key-abc", "fa_1", 123400)
	if err != nil || id != "pout_1" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	first := f.lastHeaders.Get("X-Payout-Idempotency")
	if len(first) == 0 || len(first) > 36 {
		t.Errorf("idempotency header must be short (RazorpayX limit), got %q", first)
	}
	if _, err := c.Transfer(context.Background(), "key-abc", "fa_1", 123400); err != nil || f.lastHeaders.Get("X-Payout-Idempotency") != first {
		t.Errorf("the same key must give the same header so a retry is de-duplicated")
	}
	if _, err := c.Transfer(context.Background(), "key-abc:1", "fa_1", 123400); err != nil || f.lastHeaders.Get("X-Payout-Idempotency") == first {
		t.Errorf("a different key (a retry after a failure) must give a different header")
	}
	p := f.lastPayout
	if p["fund_account_id"] != "fa_1" || p["amount"] != float64(123400) || p["currency"] != "INR" || p["mode"] != "IMPS" ||
		p["account_number"] != "2323230012345678" || p["queue_if_low_balance"] != false {
		t.Errorf("payout body = %v", p)
	}
}

func TestTransferErrorsSeparateRefusedFromUnknown(t *testing.T) {
	ctx := context.Background()
	f, c := newFake(t)

	f.payoutCode, f.payoutBody = 400, `{"error":{"code":"BAD_REQUEST_ERROR","description":"Your account does not have enough balance"}}`
	if _, err := c.Transfer(ctx, "k", "fa_1", 100); !errors.Is(err, settlements.ErrTransferRejected) {
		t.Errorf("400 must be a definite refusal, got %v", err)
	}
	for _, code := range []int{500, 502, 503, 429, 409} {
		f.payoutCode, f.payoutBody = code, `{}`
		if _, err := c.Transfer(ctx, "k", "fa_1", 100); !errors.Is(err, settlements.ErrTransferUncertain) {
			t.Errorf("%d must be treated as an unknown outcome (the money may have left), got %v", code, err)
		}
	}
	f.payoutCode, f.payoutBody = 200, `not json`
	if _, err := c.Transfer(ctx, "k", "fa_1", 100); !errors.Is(err, settlements.ErrTransferUncertain) {
		t.Errorf("unreadable success must be an unknown outcome, got %v", err)
	}
	f.payoutCode, f.payoutBody = 200, `{"id":"pout_9","status":"failed"}`
	if _, err := c.Transfer(ctx, "k", "fa_1", 100); !errors.Is(err, settlements.ErrTransferRejected) {
		t.Errorf("a payout created as failed is a refusal, got %v", err)
	}
	f.srv.Close() // connection refused: nothing known about the outcome
	if _, err := c.Transfer(ctx, "k", "fa_1", 100); !errors.Is(err, settlements.ErrTransferUncertain) {
		t.Errorf("network failure must be an unknown outcome, got %v", err)
	}
	if _, err := c.Transfer(ctx, "", "fa_1", 100); !errors.Is(err, settlements.ErrTransferRejected) {
		t.Errorf("a request without an idempotency key must never be sent, got %v", err)
	}
	if _, err := c.Transfer(ctx, "k", "fa_1", 0); !errors.Is(err, settlements.ErrTransferRejected) {
		t.Errorf("a zero amount must never be sent, got %v", err)
	}
}

func TestStatus(t *testing.T) {
	f, c := newFake(t)
	f.statusBody = `{"id":"pout_1","status":"Processed","amount":123400}`
	st, amt, err := c.Status(context.Background(), "pout_1")
	if err != nil || st != "processed" || amt != 123400 {
		t.Fatalf("st=%q amt=%d err=%v", st, amt, err)
	}
	f.statusBody = `garbage`
	if _, _, err := c.Status(context.Background(), "pout_1"); err == nil {
		t.Fatal("unreadable status must be an error, never a guess")
	}
}

func TestNamesMatch(t *testing.T) {
	cases := []struct {
		typed, bank string
		want        bool
	}{
		{"Shree Ganesh Infra", "SHREE GANESH INFRA PVT LTD", true},
		{"Mr Ramesh Patil", "PATIL RAMESH KUMAR", true},
		{"M/s Patil Earthmovers", "PATIL EARTHMOVERS", true},
		{"Shree Ganesh Infra", "RAMESH KUMAR GUPTA", false},
		{"Shree Ganesh Infra", "", true}, // some banks return no name
		{"Pvt Ltd", "Pvt Ltd", false},    // only noise words in common
	}
	for _, c := range cases {
		if got := NamesMatch(c.typed, c.bank); got != c.want {
			t.Errorf("NamesMatch(%q, %q) = %v, want %v", c.typed, c.bank, got, c.want)
		}
	}
}

func TestClientIsAPayoutProviderAndGateway(t *testing.T) {
	var _ verification.PayoutProvider = (*Client)(nil)
	var _ settlements.PayoutGateway = (*Client)(nil)
	if New("rzp_live_x", "s", "a", "IMPS").ModeName() != "live" || New("rzp_test_x", "s", "a", "IMPS").ModeName() != "test" {
		t.Error("mode must follow the key prefix")
	}
	if New("k", "s", "a", "IMPS").Sandbox() {
		t.Error("the real client is never a sandbox")
	}
}
