package tests

// Communication and operations modules: notifications, digital agreements,
// chat, maintenance and reviews — each on its own, then together in one
// rental (TestCommunicationAndOperationsTogether).

import (
	"bytes"
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/logger"
	"github.com/equiprent/backend/internal/worker"
)

// rental is a listing, a customer with a site and a booking request.
type rental struct {
	e                    *env
	owner, customer, ops *user
	kyc                  *user
	eq, siteID, id, ref  string
	start, end           time.Time
	estimatedTotal       float64
}

func newRental(t *testing.T, e *env, paymentMode string) *rental {
	t.Helper()
	kyc, ops := e.kycAdmin(), e.opsAdmin()
	owner := e.verifiedOwner(kyc, "Comms Rentals "+randomLetters(4))
	eq := e.publish(owner, ops, listingBody(map[string]any{"title": "Liugong 835H loader " + randomLetters(5)}))
	cust := e.registerAndVerify("customer", "")
	addr := must(t, cust.do(http.MethodPost, "/orgs/"+cust.org+"/addresses", map[string]any{
		"label": "Hinjewadi site", "line1": "Plot 12, Phase 2", "city": "Pune", "state_code": "27", "pincode": "411057",
		"is_site": true, "location": site,
	}), 201, "site")
	start := time.Now().Add(96 * time.Hour).UTC().Truncate(time.Hour)
	r := &rental{e: e, owner: owner, customer: cust, ops: ops, kyc: kyc, eq: eq, siteID: addr.str("id"), start: start, end: start.Add(72 * time.Hour)}
	bk := must(t, cust.do(http.MethodPost, "/bookings", map[string]any{
		"equipment_id": eq, "site_address_id": r.siteID, "period": map[string]any{"start": r.start, "end": r.end},
		"pricing_unit": "day", "with_operator": true, "work_description": "Site levelling", "payment_mode": paymentMode,
	}, "Idempotency-Key", fmt.Sprintf("comms-%d", rand.Int64())), 201, "book")
	r.id, r.ref, r.estimatedTotal = bk.str("id"), bk.str("reference"), bk.num("estimated_total_paise")
	return r
}

func (r *rental) accept(t *testing.T) {
	t.Helper()
	must(t, r.owner.do(http.MethodPost, "/owner/bookings/"+r.id+"/accept", nil), 200, "accept")
}

// finish moves an accepted, paid booking through the rental to completed
// (the database checks every step of the lifecycle graph).
func (r *rental) finish(t *testing.T, from string) {
	t.Helper()
	steps := []string{"confirmed", "dispatched", "on_site", "active", "return_initiated", "returned", "completed"}
	if i := slices.Index(steps, from); i >= 0 {
		steps = steps[i+1:]
	}
	for _, st := range steps {
		r.e.exec(`UPDATE bookings SET status = $2 WHERE id = $1`, r.id, st)
	}
}

// pay pays the advance through the sandbox gateway.
func (r *rental) pay(t *testing.T) resp {
	t.Helper()
	pay := must(t, r.customer.do(http.MethodPost, "/bookings/"+r.id+"/payments", map[string]any{"purpose": "advance"},
		"Idempotency-Key", fmt.Sprintf("pay-%d", rand.Int64())), 201, "create payment")
	must(t, r.customer.do(http.MethodPost, "/payments/"+pay.str("payment.id")+"/verify", map[string]any{
		"gateway_payment_id": pay.str("sandbox.gateway_payment_id"), "signature": pay.str("sandbox.signature"),
	}), 200, "verify payment")
	return pay
}

// drain delivers pending outbox events as notifications.
func (e *env) drain(t *testing.T) {
	t.Helper()
	sched := worker.NewScheduler(worker.NewPGLocker(e.pool, e.app.Tx), logger.Discard())
	for _, j := range (worker.Jobs{Pool: e.pool, Dispatcher: e.app.Notifications, Refunds: e.app.Payments, Tracking: e.app.Tracking, Log: logger.Discard()}).All() {
		if j.Name == "outbox.dispatch" {
			for range 40 {
				sched.RunOnce(context.Background(), j)
			}
		}
	}
}

func (e *env) runJob(t *testing.T, name string) {
	t.Helper()
	sched := worker.NewScheduler(worker.NewPGLocker(e.pool, e.app.Tx), logger.Discard())
	for _, j := range (worker.Jobs{Pool: e.pool, Dispatcher: e.app.Notifications, Refunds: e.app.Payments, Tracking: e.app.Tracking, Log: logger.Discard()}).All() {
		if j.Name == name {
			sched.RunOnce(context.Background(), j)
			return
		}
	}
	t.Fatalf("no job %s", name)
}

// noteTypes lists the notification types a user received, newest first.
func noteTypes(t *testing.T, u *user) ([]string, resp) {
	t.Helper()
	r := must(t, u.do(http.MethodGet, "/notifications?limit=100", nil), 200, "notifications")
	var out []string
	for _, n := range r.list("data") {
		out = append(out, fmt.Sprint(n.(map[string]any)["type"]))
	}
	return out, r
}

func countOf(list []string, v string) int {
	n := 0
	for _, x := range list {
		if x == v {
			n++
		}
	}
	return n
}

// ---- notifications -----------------------------------------------------------------------

