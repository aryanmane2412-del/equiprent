package tests

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// Integration review: customer rental types, owner directory and role isolation.
func TestCustomerTypesOwnerDirectoryAndIsolation(t *testing.T) {
	e := newEnv(t)
	r := newRental(t, e, "offline")
	book := func(extra map[string]any) resp {
		body := map[string]any{
			"equipment_id": r.eq, "site_address_id": r.siteID, "period": map[string]any{"start": r.start, "end": r.end},
			"pricing_unit": "day", "with_operator": true, "payment_mode": "offline",
		}
		for k, v := range extra {
			body[k] = v
		}
		return r.customer.do(http.MethodPost, "/bookings", body, "Idempotency-Key", key("typ"))
	}
	// Validation follows the chosen type.
	must(t, book(map[string]any{"customer_type": "company", "customer": map[string]any{"phone": "9800000000", "email": "a@b.in"}}), 400, "company needs business details")
	must(t, book(map[string]any{"customer_type": "individual", "customer": map[string]any{"name": "Ravi", "phone": "123", "email": "r@x.in"}}), 400, "bad phone")
	ind := must(t, book(map[string]any{"customer_type": "individual", "customer": map[string]any{"name": "Ravi Kulkarni", "phone": "9800000001", "email": "ravi@example.com"}}), 201, "individual booking")
	co := must(t, book(map[string]any{"customer_type": "company", "customer": map[string]any{
		"company_name": "Acme Infra", "contact_name": "Asha", "phone": "9800000002", "email": "asha@acme.in",
		"address": "1 Ring Rd", "city": "Pune", "state": "Maharashtra", "pin_code": "411001", "gstin": "27ABCDE1234F1Z5"}}), 201, "company booking")
	// Stored on the booking record and shown to the owner of that booking only.
	for id, want := range map[string]string{ind.str("id"): "individual", co.str("id"): "company"} {
		if got := e.query1(`SELECT customer_type FROM bookings WHERE id = $1`, id); got != want {
			t.Fatalf("stored customer_type = %s, want %s", got, want)
		}
	}
	view := must(t, r.owner.do(http.MethodGet, "/bookings/"+co.str("id"), nil), 200, "owner view")
	if view.str("customer.customer_type") != "company" || view.str("customer.company_name") != "Acme Infra" || view.str("customer.gstin") == "" {
		t.Fatalf("owner customer card = %s", view.raw)
	}
	if view.str("customer.phone") != "" && view.str("customer.phone") != "<nil>" {
		t.Fatalf("contact phone released before acceptance: %s", view.raw)
	}
	// Isolation: another customer and another owner see nothing; customers have no driver directory.
	other := e.registerAndVerify("customer", "")
	must(t, other.do(http.MethodGet, "/bookings/"+co.str("id"), nil), 404, "other customer")
	otherOwner := e.verifiedOwner(r.kyc, "Other Rentals "+randomLetters(4))
	must(t, otherOwner.do(http.MethodGet, "/bookings/"+co.str("id"), nil), 404, "other owner")
	must(t, r.customer.do(http.MethodGet, "/fleet/"+r.owner.org+"/operators", nil), 404, "customer driver directory")
	must(t, otherOwner.do(http.MethodGet, "/fleet/"+r.owner.org+"/operators", nil), 404, "foreign driver directory")

	// Admin owner directory: authorised, one row per owner, accurate counts, searchable, read-only.
	admin := r.ops
	company := "Directory Co " + randomLetters(6) // fixtures share names; make this owner unique
	e.exec(`UPDATE organizations SET display_name = $2 WHERE id = $1`, r.owner.org, company)
	dir := must(t, admin.do(http.MethodGet, "/admin/equipment-owners?q="+url.QueryEscape(company), nil), 200, "owner directory")
	rows := 0
	for _, raw := range dir.list("data") {
		row := raw.(map[string]any)
		if row["org_id"] == r.owner.org {
			rows++
			want := e.query1(`SELECT count(*)::text FROM equipment WHERE owner_org_id = $1 AND deleted_at IS NULL`, r.owner.org)
			if got := formatNum(row["equipment_count"]); got != want {
				t.Fatalf("equipment_count = %s, want %s", got, want)
			}
		}
	}
	if rows != 1 {
		t.Fatalf("owner appears %d times: %s", rows, dir.raw)
	}
	must(t, r.customer.do(http.MethodGet, "/admin/equipment-owners", nil), 403, "customer cannot list owners")
	must(t, r.owner.do(http.MethodGet, "/admin/equipment-owners", nil), 403, "owner cannot list owners")
	must(t, admin.do(http.MethodPost, "/admin/equipment-owners", map[string]any{}), 405, "directory is read-only")
}

