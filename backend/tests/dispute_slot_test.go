package tests

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/modules/disputes"
)

// moveTo walks a booking through the allowed transitions (as the lifecycle actions would).
func moveTo(e *env, id string, steps ...string) {
	e.t.Helper()
	for _, st := range steps {
		e.exec(`UPDATE bookings SET status = $2 WHERE id = $1`, id, st)
	}
}

func activeRental(t *testing.T, e *env) (*market, string) {
	t.Helper()
	m := newMarket(t, e, nil)
	id := m.request(t, "online")
	must(t, m.owner.do(http.MethodPost, "/owner/bookings/"+id+"/accept", nil), 200, "accept")
	m.advance(t, id)
	moveTo(e, id, "dispatched", "on_site", "active")
	return m, id
}

func openDispute(t *testing.T, u *user, id, category string, claimed int) resp {
	t.Helper()
	return must(t, u.do(http.MethodPost, "/bookings/"+id+"/disputes", map[string]any{
		"category": category, "summary": "The machine did not match the listing", "amount_claimed_paise": claimed,
	}), 201, "raise dispute")
}

func resolveDispute(t *testing.T, e *env, id, outcome string, amount *int64) *disputes.Dispute {
	t.Helper()
	admin := e.opsAdmin()
	p := &httpx.Principal{UserID: uuid.MustParse(admin.id), Roles: []httpx.Role{{Code: httpx.RoleAdminOps}}}
	d, err := e.app.Disputes.Resolve(context.Background(), p, uuid.MustParse(id), disputes.ResolveRequest{
		Outcome: outcome, Resolution: "Reviewed the photos and the hour log", ResolvedAmountPaise: amount,
	})
	if err != nil {
		t.Fatalf("resolve dispute: %v", err)
	}
	return &d
}

func holds(e *env, booking string) string {
	return e.query1(`SELECT booking_holds_slot(status, resume_status)::text FROM bookings WHERE id = $1`, booking)
}

