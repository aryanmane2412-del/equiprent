package tests

// The complete booking engine through the real router and database: the
// customer journey from draft to review, the owner's side, the 16-status
// state machine with its history, payment policy (deadlines, checkout in
// progress, late payments), cancellation policies and refunds, extensions,
// idempotency, and concurrency (no equipment can hold two overlapping
// confirmed bookings, however the requests race).

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/equiprent/backend/internal/modules/payments"
)

func paymentsSign(e *env, body string) string {
	return payments.Sign(e.cfg.Payments.WebhookSecret, body)
}

// market is a verified owner with one published machine and a customer
// with a saved site.
type market struct {
	e                         *env
	owner, customer, kyc, ops *user
	eq, siteID                string
	start, end                time.Time
}

func newMarket(t *testing.T, e *env, listing map[string]any) *market {
	t.Helper()
	kyc, ops := e.kycAdmin(), e.opsAdmin()
	owner := e.verifiedOwner(kyc, "Engine Rentals "+randomLetters(4))
	if listing == nil {
		listing = map[string]any{}
	}
	listing["title"] = "JCB 3DX backhoe loader " + randomLetters(5)
	eq := e.publish(owner, ops, listingBody(listing))
	m := &market{e: e, owner: owner, kyc: kyc, ops: ops, eq: eq}
	m.customer, m.siteID = m.newCustomer(t)
	m.start = time.Now().Add(96 * time.Hour).UTC().Truncate(time.Hour)
	m.end = m.start.Add(72 * time.Hour)
	return m
}

func (m *market) newCustomer(t *testing.T) (*user, string) {
	t.Helper()
	c := m.e.registerAndVerify("customer", "")
	addr := must(t, c.do(http.MethodPost, "/orgs/"+c.org+"/addresses", map[string]any{
		"label": "Wakad site", "line1": "Survey 112, Bhumkar Chowk", "city": "Pune", "state_code": "27", "pincode": "411057",
		"is_site": true, "location": site,
	}), 201, "site")
	return c, addr.str("id")
}

func (m *market) bookingBody(siteID string, start, end time.Time, mode string) map[string]any {
	return map[string]any{
		"equipment_id": m.eq, "site_address_id": siteID, "period": map[string]any{"start": start, "end": end},
		"pricing_unit": "day", "with_operator": true, "work_description": "Trenching for the boundary wall", "payment_mode": mode,
	}
}

func key(prefix string) string { return fmt.Sprintf("%s-%d", prefix, rand.Int64()) }

// request books the market's dates for the customer (waits for the owner).
func (m *market) request(t *testing.T, mode string) string {
	t.Helper()
	return must(t, m.customer.do(http.MethodPost, "/bookings", m.bookingBody(m.siteID, m.start, m.end, mode),
		"Idempotency-Key", key("book")), 201, "request").str("id")
}

func (m *market) get(t *testing.T, u *user, id string) resp {
	t.Helper()
	return must(t, u.do(http.MethodGet, "/bookings/"+id, nil), 200, "booking detail")
}

func (m *market) status(t *testing.T, id string) string {
	t.Helper()
	return m.get(t, m.customer, id).str("status")
}

// advance pays the advance through the sandbox gateway.
func (m *market) advance(t *testing.T, id string) {
	t.Helper()
	pay := must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/payments", map[string]any{"purpose": "advance"},
		"Idempotency-Key", key("pay")), 201, "create payment")
	must(t, m.customer.do(http.MethodPost, "/payments/"+pay.str("payment.id")+"/verify", map[string]any{
		"gateway_payment_id": pay.str("sandbox.gateway_payment_id"), "signature": pay.str("sandbox.signature"),
	}), 200, "verify payment")
}

// setStatus walks a booking along the lifecycle graph in SQL (the trigger
// still checks every edge), to reach states that need time to pass.
func (m *market) setStatus(id string, steps ...string) {
	for _, st := range steps {
		m.e.exec(`UPDATE bookings SET status = $2 WHERE id = $1`, id, st)
	}
}

func actions(r resp) []string {
	var out []string
	for _, a := range r.list("actions") {
		out = append(out, fmt.Sprint(a))
	}
	return out
}

func timelineStatuses(r resp) []string {
	var out []string
	for _, ev := range r.list("timeline") {
		out = append(out, fmt.Sprint(ev.(map[string]any)["to"]))
	}
	return out
}

func listIDs(r resp) []string {
	var out []string
	for _, b := range r.list("data") {
		out = append(out, fmt.Sprint(b.(map[string]any)["id"]))
	}
	return out
}

// ---- the whole journey ------------------------------------------------------------------------

