package tests

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/equiprent/backend/internal/modules/payments"
)

// liveGateway behaves like a configured (non-sandbox) gateway whose server-side answer is scripted.
type liveGateway struct {
	payments.Gateway
	payment payments.GatewayPayment
}

func (g *liveGateway) Sandbox() bool                     { return false }
func (g *liveGateway) VerifyPayment(_, _, _ string) bool { return true }
func (g *liveGateway) FetchPayment(context.Context, string) (payments.GatewayPayment, error) {
	return g.payment, nil
}

// The browser callback is never proof of payment: the backend asks the gateway, and only a
// captured payment with the right order and amount counts.
func TestVerifyConfirmsWithTheGateway(t *testing.T) {
	e := newEnv(t)
	m, id, payID, order, amount := payableBooking(t, e)
	gw := &liveGateway{Gateway: e.app.Payments.Gateway()}
	e.app.Payments.UseGateway(gw)
	// Unique per run: the suite shares one database and gateway_payment_id is unique.
	gpID := fmt.Sprintf("pay_live_%d", time.Now().UnixNano())
	sig := strings.Repeat("a", 64)
	verify := func() resp {
		return m.customer.do(http.MethodPost, "/payments/"+payID+"/verify", map[string]any{"gateway_payment_id": gpID, "signature": sig})
	}
	status := func() string { return e.query1(`SELECT status::text FROM payments WHERE id = $1`, payID) }

	gw.payment = payments.GatewayPayment{ID: gpID, OrderID: order, AmountPaise: amount, Status: "authorized"}
	must(t, verify(), 200, "authorized only")
	if status() != "created" || m.status(t, id) != "payment_processing" {
		t.Fatalf("an authorized-only payment was treated as captured: %s / %s", status(), m.status(t, id))
	}
	gw.payment = payments.GatewayPayment{ID: gpID, OrderID: "order_other", AmountPaise: amount, Status: "captured"}
	must(t, verify(), 400, "another order")
	gw.payment = payments.GatewayPayment{ID: gpID, OrderID: order, AmountPaise: amount - 1, Status: "captured"}
	must(t, verify(), 400, "wrong amount")
	if status() != "created" {
		t.Fatalf("a mismatching payment was captured: %s", status())
	}
	gw.payment = payments.GatewayPayment{ID: gpID, OrderID: order, AmountPaise: amount, Status: "captured"}
	must(t, verify(), 200, "captured")
	if status() != "captured" || m.status(t, id) != "confirmed" {
		t.Fatalf("a gateway-confirmed payment was not applied: %s / %s", status(), m.status(t, id))
	}
}