func TestNotificationsModule(t *testing.T) {
	e := newIsolatedEnv(t)
	r := newRental(t, e, "online")

	// Devices and preferences.
	must(t, r.customer.do(http.MethodPost, "/me/devices", map[string]any{"platform": "android", "push_token": "fcm-token-" + randomLetters(20)}), 200, "register device")
	devs := must(t, r.customer.do(http.MethodGet, "/me/devices", nil), 200, "devices")
	if len(devs.list("data")) != 1 || devs.str("push") != "none" || strings.Contains(devs.raw, "fcm-token-") {
		t.Fatalf("devices = %s", devs.raw)
	}
	prefs := must(t, r.customer.do(http.MethodGet, "/me/notification-preferences", nil), 200, "prefs")
	if len(prefs.list("data")) != 7 || prefs.body["data"].([]any)[0].(map[string]any)["push"] != true {
		t.Fatalf("prefs = %s", prefs.raw)
	}
	must(t, r.customer.do(http.MethodPut, "/me/notification-preferences", map[string]any{"preferences": []map[string]any{{"category": "nope", "push": false}}}), 400, "bad category")
	upd := must(t, r.customer.do(http.MethodPut, "/me/notification-preferences", map[string]any{"preferences": []map[string]any{{"category": "chat", "push": false}}}), 200, "mute chat")
	if !strings.Contains(upd.raw, `{"category":"chat","push":false}`) {
		t.Fatalf("prefs after update = %s", upd.raw)
	}

	// Booking request → owner; acceptance → customer.
	e.drain(t)
	ownerTypes, _ := noteTypes(t, r.owner)
	if countOf(ownerTypes, "booking.requested") != 1 {
		t.Fatalf("owner notifications = %v", ownerTypes)
	}
	r.accept(t)
	e.drain(t)
	custTypes, _ := noteTypes(t, r.customer)
	if countOf(custTypes, "booking.accepted") != 1 {
		t.Fatalf("customer notifications = %v", custTypes)
	}

	// Payment confirmation → both, different wording.
	pay := r.pay(t)
	e.drain(t)
	_, cn := noteTypes(t, r.customer)
	_, on := noteTypes(t, r.owner)
	if !strings.Contains(cn.raw, "Payment confirmed") || !strings.Contains(on.raw, "Payment received") ||
		!strings.Contains(cn.raw, r.ref) || !strings.Contains(cn.raw, `"category":"payments"`) {
		t.Fatalf("payment notifications:\ncustomer %s\nowner %s", cn.raw, on.raw)
	}
	if !strings.Contains(cn.raw, fmt.Sprintf(`"amount_paise":%.0f`, pay.num("payment.amount_paise"))) {
		t.Fatalf("payment data missing amount: %s", cn.raw)
	}
	custTypes, _ = noteTypes(t, r.customer)
	if countOf(custTypes, "booking.confirmed") != 1 {
		t.Fatalf("confirmation missing: %v", custTypes)
	}

	// Operator assignment → operator and customer.
	opPhone := randomPhone()
	must(t, r.owner.do(http.MethodPost, "/orgs/"+r.owner.org+"/members", map[string]any{"phone": opPhone, "role": "operator", "full_name": "Suresh"}), 201, "add operator")
	op := e.login(opPhone)
	e.verifyOperator(op, r.kyc)
	must(t, r.owner.do(http.MethodPost, "/owner/bookings/"+r.id+"/assign-operator", map[string]any{"operator_user_id": op.id}), 200, "assign")
	e.drain(t)
	if types, _ := noteTypes(t, op); countOf(types, "booking.job_assigned") != 1 {
		t.Fatalf("operator notifications = %v", types)
	}

	// Rental start and completion → both sides.
	must(t, op.do(http.MethodPost, "/operator/jobs/"+r.id+"/accept", nil), 200, "operator accepts")
	op.checklist(t, r.id, "handover", 2150)
	must(t, op.do(http.MethodPost, "/operator/jobs/"+r.id+"/dispatch", nil), 200, "dispatch")
	must(t, op.do(http.MethodPost, "/operator/jobs/"+r.id+"/arrived", nil), 200, "arrived")
	for _, who := range []*user{r.customer, op, r.owner} {
		must(t, who.do(http.MethodPost, "/bookings/"+r.id+"/inspection-acknowledgements", ack(who == r.customer)), 201, "acknowledge handover inspection")
	}
	r.customer.ackInspection(t, r.id, "handover")
	must(t, r.customer.do(http.MethodPost, "/bookings/"+r.id+"/handover/confirm", nil), 200, "rental starts")
	must(t, r.customer.do(http.MethodPost, "/bookings/"+r.id+"/return-request", nil), 200, "return")
	op.checklist(t, r.id, "return", 2210)
	op.ackInspection(t, r.id, "return")
	must(t, op.do(http.MethodPost, "/operator/jobs/"+r.id+"/returned", nil), 200, "returned")
	r.owner.e.exec(`UPDATE bookings SET final_total_paise = GREATEST(COALESCE((SELECT sum(p.amount_paise) FROM payments p WHERE p.booking_id = bookings.id AND p.status = 'captured'), 0) - deposit_paise, 0), final_charges = '{}'::jsonb, finalized_at = now() WHERE id = $1 AND finalized_at IS NULL`, r.id)
	must(t, r.owner.do(http.MethodPost, "/owner/bookings/"+r.id+"/complete", nil), 200, "complete")
	e.drain(t)
	custTypes, cn = noteTypes(t, r.customer)
	ownerTypes, on = noteTypes(t, r.owner)
	// The customer started the rental (handover) and the owner completed it,
	// so each hears about the other's step.
	if countOf(ownerTypes, "booking.active") != 1 || countOf(custTypes, "booking.completed") != 1 || countOf(ownerTypes, "booking.completed") != 0 {
		t.Fatalf("start/completion: customer %v owner %v", custTypes, ownerTypes)
	}
	if !strings.Contains(on.raw, "Rental started") || !strings.Contains(cn.raw, "Rate the machine") {
		t.Fatalf("wording: customer %s / owner %s", cn.raw, on.raw)
	}
}

func TestNotificationsSettlementAndMaintenanceReminders(t *testing.T) {
	e := newIsolatedEnv(t)
	r := newRental(t, e, "offline")
	r.accept(t)
	// Run the rental in the past so final charges can be settled.
	for _, st := range []string{"dispatched", "on_site", "active"} {
		e.exec(`UPDATE bookings SET status = $2 WHERE id = $1`, r.id, st)
	}
	past := time.Now().Add(-100 * time.Hour).UTC().Truncate(time.Hour)
	e.exec(`UPDATE bookings SET period = tstzrange($2, $3) WHERE id = $1`, r.id, past, past.Add(72*time.Hour))
	must(t, r.owner.do(http.MethodPost, "/owner/bookings/"+r.id+"/final-charges", map[string]any{
		"actual_start": past, "actual_end": past.Add(72 * time.Hour), "operating_hours": 20,
	}), 200, "settle final charges")
	e.drain(t)
	types, cn := noteTypes(t, r.customer)
	if countOf(types, "booking.final_charges") != 1 || !strings.Contains(cn.raw, "Final bill ready") || !strings.Contains(cn.raw, "₹") {
		t.Fatalf("settlement notification: %s", cn.raw)
	}

	// Maintenance reminders: a job tomorrow is announced once a day.
	tomorrow := time.Now().In(time.FixedZone("IST", 19800)).AddDate(0, 0, 1).Format(time.DateOnly)
	must(t, r.owner.do(http.MethodPost, "/owner/equipment/"+r.eq+"/maintenance/records", map[string]any{
		"task": "500 hour service", "status": "scheduled", "scheduled_for": tomorrow,
	}), 201, "schedule")
	e.runJob(t, "maintenance.reminders")
	e.runJob(t, "maintenance.reminders") // same day: no repeat
	e.drain(t)
	types, on := noteTypes(t, r.owner)
	if countOf(types, "maintenance.reminder") != 1 || !strings.Contains(on.raw, "500 hour service is scheduled tomorrow") {
		t.Fatalf("reminders: %v %s", types, on.raw)
	}
	// Notifications deep-link to the machine.
	if !strings.Contains(on.raw, `"equipment_id":"`+r.eq+`"`) {
		t.Fatalf("reminder data: %s", on.raw)
	}
	unread := must(t, r.owner.do(http.MethodGet, "/notifications/unread-count", nil), 200, "unread")
	first := on.list("data")[0].(map[string]any)["id"].(string)
	must(t, r.owner.do(http.MethodPost, "/notifications/"+first+"/read", nil), 204, "read one")
	if again := must(t, r.owner.do(http.MethodGet, "/notifications/unread-count", nil), 200, "unread"); again.num("unread") != unread.num("unread")-1 {
		t.Fatalf("unread %v → %v", unread.num("unread"), again.num("unread"))
	}
	must(t, r.customer.do(http.MethodPost, "/notifications/"+first+"/read", nil), 404, "someone else's notification")
}