func TestBookingEngineCustomerAndOwnerJourney(t *testing.T) {
	e := newEnv(t)
	m := newMarket(t, e, nil)
	cust, owner := m.customer, m.owner

	// Policy and estimate.
	pol := must(t, e.call(http.MethodGet, "/booking-policy", "", "", nil, nil), 200, "policy")
	if pol.num("advance_bps") != 5000 || pol.num("payment_window_minutes") <= 0 {
		t.Fatalf("policy = %s", pol.raw)
	}
	est := must(t, e.call(http.MethodPost, "/equipment/"+m.eq+"/estimate", "", "", map[string]any{
		"site": site, "period": map[string]any{"start": m.start, "end": m.end}, "pricing_unit": "day", "with_operator": true,
	}, nil), 200, "estimate")
	if est.num("estimated_total_paise") <= 0 || est.body["availability"].(map[string]any)["available"] != true {
		t.Fatalf("estimate = %s", est.raw)
	}

	// Draft: saved, private to the customer, editable, then submitted.
	body := m.bookingBody(m.siteID, m.start, m.end.Add(-24*time.Hour), "online")
	body["draft"] = true
	draft := must(t, cust.do(http.MethodPost, "/bookings", body, "Idempotency-Key", key("draft")), 201, "draft")
	id := draft.str("id")
	if draft.str("status") != "draft" || !slices.Contains(actions(draft), "submit") || slices.Contains(actions(draft), "pay") {
		t.Fatalf("draft = %s", draft.raw)
	}
	must(t, owner.do(http.MethodGet, "/bookings/"+id, nil), 404, "owner reads a draft")
	if slices.Contains(listIDs(must(t, owner.do(http.MethodGet, "/owner/bookings", nil), 200, "inbox")), id) {
		t.Fatal("a draft reached the owner's inbox")
	}
	edited := must(t, cust.do(http.MethodPatch, "/bookings/"+id, map[string]any{
		"site_address_id": m.siteID, "period": map[string]any{"start": m.start, "end": m.end}, "pricing_unit": "day", "with_operator": true,
		"payment_mode": "online", "work_description": "Trenching and backfilling", "version": draft.num("version"),
	}), 200, "edit draft")
	if edited.num("estimated_total_paise") <= draft.num("estimated_total_paise") {
		t.Fatalf("a longer draft should cost more: %v -> %v", draft.num("estimated_total_paise"), edited.num("estimated_total_paise"))
	}
	must(t, cust.do(http.MethodPost, "/bookings/"+id+"/submit", map[string]any{"version": draft.num("version")}, "Idempotency-Key", key("stale")),
		412, "submit a stale version")
	submitKey := key("submit")
	sub := must(t, cust.do(http.MethodPost, "/bookings/"+id+"/submit", nil, "Idempotency-Key", submitKey), 200, "submit")
	replay := must(t, cust.do(http.MethodPost, "/bookings/"+id+"/submit", nil, "Idempotency-Key", submitKey), 200, "submit replay")
	if sub.str("status") != "requested" || replay.header.Get("Idempotency-Replayed") != "true" || replay.str("id") != id {
		t.Fatalf("submit = %s / replay %s", sub.raw, replay.raw)
	}

	// Owner: inbox, customer details (no phone before acceptance), availability.
	if !slices.Contains(listIDs(must(t, owner.do(http.MethodGet, "/owner/bookings?group=action", nil), 200, "owner to-do")), id) {
		t.Fatal("request missing from the owner's to-do list")
	}
	od := m.get(t, owner, id)
	if od.str("customer.kyc_status") == "" || od.str("customer.phone") != "<nil>" || od.body["availability"].(map[string]any)["available"] != true ||
		!slices.Equal(actions(od), []string{"accept", "reject"}) {
		t.Fatalf("owner view of a request = %s", od.raw)
	}
	must(t, owner.do(http.MethodPost, "/owner/bookings/"+id+"/accept", map[string]any{"version": od.num("version") - 1}), 412, "accept a stale version")
	acc := must(t, owner.do(http.MethodPost, "/owner/bookings/"+id+"/accept", map[string]any{"version": od.num("version")}), 200, "accept")
	if acc.str("status") != "payment_pending" {
		t.Fatalf("accept = %s", acc.raw)
	}
	cd := m.get(t, cust, id)
	if cd.str("payment_due_at") == "<nil>" || !slices.Contains(actions(cd), "pay") || cd.num("money.advance_due_paise") <= 0 {
		t.Fatalf("awaiting payment = %s", cd.raw)
	}
	if m.get(t, owner, id).str("customer.phone") != cust.phone {
		t.Fatal("the owner should see the customer's phone once accepted")
	}

	// Payment: checkout in progress, then captured.
	pay := must(t, cust.do(http.MethodPost, "/bookings/"+id+"/payments", map[string]any{"purpose": "advance"}, "Idempotency-Key", key("pay")), 201, "checkout")
	if st := m.status(t, id); st != "payment_processing" {
		t.Fatalf("during checkout = %s", st)
	}
	must(t, cust.do(http.MethodPost, "/payments/"+pay.str("payment.id")+"/verify", map[string]any{
		"gateway_payment_id": pay.str("sandbox.gateway_payment_id"), "signature": pay.str("sandbox.signature"),
	}), 200, "verify")
	paid := m.get(t, cust, id)
	if paid.str("status") != "confirmed" || paid.num("money.paid_paise") != pay.num("payment.amount_paise") {
		t.Fatalf("after payment = %s", paid.raw)
	}

	// Operator, dispatch, tracking, arrival, handover.
	opPhone := randomPhone()
	must(t, owner.do(http.MethodPost, "/orgs/"+owner.org+"/members", map[string]any{"phone": opPhone, "role": "operator", "full_name": "Ramesh"}), 201, "add operator")
	operator := e.login(opPhone)
	e.verifyOperator(operator, m.kyc)
	if !slices.Contains(actions(m.get(t, owner, id)), "assign_operator") {
		t.Fatal("owner should be offered to assign an operator")
	}
	must(t, owner.do(http.MethodPost, "/owner/bookings/"+id+"/assign-operator", map[string]any{"operator_user_id": operator.id}), 200, "assign")
	must(t, operator.do(http.MethodPost, "/operator/jobs/"+id+"/accept", nil), 200, "operator accepts")
	operator.checklist(t, id, "handover", 2150)
	must(t, operator.do(http.MethodPost, "/operator/jobs/"+id+"/dispatch", nil), 200, "dispatch")
	dispatched := m.get(t, cust, id)
	if !slices.Contains(actions(dispatched), "track") || !slices.Contains(actions(dispatched), "request_cancellation") {
		t.Fatalf("dispatched actions = %v", actions(dispatched))
	}
	must(t, cust.do(http.MethodGet, "/bookings/"+id+"/track", nil), 200, "track")
	must(t, operator.do(http.MethodPost, "/operator/jobs/"+id+"/arrived", nil), 200, "arrived")
	// Arrival ends the mobilisation trip, or the machine could never be dispatched again.
	var open int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM trips WHERE booking_id = $1 AND ended_at IS NULL`, id).Scan(&open); err != nil || open != 0 {
		t.Fatalf("open trips after arrival = %d (%v)", open, err)
	}
	cust.ackInspection(t, id, "handover")
	must(t, cust.do(http.MethodPost, "/bookings/"+id+"/handover/confirm", nil), 200, "handover")

	// Extension asked by the customer, approved by the owner.
	before := m.get(t, cust, id)
	newEnd := m.end.Add(24 * time.Hour)
	q := must(t, cust.do(http.MethodPost, "/bookings/"+id+"/extensions/quote", map[string]any{"new_end": newEnd}), 200, "extension quote")
	if q.num("amount_paise") <= 0 || q.body["availability"].(map[string]any)["available"] != true {
		t.Fatalf("quote = %s", q.raw)
	}
	x := must(t, cust.do(http.MethodPost, "/bookings/"+id+"/extensions", map[string]any{"new_end": newEnd, "note": "Rain delayed the work"},
		"Idempotency-Key", key("ext")), 201, "request extension")
	extID := x.str("extension.id")
	must(t, cust.do(http.MethodPost, "/bookings/"+id+"/extensions/"+extID+"/approve", nil), 403, "self-approval")
	ov := m.get(t, owner, id)
	if ov.str("status") != "extension_requested" || ov.str("pending_extension.id") != extID || !slices.Contains(actions(ov), "approve_extension") {
		t.Fatalf("owner view of extension = %s", ov.raw)
	}
	must(t, owner.do(http.MethodPost, "/bookings/"+id+"/extensions/"+extID+"/approve", nil), 200, "approve extension")
	after := m.get(t, cust, id)
	gotEnd, _ := time.Parse(time.RFC3339, after.str("period.end"))
	if after.str("status") != "active" || !gotEnd.Equal(newEnd) ||
		after.num("estimated_total_paise") != before.num("estimated_total_paise")+q.num("amount_paise") {
		t.Fatalf("after extension = %s", after.raw)
	}
	exts := must(t, cust.do(http.MethodGet, "/bookings/"+id+"/extensions", nil), 200, "extensions")
	if len(exts.list("data")) != 1 || exts.str("data.0.status") != "approved" {
		t.Fatalf("extensions = %s", exts.raw)
	}

	// Return, completion, invoice download, review.
	must(t, cust.do(http.MethodPost, "/bookings/"+id+"/return-request", nil), 200, "return")
	operator.ackInspection(t, id, "return")
	must(t, operator.do(http.MethodPost, "/operator/jobs/"+id+"/returned", nil), 422, "returned without the return checklist")
	operator.checklist(t, id, "return", 2210)
	operator.ackInspection(t, id, "return")
	must(t, operator.do(http.MethodPost, "/operator/jobs/"+id+"/returned", nil), 200, "returned")
	owner.e.exec(`UPDATE bookings SET final_total_paise = GREATEST(COALESCE((SELECT sum(p.amount_paise) FROM payments p WHERE p.booking_id = bookings.id AND p.status = 'captured'), 0) - deposit_paise, 0), final_charges = '{}'::jsonb, finalized_at = now() WHERE id = $1 AND finalized_at IS NULL`, id)
	must(t, owner.do(http.MethodPost, "/owner/bookings/"+id+"/complete", nil), 200, "complete")
	inv := must(t, owner.do(http.MethodPost, "/owner/bookings/"+id+"/invoices", nil), 201, "issue invoice")
	list := must(t, cust.do(http.MethodGet, "/bookings/"+id+"/invoices", nil), 200, "customer invoices")
	if len(list.list("data")) != 1 {
		t.Fatalf("invoices = %s", list.raw)
	}
	pdf := must(t, cust.do(http.MethodGet, "/invoices/"+inv.str("id")+"/pdf", nil), 200, "download invoice")
	if !strings.HasPrefix(pdf.raw, "%PDF-") || pdf.header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("invoice pdf: %q %s", pdf.raw[:min(len(pdf.raw), 20)], pdf.header.Get("Content-Type"))
	}
	link := must(t, cust.do(http.MethodGet, "/invoices/"+inv.str("id")+"/link", nil), 200, "invoice link").str("url")
	signed := link[strings.Index(link, "/api/v1")+len("/api/v1"):]
	if r := must(t, e.call(http.MethodGet, signed, "", "", nil, nil), 200, "signed download"); !strings.HasPrefix(r.raw, "%PDF-") {
		t.Fatal("signed link did not serve the PDF")
	}
	must(t, e.call(http.MethodGet, strings.Replace(signed, "sig=", "sig=x", 1), "", "", nil, nil), 404, "tampered link")
	outsider, _ := m.newCustomer(t)
	must(t, outsider.do(http.MethodGet, "/invoices/"+inv.str("id")+"/pdf", nil), 404, "outsider downloads invoice")
	done := m.get(t, cust, id)
	if !slices.Contains(actions(done), "review") || !slices.Contains(actions(done), "invoices") {
		t.Fatalf("completed actions = %v", actions(done))
	}
	must(t, cust.do(http.MethodPost, "/bookings/"+id+"/reviews", map[string]any{"subject": "equipment", "rating": 5, "comment": "Strong machine"}), 201, "review")

	// Status history: every step, in order, with who did it.
	want := []string{"draft", "requested", "accepted", "payment_pending", "payment_processing", "confirmed", "operator_assigned",
		"dispatched", "on_site", "active", "extension_requested", "active", "return_initiated", "returned", "completed"}
	if got := timelineStatuses(done); !slices.Equal(got, want) {
		t.Fatalf("history = %v\nwant      %v", got, want)
	}
	for _, ev := range done.list("timeline")[1:] {
		if ev.(map[string]any)["actor_role"] == nil {
			t.Fatalf("history entry without actor: %v", ev)
		}
	}

	// Rental history on both sides.
	if !slices.Contains(listIDs(must(t, cust.do(http.MethodGet, "/bookings?group=past", nil), 200, "customer history")), id) ||
		!slices.Contains(listIDs(must(t, owner.do(http.MethodGet, "/owner/bookings?group=past", nil), 200, "owner history")), id) {
		t.Fatal("completed booking missing from rental history")
	}

	// Notifications reached the right people.
	e.drain(t)
	ct, _ := noteTypes(t, cust)
	ot, _ := noteTypes(t, owner)
	for _, want := range []string{"booking.accepted", "booking.confirmed", "payment.captured", "booking.operator_assigned",
		"booking.dispatched", "booking.extension_approved", "booking.completed"} {
		if countOf(ct, want) == 0 {
			t.Errorf("customer missed %s (got %v)", want, ct)
		}
	}
	for _, want := range []string{"booking.requested", "booking.extension_requested", "payment.captured", "booking.return_initiated"} {
		if countOf(ot, want) == 0 {
			t.Errorf("owner missed %s (got %v)", want, ot)
		}
	}
	if countOf(ct, "booking.extension_requested") != 0 {
		t.Error("the customer was notified of their own extension request")
	}
}

// ---- cancellation policies and refunds ------------------------------------------------------------

func TestBookingCancellationPoliciesAndRefunds(t *testing.T) {
	e := newEnv(t)
	m := newMarket(t, e, nil)

	// A policy charging 10% for less than 200 hours' notice (the rental
	// starts in 96 hours).
	var policyID string
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO cancellation_policies (code, name, rules)
		VALUES ($1, 'Strict test', '[{"hours_before":200,"fee_bps":0},{"hours_before":0,"fee_bps":1000}]') RETURNING id`,
		"strict_"+strings.ToLower(randomLetters(8))).Scan(&policyID); err != nil {
		t.Fatal(err)
	}

	t.Run("customer cancels a paid booking: fee kept, rest refunded", func(t *testing.T) {
		id := m.request(t, "online")
		e.exec(`UPDATE bookings SET cancellation_policy_id = $2 WHERE id = $1`, id, policyID)
		must(t, m.owner.do(http.MethodPost, "/owner/bookings/"+id+"/accept", nil), 200, "accept")
		m.advance(t, id)
		b := m.get(t, m.customer, id)
		paid, total := b.num("money.paid_paise"), b.num("estimated_total_paise")
		fee := float64(int64(total*0.1 + 0.5))
		if b.num("cancellation_policy.fee_now_paise") != fee || !strings.Contains(b.str("cancellation_policy.summary"), "10% fee") {
			t.Fatalf("policy view = %s", b.raw)
		}
		pv := must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/cancel?dry_run=true", nil), 200, "preview")
		if pv.num("fee_paise") != fee || pv.num("refund_paise") != paid-fee || pv.body["requires_approval"] != false {
			t.Fatalf("preview = %s (paid %v)", pv.raw, paid)
		}
		res := must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/cancel", map[string]any{"reason": "Project postponed"}), 200, "cancel")
		if res.str("status") != "refund_pending" || res.num("refund_paise") != paid-fee {
			t.Fatalf("cancel = %s", res.raw)
		}
		must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/cancel", map[string]any{"reason": "again"}), 409, "cancel twice")
		e.runJob(t, "refunds.process")
		done := m.get(t, m.customer, id)
		if done.str("status") != "refunded" || done.num("money.refunded_paise") != paid-fee || done.str("money.refunds.0.status") != "processed" {
			t.Fatalf("after refund job = %s", done.raw)
		}
		want := []string{"requested", "accepted", "payment_pending", "payment_processing", "confirmed", "refund_pending", "refunded"}
		if got := timelineStatuses(done); !slices.Equal(got, want) {
			t.Fatalf("history = %v", got)
		}
		e.drain(t)
		types, _ := noteTypes(t, m.customer)
		if countOf(types, "refund.processed") != 1 || countOf(types, "booking.refunded") != 1 {
			t.Fatalf("customer notifications = %v", types)
		}
		owners, _ := noteTypes(t, m.owner)
		if countOf(owners, "booking.cancelled") == 0 {
			t.Fatalf("owner notifications = %v", owners)
		}
	})

	t.Run("owner cancels: everything refunded", func(t *testing.T) {
		id := m.request(t, "online")
		e.exec(`UPDATE bookings SET cancellation_policy_id = $2 WHERE id = $1`, id, policyID)
		must(t, m.owner.do(http.MethodPost, "/owner/bookings/"+id+"/accept", nil), 200, "accept")
		m.advance(t, id)
		paid := m.get(t, m.customer, id).num("money.paid_paise")
		pv := must(t, m.owner.do(http.MethodPost, "/bookings/"+id+"/cancel?dry_run=true", nil), 200, "owner preview")
		if pv.num("fee_paise") != 0 || pv.num("refund_paise") != paid {
			t.Fatalf("owner preview = %s", pv.raw)
		}
		must(t, m.owner.do(http.MethodPost, "/bookings/"+id+"/cancel", map[string]any{"reason": "Machine broke down"}), 200, "owner cancels")
		e.runJob(t, "refunds.process")
		if b := m.get(t, m.customer, id); b.str("status") != "refunded" || b.num("money.refunded_paise") != paid {
			t.Fatalf("after owner cancel = %s", b.raw)
		}
	})

	t.Run("unpaid offline booking cancels straight to cancelled", func(t *testing.T) {
		id := m.request(t, "offline")
		must(t, m.owner.do(http.MethodPost, "/owner/bookings/"+id+"/accept", nil), 200, "accept")
		res := must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/cancel", map[string]any{"reason": "Changed plans"}), 200, "cancel")
		if res.str("status") != "cancelled" || res.num("refund_paise") != 0 {
			t.Fatalf("cancel = %s", res.raw)
		}
	})

	t.Run("after dispatch the owner decides", func(t *testing.T) {
		for _, decision := range []string{"decline", "withdraw", "approve"} {
			id := m.request(t, "offline")
			must(t, m.owner.do(http.MethodPost, "/owner/bookings/"+id+"/accept", nil), 200, "accept")
			m.owner.checklist(t, id, "handover", 2150)
			must(t, m.owner.do(http.MethodPost, "/owner/bookings/"+id+"/dispatch", nil), 200, "dispatch")
			req := must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/cancel", map[string]any{"reason": "Site access blocked"}), 200, "ask to cancel")
			if req.str("status") != "cancellation_requested" || req.body["requires_approval"] != true {
				t.Fatalf("request = %s", req.raw)
			}
			must(t, m.customer.do(http.MethodPost, "/owner/bookings/"+id+"/cancellation/approve", nil), 403, "customer approves own request")
			switch decision {
			case "decline":
				must(t, m.owner.do(http.MethodPost, "/owner/bookings/"+id+"/cancellation/decline", nil), 400, "decline without reason")
				must(t, m.owner.do(http.MethodPost, "/owner/bookings/"+id+"/cancellation/decline", map[string]any{"reason": "Machine is already at the gate"}), 200, "decline")
				if st := m.status(t, id); st != "dispatched" {
					t.Fatalf("after decline = %s", st)
				}
			case "withdraw":
				must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/cancellation/withdraw", nil), 200, "withdraw")
				if st := m.status(t, id); st != "dispatched" {
					t.Fatalf("after withdraw = %s", st)
				}
			case "approve":
				must(t, m.owner.do(http.MethodPost, "/owner/bookings/"+id+"/cancellation/approve", nil), 200, "approve")
				if st := m.status(t, id); st != "cancelled" {
					t.Fatalf("after approval = %s", st)
				}
			}
			// Cancel the leftovers so the next round can use the same dates.
			if decision != "approve" {
				must(t, m.owner.do(http.MethodPost, "/bookings/"+id+"/cancel", map[string]any{"reason": "test cleanup"}), 200, "owner cancels")
			}
		}
		e.drain(t)
		ot, _ := noteTypes(t, m.owner)
		ct, _ := noteTypes(t, m.customer)
		if countOf(ot, "booking.cancellation_requested") < 3 || countOf(ct, "booking.cancellation_declined") < 1 {
			t.Fatalf("notifications owner=%v customer=%v", ot, ct)
		}
	})
}

