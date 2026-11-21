package tests

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/modules/payments"
)

const testDeposit = 500000

// scriptedGateway wraps the sandbox gateway and scripts refund behaviour.
type scriptedGateway struct {
	payments.Gateway
	mu              sync.Mutex
	refundCalls     int
	failWith        error             // returned by Refund when set (nothing created)
	loseResponse    bool              // the gateway creates the refund but the response is lost
	pending         bool              // accepted, settles later
	statusProcessed bool              // what RefundStatus reports
	created         map[string]string // receipt -> gateway refund id
}

func newScripted(real payments.Gateway) *scriptedGateway {
	return &scriptedGateway{Gateway: real, created: map[string]string{}}
}

func (g *scriptedGateway) Refund(_ context.Context, _ string, _ int64, receipt string) (payments.RefundResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.refundCalls++
	if g.failWith != nil {
		return payments.RefundResult{}, g.failWith
	}
	id, ok := g.created[receipt] // the gateway honours the idempotency reference
	if !ok {
		id = fmt.Sprintf("rfnd_fake_%d", len(g.created)+1)
		g.created[receipt] = id
	}
	if g.loseResponse {
		return payments.RefundResult{}, &payments.RefundError{Err: fmt.Errorf("timeout")}
	}
	return payments.RefundResult{ID: id, Processed: !g.pending}, nil
}

func (g *scriptedGateway) FindRefund(_ context.Context, _ string, receipt string) (payments.RefundResult, bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	id, ok := g.created[receipt]
	return payments.RefundResult{ID: id, Processed: !g.pending}, ok, nil
}

func (g *scriptedGateway) RefundStatus(_ context.Context, id string) (payments.RefundResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return payments.RefundResult{ID: id, Processed: g.statusProcessed}, nil
}

func (g *scriptedGateway) calls() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.refundCalls
}

func depositEnv(t *testing.T) *env {
	return newIsolatedEnv(t, func(c *config.Config) { c.Booking.DefaultDepositPaise = testDeposit })
}

// paidCompletedRental pays the advance (which carries the deposit) through the
// sandbox gateway, finishes the rental, then installs the scripted gateway.
func paidCompletedRental(t *testing.T, e *env) (*rental, *scriptedGateway) {
	t.Helper()
	r := newRental(t, e, "online")
	r.accept(t)
	r.pay(t)
	r.finish(t, "confirmed")
	gw := newScripted(e.app.Payments.Gateway())
	e.app.Payments.UseGateway(gw)
	return r, gw
}

func ledgerNet(e *env, account string) string {
	return e.query1(`SELECT COALESCE(sum(CASE l.direction WHEN 'C' THEN l.amount_paise ELSE -l.amount_paise END), 0)::text
	                 FROM ledger_entries l JOIN ledger_accounts a ON a.id = l.account_id WHERE a.code = $1`, account)
}

func depositRow(e *env, bookingID string) (status, deducted, refunded, refundID string) {
	e.t.Helper()
	status = e.query1(`SELECT status::text FROM security_deposits WHERE booking_id = $1`, bookingID)
	deducted = e.query1(`SELECT deducted_paise::text FROM security_deposits WHERE booking_id = $1`, bookingID)
	refunded = e.query1(`SELECT refunded_paise::text FROM security_deposits WHERE booking_id = $1`, bookingID)
	refundID = e.query1(`SELECT COALESCE(refund_id::text, '') FROM security_deposits WHERE booking_id = $1`, bookingID)
	return
}