// ---- digital agreements ------------------------------------------------------------------

func TestAgreementsModule(t *testing.T) {
	e := newEnv(t)
	r := newRental(t, e, "offline")
	must(t, r.customer.do(http.MethodGet, "/bookings/"+r.id+"/agreement", nil), 409, "before the owner accepts")
	r.accept(t)

	ag := must(t, r.customer.do(http.MethodGet, "/bookings/"+r.id+"/agreement", nil), 200, "agreement")
	if ag.str("template_version") != "rental-v3" || ag.str("status") != "awaiting_signatures" || ag.str("your_party") != "customer" ||
		ag.body["can_sign"] != true || ag.str("signature_provider") != "equiprent" || ag.str("owner.legal_name") == "" {
		t.Fatalf("agreement = %s", ag.raw)
	}
	terms := ag.str("terms")
	for _, want := range []string{r.ref, "Owner (lessor):", "Customer (lessee):", "Machine: Liugong 835H loader", "Make and model:",
		"Registration number: MH14", "Start: ", "IST", "Estimated total including taxes: ₹", "Refundable security deposit",
		"8. CANCELLATION POLICY", "9. DAMAGE RESPONSIBILITY", "TERMS AND CONDITIONS", "7. EQUIPMENT CONDITION", "Duration: 3 days", "Site levelling", "Hinjewadi site"} {
		if !strings.Contains(terms, want) {
			t.Errorf("terms missing %q", want)
		}
	}
	hash := ag.str("terms_sha256")
	if again := must(t, r.owner.do(http.MethodGet, "/bookings/"+r.id+"/agreement", nil), 200, "owner view"); again.str("terms_sha256") != hash || again.str("your_party") != "owner" {
		t.Fatalf("owner sees a different agreement: %s", again.raw)
	}
	outsider := e.registerAndVerify("customer", "")
	must(t, outsider.do(http.MethodGet, "/bookings/"+r.id+"/agreement", nil), 404, "outsider")

	// Draft PDF before signing.
	draft := must(t, r.customer.do(http.MethodGet, "/bookings/"+r.id+"/agreement/document", nil), 200, "draft link")
	if draft.str("kind") != "draft" || !strings.HasSuffix(draft.str("file_name"), "-draft.pdf") {
		t.Fatalf("draft = %s", draft.raw)
	}
	pdf := e.fetch(draft.str("url"))
	if pdf.Code != 200 || !bytes.HasPrefix(pdf.Body.Bytes(), []byte("%PDF-")) || !bytes.Contains(pdf.Body.Bytes(), []byte("(DRAFT)")) ||
		pdf.Header().Get("Content-Type") != "application/pdf" || !strings.Contains(pdf.Header().Get("Content-Disposition"), "agreement-"+r.ref) {
		t.Fatalf("draft pdf: %d %v", pdf.Code, pdf.Header())
	}
	if tampered := e.fetch(strings.Replace(draft.str("url"), "sig=", "sig=00", 1)); tampered.Code != 403 {
		t.Fatalf("tampered link: %d", tampered.Code)
	}

	// Signing rules.
	sign := func(u *user, body map[string]any) resp {
		return u.do(http.MethodPost, "/bookings/"+r.id+"/agreement/sign", body)
	}
	must(t, sign(r.customer, map[string]any{"terms_sha256": strings.Repeat("0", 64), "signer_name": "Asha Rao", "consent": true}), 409, "wrong hash")
	must(t, sign(r.customer, map[string]any{"terms_sha256": hash, "signer_name": "Asha Rao", "consent": false}), 400, "no consent")
	must(t, sign(r.customer, map[string]any{"terms_sha256": hash, "signer_name": "A", "consent": true}), 400, "name too short")
	signed := must(t, sign(r.customer, map[string]any{"terms_sha256": hash, "signer_name": "  Asha   Rao ", "designation": "Director", "consent": true}), 200, "customer signs")
	if signed.str("agreement.status") != "partially_signed" || signed.body["agreement"].(map[string]any)["can_sign"] != false ||
		signed.str("agreement.signatures.0.signer_name") != "Asha Rao" || signed.str("agreement.signatures.0.method") != "typed_name" {
		t.Fatalf("after customer signature = %s", signed.raw)
	}
	must(t, sign(r.customer, map[string]any{"terms_sha256": hash, "signer_name": "Asha Rao", "consent": true}), 409, "sign twice")
	if strings.Contains(signed.raw, `"ip"`) || strings.Contains(signed.raw, "user_agent") {
		t.Fatal("signature metadata must not be exposed in the API")
	}

	// Fleet managers and operators cannot sign for the owner.
	fmPhone := randomPhone()
	must(t, r.owner.do(http.MethodPost, "/orgs/"+r.owner.org+"/members", map[string]any{"phone": fmPhone, "role": "fleet_manager", "full_name": "Fleet Mgr"}), 201, "add fleet manager")
	fm := e.login(fmPhone)
	fm.org = r.owner.org
	// A fleet manager without the machine in scope learns nothing about it.
	must(t, fm.do(http.MethodGet, "/bookings/"+r.id+"/agreement", nil), 404, "fleet manager outside their machines")
	fl := must(t, r.owner.do(http.MethodPost, "/owner/fleets", map[string]any{"name": "Agreement fleet " + randomLetters(4)}), 201, "fleet").str("id")
	must(t, r.owner.do(http.MethodPost, "/owner/fleets/"+fl+"/equipment", map[string]any{"equipment_ids": []string{r.eq}}), 200, "fleet machine")
	must(t, r.owner.do(http.MethodPost, "/owner/fleets/"+fl+"/managers", map[string]any{"user_id": fm.id}), 200, "fleet manager")
	if v := must(t, fm.do(http.MethodGet, "/bookings/"+r.id+"/agreement", nil), 200, "fleet manager reads"); v.body["can_sign"] != false {
		t.Fatalf("fleet manager can_sign: %s", v.raw)
	}
	must(t, sign(fm, map[string]any{"terms_sha256": hash, "signer_name": "Fleet Mgr", "consent": true}), 403, "fleet manager signs")

	done := must(t, sign(r.owner, map[string]any{"terms_sha256": hash, "signer_name": "Ravi Patil", "consent": true}), 200, "owner signs")
	if done.str("agreement.status") != "executed" || done.body["agreement"].(map[string]any)["fully_accepted"] != true || len(done.list("agreement.signatures")) != 2 {
		t.Fatalf("executed = %s", done.raw)
	}
	// The executed PDF is stored at once and served through a signed link.
	if !strings.Contains(must(t, r.customer.do(http.MethodGet, "/bookings/"+r.id+"/agreement", nil), 200, "after").raw, `"kind":"executed"`) {
		t.Fatal("executed document not stored on execution")
	}
	link := must(t, r.customer.do(http.MethodGet, "/bookings/"+r.id+"/agreement/document", nil), 200, "executed link")
	exec := e.fetch(link.str("url"))
	if link.str("kind") != "executed" || exec.Code != 200 || !bytes.Contains(exec.Body.Bytes(), []byte("ACCEPTANCE RECORD")) ||
		!bytes.Contains(exec.Body.Bytes(), []byte("Asha Rao, Director")) || bytes.Contains(exec.Body.Bytes(), []byte("(DRAFT)")) {
		t.Fatalf("executed pdf: %s / %d", link.raw, exec.Code)
	}
	if again := must(t, r.owner.do(http.MethodGet, "/bookings/"+r.id+"/agreement/document", nil), 200, "again"); again.str("sha256") != link.str("sha256") {
		t.Fatal("the executed PDF must be generated once")
	}

	// The database keeps signatures and signed agreements immutable, and audits downloads.
	if _, err := e.pool.Exec(context.Background(), `UPDATE agreement_signatures SET signer_name = 'X' WHERE agreement_id = $1`, ag.str("id")); err == nil {
		t.Fatal("signatures must be append-only")
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE rental_agreements SET snapshot = '{}' WHERE id = $1`, ag.str("id")); err == nil {
		t.Fatal("a signed agreement must be frozen")
	}
	var audits int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action = 'document.url_issued' AND entity_id = $1`, ag.str("id")).Scan(&audits)
	if audits < 3 {
		t.Fatalf("document links audited %d times", audits)
	}
	// Both parties were told.
	e.drain(t)
	if types, _ := noteTypes(t, r.owner); countOf(types, "agreement.signed") != 1 || countOf(types, "agreement.executed") != 1 {
		t.Fatalf("owner agreement notifications = %v", types)
	}
}