// ---- payment policy ------------------------------------------------------------------------------

func TestBookingPaymentDeadlinesAndLatePayments(t *testing.T) {
	e := newEnv(t)
	m := newMarket(t, e, nil)
	id := m.request(t, "online")
	must(t, m.owner.do(http.MethodPost, "/owner/bookings/"+id+"/accept", nil), 200, "accept")

	// A checkout abandoned without a gateway answer goes back to awaiting payment.
	pay := must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/payments", map[string]any{"purpose": "advance"},
		"Idempotency-Key", key("pay")), 201, "checkout")
	e.exec(`UPDATE bookings SET status_changed_at = now() - interval '2 hours' WHERE id = $1`, id)
	e.runJob(t, "bookings.release_processing")
	if st := m.status(t, id); st != "payment_pending" {
		t.Fatalf("after stale checkout = %s", st)
	}

	// A failed payment (webhook) keeps the booking awaiting payment and tells the customer.
	pay2 := must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/payments", map[string]any{"purpose": "advance"},
		"Idempotency-Key", key("pay")), 201, "second checkout")
	hook := fmt.Sprintf(`{"event":"payment.failed","payload":{"payment":{"entity":{"id":"pay_f%d","order_id":%q,"error_code":"BAD_REQUEST_ERROR","error_description":"Card declined"}}}}`,
		rand.Int64(), pay2.str("payment.gateway_order_id"))
	must(t, e.call(http.MethodPost, "/webhooks/payments/razorpay", "", "", hook, map[string]string{
		"X-Razorpay-Signature": paymentsSign(e, hook), "X-Razorpay-Event-Id": key("evt"),
	}), 200, "failed webhook")
	if st := m.status(t, id); st != "payment_pending" {
		t.Fatalf("after failed payment = %s", st)
	}

	// The deadline passes: the booking expires and the dates are free again.
	e.exec(`UPDATE bookings SET payment_due_at = now() - interval '1 minute' WHERE id = $1`, id)
	e.runJob(t, "bookings.expire_unpaid")
	if st := m.status(t, id); st != "expired" {
		t.Fatalf("after the deadline = %s", st)
	}
	must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/payments", map[string]any{"purpose": "advance"},
		"Idempotency-Key", key("pay")), 409, "pay an expired booking")
	other, siteID := m.newCustomer(t)
	rid := must(t, other.do(http.MethodPost, "/bookings", m.bookingBody(siteID, m.start, m.end, "offline"), "Idempotency-Key", key("book")), 201, "rival").str("id")
	must(t, m.owner.do(http.MethodPost, "/owner/bookings/"+rid+"/accept", nil), 200, "the freed dates can be accepted")

	// The first checkout completes after all: the money goes back.
	must(t, m.customer.do(http.MethodPost, "/payments/"+pay.str("payment.id")+"/verify", map[string]any{
		"gateway_payment_id": pay.str("sandbox.gateway_payment_id"), "signature": pay.str("sandbox.signature"),
	}), 200, "late verify")
	if st := m.status(t, id); st != "refund_pending" {
		t.Fatalf("after a late payment = %s", st)
	}
	e.runJob(t, "refunds.process")
	if b := m.get(t, m.customer, id); b.str("status") != "refunded" || b.num("money.refunded_paise") != pay.num("payment.amount_paise") {
		t.Fatalf("late payment refund = %s", b.raw)
	}
	e.drain(t)
	ct, _ := noteTypes(t, m.customer)
	ot, _ := noteTypes(t, m.owner)
	if countOf(ct, "payment.failed") != 1 || countOf(ct, "booking.payment_expired") != 1 || countOf(ot, "booking.payment_expired") != 1 {
		t.Fatalf("notifications customer=%v owner=%v", ct, ot)
	}
}