// H1: a dispute on a running rental keeps the machine reserved; H2: resolving restores the booking.
func TestDisputeOnActiveRentalKeepsSlotAndRestoresState(t *testing.T) {
	e := newEnv(t)
	m, id := activeRental(t, e)

	d := openDispute(t, m.customer, id, "quality", 0)
	if st := m.status(t, id); st != "disputed" {
		t.Fatalf("status after dispute = %s", st)
	}
	if rs := e.query1(`SELECT resume_status::text FROM bookings WHERE id = $1`, id); rs != "active" {
		t.Fatalf("previous state = %s", rs)
	}
	if holds(e, id) != "true" {
		t.Fatal("a disputed running rental released the machine")
	}

	// Another customer cannot even request the same dates while the machine is out.
	other, siteID := m.newCustomer(t)
	req := other.do(http.MethodPost, "/bookings", m.bookingBody(siteID, m.start, m.end, "online"), "Idempotency-Key", key("book"))
	if req.code != 409 || req.str("code") != "BOOKING_OVERLAP" {
		t.Fatalf("overlapping request over a disputed rental: %d %s", req.code, req.raw)
	}
	// The owner cannot block the dates either.
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO availability_blocks (equipment_id, period, reason, created_by)
	        SELECT equipment_id, period, 'x', $2::uuid FROM bookings WHERE id = $1`, id, m.owner.id); err == nil {
		t.Fatal("dates of a disputed rental could be blocked")
	}

	// Resolving returns the booking to active, still holding the slot, and does not complete it.
	resolveDispute(t, e, d.str("id"), "rejected", nil)
	if st := m.status(t, id); st != "active" {
		t.Fatalf("status after resolution = %s, want active", st)
	}
	if holds(e, id) != "true" {
		t.Fatal("slot released after resolution of an active rental")
	}
	must(t, m.owner.do(http.MethodPost, "/owner/bookings/"+id+"/complete", nil), 409, "complete an active rental")
	if r := other.do(http.MethodPost, "/bookings", m.bookingBody(siteID, m.start, m.end, "online"), "Idempotency-Key", key("book")); r.code != 409 {
		t.Fatalf("a second booking was possible after resolution: %d", r.code)
	}
}

// A dispute on a machine that is already back does not reserve it; resolving restores
// 'returned' and the final-payment requirement still applies. Approved money becomes a refund.
func TestDisputeOnReturnedBookingRefundsAndKeepsPaymentRules(t *testing.T) {
	e := depositEnv(t)
	m, id := activeRental(t, e)
	moveTo(e, id, "return_initiated", "returned")

	d := openDispute(t, m.customer, id, "payment", 300000)
	if st := m.status(t, id); st != "disputed" || e.query1(`SELECT resume_status::text FROM bookings WHERE id = $1`, id) != "returned" {
		t.Fatalf("after dispute: %s", st)
	}
	if holds(e, id) != "false" {
		t.Fatal("a dispute on a returned machine kept the slot")
	}
	// Approving more than claimed is refused; an approved amount becomes a real refund.
	over := int64(400000)
	admin := e.opsAdmin()
	if _, err := e.app.Disputes.Resolve(context.Background(),
		&httpx.Principal{UserID: uuid.MustParse(admin.id), Roles: []httpx.Role{{Code: httpx.RoleAdminOps}}}, uuid.MustParse(d.str("id")),
		disputes.ResolveRequest{Outcome: "resolved", Resolution: "Overcharged beyond the claim", ResolvedAmountPaise: &over}); err == nil {
		t.Fatal("resolved above the claimed amount")
	}
	amount := int64(200000)
	got := resolveDispute(t, e, d.str("id"), "resolved", &amount)
	if got.RefundedPaise != 200000 || got.UnrecoveredPaise != 0 {
		t.Fatalf("settlement = refunded %d, unrecovered %d", got.RefundedPaise, got.UnrecoveredPaise)
	}
	if n := e.query1(`SELECT count(*) FROM refunds WHERE booking_id = $1 AND amount_paise = 200000 AND status = 'pending' AND reason LIKE 'Dispute%'`, id); n != "1" {
		t.Fatalf("dispute refunds = %s", n)
	}
	// The held security deposit is not touched by the dispute refund.
	if ok := e.query1(`SELECT (sum(p.amount_paise) - sum(r.amount_paise) >= (SELECT amount_paise FROM security_deposits WHERE booking_id = $1))::text
	                   FROM payments p JOIN refunds r ON r.payment_id = p.id WHERE p.booking_id = $1`, id); ok != "true" {
		t.Fatal("the dispute refund ate into the held deposit")
	}
	if st := m.status(t, id); st != "returned" {
		t.Fatalf("status after resolution = %s, want returned", st)
	}
	// Unpaid final balance: the owner cannot complete until the bill is settled and paid.
	must(t, m.owner.do(http.MethodPost, "/owner/bookings/"+id+"/complete", nil), 422, "complete with the final bill unsettled")
}

// A booking returns to its previous state only when every dispute on it is closed, and cannot
// complete while one is open.
func TestMultipleDisputesAndCompletionGuard(t *testing.T) {
	e := newEnv(t)
	m, id := activeRental(t, e)
	a := openDispute(t, m.customer, id, "quality", 0)
	b := openDispute(t, m.owner, id, "damage", 100000)
	if st := m.status(t, id); st != "disputed" {
		t.Fatalf("status = %s", st)
	}
	if rs := e.query1(`SELECT resume_status::text FROM bookings WHERE id = $1`, id); rs != "active" {
		t.Fatalf("a second dispute overwrote the previous state: %s", rs)
	}

	resolveDispute(t, e, a.str("id"), "rejected", nil)
	if st := m.status(t, id); st != "disputed" {
		t.Fatalf("one resolved dispute restored the booking while another is open: %s", st)
	}
	amount := int64(50000)
	got := resolveDispute(t, e, b.str("id"), "resolved", &amount)
	if st := m.status(t, id); st != "active" {
		t.Fatalf("status after the last dispute = %s", st)
	}
	if got.UnrecoveredPaise != 50000 { // no deposit was collected in this setup
		t.Fatalf("unrecovered = %d", got.UnrecoveredPaise)
	}

	// Completion is blocked at the database while any dispute is open.
	moveTo(e, id, "return_initiated", "returned")
	openDispute(t, m.owner, id, "other", 0)                             // booking -> disputed
	e.exec(`UPDATE bookings SET status = 'returned' WHERE id = $1`, id) // legacy-style restore must still work...
	var open int
	e.pool.QueryRow(context.Background(), `SELECT count(*) FROM disputes WHERE booking_id = $1 AND status IN ('open','under_review')`, id).Scan(&open)
	if open != 1 {
		t.Fatalf("open disputes = %d", open)
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE bookings SET status = 'completed' WHERE id = $1`, id); err == nil {
		t.Fatal("a booking completed with an unresolved dispute")
	}
}