func TestAgreementSandboxProvider(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.ESign.Provider = "sandbox" })
	r := newRental(t, e, "offline")
	r.accept(t)
	ag := must(t, r.customer.do(http.MethodGet, "/bookings/"+r.id+"/agreement", nil), 200, "agreement")
	if ag.str("signature_provider") != "sandbox" {
		t.Fatalf("provider = %s", ag.raw)
	}
	res := must(t, r.customer.do(http.MethodPost, "/bookings/"+r.id+"/agreement/sign", map[string]any{
		"terms_sha256": ag.str("terms_sha256"), "signer_name": "Asha Rao", "consent": true,
	}), 200, "begin signing")
	if !strings.HasPrefix(res.str("redirect.url"), "https://") || res.str("sandbox.signature") == "" || res.str("agreement.status") != "awaiting_signatures" {
		t.Fatalf("begin = %s", res.raw)
	}
	body := []byte(res.raw[strings.Index(res.raw, `"body":`)+7:])
	body = body[:bytes.IndexByte(body, '}')+1]
	hook := func(sig string) resp {
		return e.call(http.MethodPost, "/webhooks/esign/sandbox", "", "", body, map[string]string{"X-ESign-Signature": sig})
	}
	must(t, hook(strings.Repeat("a", 64)), 401, "forged webhook")
	must(t, e.call(http.MethodPost, "/webhooks/esign/docusign", "", "", body, nil), 404, "unknown provider")
	if ack := must(t, hook(res.str("sandbox.signature")), 200, "webhook"); ack.str("status") != "processed" {
		t.Fatalf("ack = %s", ack.raw)
	}
	must(t, hook(res.str("sandbox.signature")), 200, "redelivery")
	after := must(t, r.customer.do(http.MethodGet, "/bookings/"+r.id+"/agreement", nil), 200, "after webhook")
	if after.str("status") != "partially_signed" || len(after.list("signatures")) != 1 || after.str("signatures.0.method") != "provider" ||
		after.str("signatures.0.provider") != "sandbox" {
		t.Fatalf("after webhook = %s", after.raw)
	}
}

// ---- chat ----------------------------------------------------------------------------------

