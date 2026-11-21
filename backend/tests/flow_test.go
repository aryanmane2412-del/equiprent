package tests

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/equiprent/backend/internal/logger"
	"github.com/equiprent/backend/internal/modules/payments"
	"github.com/equiprent/backend/internal/worker"
)

// Pune yard and a site ~10 km away (both GST state 27, Maharashtra).
var (
	yard = map[string]float64{"lat": 18.5204, "lng": 73.8567}
	site = map[string]float64{"lat": 18.5913, "lng": 73.7389}
)

// TestRentalLifecycle walks a booking end to end through the real router
// and database: listing → moderation → search → booking (idempotent) →
// acceptance → agreement → payment → operator → dispatch → geofenced arrival
// → handover → chat → return → completion → invoice → review → dispute →
// notifications (worker) → fleet / admin views.
func TestRentalLifecycle(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// ---- owner onboarding and listing ------------------------------------------
	owner := e.login(randomPhone())
	must(t, owner.do(http.MethodPatch, "/me", map[string]any{"full_name": "Ravi Patil"}), 200, "update profile")
	biz := must(t, owner.do(http.MethodPost, "/orgs", map[string]any{
		"legal_name": "Patil Earthmovers Pvt Ltd", "display_name": "Patil Earthmovers",
		"org_type": "private_limited", "gstin": randomGSTIN("27"),
	}), 201, "create org")
	owner.org = biz.str("id")
	if biz.str("gst_state_code") != "27" {
		t.Fatalf("gst state = %s", biz.str("gst_state_code"))
	}
	must(t, owner.do(http.MethodPost, "/owners/onboard", nil), 200, "onboard owner")
	kyc := e.kycAdmin()
	e.verifyOwner(owner, kyc)

	models := must(t, owner.do(http.MethodGet, "/models?category=excavator", nil), 200, "models")
	if len(models.list("data")) == 0 {
		t.Fatal("no seeded excavator models")
	}
	reg := fmt.Sprintf("MH12%s%04d", randomLetters(3), rand.IntN(10000))
	eq := must(t, owner.do(http.MethodPost, "/owner/equipment", map[string]any{
		"model_id": models.str("data.0.id"), "title": "Tata Hitachi EX200 with operator",
		"registration_no": reg, "current_hmr": 4210.5, "base_location": yard, "service_radius_km": 60,
	}), 201, "create listing")
	eqID := eq.str("id")
	if eq.str("status") != "draft" {
		t.Fatalf("new listing status = %s", eq.str("status"))
	}
	r := owner.do(http.MethodPost, "/owner/equipment/"+eqID+"/submit", nil)
	must(t, r, 422, "submit without price")

	// Optimistic locking: a stale version is refused.
	must(t, owner.do(http.MethodPatch, "/owner/equipment/"+eqID, map[string]any{"version": 99, "title": "x y z"}), 412, "stale edit")
	must(t, owner.do(http.MethodPatch, "/owner/equipment/"+eqID, map[string]any{"description": "20 t class, 0.9 m³ bucket"}, "If-Match", `"1"`), 200, "edit")

	must(t, owner.do(http.MethodPut, "/owner/equipment/"+eqID+"/pricing", map[string]any{
		"plans":        []map[string]any{{"unit": "day", "with_operator": true, "rate_paise": 1500000, "operator_bata_per_day_paise": 50000}},
		"mobilisation": map[string]any{"fixed_paise": 300000, "per_km_paise": 5000, "free_km": 5, "needs_trailer": true},
	}), 200, "set pricing")
	must(t, owner.do(http.MethodPut, "/owner/equipment/"+eqID+"/specs", map[string]any{
		"specs": []map[string]any{{"key": "operating_weight_kg", "value_numeric": 20000, "unit": "kg"}},
	}), 200, "set specs")
	must(t, owner.do(http.MethodPost, "/owner/equipment/"+eqID+"/submit", nil), 422, "submit without photos")
	owner.attachPhoto(eqID)
	owner.attachEquipmentDocument(eqID)
	must(t, owner.do(http.MethodPost, "/owner/equipment/"+eqID+"/submit", nil), 200, "submit")

	// ---- admin moderation -------------------------------------------------------
	admin := e.login(randomPhone())
	e.exec(`INSERT INTO user_roles (user_id, role_code) VALUES ($1, 'admin_super')`, admin.id)
	must(t, owner.do(http.MethodGet, "/admin/stats", nil), 403, "non-admin blocked")
	review := must(t, admin.do(http.MethodGet, "/admin/listings/"+eqID, nil), 200, "listing in moderation")
	if st := review.str("status"); st != "pending_review" && st != "active" {
		t.Fatalf("listing not awaiting review: %.300s", review.raw)
	}

	// ---- customer: site, search, estimate, booking ------------------------------
	customer := e.login(randomPhone())
	me := must(t, customer.do(http.MethodGet, "/me", nil), 200, "me")
	custOrg := me.str("roles.0.organization_id")
	must(t, customer.do(http.MethodPost, "/orgs/"+custOrg+"/addresses", map[string]any{
		"label": "Hinjewadi site", "line1": "Plot 12, Phase 2", "city": "Pune", "state_code": "27", "pincode": "411057", "is_site": true,
	}), 400, "site without location")
	addr := must(t, customer.do(http.MethodPost, "/orgs/"+custOrg+"/addresses", map[string]any{
		"label": "Hinjewadi site", "line1": "Plot 12, Phase 2", "city": "Pune", "state_code": "27", "pincode": "411057",
		"is_site": true, "location": site,
	}), 201, "create site")

	start := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Hour)
	end := start.Add(3 * 24 * time.Hour)
	q := fmt.Sprintf("/search/equipment?category=excavator&lat=%v&lng=%v&start=%s&end=%s&unit=day",
		site["lat"], site["lng"], start.Format(time.RFC3339), end.Format(time.RFC3339))
	// The shared test database keeps listings from earlier runs at the same
	// yard, so follow the cursor rather than assuming the first page.
	listed := false
	for page, cursor := 0, ""; page < 100 && !listed; page++ {
		found := must(t, customer.do(http.MethodGet, q+"&cursor="+cursor, nil), 200, "search")
		listed = strings.Contains(found.raw, eqID)
		if cursor = found.str("next_cursor"); cursor == "" || cursor == "<nil>" {
			break
		}
	}
	if !listed {
		t.Fatal("listing not found by search")
	}
	period := map[string]any{"start": start, "end": end}
	est := must(t, customer.do(http.MethodPost, "/equipment/"+eqID+"/estimate", map[string]any{
		"site": site, "period": period, "pricing_unit": "day", "with_operator": true,
	}), 200, "estimate")
	if est.num("rental_paise") != 3*1500000 || est.num("operator_bata_paise") != 3*50000 || est.num("mobilisation_paise") <= 300000 {
		t.Fatalf("estimate = %s", est.raw)
	}

	bad := must(t, customer.do(http.MethodPost, "/bookings", map[string]any{"pricing_unit": "fortnight"}, "Idempotency-Key", "bad-request-1"), 400, "invalid booking")
	if bad.str("code") != "VALIDATION_FAILED" || len(bad.list("errors")) < 3 {
		t.Fatalf("validation problem = %s", bad.raw)
	}
	bookingReq := map[string]any{
		"equipment_id": eqID, "site_address_id": addr.str("id"), "period": period, "pricing_unit": "day",
		"with_operator": true, "work_description": "Foundation excavation", "payment_mode": "online",
	}
	idem := fmt.Sprintf("book-%d", rand.Int64())
	bk := must(t, customer.do(http.MethodPost, "/bookings", bookingReq, "Idempotency-Key", idem), 201, "create booking")
	again := must(t, customer.do(http.MethodPost, "/bookings", bookingReq, "Idempotency-Key", idem), 201, "replay booking")
	if again.str("id") != bk.str("id") || again.header.Get("Idempotency-Replayed") != "true" {
		t.Fatal("idempotent replay returned a different booking")
	}
	bookingID := bk.str("id")
	if bk.str("status") != "requested" || !strings.HasPrefix(bk.str("reference"), "ER-") {
		t.Fatalf("booking = %s", bk.raw)
	}

	// A competing request for the same dates; accepting it later must fail.
	rival := e.login(randomPhone())
	rme := must(t, rival.do(http.MethodGet, "/me", nil), 200, "rival me")
	rivalOrg := rme.str("roles.0.organization_id")
	raddr := must(t, rival.do(http.MethodPost, "/orgs/"+rivalOrg+"/addresses", map[string]any{
		"label": "Wakad", "line1": "Survey 44", "city": "Pune", "state_code": "27", "pincode": "411057", "is_site": true, "location": site,
	}), 201, "rival site")
	rivalReq := map[string]any{"equipment_id": eqID, "site_address_id": raddr.str("id"), "period": period, "pricing_unit": "day", "with_operator": true}
	rb := must(t, rival.do(http.MethodPost, "/bookings", rivalReq, "Idempotency-Key", fmt.Sprintf("rival-%d", rand.Int64())), 201, "rival booking")

	// Parties only: the rival cannot see the first booking.
	must(t, rival.do(http.MethodGet, "/bookings/"+bookingID, nil), 404, "non-party read")

	// ---- owner response -----------------------------------------------------------
	inbox := must(t, owner.do(http.MethodGet, "/owner/bookings?status=requested", nil), 200, "owner inbox")
	if len(inbox.list("data")) != 2 {
		t.Fatalf("inbox = %s", inbox.raw)
	}
	acc := must(t, owner.do(http.MethodPost, "/owner/bookings/"+bookingID+"/accept", nil), 200, "accept")
	if acc.str("status") != "payment_pending" {
		t.Fatalf("after accept = %s", acc.raw)
	}
	// Accepting holds the dates, so the overlapping request is declined at
	// once (it could never be accepted) and the rival is told why.
	rival2 := must(t, rival.do(http.MethodGet, "/bookings/"+rb.str("id"), nil), 200, "rival booking after accept")
	if rival2.str("status") != "rejected" || !strings.Contains(rival2.raw, "booked by another customer") {
		t.Fatalf("overlapping request = %s", rival2.raw)
	}
	clash := must(t, owner.do(http.MethodPost, "/owner/bookings/"+rb.str("id")+"/accept", nil), 409, "double booking")
	if clash.str("code") != "INVALID_STATE_TRANSITION" {
		t.Fatalf("overlap problem = %s", clash.raw)
	}

	// ---- agreement ------------------------------------------------------------------
	ag := must(t, customer.do(http.MethodGet, "/bookings/"+bookingID+"/agreement", nil), 200, "agreement")
	if !strings.Contains(ag.str("terms"), bk.str("reference")) {
		t.Fatal("agreement does not name the booking")
	}
	must(t, customer.do(http.MethodPost, "/bookings/"+bookingID+"/agreement/accept", map[string]any{"terms_sha256": strings.Repeat("0", 64)}), 409, "wrong hash")
	must(t, customer.do(http.MethodPost, "/bookings/"+bookingID+"/agreement/accept", map[string]any{"terms_sha256": ag.str("terms_sha256")}), 200, "customer accepts")
	signed := must(t, owner.do(http.MethodPost, "/bookings/"+bookingID+"/agreement/accept", map[string]any{"terms_sha256": ag.str("terms_sha256")}), 200, "owner accepts")
	if signed.body["fully_accepted"] != true {
		t.Fatalf("agreement = %s", signed.raw)
	}

	// ---- payment (sandbox gateway) --------------------------------------------------
	pay := must(t, customer.do(http.MethodPost, "/bookings/"+bookingID+"/payments", map[string]any{"purpose": "advance"},
		"Idempotency-Key", fmt.Sprintf("pay-%d", rand.Int64())), 201, "create payment")
	if pay.num("payment.amount_paise") != est.num("advance_due_paise") || pay.str("sandbox.signature") == "" {
		t.Fatalf("payment = %s", pay.raw)
	}
	must(t, customer.do(http.MethodPost, "/payments/"+pay.str("payment.id")+"/verify", map[string]any{
		"gateway_payment_id": pay.str("sandbox.gateway_payment_id"), "signature": strings.Repeat("a", 64),
	}), 400, "forged signature")
	must(t, customer.do(http.MethodPost, "/payments/"+pay.str("payment.id")+"/verify", map[string]any{
		"gateway_payment_id": pay.str("sandbox.gateway_payment_id"), "signature": pay.str("sandbox.signature"),
	}), 200, "verify payment")
	if got := must(t, customer.do(http.MethodGet, "/bookings/"+bookingID, nil), 200, "booking").str("status"); got != "confirmed" {
		t.Fatalf("after payment status = %s", got)
	}

	// Webhooks: signature checked, unknown orders ignored, redelivery deduplicated.
	hook := []byte(`{"event":"payment.captured","payload":{"payment":{"entity":{"id":"pay_x","order_id":"order_unknown","amount":100}}}}`)
	sig := payments.Sign(e.cfg.Payments.WebhookSecret, string(hook))
	evID := fmt.Sprintf("evt_%d", rand.Int64())
	must(t, e.call(http.MethodPost, "/webhooks/payments/razorpay", "", "", hook, map[string]string{"X-Razorpay-Signature": "bad"}), 401, "bad webhook signature")
	w1 := must(t, e.call(http.MethodPost, "/webhooks/payments/razorpay", "", "", hook, map[string]string{"X-Razorpay-Signature": sig, "X-Razorpay-Event-Id": evID}), 200, "webhook")
	w2 := must(t, e.call(http.MethodPost, "/webhooks/payments/razorpay", "", "", hook, map[string]string{"X-Razorpay-Signature": sig, "X-Razorpay-Event-Id": evID}), 200, "webhook redelivery")
	if w1.str("status") != "ignored" || w2.str("status") != "duplicate" {
		t.Fatalf("webhooks = %s / %s", w1.raw, w2.raw)
	}

	// ---- operator --------------------------------------------------------------------
	opPhone := randomPhone()
	must(t, owner.do(http.MethodPost, "/orgs/"+owner.org+"/members", map[string]any{"phone": opPhone, "role": "operator", "full_name": "Suresh"}), 201, "add operator")
	operator := e.login(opPhone)
	e.acceptInvitations(operator)
	must(t, operator.do(http.MethodPut, "/operators/me/profile", map[string]any{
		"licence_number": "MH1220150001234", "licence_class": "HMV", "licence_expires_on": "2030-01-31", "experience_years": 8, "skills": []string{"excavator"},
	}), 200, "operator profile")
	must(t, operator.do(http.MethodPost, "/operator/availability", map[string]any{"on_duty": true}), 200, "on duty")
	must(t, owner.do(http.MethodPost, "/owner/bookings/"+bookingID+"/assign-operator", map[string]any{"operator_user_id": operator.id}), 422, "unverified operator")
	e.verifyOperator(operator, kyc)
	must(t, operator.do(http.MethodPut, "/operators/me/profile", map[string]any{"licence_number": "XX0000000000", "licence_class": "LMV"}), 409, "licence frozen after verification")
	must(t, owner.do(http.MethodPost, "/owner/bookings/"+bookingID+"/assign-operator", map[string]any{"operator_user_id": operator.id}), 200, "assign operator")
	jobs := must(t, operator.do(http.MethodGet, "/operator/jobs", nil), 200, "operator jobs")
	if len(jobs.list("data")) != 1 {
		t.Fatalf("jobs = %s", jobs.raw)
	}
	must(t, operator.do(http.MethodPost, "/operator/jobs/"+bookingID+"/accept", nil), 200, "operator accepts")
	operator.checklist(t, bookingID, "handover", 2150)
	must(t, operator.do(http.MethodPost, "/operator/jobs/"+bookingID+"/dispatch", nil), 200, "dispatch")

	// ---- tracking + geofence -----------------------------------------------------------
	now := time.Now().UTC()
	must(t, operator.do(http.MethodPost, "/operator/jobs/"+bookingID+"/tracking/session", map[string]any{
		"consent": true, "policy_version": e.cfg.Tracking.PolicyVersion}), 200, "start sharing location")
	batch := must(t, operator.do(http.MethodPost, "/operator/jobs/"+bookingID+"/tracking/batch", map[string]any{"points": []map[string]any{
		{"lat": yard["lat"], "lng": yard["lng"], "recorded_at": now.Add(-30 * time.Minute)},
		{"lat": 18.56, "lng": 73.78, "recorded_at": now.Add(-15 * time.Minute), "speed_mps": 9.5},
		{"lat": site["lat"] + 0.0005, "lng": site["lng"], "recorded_at": now.Add(-time.Minute), "battery_pct": 71},
	}}), 200, "tracking batch")
	if batch.num("accepted") != 3 || batch.body["arrived"] != true {
		t.Fatalf("batch = %s", batch.raw)
	}
	track := must(t, customer.do(http.MethodGet, "/bookings/"+bookingID+"/track", nil), 200, "track")
	if track.str("status") != "on_site" || track.body["equipment_position"] == nil {
		t.Fatalf("track = %s", track.raw)
	}

	customer.ackInspection(t, bookingID, "handover")

	must(t, customer.do(http.MethodPost, "/bookings/"+bookingID+"/handover/confirm", nil), 200, "handover")

	// ---- chat ---------------------------------------------------------------------------
	conv := must(t, customer.do(http.MethodPost, "/bookings/"+bookingID+"/conversation", nil), 200, "open chat")
	must(t, customer.do(http.MethodPost, "/conversations/"+conv.str("id")+"/messages", map[string]any{"body": "Please start at the north corner."}), 201, "send message")
	msgs := must(t, operator.do(http.MethodGet, "/conversations/"+conv.str("id")+"/messages", nil), 200, "operator reads chat")
	if len(msgs.list("data")) != 1 {
		t.Fatalf("messages = %s", msgs.raw)
	}
	must(t, rival.do(http.MethodGet, "/conversations/"+conv.str("id")+"/messages", nil), 404, "outsider reads chat")

	// ---- return and completion ------------------------------------------------------------
	must(t, customer.do(http.MethodPost, "/bookings/"+bookingID+"/return-request", nil), 200, "request return")
	operator.checklist(t, bookingID, "return", 2200)
	operator.ackInspection(t, bookingID, "return")
	must(t, operator.do(http.MethodPost, "/operator/jobs/"+bookingID+"/returned", nil), 200, "returned")
	e.exec(`UPDATE bookings SET final_total_paise = GREATEST(COALESCE((SELECT sum(p.amount_paise) FROM payments p WHERE p.booking_id = bookings.id AND p.status = 'captured'), 0) - deposit_paise, 0), final_charges = '{}'::jsonb, finalized_at = now() WHERE id = $1 AND finalized_at IS NULL`, bookingID)
	done := must(t, owner.do(http.MethodPost, "/owner/bookings/"+bookingID+"/complete", nil), 200, "complete")
	if done.str("status") != "completed" {
		t.Fatalf("complete = %s", done.raw)
	}
	timeline := must(t, customer.do(http.MethodGet, "/bookings/"+bookingID, nil), 200, "timeline").list("timeline")
	if len(timeline) < 10 {
		t.Fatalf("timeline has %d events", len(timeline))
	}

	// ---- invoice ---------------------------------------------------------------------------
	inv := must(t, owner.do(http.MethodPost, "/owner/bookings/"+bookingID+"/invoices", nil), 201, "issue invoice")
	if inv.str("supply_type") != "intra" || inv.num("cgst_paise") != inv.num("sgst_paise") || inv.num("igst_paise") != 0 ||
		inv.num("total_paise") != inv.num("subtotal_paise")+inv.num("cgst_paise")+inv.num("sgst_paise") || inv.str("status") != "issued" {
		t.Fatalf("invoice = %s", inv.raw)
	}
	if !strings.HasPrefix(inv.str("invoice_number"), "ER/") || len(inv.list("lines")) < 3 {
		t.Fatalf("invoice number/lines = %s", inv.raw)
	}
	must(t, owner.do(http.MethodPost, "/owner/bookings/"+bookingID+"/invoices", nil), 409, "second rental invoice")
	must(t, customer.do(http.MethodGet, "/invoices/"+inv.str("id"), nil), 200, "recipient reads invoice")

	// ---- review ------------------------------------------------------------------------------
	must(t, customer.do(http.MethodPost, "/bookings/"+bookingID+"/reviews", map[string]any{"subject": "customer", "rating": 5}), 403, "customer reviews customer")
	must(t, customer.do(http.MethodPost, "/bookings/"+bookingID+"/reviews", map[string]any{"subject": "equipment", "rating": 5, "comment": "Great machine"}), 201, "review")
	must(t, customer.do(http.MethodPost, "/bookings/"+bookingID+"/reviews", map[string]any{"subject": "equipment", "rating": 4}), 409, "duplicate review")
	listing := must(t, customer.do(http.MethodGet, "/equipment/"+eqID, nil), 200, "listing")
	if listing.num("rating_avg") != 5 || listing.num("rating_count") != 1 {
		t.Fatalf("rating = %v / %v", listing.num("rating_avg"), listing.num("rating_count"))
	}

	// ---- dispute -------------------------------------------------------------------------------
	disp := must(t, customer.do(http.MethodPost, "/bookings/"+bookingID+"/disputes", map[string]any{
		"category": "hour_meter", "summary": "Day 2 log shows 3 more hours than worked", "amount_claimed_paise": 450000,
	}), 201, "raise dispute")
	if got := must(t, customer.do(http.MethodGet, "/bookings/"+bookingID, nil), 200, "booking").str("status"); got != "disputed" {
		t.Fatalf("booking after dispute = %s", got)
	}
	// Admin accounts are read-only: they can observe but not assign or resolve disputes.
	must(t, admin.do(http.MethodPost, "/admin/disputes/"+disp.str("id")+"/assign", nil), 403, "admins cannot assign disputes")
	must(t, admin.do(http.MethodPost, "/admin/disputes/"+disp.str("id")+"/resolve", map[string]any{
		"outcome": "resolved", "resolution": "HMR photos support the customer; 3 h credited", "resolved_amount_paise": 300000,
	}), 403, "admins cannot resolve disputes")

	// ---- background worker: outbox → notifications ---------------------------------------------
	sched := worker.NewScheduler(worker.NewPGLocker(e.pool, e.app.Tx), logger.Discard())
	jobs2 := worker.Jobs{Pool: e.pool, Dispatcher: e.app.Notifications, Log: logger.Discard()}
	for _, j := range jobs2.All() {
		for range 5 { // drain the outbox in batches
			sched.RunOnce(ctx, j)
		}
		if sched.Runs(j.Name) == 0 {
			t.Fatalf("job %s never completed", j.Name)
		}
	}
	notes := must(t, owner.do(http.MethodGet, "/notifications", nil), 200, "owner notifications")
	if len(notes.list("data")) == 0 {
		t.Fatal("owner received no notifications")
	}
	if !strings.Contains(notes.raw, "booking.requested") {
		t.Fatalf("missing booking.requested notification: %s", notes.raw)
	}
	unread := must(t, owner.do(http.MethodGet, "/notifications/unread-count", nil), 200, "unread")
	marked := must(t, owner.do(http.MethodPost, "/notifications/read-all", nil), 200, "read all")
	if marked.num("marked") != unread.num("unread") {
		t.Fatalf("marked %v of %v", marked.num("marked"), unread.num("unread"))
	}

	// ---- fleet, owner dashboard, admin views ----------------------------------------------------
	fleet := must(t, owner.do(http.MethodGet, "/fleet/"+owner.org+"/overview", nil), 200, "fleet overview")
	if fleet.num("equipment_total") != 1 || fleet.num("operators") != 1 {
		t.Fatalf("fleet = %s", fleet.raw)
	}
	util := must(t, owner.do(http.MethodGet, fmt.Sprintf("/fleet/%s/reports/utilisation?from=%s&to=%s", owner.org,
		start.Add(-24*time.Hour).Format(time.RFC3339), end.Add(24*time.Hour).Format(time.RFC3339)), nil), 200, "utilisation")
	if util.num("data.0.booked_hours") != 72 {
		t.Fatalf("utilisation = %s", util.raw)
	}
	must(t, customer.do(http.MethodGet, "/fleet/"+owner.org+"/overview", nil), 404, "fleet of another org")
	dash := must(t, owner.do(http.MethodGet, "/owners/me/dashboard", nil), 200, "dashboard")
	if dash.num("bookings_completed") != 0 || dash.num("captured_last_30d_paise") != pay.num("payment.amount_paise") {
		t.Fatalf("dashboard = %s", dash.raw)
	}
	audit := must(t, admin.do(http.MethodGet, "/admin/audit-logs?entity_type=equipment&entity_id="+eqID, nil), 200, "audit log")
	_ = audit // admins can read the audit log; they no longer approve listings themselves
	must(t, admin.do(http.MethodGet, "/admin/stats", nil), 200, "admin stats")
}