// Successful release: the deposit is tracked at payment, refunded once after
// completion, confirmed by the gateway, posted to the ledger and notified.
func TestDepositReleaseAndLedgerReconciliation(t *testing.T) {
	e := depositEnv(t)
	r, gw := paidCompletedRental(t, e)
	advance := e.query1(`SELECT amount_paise::text FROM payments WHERE booking_id = $1 AND purpose = 'advance'`, r.id)

	if st, _, _, _ := depositRow(e, r.id); st != "held" {
		t.Fatalf("deposit after payment = %s", st)
	}
	if got := ledgerNet(e, "customer_deposits"); got != fmt.Sprint(testDeposit) {
		t.Fatalf("customer_deposits after capture = %s", got)
	}

	e.runJob(t, "deposits.release")
	st, ded, ref, rid := depositRow(e, r.id)
	if st != "released" || ded != "0" || ref != fmt.Sprint(testDeposit) || rid == "" {
		t.Fatalf("deposit after release = %s %s %s %s", st, ded, ref, rid)
	}
	// Queued, not yet confirmed: no ledger release and no claim of success.
	if got := ledgerNet(e, "customer_deposits"); got != fmt.Sprint(testDeposit) {
		t.Fatalf("ledger moved before the gateway confirmed: %s", got)
	}

	e.runJob(t, "refunds.process")
	if s := e.query1(`SELECT status::text FROM refunds WHERE id = $1`, rid); s != "processed" {
		t.Fatalf("refund status = %s", s)
	}
	if e.query1(`SELECT gateway_refund_id FROM refunds WHERE id = $1`, rid) == "" {
		t.Fatal("gateway refund id not stored")
	}
	if got := ledgerNet(e, "customer_deposits"); got != "0" {
		t.Fatalf("customer_deposits after refund = %s", got)
	}
	want := fmt.Sprint(atoi(advance) - testDeposit)
	if got := ledgerNet(e, "gateway_clearing"); got != "-"+want && got != fmt.Sprint(-atoi(advance)+testDeposit) {
		t.Fatalf("gateway_clearing net = %s, want -%s", got, want)
	}

	// Duplicate release prevention: re-running both jobs changes nothing.
	for range 3 {
		e.runJob(t, "deposits.release")
		e.runJob(t, "refunds.process")
	}
	if n := e.query1(`SELECT count(*) FROM refunds WHERE booking_id = $1`, r.id); n != "1" {
		t.Fatalf("refund rows = %s", n)
	}
	if gw.calls() != 1 {
		t.Fatalf("gateway refund calls = %d", gw.calls())
	}
	if s := e.query1(`SELECT status::text FROM payments WHERE booking_id = $1 AND purpose = 'advance'`, r.id); s != "partially_refunded" {
		t.Fatalf("payment status = %s", s)
	}

	e.drain(t)
	types, _ := noteTypes(t, r.customer)
	if countOf(types, "deposit.released") != 1 || countOf(types, "refund.processed") != 1 {
		t.Fatalf("customer notifications = %v", types)
	}
}

func atoi(s string) int {
	var n int
	fmt.Sscan(s, &n)
	return n
}

// Only an approved (resolved, documented) damage claim is deducted; an open or
// rejected claim is not.
func TestDepositDamageDeduction(t *testing.T) {
	e := depositEnv(t)
	r, gw := paidCompletedRental(t, e)
	dispute := func(status string, amount int) {
		e.exec(`INSERT INTO disputes (booking_id, raised_by_org_id, raised_by_user_id, against_org_id, category, summary, amount_claimed_paise,
		          resolved_amount_paise, status, resolution, resolved_at)
		        SELECT b.id, b.owner_org_id, $2::uuid, b.customer_org_id, 'damage', 'Bucket tooth broken', $3::bigint,
		          CASE WHEN $4::text IN ('resolved', 'rejected') THEN $3::bigint END, $4::dispute_status,
		          CASE WHEN $4::text IN ('resolved', 'rejected') THEN 'Inspection photos confirm the damage' END,
		          CASE WHEN $4::text IN ('resolved', 'rejected') THEN now() END
		        FROM bookings b WHERE b.id = $1`, r.id, r.owner.id, amount, status)
	}

	dispute("open", 400000)
	e.runJob(t, "deposits.release")
	if st, _, _, _ := depositRow(e, r.id); st != "held" {
		t.Fatalf("deposit released while a damage claim is open: %s", st)
	}
	e.exec(`DELETE FROM disputes WHERE booking_id = $1`, r.id)
	dispute("rejected", 100000) // rejected claims never deduct
	dispute("resolved", 200000)
	e.runJob(t, "deposits.release")
	st, ded, ref, rid := depositRow(e, r.id)
	if st != "partially_forfeited" || ded != "200000" || ref != "300000" || rid == "" {
		t.Fatalf("deposit after deduction = %s %s %s %s", st, ded, ref, rid)
	}
	if got := e.query1(`SELECT amount_paise::text FROM refunds WHERE id = $1`, rid); got != "300000" {
		t.Fatalf("refund amount = %s", got)
	}
	if got := ledgerNet(e, "owner_payable:"+r.owner.org); got != "200000" {
		t.Fatalf("owner payable = %s", got)
	}
	e.runJob(t, "refunds.process")
	if got := ledgerNet(e, "customer_deposits"); got != "0" {
		t.Fatalf("customer_deposits = %s", got)
	}
	if gw.calls() != 1 {
		t.Fatalf("gateway calls = %d", gw.calls())
	}
}