func TestChatModule(t *testing.T) {
	e := newEnv(t)
	r := newRental(t, e, "offline")
	r.accept(t)
	conv := must(t, r.customer.do(http.MethodPost, "/bookings/"+r.id+"/conversation", nil), 200, "open chat")
	cid := conv.str("id")
	if conv.str("booking_reference") != r.ref || conv.str("kind") != "booking" {
		t.Fatalf("conversation = %s", conv.raw)
	}
	msgs := "/conversations/" + cid + "/messages"

	// Text, idempotent resend.
	clientID := "0193c0de-0000-7000-8000-" + fmt.Sprintf("%012d", rand.Int64N(1e12))
	m1 := must(t, r.customer.do(http.MethodPost, msgs, map[string]any{"body": "  Can you deliver by 8 am? ", "client_id": clientID}), 201, "send")
	m1b := must(t, r.customer.do(http.MethodPost, msgs, map[string]any{"body": "Can you deliver by 8 am?", "client_id": clientID}), 201, "resend")
	if m1.str("id") != m1b.str("id") || m1.str("body") != "Can you deliver by 8 am?" || m1.body["mine"] != true {
		t.Fatalf("resend: %s / %s", m1.raw, m1b.raw)
	}
	must(t, r.customer.do(http.MethodPost, msgs, map[string]any{"body": "   "}), 400, "empty")

	// Owner side sees it, replies with a photo.
	hist := must(t, r.owner.do(http.MethodGet, msgs, nil), 200, "owner history")
	if len(hist.list("data")) != 1 || hist.str("data.0.mine") != "false" || hist.str("data.0.sender_name") == "" {
		t.Fatalf("history = %s", hist.raw)
	}
	photo := r.owner.uploadID("chat_image", "site.png", samplePNG(t))
	must(t, r.customer.do(http.MethodPost, msgs, map[string]any{"attachment_upload_id": photo}), 400, "someone else's upload")
	must(t, r.owner.do(http.MethodPost, msgs, map[string]any{"attachment_upload_id": r.owner.uploadID("avatar", "me.png", samplePNG(t))}), 400, "wrong purpose")
	m2 := must(t, r.owner.do(http.MethodPost, msgs, map[string]any{"body": "Yes, see the route", "attachment_upload_id": photo}), 201, "photo")
	img := e.fetch(m2.str("attachment.url"))
	if img.Code != 200 || img.Header().Get("Content-Type") != "image/jpeg" || img.Header().Get("Content-Disposition") != "" {
		t.Fatalf("attachment fetch: %d %v", img.Code, img.Header())
	}

	// Polling: only messages after the last one seen, oldest first.
	m3 := must(t, r.customer.do(http.MethodPost, msgs, map[string]any{"body": "Thanks!"}), 201, "send 3")
	poll := must(t, r.customer.do(http.MethodGet, msgs+"?after="+m1.str("id"), nil), 200, "poll")
	if len(poll.list("data")) != 2 || poll.str("data.0.id") != m2.str("id") || poll.str("data.1.id") != m3.str("id") ||
		poll.str("data.0.attachment.content_type") != "image/jpeg" {
		t.Fatalf("poll = %s", poll.raw)
	}
	older := must(t, r.customer.do(http.MethodGet, msgs+"?before="+m3.str("id")+"&limit=1", nil), 200, "older")
	if len(older.list("data")) != 1 || older.str("data.0.id") != m2.str("id") {
		t.Fatalf("history page = %s", older.raw)
	}
	must(t, r.customer.do(http.MethodGet, msgs+"?before="+m3.str("id")+"&after="+m1.str("id"), nil), 400, "both cursors")

	// Unread counts and read receipts.
	list := must(t, r.owner.do(http.MethodGet, "/conversations", nil), 200, "owner conversations")
	if !strings.Contains(list.raw, `"unread":2`) || !strings.Contains(list.raw, `"booking_reference":"`+r.ref+`"`) {
		t.Fatalf("owner list = %s", list.raw)
	}
	if u := must(t, r.owner.do(http.MethodGet, "/conversations/unread-count", nil), 200, "unread"); u.num("unread") != 2 {
		t.Fatalf("unread = %s", u.raw)
	}
	must(t, r.owner.do(http.MethodPost, "/conversations/"+cid+"/read", nil), 204, "read")
	if u := must(t, r.owner.do(http.MethodGet, "/conversations/unread-count", nil), 200, "unread"); u.num("unread") != 0 {
		t.Fatalf("unread after read = %s", u.raw)
	}

	// Deleting: own messages only.
	must(t, r.owner.do(http.MethodDelete, msgs+"/"+m3.str("id"), nil), 422, "delete someone else's")
	must(t, r.customer.do(http.MethodDelete, msgs+"/"+m3.str("id"), nil), 204, "delete own")
	gone := must(t, r.owner.do(http.MethodGet, msgs+"?after="+m2.str("id"), nil), 200, "after delete")
	if gone.body["data"].([]any)[0].(map[string]any)["deleted"] != true || strings.Contains(gone.raw, "Thanks!") {
		t.Fatalf("deleted message = %s", gone.raw)
	}

	// Secure access: outsiders cannot read, post or learn the conversation exists.
	outsider := e.registerAndVerify("customer", "")
	must(t, outsider.do(http.MethodGet, msgs, nil), 404, "outsider reads")
	must(t, outsider.do(http.MethodPost, msgs, map[string]any{"body": "hi"}), 404, "outsider posts")
	must(t, outsider.do(http.MethodGet, "/conversations/"+cid, nil), 404, "outsider opens")
	must(t, outsider.do(http.MethodPost, "/bookings/"+r.id+"/conversation", nil), 404, "outsider opens booking chat")

	// New messages are announced by push (outbox), not stored as in-app notifications.
	var events int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM outbox_events WHERE event_type = 'chat.message' AND aggregate_id = $1`, cid).Scan(&events)
	if events != 3 { // m1 (once despite the resend), m2, m3
		t.Fatalf("chat.message events = %d", events)
	}

	// Listing enquiry before any booking.
	buyer := e.registerAndVerify("customer", "")
	enq := must(t, buyer.do(http.MethodPost, "/equipment/"+r.eq+"/enquiry", map[string]any{"message": "Is it free next week?"}), 200, "enquiry")
	if enq.str("kind") != "direct" || enq.str("equipment_id") != r.eq {
		t.Fatalf("enquiry = %s", enq.raw)
	}
	again := must(t, buyer.do(http.MethodPost, "/equipment/"+r.eq+"/enquiry", nil), 200, "enquiry again")
	if again.str("id") != enq.str("id") {
		t.Fatal("one enquiry per customer and machine")
	}
	ownerList := must(t, r.owner.do(http.MethodGet, "/conversations", nil), 200, "owner sees enquiry")
	if !strings.Contains(ownerList.raw, enq.str("id")) || !strings.Contains(ownerList.raw, "Is it free next week?") {
		t.Fatalf("owner conversations = %s", ownerList.raw)
	}
	must(t, r.owner.do(http.MethodPost, "/conversations/"+enq.str("id")+"/messages", map[string]any{"body": "Yes, from Monday."}), 201, "owner replies")
	must(t, outsider.do(http.MethodGet, "/conversations/"+enq.str("id")+"/messages", nil), 404, "outsider reads enquiry")
	must(t, r.customer.do(http.MethodGet, "/conversations/"+enq.str("id")+"/messages", nil), 404, "another customer reads enquiry")
	must(t, r.owner.do(http.MethodPost, "/equipment/"+r.eq+"/enquiry", nil), 422, "owner enquires about their own machine")
}

// ---- maintenance ---------------------------------------------------------------------------

func TestMaintenanceModule(t *testing.T) {
	e := newEnv(t)
	r := newRental(t, e, "offline")
	r.accept(t) // holds r.start .. r.end
	base := "/owner/equipment/" + r.eq + "/maintenance"

	plan := must(t, r.owner.do(http.MethodPost, base+"/plans", map[string]any{"task": "Engine oil and filters", "every_hours": 250, "every_days": 90}), 201, "plan")
	must(t, r.owner.do(http.MethodPatch, base+"/plans/"+plan.str("id"), map[string]any{"every_hours": 0, "every_days": 0}), 400, "plan without interval")
	if p := must(t, r.owner.do(http.MethodPatch, base+"/plans/"+plan.str("id"), map[string]any{"every_hours": 300}), 200, "edit plan"); p.num("every_hours") != 300 {
		t.Fatalf("plan = %s", p.raw)
	}

	// Scheduling a downtime over a held booking is refused with the reference.
	clash := must(t, r.owner.do(http.MethodPost, base+"/records", map[string]any{
		"task": "Hydraulic hose replacement", "kind": "repair", "status": "scheduled",
		"downtime": map[string]any{"start": r.start.Add(24 * time.Hour), "end": r.start.Add(30 * time.Hour)},
	}), 409, "downtime over a booking")
	if !strings.Contains(clash.raw, r.ref) {
		t.Fatalf("clash = %s", clash.raw)
	}

	// Downtime after the booking blocks the calendar.
	dStart := r.end.Add(24 * time.Hour)
	job := must(t, r.owner.do(http.MethodPost, base+"/records", map[string]any{
		"plan_id": plan.str("id"), "task": "250 h service", "kind": "service", "status": "scheduled",
		"downtime": map[string]any{"start": dStart, "end": dStart.Add(48 * time.Hour)}, "vendor": "Liugong service centre",
	}), 201, "schedule service")
	if job.body["blocks_calendar"] != true || job.str("scheduled_for") == "" || job.str("kind") != "service" {
		t.Fatalf("job = %s", job.raw)
	}
	avail := must(t, e.call(http.MethodGet, fmt.Sprintf("/equipment/%s/availability?from=%s&to=%s", r.eq,
		dStart.Add(-time.Hour).Format(time.RFC3339), dStart.Add(72*time.Hour).Format(time.RFC3339)), "", "", nil, nil), 200, "availability")
	if !strings.Contains(avail.raw, "blocked") {
		t.Fatalf("availability = %s", avail.raw)
	}
	cust2 := e.registerAndVerify("customer", "")
	addr := must(t, cust2.do(http.MethodPost, "/orgs/"+cust2.org+"/addresses", map[string]any{
		"label": "Site", "line1": "Plot 9", "city": "Pune", "state_code": "27", "pincode": "411057", "is_site": true, "location": site,
	}), 201, "site")
	bk := cust2.do(http.MethodPost, "/bookings", map[string]any{
		"equipment_id": r.eq, "site_address_id": addr.str("id"), "period": map[string]any{"start": dStart, "end": dStart.Add(24 * time.Hour)},
		"pricing_unit": "day", "with_operator": true, "payment_mode": "offline",
	}, "Idempotency-Key", fmt.Sprintf("mnt-%d", rand.Int64()))
	if bk.code != 409 && bk.code != 422 {
		t.Fatalf("booking during maintenance: %d %s", bk.code, bk.raw)
	}

	// Rescheduling moves the block; cancelling frees it.
	moved := dStart.Add(96 * time.Hour)
	rec := must(t, r.owner.do(http.MethodPatch, base+"/records/"+job.str("id"), map[string]any{
		"status": "scheduled", "downtime": map[string]any{"start": moved, "end": moved.Add(24 * time.Hour)},
	}), 200, "reschedule")
	if rec.str("downtime.start") != moved.Format(time.RFC3339) || rec.body["blocks_calendar"] != true {
		t.Fatalf("rescheduled = %s", rec.raw)
	}
	var blocks int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM availability_blocks WHERE equipment_id = $1 AND reason = 'maintenance'`, r.eq).Scan(&blocks)
	if blocks != 1 {
		t.Fatalf("blocks after reschedule = %d", blocks)
	}
	// Starting and finishing: the plan resets; the remaining block is freed.
	must(t, r.owner.do(http.MethodPatch, base+"/records/"+job.str("id"), map[string]any{"status": "in_progress"}), 200, "start")
	done := must(t, r.owner.do(http.MethodPatch, base+"/records/"+job.str("id"), map[string]any{
		"status": "done", "parts_cost_paise": 450000, "labour_cost_paise": 150000, "hmr_at": 2400,
	}), 200, "complete")
	if done.num("cost_paise") != 600000 || done.str("performed_at") == "" {
		t.Fatalf("done = %s", done.raw)
	}
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM availability_blocks WHERE equipment_id = $1 AND reason = 'maintenance'`, r.eq).Scan(&blocks)
	if blocks != 0 {
		t.Fatalf("a job finished before its window must free the calendar (blocks = %d)", blocks)
	}
	must(t, r.owner.do(http.MethodPatch, base+"/records/"+job.str("id"), map[string]any{"status": "cancelled"}), 409, "closed job")
	plans := must(t, r.owner.do(http.MethodGet, base+"/plans", nil), 200, "plans")
	if plans.num("data.0.last_done_hmr") != 2400 {
		t.Fatalf("plan not reset: %s", plans.raw)
	}

	// Repair record with invoice, logged after the fact.
	inv := r.owner.uploadID("maintenance_document", "invoice.pdf", []byte("%PDF-1.4\n1 0 obj << >> endobj\ntrailer << >>\n%%EOF\n"))
	repair := must(t, r.owner.do(http.MethodPost, base+"/records", map[string]any{
		"task": "Starter motor rewinding", "kind": "repair", "status": "done", "cost_paise": 820000, "parts_cost_paise": 500000,
		"labour_cost_paise": 200000, "performed_at": time.Now().Add(-48 * time.Hour), "invoice_upload_id": inv,
		"downtime": map[string]any{"start": time.Now().Add(-60 * time.Hour), "end": time.Now().Add(-48 * time.Hour)},
	}), 201, "repair")
	if repair.body["has_invoice"] != true || repair.body["blocks_calendar"] != false {
		t.Fatalf("repair = %s", repair.raw)
	}
	must(t, r.owner.do(http.MethodPost, base+"/records", map[string]any{"task": "Bad costs", "status": "done", "cost_paise": 100, "parts_cost_paise": 500, "labour_cost_paise": 1}), 400, "split above total")
	link := must(t, r.owner.do(http.MethodGet, base+"/records/"+repair.str("id")+"/invoice", nil), 200, "invoice link")
	if f := e.fetch(link.str("url")); f.Code != 200 || f.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("invoice fetch %d", f.Code)
	}

	// History and costs.
	repairs := must(t, r.owner.do(http.MethodGet, "/owner/maintenance/records?kind=repair&equipment_id="+r.eq, nil), 200, "history")
	if len(repairs.list("data")) != 1 || repairs.str("data.0.equipment_title") == "" {
		t.Fatalf("history = %s", repairs.raw)
	}
	costs := must(t, r.owner.do(http.MethodGet, "/owner/maintenance/costs", nil), 200, "costs")
	if costs.num("total_paise") != 1420000 || costs.num("jobs") != 2 || len(costs.list("by_kind")) != 2 || costs.num("downtime_hours") < 12 {
		t.Fatalf("costs = %s", costs.raw)
	}
	must(t, r.owner.do(http.MethodGet, "/owner/maintenance/costs?from=2026-12-01&to=2026-01-01", nil), 400, "inverted range")

	// Overdue jobs are announced once.
	e.exec(`INSERT INTO maintenance_records (equipment_id, task, status, scheduled_for) VALUES ($1, 'Track tension check', 'scheduled', current_date - 3)`, r.eq)
	e.runJob(t, "maintenance.reminders")
	e.runJob(t, "maintenance.reminders")
	var overdue int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM outbox_events WHERE event_type = 'maintenance.overdue' AND aggregate_id = $1`, r.eq).Scan(&overdue)
	if overdue != 1 {
		t.Fatalf("overdue events = %d", overdue)
	}

	// Other organisations cannot see or touch the machine's maintenance.
	other := e.verifiedOwner(r.kyc, "Other Rentals "+randomLetters(3))
	must(t, other.do(http.MethodGet, base+"/records", nil), 404, "other owner")
	must(t, r.customer.do(http.MethodGet, "/owner/maintenance/records", nil), 403, "customer")
}

