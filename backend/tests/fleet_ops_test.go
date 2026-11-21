package tests

// Fleet and operator operations through the real router and database:
// fleets and the fleet-manager machine scope (equipment, bookings, fleet
// views, notifications, restrictions), operator assignment conflicts and
// reassignment, the job workspace and its status changes, handover/return
// evidence with photos, issue reports and the admin review.

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// jobPhoto uploads a job photograph and returns its id.
func (u *user) jobPhoto(t *testing.T) string {
	t.Helper()
	return u.uploadID("job_photo", "machine.jpg", photoJPEG(t, 640, 480))
}

// checklist submits a handover/return checklist with photos.
func (u *user) checklist(t *testing.T, bookingID, kind string, hmr float64) resp {
	t.Helper()
	return must(t, u.do(http.MethodPost, "/operator/jobs/"+bookingID+"/checklists", u.checklistBody(t, kind, hmr)), 201, kind+" checklist")
}

func (u *user) checklistBody(t *testing.T, kind string, hmr float64) map[string]any {
	t.Helper()
	return map[string]any{
		"kind": kind, "hour_meter": hmr, "hour_meter_photo_upload_id": u.jobPhoto(t),
		"photo_upload_ids": []string{u.jobPhoto(t), u.jobPhoto(t)}, "fuel_level_pct": 70,
		"items": map[string]bool{"tyres": true, "hydraulics": true, "lights": true}, "captured_at": time.Now().Add(-time.Minute).UTC(),
		"client_id": fmt.Sprintf("018f0000-0000-7000-8000-%012d", rand.Int64N(1e12)), "lat": site["lat"], "lng": site["lng"],
	}
}

// supportAdmin can review issues and restrict accounts.
func (e *env) supportAdmin() *user {
	e.t.Helper()
	a := e.login(randomPhone())
	e.exec(`INSERT INTO user_roles (user_id, role_code) VALUES ($1, 'admin_support')`, a.id)
	return a
}

// addMember adds a person to the owner's organisation with a role.
func (e *env) addMember(t *testing.T, owner *user, role, name string) *user {
	t.Helper()
	phone := randomPhone()
	must(t, owner.do(http.MethodPost, "/orgs/"+owner.org+"/members", map[string]any{"phone": phone, "role": role, "full_name": name}), 201, "add "+role)
	u := e.login(phone)
	e.acceptInvitations(u)
	u.org = owner.org
	return u
}

func equipmentIDs(r resp, field string) []string {
	var out []string
	for _, x := range r.list("data") {
		out = append(out, fmt.Sprint(x.(map[string]any)[field]))
	}
	return out
}

