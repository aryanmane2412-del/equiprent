package tests

import (
	"net/http"
	"testing"
)

// Driver accept/decline reach the owner, never auto-reassign, and the
// driver roster/jobs stay private to the owning organisation.
func TestDriverJobDecisionsAndPrivacy(t *testing.T) {
	e := newEnv(t)
	o := newOperatorRental(t, e)
	assign := func(u *user, op string) resp {
		return u.do(http.MethodPost, "/owner/bookings/"+o.id+"/assign-operator", map[string]any{"operator_user_id": op})
	}
	// Another owner cannot assign this owner's driver; customers cannot at all.
	other := newOperatorRental(t, e)
	must(t, assign(other.owner, o.op1.id), 404, "other owner assigns")
	must(t, assign(o.customer, o.op1.id), 403, "customer assigns")
	must(t, assign(e.supportAdmin(), o.op1.id), 403, "admin assigns")
	must(t, o.owner.do(http.MethodPost, "/admin/bookings/"+o.id+"/transition", map[string]any{}), 403, "owner uses admin transition")

	// Roster is private.
	must(t, o.customer.do(http.MethodGet, "/fleet/"+o.owner.org+"/operators", nil), 404, "customer roster")
	must(t, other.owner.do(http.MethodGet, "/fleet/"+o.owner.org+"/operators", nil), 404, "foreign owner roster")
	must(t, o.owner.do(http.MethodGet, "/fleet/"+o.owner.org+"/operators", nil), 200, "own roster")

	// Decline: booking back to confirmed, owner notified, no auto-assignment.
	must(t, assign(o.owner, o.op1.id), 200, "assign")
	must(t, o.op1.do(http.MethodPost, "/operator/jobs/"+o.id+"/decline", nil), 200, "decline")
	if st := must(t, o.owner.do(http.MethodGet, "/bookings/"+o.id, nil), 200, "booking").str("status"); st != "confirmed" {
		t.Fatalf("status after decline = %s", st)
	}
	e.drain(t)
	if types, _ := noteTypes(t, o.owner); countOf(types, "booking.operator_declined") != 1 {
		t.Fatalf("owner notes = %v", types)
	}
	// Reassign and accept: owner told again; customer never gets driver profile.
	must(t, assign(o.owner, o.op2.id), 200, "assign op2")
	must(t, o.op2.do(http.MethodPost, "/operator/jobs/"+o.id+"/accept", nil), 200, "accept")
	e.drain(t)
	if types, _ := noteTypes(t, o.owner); countOf(types, "booking.operator_accepted") != 1 {
		t.Fatalf("owner notes = %v", types)
	}
	must(t, o.op1.do(http.MethodGet, "/operator/jobs/"+o.id, nil), 200, "declined driver sees only history")
	must(t, o.customer.do(http.MethodGet, "/operator/jobs", nil), 403, "customer driver dashboard")
}