// ---- reviews -------------------------------------------------------------------------------

func TestReviewsModule(t *testing.T) {
	e := newEnv(t)
	r := newRental(t, e, "offline")
	r.accept(t)
	must(t, r.customer.do(http.MethodPost, "/bookings/"+r.id+"/reviews", map[string]any{"subject": "equipment", "rating": 5}), 422, "before completion")
	r.finish(t, "confirmed")

	open := must(t, r.customer.do(http.MethodGet, "/bookings/"+r.id+"/reviews", nil), 200, "booking reviews")
	if open.body["review_open"] != true || !strings.Contains(open.raw, `"can_review":["equipment","owner"]`) {
		t.Fatalf("booking reviews = %s", open.raw)
	}
	eqRev := must(t, r.customer.do(http.MethodPost, "/bookings/"+r.id+"/reviews", map[string]any{
		"subject": "equipment", "rating": 4, "comment": "Strong loader, a little slow to start.",
	}), 201, "equipment rating")
	if eqRev.str("status") != "published" {
		t.Fatalf("review = %s", eqRev.raw)
	}
	held := must(t, r.customer.do(http.MethodPost, "/bookings/"+r.id+"/reviews", map[string]any{
		"subject": "owner", "rating": 5, "comment": "Very helpful, call Ravi on 98765 43210 directly next time",
	}), 201, "owner service rating (held)")
	if held.str("status") != "pending" {
		t.Fatalf("screened review = %s", held.raw)
	}
	must(t, r.owner.do(http.MethodPost, "/bookings/"+r.id+"/reviews", map[string]any{"subject": "customer", "rating": 5, "comment": "Paid on time"}), 201, "owner rates customer")

	pub := must(t, e.call(http.MethodGet, "/equipment/"+r.eq+"/reviews", "", "", nil, nil), 200, "public equipment reviews")
	if len(pub.list("data")) != 1 || pub.num("summary.count") != 1 || pub.num("summary.average") != 4 || pub.num("summary.histogram.4") != 1 {
		t.Fatalf("equipment reviews = %s", pub.raw)
	}
	if l := must(t, r.customer.do(http.MethodGet, "/equipment/"+r.eq, nil), 200, "listing"); l.num("rating_avg") != 4 || l.num("rating_count") != 1 {
		t.Fatalf("listing rating = %v/%v", l.num("rating_avg"), l.num("rating_count"))
	}
	ownerPub := must(t, e.call(http.MethodGet, "/owners/"+r.owner.org+"/reviews", "", "", nil, nil), 200, "owner reviews")
	if len(ownerPub.list("data")) != 0 {
		t.Fatalf("a held review must not be public: %s", ownerPub.raw)
	}
	mine := must(t, r.customer.do(http.MethodGet, "/bookings/"+r.id+"/reviews", nil), 200, "my booking reviews")
	if !strings.Contains(mine.raw, `"status":"pending"`) || !strings.Contains(mine.raw, `"can_review":[]`) {
		t.Fatalf("customer's booking reviews = %s", mine.raw)
	}

	// Owner reply (once, screened).
	must(t, r.customer.do(http.MethodPost, "/reviews/"+eqRev.str("id")+"/reply", map[string]any{"reply": "me too"}), 403, "reviewer replies")
	must(t, r.owner.do(http.MethodPost, "/reviews/"+eqRev.str("id")+"/reply", map[string]any{"reply": "Visit www.example.in"}), 400, "screened reply")
	must(t, r.owner.do(http.MethodPost, "/reviews/"+eqRev.str("id")+"/reply", map[string]any{"reply": "Thanks! The starter is fixed now."}), 200, "reply")
	must(t, r.owner.do(http.MethodPost, "/reviews/"+eqRev.str("id")+"/reply", map[string]any{"reply": "Again"}), 409, "second reply")
	recv := must(t, r.owner.do(http.MethodGet, "/owner/reviews", nil), 200, "owner received")
	if !strings.Contains(recv.raw, "The starter is fixed now.") {
		t.Fatalf("owner received = %s", recv.raw)
	}

	// Reports: three hide the review until moderated, and the rating drops it.
	must(t, r.customer.do(http.MethodPost, "/reviews/"+eqRev.str("id")+"/report", map[string]any{"reason": "spam"}), 422, "report own review")
	for i := range 3 {
		u := e.registerAndVerify("customer", "")
		rep := must(t, u.do(http.MethodPost, "/reviews/"+eqRev.str("id")+"/report", map[string]any{"reason": "false_information"}), 200, "report")
		if rep.body["reported"] != true {
			t.Fatalf("report %d = %s", i, rep.raw)
		}
		if i == 0 {
			if again := must(t, u.do(http.MethodPost, "/reviews/"+eqRev.str("id")+"/report", map[string]any{"reason": "spam"}), 200, "report twice"); again.body["reported"] != false {
				t.Fatal("a second report by the same user must not count")
			}
		}
	}
	if l := must(t, r.customer.do(http.MethodGet, "/equipment/"+r.eq, nil), 200, "listing"); l.num("rating_count") != 0 {
		t.Fatalf("flagged review still counted: %v", l.num("rating_count"))
	}

	// Moderation queue.
	must(t, r.owner.do(http.MethodGet, "/admin/reviews", nil), 403, "non-admin")
	mod := e.opsAdmin()
	q := must(t, mod.do(http.MethodGet, "/admin/reviews?status=pending", nil), 200, "pending queue")
	if !strings.Contains(q.raw, held.str("id")) || !strings.Contains(q.raw, "contact_details") {
		t.Fatalf("pending queue = %s", q.raw)
	}
	fl := must(t, mod.do(http.MethodGet, "/admin/reviews?status=flagged&reported=true", nil), 200, "flagged queue")
	if !strings.Contains(fl.raw, eqRev.str("id")) || !strings.Contains(fl.raw, "false_information") {
		t.Fatalf("flagged queue = %s", fl.raw)
	}
	must(t, mod.do(http.MethodPost, "/admin/reviews/"+held.str("id")+"/moderate", map[string]any{"action": "publish", "reason": "Phone number is the owner's public business line"}), 403, "admins cannot moderate (read-only)")
}

