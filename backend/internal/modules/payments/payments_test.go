package payments

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/modules/bookings"
)

func TestRazorpaySignatures(t *testing.T) {
	live := NewRazorpay("rzp_live_key", "key-secret", "hook-secret")
	sig := Sign("key-secret", "order_1|pay_1")
	if !live.VerifyPayment("order_1", "pay_1", sig) {
		t.Fatal("valid payment signature rejected")
	}
	if live.VerifyPayment("order_1", "pay_2", sig) || live.VerifyPayment("order_1", "pay_1", "00"+sig[2:]) {
		t.Fatal("forged payment signature accepted")
	}
	body := []byte(`{"event":"payment.captured"}`)
	if !live.VerifyWebhook(body, Sign("hook-secret", string(body))) || live.VerifyWebhook(body, Sign("other", string(body))) {
		t.Fatal("webhook verification wrong")
	}
	if (&Razorpay{}).VerifyWebhook(body, Sign("", string(body))) {
		t.Fatal("must refuse webhooks without a configured secret")
	}
}

func TestSandboxOrdersAndPayments(t *testing.T) {
	sb := NewRazorpay("", "", "hook-secret")
	if !sb.Sandbox() {
		t.Fatal("no credentials = sandbox")
	}
	o, err := sb.CreateOrder(context.Background(), 5000, "ER-1", nil)
	if err != nil || o.ID == "" || o.Checkout["amount"] != int64(5000) {
		t.Fatalf("order = %+v %v", o, err)
	}
	pid, sig := sb.SandboxPayment(o.ID)
	if !sb.VerifyPayment(o.ID, pid, sig) {
		t.Fatal("sandbox payment does not verify")
	}
}

func TestLiveCreateOrderCallsAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, _ := r.BasicAuth()
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/orders" || user != "key" || pass != "secret" || body["amount"] != float64(12345) || body["currency"] != "INR" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"id":"order_live_1"}`))
	}))
	defer srv.Close()
	gw := NewRazorpay("key", "secret", "hook")
	gw.BaseURL = srv.URL
	o, err := gw.CreateOrder(context.Background(), 12345, "ER-2", nil)
	if err != nil || o.ID != "order_live_1" || o.Checkout["key"] != "key" {
		t.Fatalf("order = %+v %v", o, err)
	}
	gw.BaseURL = srv.URL + "/broken"
	if _, err := gw.CreateOrder(context.Background(), 1, "x", nil); err == nil {
		t.Fatal("gateway errors must surface")
	}
}

func TestAmountFor(t *testing.T) {
	price, _ := json.Marshal(bookings.Price{AdvanceDuePaise: 500, DepositPaise: 200})
	b := bookings.Booking{Status: bookings.StatusPaymentPending, Price: price, EstimatedTotal: 1000}
	if n, err := amountFor(b, "advance", 0); err != nil || n != 700 {
		t.Fatalf("advance = %d %v", n, err)
	}
	if _, err := amountFor(b, "balance", 0); !errors.Is(err, apperror.ErrInvalidTransition) {
		t.Fatalf("balance before work: %v", err)
	}
	b.Status = bookings.StatusCompleted
	b.FinalizedAt = &time.Time{}
	if _, err := amountFor(b, "advance", 0); !errors.Is(err, apperror.ErrInvalidTransition) {
		t.Fatalf("advance after completion: %v", err)
	}
	final := int64(1100)
	b.FinalTotal = &final
	if n, _ := amountFor(b, "balance", 700); n != 1100+200-700 {
		t.Fatalf("balance = %d", n)
	}
}