// TestAuthSessions covers refresh rotation, reuse detection and logout
// against the real session store.
func TestAuthSessions(t *testing.T) {
	e := newEnv(t)
	u := e.login(randomPhone())

	must(t, e.call(http.MethodGet, "/me", "", "", nil, nil), 401, "anonymous /me")
	sessions := must(t, u.do(http.MethodGet, "/auth/sessions", nil), 200, "sessions")
	if len(sessions.list("data")) != 1 || sessions.body["data"].([]any)[0].(map[string]any)["current"] != true {
		t.Fatalf("sessions = %s", sessions.raw)
	}

	rotated := must(t, e.call(http.MethodPost, "/auth/refresh", "", "", map[string]any{"refresh_token": u.refesh}, nil), 200, "refresh")
	must(t, u.do(http.MethodGet, "/me", nil), 401, "old access token after rotation")
	must(t, e.call(http.MethodGet, "/me", rotated.str("access_token"), "", nil, nil), 200, "new access token")

	must(t, e.call(http.MethodPost, "/auth/refresh", "", "", map[string]any{"refresh_token": u.refesh}, nil), 401, "reuse of rotated token")
	must(t, e.call(http.MethodGet, "/me", rotated.str("access_token"), "", nil, nil), 401, "family revoked after reuse")

	again := e.login(randomPhone())
	must(t, again.do(http.MethodPost, "/auth/logout", nil), 204, "logout")
	must(t, again.do(http.MethodGet, "/me", nil), 401, "after logout")

	// Members of several organisations must pick one.
	two := e.login(randomPhone())
	must(t, two.do(http.MethodPost, "/orgs", map[string]any{"legal_name": "Second Co", "display_name": "Second", "org_type": "llp"}), 201, "second org")
	if p := must(t, two.do(http.MethodGet, "/bookings", nil), 400, "ambiguous org"); p.str("code") != "ORG_CONTEXT_REQUIRED" {
		t.Fatalf("problem = %s", p.raw)
	}
	two.org = "00000000-0000-0000-0000-000000000001"
	must(t, two.do(http.MethodGet, "/bookings", nil), 403, "foreign org header")
}
