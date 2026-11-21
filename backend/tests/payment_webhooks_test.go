package tests

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
)

var runSuffix = key("r")

// payHook posts a signed Razorpay webhook with the given event id.
func payHook(e *env, event, orderID, paymentID string, amount int64, evID string) resp {
	body := fmt.Sprintf(`{"event":%q,"payload":{"payment":{"entity":{"id":%q,"order_id":%q,"amount":%d,"method":"card","error_code":"BAD_REQUEST_ERROR","error_description":"Card declined"}}}}`,
		event, paymentID+runSuffix, orderID, amount)
	return e.call(http.MethodPost, "/webhooks/payments/razorpay", "", "", body, map[string]string{
		"X-Razorpay-Signature": paymentsSign(e, body), "X-Razorpay-Event-Id": evID,
	})
}

// payableBooking returns an accepted online booking with its advance order started.
func payableBooking(t *testing.T, e *env) (m *market, bookingID, paymentID, orderID string, amount int64) {
	t.Helper()
	m = newMarket(t, e, nil)
	bookingID = m.request(t, "online")
	must(t, m.owner.do(http.MethodPost, "/owner/bookings/"+bookingID+"/accept", nil), 200, "accept")
	pay := must(t, m.customer.do(http.MethodPost, "/bookings/"+bookingID+"/payments", map[string]any{"purpose": "advance"},
		"Idempotency-Key", key("pay")), 201, "checkout")
	paymentID, orderID = pay.str("payment.id"), pay.str("payment.gateway_order_id")
	var amt int64
	fmt.Sscan(e.query1(`SELECT amount_paise::text FROM payments WHERE id = $1`, paymentID), &amt)
	return m, bookingID, paymentID, orderID, amt
}

func whStatus(e *env, evID string) string {
	return e.query1(`SELECT status FROM webhook_events WHERE event_id = $1`, evID)
}

const failCaptureTrigger = `
CREATE OR REPLACE FUNCTION zz_fail_capture() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF NEW.status = 'captured' THEN RAISE EXCEPTION 'simulated database failure'; END IF; RETURN NEW; END; $$;
CREATE TRIGGER zz_fail_capture BEFORE UPDATE ON payments FOR EACH ROW EXECUTE FUNCTION zz_fail_capture();`

const dropFailTrigger = `DROP TRIGGER IF EXISTS zz_fail_capture ON payments; DROP FUNCTION IF EXISTS zz_fail_capture();`

// A webhook whose database update fails is stored as failed (not processed),
// reported as an error, rolled back, and succeeds when redelivered.
func TestWebhookFailureRollsBackAndRetries(t *testing.T) {
	e := newEnv(t)
	m, id, payID, order, amount := payableBooking(t, e)

	e.exec(failCaptureTrigger)
	t.Cleanup(func() { e.exec(dropFailTrigger) })
	ev := key("evt")
	must(t, payHook(e, "payment.captured", order, "pay_retry1", amount, ev), 503, "captured webhook with failing database")
	if got := whStatus(e, ev); got != "failed" {
		t.Fatalf("webhook status after failure = %s", got)
	}
	if e.query1(`SELECT count(*) FROM webhook_events WHERE event_id = $1 AND processed_at IS NOT NULL`, ev) != "0" {
		t.Fatal("a failed webhook was marked processed")
	}
	if st := e.query1(`SELECT status::text FROM payments WHERE id = $1`, payID); st != "created" {
		t.Fatalf("payment after rolled-back capture = %s", st)
	}
	if st := m.status(t, id); st != "payment_processing" {
		t.Fatalf("booking after rolled-back capture = %s", st)
	}

	// Redelivery after the fault is fixed applies the event.
	e.exec(dropFailTrigger)
	if ack := must(t, payHook(e, "payment.captured", order, "pay_retry1", amount, ev), 200, "retry"); ack.str("status") != "processed" {
		t.Fatalf("retry ack = %s", ack.raw)
	}
	if got := whStatus(e, ev); got != "processed" {
		t.Fatalf("webhook status after retry = %s", got)
	}
	if st := m.status(t, id); st != "confirmed" {
		t.Fatalf("booking after retry = %s", st)
	}
}

// The worker retries failed events from the stored payload.
func TestWebhookReconciliationJob(t *testing.T) {
	e := newEnv(t)
	m, id, payID, order, amount := payableBooking(t, e)

	e.exec(failCaptureTrigger)
	t.Cleanup(func() { e.exec(dropFailTrigger) })
	ev := key("evt")
	must(t, payHook(e, "payment.captured", order, "pay_recon1", amount, ev), 503, "failing webhook")
	e.exec(dropFailTrigger)

	e.exec(`UPDATE webhook_events SET received_at = now() - interval '5 minutes' WHERE event_id = $1`, ev)
	e.runJob(t, "payments.reconcile_webhooks")
	if got := whStatus(e, ev); got != "processed" {
		t.Fatalf("webhook status after reconciliation = %s", got)
	}
	if st := e.query1(`SELECT status::text FROM payments WHERE id = $1`, payID); st != "captured" {
		t.Fatalf("payment after reconciliation = %s", st)
	}
	if st := m.status(t, id); st != "confirmed" {
		t.Fatalf("booking after reconciliation = %s", st)
	}
}

