package tests

import (
	"fmt"
	"net/http"
	"testing"
)

func availabilityOf(t *testing.T, owner *user, driverID string) string {
	t.Helper()
	roster := must(t, owner.do(http.MethodGet, "/fleet/"+owner.org+"/operators", nil), 200, "roster")
	for _, x := range roster.list("data") {
		if m := x.(map[string]any); m["user_id"] == driverID {
			return fmt.Sprint(m["availability"])
		}
	}
	t.Fatalf("driver %s missing from roster %s", driverID, roster.raw)
	return ""
}

// Driver availability is derived from assignments; the manual duty switch is never overwritten.
func TestDriverAvailabilityTransitions(t *testing.T) {
	e := newEnv(t)
	o := newOperatorRental(t, e)
	if got := availabilityOf(t, o.owner, o.op1.id); got != "available" {
		t.Fatalf("verified driver should default to available, got %s", got)
	}
	assign := func(op string) resp {
		return o.owner.do(http.MethodPost, "/owner/bookings/"+o.id+"/assign-operator", map[string]any{"operator_user_id": op})
	}
	must(t, assign(o.op1.id), 200, "assign")
	if got := availabilityOf(t, o.owner, o.op1.id); got != "assigned" {
		t.Fatalf("after assignment = %s", got)
	}
	must(t, o.op1.do(http.MethodPost, "/operator/jobs/"+o.id+"/decline", nil), 200, "decline")
	if got := availabilityOf(t, o.owner, o.op1.id); got != "available" {
		t.Fatalf("after decline = %s", got)
	}
	// An owner may assign an Off Duty driver; the driver's manual switch never changes.
	must(t, o.op1.do(http.MethodPost, "/operator/availability", map[string]any{"on_duty": false}), 200, "off duty")
	if got := availabilityOf(t, o.owner, o.op1.id); got != "off_duty" {
		t.Fatalf("manual off duty = %s", got)
	}
	must(t, o.customer.do(http.MethodPost, "/owner/bookings/"+o.id+"/assign-operator", map[string]any{"operator_user_id": o.op1.id}), 403, "customer cannot assign")
	must(t, assign(o.op1.id), 200, "assign an off-duty driver")
	if got := availabilityOf(t, o.owner, o.op1.id); got != "off_duty" {
		t.Fatalf("assignment changed the manual switch: %s", got)
	}
	if st := must(t, o.owner.do(http.MethodGet, "/bookings/"+o.id, nil), 200, "booking").str("status"); st != "operator_assigned" {
		t.Fatalf("status after assigning = %s (job must not start)", st)
	}
	must(t, assign(o.op1.id), 409, "duplicate assignment")
	e.drain(t)
	if types, _ := noteTypes(t, o.op1); countOf(types, "booking.job_assigned") != 2 {
		t.Fatalf("driver notes = %v", types)
	}
	// Accepting keeps Off Duty and does not start the job.
	must(t, o.op1.do(http.MethodPost, "/operator/jobs/"+o.id+"/accept", nil), 200, "accept")
	if got := availabilityOf(t, o.owner, o.op1.id); got != "off_duty" {
		t.Fatalf("after accept = %s", got)
	}
	if st := must(t, o.owner.do(http.MethodGet, "/bookings/"+o.id, nil), 200, "booking").str("status"); st != "operator_assigned" {
		t.Fatalf("status after accept = %s", st)
	}
	// An off-duty driver who rejects stays off duty and the job is not silently reassigned.
	must(t, o.op2.do(http.MethodPost, "/operator/availability", map[string]any{"on_duty": false}), 200, "op2 off duty")
	must(t, assign(o.op2.id), 200, "reassign to off-duty op2")
	must(t, o.op2.do(http.MethodPost, "/operator/jobs/"+o.id+"/decline", nil), 200, "op2 rejects")
	if got := availabilityOf(t, o.owner, o.op2.id); got != "off_duty" {
		t.Fatalf("after reject = %s", got)
	}
	if n := e.query1(`SELECT count(*)::text FROM operator_assignments WHERE booking_id = $1 AND status IN ('assigned','accepted')`, o.id); n != "0" {
		t.Fatalf("active assignments after reject = %s", n)
	}
	e.drain(t)
	if types, _ := noteTypes(t, o.owner); countOf(types, "booking.operator_declined") != 2 {
		t.Fatalf("owner notes = %v", types)
	}
	// Another owner can neither see nor assign this owner's driver.
	other := newOperatorRental(t, e)
	must(t, other.owner.do(http.MethodGet, "/fleet/"+o.owner.org+"/operators", nil), 404, "foreign roster")
	must(t, other.owner.do(http.MethodPost, "/owner/bookings/"+o.id+"/assign-operator", map[string]any{"operator_user_id": o.op2.id}), 404, "foreign assign")
}

// A verified business can be reopened for editing; it goes back to pending, never auto-verified.
func TestOwnerBusinessReopenForEditing(t *testing.T) {
	e := newEnv(t)
	kyc := e.kycAdmin()
	a := e.verifiedOwner(kyc, "Reopen Rentals")
	b := e.verifiedOwner(kyc, "Other Rentals")
	before := must(t, a.do(http.MethodGet, "/verification/owner", nil), 200, "status")
	if before.str("application.status") != "approved" || before.str("editable") != "false" {
		t.Fatalf("approved business should be locked: %s", before.raw)
	}
	re := must(t, a.do(http.MethodPost, "/verification/owner/reopen", nil), 200, "reopen")
	if re.str("editable") != "true" || re.str("application.status") == "approved" {
		t.Fatalf("reopened = %s", re.raw)
	}
	if st := e.query1(`SELECT kyc_status::text FROM organizations WHERE id = $1`, a.org); st != "pending" {
		t.Fatalf("org kyc status = %s, want pending", st)
	}
	// The other owner's business is untouched.
	if st := e.query1(`SELECT kyc_status::text FROM organizations WHERE id = $1`, b.org); st != "verified" {
		t.Fatalf("other owner kyc = %s", st)
	}
	// Reopening again is harmless.
	must(t, a.do(http.MethodPost, "/verification/owner/reopen", nil), 200, "reopen twice")
	// Customers cannot reopen a business.
	must(t, newOperatorRental(t, e).customer.do(http.MethodPost, "/verification/owner/reopen", nil), 403, "customer reopen")
}