func TestFleetManagerMachineScope(t *testing.T) {
	e := newEnv(t)
	m := newMarket(t, e, nil) // owner with one published machine (m.eq)
	eqB := e.publish(m.owner, m.ops, listingBody(map[string]any{"title": "Second backhoe " + randomLetters(5)}))
	eqC := e.publish(m.owner, m.ops, listingBody(map[string]any{"title": "Other yard crane " + randomLetters(5)}))
	org := m.owner.org
	fm := e.addMember(t, m.owner, "fleet_manager", "Fleet Manager North")

	// Before any fleet the manager manages nothing.
	if ov := must(t, fm.do(http.MethodGet, "/fleet/"+org+"/overview", nil), 200, "overview"); ov.num("equipment_total") != 0 || ov.str("scope") != "assigned" {
		t.Fatalf("empty scope overview = %s", ov.raw)
	}
	// The owner creates a fleet with two machines and gives it to the manager.
	fleet := must(t, m.owner.do(http.MethodPost, "/owner/fleets", map[string]any{"name": "North yard " + randomLetters(3)}), 201, "create fleet")
	fid := fleet.str("id")
	must(t, m.owner.do(http.MethodPost, "/owner/fleets/"+fid+"/equipment", map[string]any{"equipment_ids": []string{m.eq, eqB}}), 200, "add machines")
	must(t, m.owner.do(http.MethodPost, "/owner/fleets/"+fid+"/managers", map[string]any{"user_id": fm.id}), 200, "add manager")
	must(t, m.owner.do(http.MethodPost, "/owner/fleets/"+fid+"/managers", map[string]any{"user_id": m.customer.id}), 400, "a non-fleet-manager")
	must(t, m.owner.do(http.MethodPost, "/owner/fleets", map[string]any{"name": fleet.str("name")}), 409, "duplicate fleet name")

	// Equipment: only the fleet's machines.
	mine := must(t, fm.do(http.MethodGet, "/owner/equipment", nil), 200, "fm listings")
	ids := equipmentIDs(mine, "id")
	if len(ids) != 2 || !slices.Contains(ids, m.eq) || !slices.Contains(ids, eqB) {
		t.Fatalf("fleet manager listings = %v", ids)
	}
	must(t, fm.do(http.MethodGet, "/owner/equipment/"+eqC, nil), 404, "machine outside the fleet")
	must(t, fm.do(http.MethodPatch, "/owner/equipment/"+eqC, map[string]any{"title": "Renamed by FM"}), 404, "edit outside the fleet")
	must(t, fm.do(http.MethodGet, "/owner/equipment/"+m.eq, nil), 200, "machine in the fleet")

	// Fleet views: board, overview, utilisation, schedule — scoped.
	board := must(t, fm.do(http.MethodGet, "/fleet/"+org+"/equipment", nil), 200, "board")
	if got := equipmentIDs(board, "equipment_id"); len(got) != 2 || slices.Contains(got, eqC) {
		t.Fatalf("board = %v", got)
	}
	if board.str("data.0.state") == "" || board.str("data.0.fleet_name") != fleet.str("name") {
		t.Fatalf("board row = %s", board.raw)
	}
	if ownerBoard := must(t, m.owner.do(http.MethodGet, "/fleet/"+org+"/equipment", nil), 200, "owner board"); len(ownerBoard.list("data")) != 3 {
		t.Fatalf("owner board = %d", len(ownerBoard.list("data")))
	}
	if ov := must(t, fm.do(http.MethodGet, "/fleet/"+org+"/overview", nil), 200, "overview"); ov.num("equipment_total") != 2 {
		t.Fatalf("fm overview = %s", ov.raw)
	}
	window := fmt.Sprintf("?from=%s&to=%s", time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339), time.Now().Add(30*24*time.Hour).UTC().Format(time.RFC3339))
	util := must(t, fm.do(http.MethodGet, "/fleet/"+org+"/reports/utilisation"+window, nil), 200, "utilisation")
	if len(util.list("data")) != 2 {
		t.Fatalf("utilisation rows = %s", util.raw)
	}
	must(t, m.customer.do(http.MethodGet, "/fleet/"+org+"/overview", nil), 404, "outsider fleet views")
	must(t, fm.do(http.MethodGet, "/owners/me/dashboard", nil), 403, "org-wide dashboard is for owners")

	// Bookings: one on a fleet machine, one on the machine outside it.
	inFleet := m.request(t, "offline")
	cust2, site2 := m.newCustomer(t)
	outside := must(t, cust2.do(http.MethodPost, "/bookings", map[string]any{
		"equipment_id": eqC, "site_address_id": site2, "period": map[string]any{"start": m.start, "end": m.end},
		"pricing_unit": "day", "with_operator": true, "work_description": "Lifting steel", "payment_mode": "offline",
	}, "Idempotency-Key", key("book")), 201, "booking outside the fleet").str("id")
	inbox := listIDs(must(t, fm.do(http.MethodGet, "/owner/bookings", nil), 200, "fm inbox"))
	if !slices.Contains(inbox, inFleet) || slices.Contains(inbox, outside) {
		t.Fatalf("fm inbox = %v (in %s, out %s)", inbox, inFleet, outside)
	}
	must(t, fm.do(http.MethodGet, "/bookings/"+outside, nil), 404, "booking outside the fleet")
	must(t, fm.do(http.MethodPost, "/owner/bookings/"+outside+"/accept", nil), 404, "accept outside the fleet")
	must(t, fm.do(http.MethodGet, "/bookings/"+outside+"/agreement", nil), 404, "agreement outside the fleet")
	must(t, fm.do(http.MethodPost, "/owner/bookings/"+inFleet+"/accept", nil), 200, "accept in the fleet")
	if ownerInbox := listIDs(must(t, m.owner.do(http.MethodGet, "/owner/bookings", nil), 200, "owner inbox")); !slices.Contains(ownerInbox, outside) {
		t.Fatal("the owner sees every booking")
	}

	// Notifications: the manager hears only about their machines.
	e.drain(t)
	_, notes := noteTypes(t, fm)
	if !strings.Contains(notes.raw, inFleet) || strings.Contains(notes.raw, outside) {
		t.Fatalf("fm notifications leak or miss: %s", notes.raw)
	}

	// A fleet manager creates a fleet only with machines they manage.
	must(t, fm.do(http.MethodPost, "/owner/fleets", map[string]any{"name": "FM yard " + randomLetters(3)}), 201, "fm creates a fleet")
	fmFleets := must(t, fm.do(http.MethodGet, "/fleet/"+org+"/fleets", nil), 200, "fm fleets")
	if len(fmFleets.list("data")) != 2 {
		t.Fatalf("fm fleets = %s", fmFleets.raw)
	}
	var own string
	for _, f := range fmFleets.list("data") {
		if fx := f.(map[string]any); fx["id"] != fid {
			own = fmt.Sprint(fx["id"])
		}
	}
	must(t, fm.do(http.MethodPost, "/owner/fleets/"+own+"/equipment", map[string]any{"equipment_ids": []string{eqC}}), 400, "fm adds a machine they do not manage")
	must(t, fm.do(http.MethodPost, "/owner/fleets/"+own+"/managers", map[string]any{"user_id": fm.id}), 403, "only owners assign managers")

	// Restriction: fleet management blocked → no machines, no new listings.
	support := e.supportAdmin()
	must(t, support.do(http.MethodPost, "/admin/users/"+fm.id+"/restrictions", map[string]any{"kind": "fleet_management", "reason": "Under investigation"}), 201, "support restricts")
	rst := e.query1(`SELECT id::text FROM account_restrictions WHERE user_id = $1 AND kind = 'fleet_management' AND lifted_at IS NULL`, fm.id)
	if ov := must(t, fm.do(http.MethodGet, "/fleet/"+org+"/overview", nil), 200, "restricted overview"); ov.num("equipment_total") != 0 {
		t.Fatalf("restricted overview = %s", ov.raw)
	}
	must(t, fm.do(http.MethodGet, "/bookings/"+inFleet, nil), 404, "restricted booking access")
	must(t, fm.do(http.MethodPost, "/owner/equipment", listingBody(nil)), 403, "restricted listing creation")
	must(t, support.do(http.MethodPost, "/admin/restrictions/"+rst+"/lift", map[string]any{"reason": "Cleared"}), 200, "support lifts")
	must(t, fm.do(http.MethodGet, "/bookings/"+inFleet, nil), 200, "access back after lifting")

	// Owner removes the manager from the fleet → access gone.
	must(t, m.owner.do(http.MethodDelete, "/owner/fleets/"+fid+"/managers/"+fm.id, nil), 200, "remove manager")
	must(t, fm.do(http.MethodGet, "/owner/equipment/"+m.eq, nil), 404, "access ends with the fleet")
	// Admins see fleet accounts and can archive them.
	ops := m.ops
	all := must(t, ops.do(http.MethodGet, "/admin/fleets?org_id="+org, nil), 200, "admin fleets")
	if len(all.list("data")) != 2 {
		t.Fatalf("admin fleets = %s", all.raw)
	}
	must(t, ops.do(http.MethodPost, "/admin/fleets/"+fid+"/archive", map[string]any{"reason": "Duplicate account"}), 200, "ops archives")
	must(t, m.customer.do(http.MethodGet, "/admin/fleets", nil), 403, "non-admin fleet accounts")
}