// A genuine capture after an earlier payment.failed is applied.
func TestCaptureAfterFailedPayment(t *testing.T) {
	e := newEnv(t)
	m, id, payID, order, amount := payableBooking(t, e)

	must(t, payHook(e, "payment.failed", order, "pay_f1", amount, key("evt")), 200, "failed")
	if st := e.query1(`SELECT status::text FROM payments WHERE id = $1`, payID); st != "failed" {
		t.Fatalf("payment after failure = %s", st)
	}
	if st := m.status(t, id); st != "payment_pending" {
		t.Fatalf("booking after failure = %s", st)
	}
	if ack := must(t, payHook(e, "payment.captured", order, "pay_c1", amount, key("evt")), 200, "captured"); ack.str("status") != "processed" {
		t.Fatalf("capture ack = %s", ack.raw)
	}
	if st := e.query1(`SELECT status::text FROM payments WHERE id = $1`, payID); st != "captured" {
		t.Fatalf("payment after late capture = %s", st)
	}
	if st := m.status(t, id); st != "confirmed" {
		t.Fatalf("booking after late capture = %s", st)
	}
}

// A redelivered webhook is deduplicated and applies once.
func TestWebhookDuplicateDelivery(t *testing.T) {
	e := newEnv(t)
	_, _, payID, order, amount := payableBooking(t, e)
	ev := key("evt")
	if a := must(t, payHook(e, "payment.captured", order, "pay_d1", amount, ev), 200, "first"); a.str("status") != "processed" {
		t.Fatalf("first ack = %s", a.raw)
	}
	if a := must(t, payHook(e, "payment.captured", order, "pay_d1", amount, ev), 200, "second"); a.str("status") != "duplicate" {
		t.Fatalf("second ack = %s", a.raw)
	}
	// Same payment redelivered under a new event id changes nothing and raises no error.
	must(t, payHook(e, "payment.captured", order, "pay_d1", amount, key("evt")), 200, "new event id")
	if n := e.query1(`SELECT count(*) FROM payments WHERE id = $1 AND status = 'captured'`, payID); n != "1" {
		t.Fatalf("captured rows = %s", n)
	}
	if n := e.query1(`SELECT count(*) FROM webhook_events WHERE event_id = $1`, ev); n != "1" {
		t.Fatalf("stored events = %s", n)
	}
}

// Concurrent checkout requests share one advance order.
func TestConcurrentAdvanceOrders(t *testing.T) {
	e := newEnv(t)
	m := newMarket(t, e, nil)
	id := m.request(t, "online")
	must(t, m.owner.do(http.MethodPost, "/owner/bookings/"+id+"/accept", nil), 200, "accept")

	const n = 8
	ids := make([]string, n)
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := m.customer.do(http.MethodPost, "/bookings/"+id+"/payments", map[string]any{"purpose": "advance"}, "Idempotency-Key", key("pay"))
			codes[i] = r.code
			ids[i] = r.str("payment.id")
		}()
	}
	wg.Wait()
	for i := range n {
		if codes[i] != 201 || ids[i] != ids[0] {
			t.Fatalf("request %d: status %d payment %s (first %s)", i, codes[i], ids[i], ids[0])
		}
	}
	if c := e.query1(`SELECT count(*) FROM payments WHERE booking_id = $1 AND purpose = 'advance'`, id); c != "1" {
		t.Fatalf("advance payments = %s", c)
	}
}

// Advance/deposit cannot be ordered again once paid, and a duplicate captured
// amount is queued for refund.
func TestDuplicateAdvanceAndDeposit(t *testing.T) {
	e := newEnv(t)
	m, id, payID, order, amount := payableBooking(t, e)
	must(t, payHook(e, "payment.captured", order, "pay_a1", amount, key("evt")), 200, "capture")

	must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/payments", map[string]any{"purpose": "deposit"},
		"Idempotency-Key", key("pay")), 422, "deposit after the advance")

	// A second captured payment for the same advance (order paid twice) is refunded.
	e.exec(`INSERT INTO payments (booking_id, payer_org_id, purpose, amount_paise, gateway, gateway_order_id, idempotency_key)
	        SELECT booking_id, payer_org_id, purpose, amount_paise, gateway, 'order_dup_' || id, 'dup-' || id FROM payments WHERE id = $1`, payID)
	dupID := e.query1(`SELECT id::text FROM payments WHERE booking_id = $1 AND id <> $2`, id, payID)
	dupOrder := e.query1(`SELECT gateway_order_id FROM payments WHERE id = $1`, dupID)
	must(t, payHook(e, "payment.captured", dupOrder, "pay_a2", amount, key("evt")), 200, "duplicate capture")
	if n := e.query1(`SELECT count(*) FROM refunds WHERE payment_id = $1 AND amount_paise = $2 AND status = 'pending'`, dupID, amount); n != "1" {
		t.Fatalf("duplicate refunds queued = %s", n)
	}
	if st := m.status(t, id); st != "confirmed" {
		t.Fatalf("booking after duplicate = %s", st)
	}
	must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/payments", map[string]any{"purpose": "advance"},
		"Idempotency-Key", key("pay")), 422, "advance again")
}