func formatNum(v any) string {
	f, _ := v.(float64)
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// Customers photograph the delivered machine: upload, attach once on site, only they and the owner see it.
func TestCustomerDeliveryPhotos(t *testing.T) {
	e := newEnv(t)
	r := newRental(t, e, "offline")
	for _, st := range []string{"accepted", "confirmed", "dispatched", "on_site"} {
		e.exec(`UPDATE bookings SET status = $2 WHERE id = $1`, r.id, st)
	}
	up := r.customer.jobPhoto(t) // was 403: customers could not upload job photos
	body := map[string]any{"upload_id": up, "captured_at": time.Now().Add(-time.Minute).UTC(), "client_id": fmt.Sprintf("018f0000-0000-7000-8000-%012d", rand.Int64N(1e12))}
	must(t, r.customer.do(http.MethodPost, "/bookings/"+r.id+"/delivery-photos", body), 201, "attach delivery photo")
	list := must(t, r.owner.do(http.MethodGet, "/bookings/"+r.id+"/delivery-photos", nil), 200, "owner sees photo")
	if len(list.list("data")) != 1 {
		t.Fatalf("delivery photos = %s", list.raw)
	}
	photoID := list.str("data.0.id")
	approve := "/bookings/" + r.id + "/delivery-photos/" + photoID + "/approve"
	must(t, r.customer.do(http.MethodPost, approve, nil), 403, "customer cannot approve")
	foreign := e.verifiedOwner(r.kyc, "Foreign Co "+randomLetters(4))
	must(t, foreign.do(http.MethodPost, approve, nil), 404, "another owner cannot approve")
	done := must(t, r.owner.do(http.MethodPost, approve, nil), 200, "owner approves")
	if done.str("data.0.approved_at") == "<nil>" || done.str("data.0.approved_at") == "" {
		t.Fatalf("not approved: %s", done.raw)
	}
	must(t, r.owner.do(http.MethodPost, approve, nil), 200, "approve again is idempotent")
	must(t, r.owner.do(http.MethodPost, "/bookings/"+r.id+"/delivery-photos/018f0000-0000-7000-8000-00000000dead/approve", nil), 404, "unknown photo")
	e.drain(t)
	ownerNotes, _ := noteTypes(t, r.owner)
	custNotes, _ := noteTypes(t, r.customer)
	if countOf(ownerNotes, "booking.delivery_photo_submitted") != 1 || countOf(custNotes, "booking.delivery_photo_approved") != 1 {
		t.Fatalf("notifications owner=%v customer=%v", ownerNotes, custNotes)
	}
	other := e.registerAndVerify("customer", "")
	must(t, other.do(http.MethodGet, "/bookings/"+r.id+"/delivery-photos", nil), 404, "outsider")
	must(t, other.do(http.MethodPost, "/bookings/"+r.id+"/delivery-photos", body), 404, "outsider attach")
}

// The real UI path: no explicit acknowledgement calls. Operator checklists and the customer's own
// confirm / release actions supply the acknowledgements the start and return gates need.
func TestHandoverConfirmNeedsNoExtraSteps(t *testing.T) {
	e := newEnv(t)
	o := newOperatorRental(t, e)
	op := o.op1
	id := o.id
	must(t, o.owner.do(http.MethodPost, "/owner/bookings/"+id+"/assign-operator", map[string]any{"operator_user_id": op.id}), 200, "assign")
	must(t, op.do(http.MethodPost, "/operator/jobs/"+id+"/accept", nil), 200, "accept")
	// The driver's checklist needs no hour-meter reading or photo.
	hb := op.checklistBody(t, "handover", 0)
	delete(hb, "hour_meter")
	delete(hb, "hour_meter_photo_upload_id")
	must(t, op.do(http.MethodPost, "/operator/jobs/"+id+"/checklists", hb), 201, "handover checklist without hour meter")
	must(t, op.do(http.MethodPost, "/operator/jobs/"+id+"/dispatch", nil), 200, "dispatch")
	must(t, op.do(http.MethodPost, "/operator/jobs/"+id+"/arrived", nil), 200, "arrived")
	job := must(t, op.do(http.MethodGet, "/operator/jobs/"+id, nil), 200, "job on site")
	if got := listStrings(job, "next_steps"); len(got) != 1 || got[0] != "await_customer" {
		t.Fatalf("driver next steps before customer confirms = %v", got)
	}
	must(t, op.do(http.MethodPost, "/operator/jobs/"+id+"/start", nil), 422, "start before the customer confirms")
	must(t, o.customer.do(http.MethodPost, "/bookings/"+id+"/handover/confirm", nil), 200, "customer confirms handover")
	if st := must(t, o.customer.do(http.MethodGet, "/bookings/"+id, nil), 200, "booking").str("status"); st != "active" {
		t.Fatalf("status after confirm = %s", st)
	}
	must(t, o.customer.do(http.MethodPost, "/bookings/"+id+"/return-request", nil), 200, "release machine")
	rb := op.checklistBody(t, "return", 0)
	delete(rb, "hour_meter")
	delete(rb, "hour_meter_photo_upload_id")
	must(t, op.do(http.MethodPost, "/operator/jobs/"+id+"/checklists", rb), 201, "return checklist without hour meter")
	must(t, op.do(http.MethodPost, "/operator/jobs/"+id+"/returned", nil), 200, "returned")
}

// Calendar: only two states. Requests do not hold dates; accepted bookings do; cancelling frees
// them unless another booking overlaps; overlapping submissions are refused.
func TestCalendarAvailabilityAndOverlaps(t *testing.T) {
	e := newEnv(t)
	r := newRental(t, e, "offline")
	day := 24 * time.Hour
	start, mid, end := r.start, r.start.Add(3*day), r.start.Add(6*day)
	busy := func(who *user) []any {
		q := "?from=" + url.QueryEscape(start.Add(-day).UTC().Format(time.RFC3339)) + "&to=" + url.QueryEscape(end.Add(day).UTC().Format(time.RFC3339))
		return must(t, who.do(http.MethodGet, "/equipment/"+r.eq+"/availability"+q, nil), 200, "availability").list("busy")
	}
	book := func(c *user, s, en time.Time) resp {
		return c.do(http.MethodPost, "/bookings", map[string]any{
			"equipment_id": r.eq, "site_address_id": r.siteID, "period": map[string]any{"start": s, "end": en},
			"pricing_unit": "day", "with_operator": true, "payment_mode": "offline",
		}, "Idempotency-Key", key("cal"))
	}
	// 1. A request awaiting the owner holds nothing: dates stay available.
	if n := len(busy(r.customer)); n != 0 {
		t.Fatalf("pending request marked dates busy: %d", n)
	}
	// 2. Accepted: dates become booked for customers and the owner alike.
	must(t, r.owner.do(http.MethodPost, "/owner/bookings/"+r.id+"/accept", nil), 200, "accept")
	if n := len(busy(r.customer)); n != 1 {
		t.Fatalf("accepted booking not shown as booked: %d", n)
	}
	if n := len(busy(r.owner)); n != 1 {
		t.Fatalf("owner sees %d booked ranges", n)
	}
	// 3. Booked dates cannot be booked again (full or partial overlap).
	other := e.registerAndVerify("customer", "")
	site := must(t, other.do(http.MethodPost, "/orgs/"+other.org+"/addresses", map[string]any{
		"label": "Other site", "line1": "Plot 1", "city": "Pune", "state_code": "27", "pincode": "411057",
		"is_site": true, "location": map[string]any{"lat": 18.5308, "lng": 73.8475},
	}), 201, "address").str("id")
	overlap := func(s, en time.Time) int {
		return other.do(http.MethodPost, "/bookings", map[string]any{
			"equipment_id": r.eq, "site_address_id": site, "period": map[string]any{"start": s, "end": en},
			"pricing_unit": "day", "with_operator": true, "payment_mode": "offline",
		}, "Idempotency-Key", key("ov")).code
	}
	if c := overlap(start.Add(day), mid.Add(day)); c != 409 {
		t.Fatalf("partial overlap status = %d, want 409", c)
	}
	if c := overlap(start, mid); c != 409 {
		t.Fatalf("identical dates status = %d, want 409", c)
	}
	// 4. An adjacent booking is allowed; cancelling the first frees only its own dates.
	adj := must(t, book(r.customer, mid, end), 201, "adjacent booking")
	must(t, r.owner.do(http.MethodPost, "/owner/bookings/"+adj.str("id")+"/accept", nil), 200, "accept adjacent")
	must(t, r.customer.do(http.MethodPost, "/bookings/"+r.id+"/cancel", map[string]any{"reason": "Changed plans"}), 200, "cancel first")
	left := busy(r.customer)
	if len(left) != 1 {
		t.Fatalf("after cancelling the first booking: %d booked ranges, want 1 (the adjacent one)", len(left))
	}
	// 5. Cancelled dates can be booked again by someone else.
	if c := overlap(start, mid); c != 201 {
		t.Fatalf("freed dates not bookable: %d", c)
	}
}

// Owner blocks: dates become booked for customers, cannot be booked, and free up when unblocked.
func TestOwnerBlockDates(t *testing.T) {
	e := newEnv(t)
	r := newRental(t, e, "offline")
	day := 24 * time.Hour
	from, to := r.start.Add(10*day), r.start.Add(13*day)
	busy := func() int {
		q := "?from=" + url.QueryEscape(from.Add(-day).UTC().Format(time.RFC3339)) + "&to=" + url.QueryEscape(to.Add(day).UTC().Format(time.RFC3339))
		return len(must(t, r.customer.do(http.MethodGet, "/equipment/"+r.eq+"/availability"+q, nil), 200, "availability").list("busy"))
	}
	body := map[string]any{"start": from, "end": to, "reason": "owner_block"}
	must(t, r.customer.do(http.MethodPost, "/owner/equipment/"+r.eq+"/blocks", body), 403, "customer cannot block")
	foreign := e.verifiedOwner(r.kyc, "Foreign Blocks "+randomLetters(4))
	if c := foreign.do(http.MethodPost, "/owner/equipment/"+r.eq+"/blocks", body).code; c != 404 && c != 403 {
		t.Fatalf("another owner blocked this machine: %d", c)
	}
	blk := must(t, r.owner.do(http.MethodPost, "/owner/equipment/"+r.eq+"/blocks", body), 201, "owner blocks")
	if busy() != 1 {
		t.Fatal("blocked dates are not shown as booked")
	}
	if c := r.customer.do(http.MethodPost, "/bookings", map[string]any{
		"equipment_id": r.eq, "site_address_id": r.siteID, "period": map[string]any{"start": from, "end": to},
		"pricing_unit": "day", "with_operator": true, "payment_mode": "offline",
	}, "Idempotency-Key", key("blk")).code; c != 409 {
		t.Fatalf("booking blocked dates = %d, want 409", c)
	}
	cal := must(t, r.owner.do(http.MethodGet, "/owner/equipment/"+r.eq+"/calendar?from="+url.QueryEscape(from.Add(-day).UTC().Format(time.RFC3339))+"&to="+url.QueryEscape(to.Add(day).UTC().Format(time.RFC3339)), nil), 200, "owner calendar")
	if len(cal.list("blocks")) != 1 {
		t.Fatalf("owner calendar blocks = %s", cal.raw)
	}
	must(t, r.owner.do(http.MethodDelete, "/owner/equipment/"+r.eq+"/blocks/"+blk.str("id"), nil), 204, "unblock")
	if busy() != 0 {
		t.Fatal("unblocked dates still booked")
	}
}
