package tests

import (
	"fmt"
	"net/http"
	"testing"
)

// backdatePayouts makes payouts look hours old (the updated_at trigger is
// paused, otherwise it would reset the timestamp).
func (f *payoutFixture) backdatePayouts(hours int) {
	f.e.exec(fmt.Sprintf(`ALTER TABLE payouts DISABLE TRIGGER payouts_updated_at;
	          UPDATE payouts SET created_at = now() - interval '%d hours';
	          ALTER TABLE payouts ENABLE TRIGGER payouts_updated_at;`, hours))
}

// Finance must be able to SEE a payout that is stuck, and one the bank
// reversed after it was marked paid; neither may exist only in a log line.
func TestFinanceSeesStuckAndReversedPayouts(t *testing.T) {
	f := newPayoutFixture(t, 500_000, 700_000)
	gw := newPayoutGateway()
	f.liveAccount()
	svc := f.service(gw, nil)
	fin := f.e.financeAdmin()
	attention := func() resp { return must(t, fin.do(http.MethodGet, "/admin/payouts/attention", nil), 200, "attention") }

	if err := svc.RunPayouts(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if a := attention(); problems(a) != 0 {
		t.Fatalf("a payout that was just sent is not a problem yet: %s", a.raw)
	}

	// Still processing hours later: stuck.
	f.backdatePayouts(3)
	a := attention()
	if a.body["needs_attention"] != true || len(a.list("stuck")) != 1 || a.str("stuck.0.status") != "processing" {
		t.Fatalf("a payout stuck for 3 hours must be reported: %s", a.raw)
	}

	// The bank completes it: no longer stuck.
	gw.setAll("processed")
	if err := svc.SettleProcessing(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if err := svc.Reconcile(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if a := attention(); problems(a) != 0 {
		t.Fatalf("a paid and reconciled payout needs nothing: %s", a.raw)
	}

	// Days later the bank reverses it. It was reconciled OK before, so it must be re-checked.
	gw.setAll("reversed")
	f.e.exec(`UPDATE payout_reconciliations SET checked_at = now() - interval '7 hours'`)
	if err := svc.Reconcile(pctx, 50); err != nil {
		t.Fatal(err)
	}
	a = attention()
	if len(a.list("mismatches")) != 1 || a.str("mismatches.0.our_status") != "paid" || a.str("mismatches.0.gateway_status") != "reversed" {
		t.Fatalf("a payout reversed after being paid must reach finance: %s", a.raw)
	}
	// Re-checking again does not duplicate it in the list.
	f.e.exec(`UPDATE payout_reconciliations SET checked_at = now() - interval '7 hours'`)
	if err := svc.Reconcile(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if n := len(attention().list("mismatches")); n != 1 {
		t.Fatalf("one payout is listed once, got %d", n)
	}

	// The list endpoint, with a filter.
	paid := must(t, fin.do(http.MethodGet, "/admin/payouts?status=paid", nil), 200, "list paid")
	if len(paid.list("data")) != 1 || paid.num("data.0.amount_paise") != 1200000 {
		t.Fatalf("paid payouts: %s", paid.raw)
	}
	if none := must(t, fin.do(http.MethodGet, "/admin/payouts?status=failed", nil), 200, "list failed"); len(none.list("data")) != 0 {
		t.Fatalf("no failed payouts expected: %s", none.raw)
	}
	must(t, fin.do(http.MethodGet, "/admin/payouts?status=bogus", nil), 400, "bad filter")
}

// Money waiting for an owner with no verified payout account is reported.
func TestFinanceSeesOwnersWhoCannotBePaid(t *testing.T) {
	f := newPayoutFixture(t, 500_000)
	f.e.exec(`UPDATE payout_linked_accounts SET activation_status = 'suspended'`)
	svc := f.service(newPayoutGateway(), nil)
	if err := svc.RunPayouts(pctx, 50); err != nil {
		t.Fatal(err)
	}
	a := must(t, f.e.financeAdmin().do(http.MethodGet, "/admin/payouts/attention", nil), 200, "attention")
	if len(a.list("unpayable_owners")) != 1 || a.str("unpayable_owners.0.eligible_paise") != "500000" || a.str("unpayable_owners.0.eligible_count") != "1" {
		t.Fatalf("an owner with money waiting and no payout account must be listed: %s", a.raw)
	}
	// Still activated, but verified by a provider/mode the payout gateway in use does not pay:
	// after switching from test to live keys the owner has to verify again, and finance must see it.
	f.e.exec(`UPDATE payout_linked_accounts SET activation_status = 'activated', provider = 'razorpay', mode = 'live'`)
	a = must(t, f.e.financeAdmin().do(http.MethodGet, "/admin/payouts/attention", nil), 200, "attention")
	if len(a.list("unpayable_owners")) != 1 || a.body["automatic_payouts"] != true {
		t.Fatalf("an account the gateway cannot pay must be listed as unpayable: %s", a.raw)
	}
}

// Only finance/ops admins see payouts.
func TestPayoutAdminEndpointsArePrivate(t *testing.T) {
	e := newEnv(t)
	owner := e.registerAndVerify("equipment_owner", "Patil Earthmovers")
	customer := e.registerAndVerify("customer", "")
	for _, who := range []*user{owner, customer, e.kycAdmin(), e.supportAdmin()} {
		for _, path := range []string{"/admin/payouts", "/admin/payouts/attention"} {
			must(t, who.do(http.MethodGet, path, nil), 403, "forbidden "+path)
		}
	}
	must(t, e.call(http.MethodGet, "/admin/payouts", "", "", nil, nil), 401, "anonymous")
	must(t, e.financeAdmin().do(http.MethodGet, "/admin/payouts", nil), 200, "finance")
}

// problems counts the payouts that went wrong (not the owners who merely have no payout account).
func problems(a resp) int {
	return len(a.list("stuck")) + len(a.list("failed_recently")) + len(a.list("mismatches"))
}