// ---- concurrency ------------------------------------------------------------------------------------

// holding counts slot-holding bookings of a machine that overlap another one.
func overlappingHolders(t *testing.T, e *env, eq string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM bookings a JOIN bookings b ON a.equipment_id = b.equipment_id AND a.id < b.id
		WHERE a.equipment_id = $1 AND a.period && b.period AND booking_holds_slot(a.status) AND booking_holds_slot(b.status)`, eq).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBookingConcurrencyNeverDoubleBooks(t *testing.T) {
	e := newEnv(t)

	t.Run("instant book: racing customers", func(t *testing.T) {
		m := newMarket(t, e, map[string]any{"instant_book": true})
		const n = 8
		type party struct {
			u    *user
			site string
		}
		parties := make([]party, n)
		for i := range parties {
			parties[i].u, parties[i].site = m.newCustomer(t)
		}
		codes := make([]int, n)
		var wg sync.WaitGroup
		for i, p := range parties {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r := e.call(http.MethodPost, "/bookings", p.u.token, p.u.org, m.bookingBody(p.site, m.start, m.end, "online"),
					map[string]string{"Idempotency-Key": key("race")})
				codes[i] = r.code
			}()
		}
		wg.Wait()
		won := 0
		for _, c := range codes {
			switch c {
			case 201:
				won++
			case 409:
			default:
				t.Fatalf("unexpected status %d in %v", c, codes)
			}
		}
		if won != 1 || overlappingHolders(t, e, m.eq) != 0 {
			t.Fatalf("winners = %d (codes %v), overlaps = %d", won, codes, overlappingHolders(t, e, m.eq))
		}
	})

	t.Run("owner accepts overlapping requests at once", func(t *testing.T) {
		m := newMarket(t, e, nil)
		const n = 6
		ids := make([]string, n)
		for i := range ids {
			c, s := m.newCustomer(t)
			// Overlapping but not identical periods.
			start := m.start.Add(time.Duration(i) * time.Hour)
			ids[i] = must(t, c.do(http.MethodPost, "/bookings", m.bookingBody(s, start, start.Add(48*time.Hour), "offline"),
				"Idempotency-Key", key("req")), 201, "request").str("id")
		}
		codes := make([]int, n)
		var wg sync.WaitGroup
		for i, id := range ids {
			wg.Add(1)
			go func() {
				defer wg.Done()
				codes[i] = e.call(http.MethodPost, "/owner/bookings/"+id+"/accept", m.owner.token, m.owner.org, nil, nil).code
			}()
		}
		wg.Wait()
		accepted := 0
		for _, c := range codes {
			switch c {
			case 200:
				accepted++
			case 409, 412:
			default:
				t.Fatalf("unexpected status %d in %v", c, codes)
			}
		}
		if accepted != 1 || overlappingHolders(t, e, m.eq) != 0 {
			t.Fatalf("accepted = %d (codes %v)", accepted, codes)
		}
		// Every loser ends up rejected (auto) or still requested (lost the race
		// before the winner committed) — never holding the dates.
		for _, id := range ids {
			if st := m.get(t, m.owner, id).str("status"); st != "confirmed" && st != "rejected" && st != "requested" {
				t.Fatalf("booking %s is %s", id, st)
			}
		}
	})

	t.Run("same idempotency key twice at once creates one booking", func(t *testing.T) {
		m := newMarket(t, e, nil)
		k := key("dup")
		var wg sync.WaitGroup
		codes := make([]int, 2)
		for i := range codes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				codes[i] = e.call(http.MethodPost, "/bookings", m.customer.token, m.customer.org, m.bookingBody(m.siteID, m.start, m.end, "online"),
					map[string]string{"Idempotency-Key": k}).code
			}()
		}
		wg.Wait()
		var n int
		if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM bookings WHERE equipment_id = $1`, m.eq).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 || !slices.Contains(codes, 201) {
			t.Fatalf("bookings = %d, codes = %v", n, codes)
		}
		// Reusing the key for a different request is refused.
		r := m.customer.do(http.MethodPost, "/bookings", m.bookingBody(m.siteID, m.start.Add(240*time.Hour), m.end.Add(240*time.Hour), "online"), "Idempotency-Key", k)
		if r.code != 422 && r.code != 409 {
			t.Fatalf("key reuse = %d %s", r.code, r.raw)
		}
	})

	t.Run("the database refuses an overlap written behind the API", func(t *testing.T) {
		m := newMarket(t, e, nil)
		id := m.request(t, "offline")
		must(t, m.owner.do(http.MethodPost, "/owner/bookings/"+id+"/accept", nil), 200, "accept")
		_, err := e.pool.Exec(context.Background(), `
			INSERT INTO bookings (reference, equipment_id, owner_org_id, customer_org_id, requested_by, site_address_id, site_location,
			  period, with_operator, pricing_unit, price_snapshot, estimated_total_paise, status)
			SELECT 'ER-2611-' || upper(substr(md5(random()::text), 1, 4)), equipment_id, owner_org_id, customer_org_id, requested_by, site_address_id,
			  site_location, period, with_operator, pricing_unit, price_snapshot, estimated_total_paise, 'confirmed'
			FROM bookings WHERE id = $1`, id)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23P01" {
			t.Fatalf("overlapping insert: %v", err)
		}
	})
}