// ---- all together ----------------------------------------------------------------------

// TestCommunicationAndOperationsTogether runs one rental through every
// module: notifications at each step, the signed agreement and its PDF, the
// booking chat, maintenance after the job, and reviews.
func TestCommunicationAndOperationsTogether(t *testing.T) {
	e := newIsolatedEnv(t)
	r := newRental(t, e, "online")
	must(t, r.customer.do(http.MethodPost, "/me/devices", map[string]any{"platform": "web", "push_token": "web-token-" + randomLetters(24)}), 200, "device")

	// Enquiry turned booking: the customer asked first.
	enq := must(t, r.customer.do(http.MethodPost, "/equipment/"+r.eq+"/enquiry", map[string]any{"message": "Do you deliver to Hinjewadi?"}), 200, "enquiry")
	must(t, r.owner.do(http.MethodPost, "/conversations/"+enq.str("id")+"/messages", map[string]any{"body": "Yes, booking accepted shortly."}), 201, "owner answers")

	r.accept(t)
	ag := must(t, r.customer.do(http.MethodGet, "/bookings/"+r.id+"/agreement", nil), 200, "agreement")
	for _, u := range []*user{r.customer, r.owner} {
		must(t, u.do(http.MethodPost, "/bookings/"+r.id+"/agreement/sign", map[string]any{"terms_sha256": ag.str("terms_sha256"), "signer_name": "Signer " + randomLetters(4), "consent": true}), 200, "sign")
	}
	r.pay(t)
	chat := must(t, r.customer.do(http.MethodPost, "/bookings/"+r.id+"/conversation", nil), 200, "chat")
	must(t, r.customer.do(http.MethodPost, "/conversations/"+chat.str("id")+"/messages", map[string]any{"body": "Gate 2, please."}), 201, "message")
	r.finish(t, "confirmed")
	must(t, r.customer.do(http.MethodPost, "/bookings/"+r.id+"/reviews", map[string]any{"subject": "equipment", "rating": 5, "comment": "Excellent"}), 201, "review")
	must(t, r.customer.do(http.MethodPost, "/bookings/"+r.id+"/reviews", map[string]any{"subject": "owner", "rating": 4}), 201, "review owner")
	tomorrow := time.Now().In(time.FixedZone("IST", 19800)).AddDate(0, 0, 1).Format(time.DateOnly)
	must(t, r.owner.do(http.MethodPost, "/owner/equipment/"+r.eq+"/maintenance/records", map[string]any{
		"task": "Post-rental inspection", "kind": "inspection", "status": "scheduled", "scheduled_for": tomorrow,
	}), 201, "inspection")
	e.runJob(t, "maintenance.reminders")
	e.drain(t)

	custTypes, _ := noteTypes(t, r.customer)
	ownerTypes, _ := noteTypes(t, r.owner)
	for _, want := range []string{"booking.accepted", "agreement.signed", "agreement.executed", "payment.captured", "booking.confirmed"} {
		if countOf(custTypes, want) == 0 {
			t.Errorf("customer did not get %s (got %v)", want, custTypes)
		}
	}
	for _, want := range []string{"booking.requested", "agreement.signed", "agreement.executed", "payment.captured", "review.received", "maintenance.reminder"} {
		if countOf(ownerTypes, want) == 0 {
			t.Errorf("owner did not get %s (got %v)", want, ownerTypes)
		}
	}
	if countOf(custTypes, "chat.message")+countOf(ownerTypes, "chat.message") != 0 {
		t.Error("chat messages are push-only")
	}
	// The executed agreement and the chat history stay available after the rental.
	if d := must(t, r.customer.do(http.MethodGet, "/bookings/"+r.id+"/agreement/document", nil), 200, "pdf"); d.str("kind") != "executed" {
		t.Fatalf("document = %s", d.raw)
	}
	if h := must(t, r.owner.do(http.MethodGet, "/conversations/"+chat.str("id")+"/messages", nil), 200, "history"); len(h.list("data")) != 1 {
		t.Fatalf("history = %s", h.raw)
	}
	if p := must(t, e.call(http.MethodGet, "/owners/"+r.owner.org+"/reviews", "", "", nil, nil), 200, "owner rating"); p.num("summary.average") != 4 {
		t.Fatalf("owner rating = %s", p.raw)
	}
}