// operatorRental is an accepted, confirmed wet-hire rental with two
// verified operators in the owner's organisation.
type operatorRental struct {
	*rental
	op1, op2 *user
}

func newOperatorRental(t *testing.T, e *env) *operatorRental {
	t.Helper()
	r := newRental(t, e, "offline")
	r.accept(t)
	o := &operatorRental{rental: r}
	o.op1 = e.addMember(t, r.owner, "operator", "Suresh Kale")
	o.op2 = e.addMember(t, r.owner, "operator", "Imran Shaikh")
	e.verifyOperator(o.op1, r.kyc)
	e.verifyOperator(o.op2, r.kyc)
	o.op1.org, o.op2.org = "", ""
	return o
}

func TestOperatorAssignmentConflictsAndReassignment(t *testing.T) {
	e := newEnv(t)
	o := newOperatorRental(t, e)
	assign := func(u *user, bookingID, operatorID string) resp {
		return u.do(http.MethodPost, "/owner/bookings/"+bookingID+"/assign-operator", map[string]any{"operator_user_id": operatorID})
	}
	must(t, assign(o.owner, o.id, o.op1.id), 200, "assign op1")

	// A second, overlapping booking on another machine cannot take op1.
	eq2 := e.publish(o.owner, o.ops, listingBody(map[string]any{"title": "Overlap loader " + randomLetters(5)}))
	b2 := must(t, o.customer.do(http.MethodPost, "/bookings", map[string]any{
		"equipment_id": eq2, "site_address_id": o.siteID, "period": map[string]any{"start": o.start.Add(24 * time.Hour), "end": o.end.Add(24 * time.Hour)},
		"pricing_unit": "day", "with_operator": true, "work_description": "Overlapping job", "payment_mode": "offline",
	}, "Idempotency-Key", key("book")), 201, "overlapping booking").str("id")
	must(t, o.owner.do(http.MethodPost, "/owner/bookings/"+b2+"/accept", nil), 200, "accept b2")
	conflict := assign(o.owner, b2, o.op1.id)
	if conflict.code != 409 || !strings.Contains(conflict.raw, "overlaps") {
		t.Fatalf("overlapping assignment = %d %s", conflict.code, conflict.raw)
	}
	must(t, assign(o.owner, b2, o.op2.id), 200, "op2 takes the overlapping job")

	// A later, non-overlapping booking can take op1.
	b3 := must(t, o.customer.do(http.MethodPost, "/bookings", map[string]any{
		"equipment_id": eq2, "site_address_id": o.siteID, "period": map[string]any{"start": o.end.Add(10 * 24 * time.Hour), "end": o.end.Add(12 * 24 * time.Hour)},
		"pricing_unit": "day", "with_operator": true, "work_description": "Later job", "payment_mode": "offline",
	}, "Idempotency-Key", key("book")), 201, "later booking").str("id")
	must(t, o.owner.do(http.MethodPost, "/owner/bookings/"+b3+"/accept", nil), 200, "accept b3")
	must(t, assign(o.owner, b3, o.op1.id), 200, "non-overlapping assignment")

	// Reassignment: op1 is replaced by op2? op2 is busy on b2 (overlap) → 409;
	// a fresh operator takes it and op1's job moves to history.
	if r := assign(o.owner, o.id, o.op2.id); r.code != 409 {
		t.Fatalf("reassign to a busy operator = %d %s", r.code, r.raw)
	}
	op3 := e.addMember(t, o.owner, "operator", "Vijay More")
	op3.org = ""
	must(t, assign(o.owner, o.id, op3.id), 422, "unverified operator") // KYC required
	e.verifyOperator(op3, o.kyc)
	must(t, assign(o.owner, o.id, op3.id), 200, "reassign to op3")
	if st := must(t, o.owner.do(http.MethodGet, "/bookings/"+o.id, nil), 200, "after reassign").str("status"); st != "operator_assigned" {
		t.Fatalf("status after reassign = %s", st)
	}
	must(t, o.op1.do(http.MethodPost, "/operator/jobs/"+o.id+"/accept", nil), 404, "replaced operator accepts")
	hist := must(t, o.op1.do(http.MethodGet, "/operator/jobs?scope=history", nil), 200, "op1 history")
	found := false
	for _, j := range hist.list("data") {
		if jm := j.(map[string]any); jm["booking_id"] == o.id && jm["assignment_status"] == "replaced" {
			found = true
		}
	}
	if !found {
		t.Fatalf("op1 history = %s", hist.raw)
	}
	if cur := must(t, o.op1.do(http.MethodGet, "/operator/jobs?scope=current", nil), 200, "op1 current"); strings.Contains(cur.raw, o.id) {
		t.Fatal("a replaced job is not current")
	}
	if job := must(t, o.op1.do(http.MethodGet, "/operator/jobs/"+o.id, nil), 200, "replaced job detail"); job.body["current"] != false || job.str("customer.phone") != "<nil>" {
		t.Fatalf("replaced job detail = %s", job.raw)
	}

	// Restricted operators cannot be assigned.
	support := e.supportAdmin()
	must(t, support.do(http.MethodPost, "/admin/users/"+o.op1.id+"/restrictions", map[string]any{"kind": "operator_assignments", "reason": "Licence under review"}), 201, "support restricts")
	b4 := must(t, o.customer.do(http.MethodPost, "/bookings", map[string]any{
		"equipment_id": eq2, "site_address_id": o.siteID, "period": map[string]any{"start": o.end.Add(20 * 24 * time.Hour), "end": o.end.Add(21 * 24 * time.Hour)},
		"pricing_unit": "day", "with_operator": true, "work_description": "Restricted job", "payment_mode": "offline",
	}, "Idempotency-Key", key("book")), 201, "b4").str("id")
	must(t, o.owner.do(http.MethodPost, "/owner/bookings/"+b4+"/accept", nil), 200, "accept b4")
	if r := assign(o.owner, b4, o.op1.id); r.code != 422 || !strings.Contains(r.raw, "restricted") {
		t.Fatalf("restricted assignment = %d %s", r.code, r.raw)
	}
	roster := must(t, o.owner.do(http.MethodGet, "/fleet/"+o.owner.org+"/operators", nil), 200, "roster")
	if !strings.Contains(roster.raw, `"assignments_blocked":true`) {
		t.Fatalf("roster = %s", roster.raw)
	}
}