// An unresolved claim cannot hold the deposit past the claim window.
func TestDepositClaimWindow(t *testing.T) {
	e := depositEnv(t)
	r, _ := paidCompletedRental(t, e)
	e.exec(`INSERT INTO disputes (booking_id, raised_by_org_id, raised_by_user_id, against_org_id, category, summary, amount_claimed_paise, status)
	        SELECT b.id, b.owner_org_id, $2::uuid, b.customer_org_id, 'damage', 'Cracked boom', 300000, 'open' FROM bookings b WHERE b.id = $1`, r.id, r.owner.id)
	e.exec(`UPDATE bookings SET status_changed_at = now() - interval '20 days' WHERE id = $1`, r.id)
	e.runJob(t, "deposits.release")
	if st, ded, ref, _ := depositRow(e, r.id); st != "released" || ded != "0" || ref != fmt.Sprint(testDeposit) {
		t.Fatalf("deposit after the window = %s %s %s", st, ded, ref)
	}
}

func queuedDepositRefund(t *testing.T) (*env, *rental, *scriptedGateway, string) {
	e := depositEnv(t)
	r, gw := paidCompletedRental(t, e)
	e.runJob(t, "deposits.release")
	_, _, _, rid := depositRow(e, r.id)
	if rid == "" {
		t.Fatal("no deposit refund queued")
	}
	return e, r, gw, rid
}

func refundState(e *env, rid string) string {
	return e.query1(`SELECT status::text || '/' || submit_state || '/' || attempts::text FROM refunds WHERE id = $1`, rid)
}

// A gateway rejection is retried with backoff, never reported as success, and
// succeeds once the gateway accepts it; exhausted attempts end as failed.
func TestRefundGatewayFailureAndRetry(t *testing.T) {
	e, r, gw, rid := queuedDepositRefund(t)
	gw.failWith = &payments.RefundError{Definite: true, Err: fmt.Errorf("razorpay refund: status 400")}

	e.runJob(t, "refunds.process")
	if got := refundState(e, rid); got != "pending/queued/1" {
		t.Fatalf("after rejection = %s", got)
	}
	if got := ledgerNet(e, "customer_deposits"); got != fmt.Sprint(testDeposit) {
		t.Fatalf("ledger changed on a failed refund: %s", got)
	}
	e.runJob(t, "refunds.process") // backoff: no second call yet
	if gw.calls() != 1 {
		t.Fatalf("retried before backoff: %d calls", gw.calls())
	}

	gw.failWith = nil
	e.exec(`UPDATE refunds SET next_attempt_at = now() WHERE id = $1`, rid)
	e.runJob(t, "refunds.process")
	if got := refundState(e, rid); !strings.HasPrefix(got, "processed/submitted/") {
		t.Fatalf("after retry = %s", got)
	}

	// Exhausted rejections end as failed (finance), never processed.
	r2 := newRental(t, e, "online")
	r2.accept(t)
	e.app.Payments.UseGateway(gw.Gateway) // the sandbox gateway simulates the checkout
	r2.pay(t)
	e.app.Payments.UseGateway(gw)
	_ = r
	r2.finish(t, "confirmed")
	gw.failWith = &payments.RefundError{Definite: true, Err: fmt.Errorf("razorpay refund: status 400")}
	e.runJob(t, "deposits.release")
	rid2 := e.query1(`SELECT refund_id::text FROM security_deposits WHERE booking_id = $1`, r2.id)
	e.exec(`UPDATE refunds SET attempts = 9 WHERE id = $1`, rid2)
	e.runJob(t, "refunds.process")
	if got := refundState(e, rid2); !strings.HasPrefix(got, "failed/") {
		t.Fatalf("after exhausted attempts = %s", got)
	}
}