// The notification set for a booking: submitted + request details, driver job, admin alerts.
func TestBookingNotificationRouting(t *testing.T) {
	e := newEnv(t)
	o := newOperatorRental(t, e)
	e.drain(t)
	custTypes, _ := noteTypes(t, o.customer)
	if countOf(custTypes, "booking.requested") < 1 {
		t.Fatalf("customer should get 'booking submitted': %v", custTypes)
	}
	body := e.query1(`SELECT body FROM notifications n JOIN users u ON u.id = n.user_id WHERE n.type = 'booking.requested' AND n.title = 'New booking request' ORDER BY n.created_at DESC LIMIT 1`)
	if !strings.Contains(body, "ER-") || strings.Contains(body, "{") {
		t.Fatalf("owner request note lacks booking details or has unfilled placeholders: %q", body)
	}
	must(t, o.owner.do(http.MethodPost, "/owner/bookings/"+o.id+"/assign-operator", map[string]any{"operator_user_id": o.op1.id}), 200, "assign")
	e.drain(t)
	opTypes, _ := noteTypes(t, o.op1)
	if countOf(opTypes, "booking.job_assigned") != 1 {
		t.Fatalf("driver notes = %v", opTypes)
	}
	if other, _ := noteTypes(t, o.op2); countOf(other, "booking.job_assigned") != 0 {
		t.Fatalf("a different driver was notified: %v", other)
	}
	// Replaying the same outbox event must not duplicate (unique per event and user).
	if n := e.query1(`SELECT count(*)::text FROM (SELECT event_id, user_id FROM notifications WHERE event_id IS NOT NULL GROUP BY 1, 2 HAVING count(*) > 1) d`); n != "0" {
		t.Fatalf("duplicate notifications per event: %s", n)
	}
	// Admin alerts for new owners and verification submissions.
	if n := e.query1(`SELECT count(*)::text FROM notifications WHERE type IN ('owner.registered', 'verification.submitted')`); n == "0" {
		t.Fatal("admins were not notified of owner registration / verification submission")
	}
}

func ack(customer bool) map[string]any {
	if customer {
		return map[string]any{"stage": "handover"}
	}
	return map[string]any{"stage": "handover"}
}