func TestOperatorJobStatusChangesAndEvidence(t *testing.T) {
	e := newEnv(t)
	o := newOperatorRental(t, e)
	must(t, o.owner.do(http.MethodPost, "/owner/bookings/"+o.id+"/assign-operator", map[string]any{"operator_user_id": o.op1.id}), 200, "assign")
	op := o.op1

	// Before accepting: details without phone numbers; next steps accept/decline.
	job := must(t, op.do(http.MethodGet, "/operator/jobs/"+o.id, nil), 200, "job")
	if job.str("customer.phone") != "<nil>" || !slices.Contains(listStrings(job, "next_steps"), "accept") ||
		job.str("equipment.registration_no") == "" || !strings.HasPrefix(job.str("navigation.url"), "https://www.google.com/maps/dir/") ||
		job.str("site.address") == "" || job.str("customer.name") == "" {
		t.Fatalf("job before accepting = %s", job.raw)
	}
	must(t, o.op2.do(http.MethodGet, "/operator/jobs/"+o.id, nil), 404, "another operator's job")
	must(t, o.customer.do(http.MethodGet, "/operator/jobs/"+o.id, nil), 404, "customer on the operator endpoint")
	op.checklist(t, o.id, "handover", 2150)
	must(t, op.do(http.MethodPost, "/operator/jobs/"+o.id+"/dispatch", nil), 200, "dispatch is allowed once assigned")
	// (The lifecycle lets an assigned operator dispatch; accepting records the commitment.)
	must(t, op.do(http.MethodPost, "/operator/jobs/"+o.id+"/accept", nil), 200, "accept")
	job = must(t, op.do(http.MethodGet, "/operator/jobs/"+o.id, nil), 200, "job after accept")
	if job.str("customer.phone") == "<nil>" || !slices.Contains(listStrings(job, "next_steps"), "arrived") {
		t.Fatalf("job after accept = %s", job.raw)
	}
	sched := must(t, op.do(http.MethodGet, fmt.Sprintf("/operator/schedule?from=%s&to=%s", o.start.Add(-time.Hour).Format(time.RFC3339), o.end.Add(time.Hour).Format(time.RFC3339)), nil), 200, "schedule")
	if !strings.Contains(sched.raw, o.id) {
		t.Fatalf("schedule = %s", sched.raw)
	}

	// On site: the handover checklist was completed before dispatch.
	must(t, op.do(http.MethodPost, "/operator/jobs/"+o.id+"/arrived", nil), 200, "arrived")
	must(t, op.do(http.MethodPost, "/operator/jobs/"+o.id+"/checklists", op.checklistBody(t, "handover", 2151)), 409, "second handover")
	op.ackInspection(t, o.id, "handover")
	must(t, op.do(http.MethodPost, "/operator/jobs/"+o.id+"/start", nil), 200, "start after handover")

	// Evidence is visible to the parties, not to outsiders.
	evidence := must(t, o.customer.do(http.MethodGet, "/bookings/"+o.id+"/checklists", nil), 200, "customer sees evidence")
	if len(evidence.list("data")) != 1 {
		t.Fatalf("evidence = %s", evidence.raw)
	}
	if photo := e.fetch(evidence.str("data.0.photos.0.url")); photo.Code != 200 || !strings.HasPrefix(photo.Header().Get("Content-Type"), "image/") {
		t.Fatalf("evidence photo: %d %v", photo.Code, photo.Header())
	}
	outsider := e.registerAndVerify("customer", "")
	must(t, outsider.do(http.MethodGet, "/bookings/"+o.id+"/checklists", nil), 404, "outsider evidence")

	// Issues: reported by the customer during the rental, reviewed by EQUIPRENT.
	issue := must(t, o.customer.do(http.MethodPost, "/bookings/"+o.id+"/issues", map[string]any{
		"kind": "breakdown", "stage": "during_rental", "severity": "major", "description": "Hydraulic hose burst on the boom",
	}), 201, "customer reports")
	must(t, outsider.do(http.MethodPost, "/bookings/"+o.id+"/issues", map[string]any{"kind": "other", "stage": "during_rental", "severity": "minor", "description": "Not mine"}), 404, "outsider reports")
	support := e.supportAdmin()
	queue := must(t, support.do(http.MethodGet, "/admin/issues?status=open", nil), 200, "issue queue")
	if !strings.Contains(queue.raw, issue.str("id")) {
		t.Fatalf("queue = %s", queue.raw)
	}
	must(t, o.owner.do(http.MethodGet, "/admin/issues", nil), 403, "owners do not see the admin queue")
	must(t, support.do(http.MethodPost, "/admin/issues/"+issue.str("id")+"/review", map[string]any{"status": "resolved", "note": "Owner replaced the hose"}), 200, "support reviews")

	// Return: the after-checklist (not below the handover hour meter) comes first.
	must(t, o.customer.do(http.MethodPost, "/bookings/"+o.id+"/return-request", nil), 200, "return requested")
	op.ackInspection(t, o.id, "return")
	must(t, op.do(http.MethodPost, "/operator/jobs/"+o.id+"/returned", nil), 422, "returned without the after-checklist")
	low := op.checklistBody(t, "return", 2100)
	must(t, op.do(http.MethodPost, "/operator/jobs/"+o.id+"/checklists", low), 400, "hour meter went backwards")
	ret := op.checklistBody(t, "return", 2178.5)
	ret["damage_notes"] = "Dent on the left bucket tooth"
	must(t, op.do(http.MethodPost, "/operator/jobs/"+o.id+"/checklists", ret), 201, "return checklist with damage")
	job = must(t, op.do(http.MethodGet, "/operator/jobs/"+o.id, nil), 200, "job before returning")
	if !slices.Contains(listStrings(job, "next_steps"), "returned") || len(job.list("issues")) != 2 {
		t.Fatalf("job before returning = %s", job.raw)
	}
	op.ackInspection(t, o.id, "return")
	must(t, op.do(http.MethodPost, "/operator/jobs/"+o.id+"/returned", nil), 200, "returned")

	// Completion: the assignment is done and the job moves to history.
	hist := must(t, op.do(http.MethodGet, "/operator/jobs?scope=history", nil), 200, "history")
	if !strings.Contains(hist.raw, o.id) || !strings.Contains(hist.raw, `"assignment_status":"completed"`) {
		t.Fatalf("history = %s", hist.raw)
	}
	if eqv := must(t, o.owner.do(http.MethodGet, "/owner/equipment/"+o.eq, nil), 200, "machine"); eqv.num("current_hmr") != 2178.5 {
		t.Fatalf("hour meter not updated: %s", eqv.raw)
	}
	e.drain(t)
	if types, _ := noteTypes(t, o.owner); countOf(types, "issue.reported") != 2 {
		t.Fatalf("owner issue notifications = %v", types)
	}
}

func listStrings(r resp, path string) []string {
	var out []string
	for _, v := range r.list(path) {
		out = append(out, fmt.Sprint(v))
	}
	return out
}