// An uncertain answer (response lost) is looked up, never sent twice; a worker
// crash mid-submit is recovered the same way.
func TestRefundUncertainResponseAndWorkerRestart(t *testing.T) {
	e, _, gw, rid := queuedDepositRefund(t)
	gw.loseResponse = true
	e.runJob(t, "refunds.process")
	if got := refundState(e, rid); got != "pending/uncertain/1" {
		t.Fatalf("after lost response = %s", got)
	}
	if gw.calls() != 1 {
		t.Fatalf("calls = %d", gw.calls())
	}
	gw.loseResponse = false
	e.exec(`UPDATE refunds SET next_attempt_at = now() WHERE id = $1`, rid)
	e.runJob(t, "refunds.process")
	if got := refundState(e, rid); !strings.HasPrefix(got, "processed/submitted/2") {
		t.Fatalf("after lookup = %s", got)
	}
	if gw.calls() != 1 {
		t.Fatalf("the refund was sent again: %d calls", gw.calls())
	}

	// Worker restart: a refund left in 'submitting' is not touched until stale, then looked up.
	e2, _, gw2, rid2 := queuedDepositRefund(t)
	gw2.created["deposit:"+e2.query1(`SELECT id::text FROM security_deposits WHERE refund_id = $1`, rid2)] = "rfnd_fake_crash"
	e2.exec(`UPDATE refunds SET submit_state = 'submitting', submit_started_at = now(), attempts = 1 WHERE id = $1`, rid2)
	e2.runJob(t, "refunds.process")
	if got := refundState(e2, rid2); got != "pending/submitting/1" {
		t.Fatalf("fresh submitting refund was reclaimed: %s", got)
	}
	e2.exec(`UPDATE refunds SET submit_started_at = now() - interval '20 minutes' WHERE id = $1`, rid2)
	e2.runJob(t, "refunds.process")
	if got := refundState(e2, rid2); !strings.HasPrefix(got, "processed/submitted/2") {
		t.Fatalf("after restart recovery = %s", got)
	}
	if gw2.calls() != 0 {
		t.Fatalf("recovery sent a new refund: %d calls", gw2.calls())
	}
}

// Accepted-but-pending stays pending until the gateway confirms it.
func TestRefundPendingUntilGatewayConfirms(t *testing.T) {
	e, _, gw, rid := queuedDepositRefund(t)
	gw.pending = true
	e.runJob(t, "refunds.process")
	if got := refundState(e, rid); got != "pending/submitted/1" {
		t.Fatalf("accepted refund = %s", got)
	}
	if got := ledgerNet(e, "customer_deposits"); got != fmt.Sprint(testDeposit) {
		t.Fatalf("ledger moved before confirmation: %s", got)
	}
	e.exec(`UPDATE refunds SET last_checked_at = now() - interval '1 hour' WHERE id = $1`, rid)
	e.runJob(t, "refunds.process") // gateway still pending
	if got := refundState(e, rid); !strings.HasPrefix(got, "pending/") {
		t.Fatalf("still-pending refund = %s", got)
	}
	gw.statusProcessed = true
	e.exec(`UPDATE refunds SET last_checked_at = now() - interval '1 hour' WHERE id = $1`, rid)
	e.runJob(t, "refunds.process")
	if got := refundState(e, rid); !strings.HasPrefix(got, "processed/") {
		t.Fatalf("after confirmation = %s", got)
	}
	if got := ledgerNet(e, "customer_deposits"); got != "0" {
		t.Fatalf("customer_deposits = %s", got)
	}
	if gw.calls() != 1 {
		t.Fatalf("calls = %d", gw.calls())
	}
}