// Disputes after completion are accepted for seven days only.
func TestDisputeWindowAfterCompletion(t *testing.T) {
	e := newEnv(t)
	r := completedRental(t, e)
	must(t, r.customer.do(http.MethodPost, "/bookings/"+r.id+"/disputes", map[string]any{
		"category": "quality", "summary": "Hydraulic leak found after return",
	}), 201, "within the window")
	e.exec(`UPDATE disputes SET status = 'rejected', resolution = 'Checked', resolved_at = now() WHERE booking_id = $1`, r.id)
	e.exec(`UPDATE bookings SET status = 'completed' WHERE id = $1`, r.id)
	e.exec(`UPDATE bookings SET completed_at = now() - interval '8 days' WHERE id = $1`, r.id)
	must(t, r.customer.do(http.MethodPost, "/bookings/"+r.id+"/disputes", map[string]any{
		"category": "other", "summary": "Another problem found much later",
	}), 422, "after seven days")
}

// Concurrent accepts for the same dates: exactly one wins; none while the rental is disputed.
func TestConcurrentBookingRequestsForSameDates(t *testing.T) {
	e := newEnv(t)
	m := newMarket(t, e, nil)
	const n = 5
	ids := make([]string, n)
	for i := range n {
		c, site := m.newCustomer(t)
		ids[i] = must(t, c.do(http.MethodPost, "/bookings", m.bookingBody(site, m.start, m.end, "online"), "Idempotency-Key", key("book")), 201, "request").str("id")
		_ = i
	}
	accept := func(ids []string) int {
		var wg sync.WaitGroup
		var mu sync.Mutex
		wins := 0
		for _, id := range ids {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if r := m.owner.do(http.MethodPost, "/owner/bookings/"+id+"/accept", nil); r.code == 200 {
					mu.Lock()
					wins++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		return wins
	}
	if w := accept(ids); w != 1 {
		t.Fatalf("accepted %d overlapping bookings", w)
	}
	if c := e.query1(`SELECT count(*) FROM bookings WHERE equipment_id = $1 AND booking_holds_slot(status, resume_status)`, m.eq); c != "1" {
		t.Fatalf("accepted rows = %s", c)
	}

	// The same race against a disputed running rental: every concurrent request is refused.
	m2, id := activeRental(t, e)
	openDispute(t, m2.customer, id, "quality", 0)
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := 0
	for range 3 {
		c, site := m2.newCustomer(t)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r := c.do(http.MethodPost, "/bookings", m2.bookingBody(site, m2.start, m2.end, "online"), "Idempotency-Key", key("book")); r.code != 409 {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if accepted != 0 {
		t.Fatalf("%d concurrent requests got through over a disputed rental", accepted)
	}
}