// ---- extensions ------------------------------------------------------------------------------------

func TestBookingExtensions(t *testing.T) {
	e := newEnv(t)
	m := newMarket(t, e, nil)
	id := m.request(t, "offline")
	must(t, m.owner.do(http.MethodPost, "/owner/bookings/"+id+"/accept", nil), 200, "accept")
	must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/extensions", map[string]any{"new_end": m.end.Add(24 * time.Hour)},
		"Idempotency-Key", key("ext")), 409, "extend before the rental starts")
	m.setStatus(id, "dispatched", "on_site", "active")

	// Another customer holds the machine from a day after our end.
	other, siteID := m.newCustomer(t)
	next := must(t, other.do(http.MethodPost, "/bookings", m.bookingBody(siteID, m.end.Add(24*time.Hour), m.end.Add(72*time.Hour), "offline"),
		"Idempotency-Key", key("next")), 201, "next booking").str("id")
	must(t, m.owner.do(http.MethodPost, "/owner/bookings/"+next+"/accept", nil), 200, "accept next")

	q := must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/extensions/quote", map[string]any{"new_end": m.end.Add(36 * time.Hour)}), 200, "quote")
	if q.body["availability"].(map[string]any)["available"] != false || len(q.list("availability.conflicts")) != 1 {
		t.Fatalf("overlapping quote = %s", q.raw)
	}
	must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/extensions", map[string]any{"new_end": m.end.Add(36 * time.Hour)},
		"Idempotency-Key", key("ext")), 409, "extend into the next booking")

	// The owner proposes 12 hours; the customer must answer.
	x := must(t, m.owner.do(http.MethodPost, "/bookings/"+id+"/extensions", map[string]any{"new_end": m.end.Add(12 * time.Hour)},
		"Idempotency-Key", key("ext")), 201, "owner proposes").str("extension.id")
	must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/extensions", map[string]any{"new_end": m.end.Add(6 * time.Hour)},
		"Idempotency-Key", key("ext")), 409, "second open request")
	must(t, m.owner.do(http.MethodPost, "/bookings/"+id+"/extensions/"+x+"/approve", nil), 403, "owner approves own proposal")
	must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/extensions/"+x+"/decline", nil), 400, "decline without reason")
	must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/extensions/"+x+"/decline", map[string]any{"reason": "Work finishes on time"}), 200, "decline")
	b := m.get(t, m.customer, id)
	if end, _ := time.Parse(time.RFC3339, b.str("period.end")); b.str("status") != "active" || !end.Equal(m.end) {
		t.Fatalf("after decline = %s", b.raw)
	}
	if tl := b.list("timeline"); fmt.Sprint(tl[len(tl)-1].(map[string]any)["reason"]) != "Work finishes on time" {
		t.Fatalf("the decline reason is not in the history: %v", tl[len(tl)-1])
	}

	// Withdrawn by the side that asked.
	x = must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/extensions", map[string]any{"new_end": m.end.Add(12 * time.Hour)},
		"Idempotency-Key", key("ext")), 201, "customer asks").str("extension.id")
	must(t, m.owner.do(http.MethodPost, "/bookings/"+id+"/extensions/"+x+"/withdraw", nil), 403, "owner withdraws customer's request")
	must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/extensions/"+x+"/withdraw", nil), 200, "withdraw")

	// Unanswered until the rental's end: the request lapses.
	e.exec(`UPDATE bookings SET period = tstzrange(now() - interval '2 days', now() + interval '3 seconds', '[)') WHERE id = $1`, id)
	must(t, m.customer.do(http.MethodPost, "/bookings/"+id+"/extensions", map[string]any{"new_end": time.Now().Add(6 * time.Hour)},
		"Idempotency-Key", key("ext")), 201, "late request")
	time.Sleep(4 * time.Second)
	e.runJob(t, "bookings.lapse_extensions")
	exts := must(t, m.customer.do(http.MethodGet, "/bookings/"+id+"/extensions", nil), 200, "extensions")
	var statuses []string
	for _, x := range exts.list("data") {
		statuses = append(statuses, fmt.Sprint(x.(map[string]any)["status"]))
	}
	if !slices.Equal(statuses, []string{"declined", "withdrawn", "expired"}) || m.status(t, id) != "active" {
		t.Fatalf("extensions = %v, status %s", statuses, m.status(t, id))
	}
	e.drain(t)
	ct, _ := noteTypes(t, m.customer)
	ot, _ := noteTypes(t, m.owner)
	if countOf(ct, "booking.extension_requested") != 1 || countOf(ot, "booking.extension_declined") != 1 || countOf(ct, "booking.extension_expired") != 1 {
		t.Fatalf("notifications customer=%v owner=%v", ct, ot)
	}
}
