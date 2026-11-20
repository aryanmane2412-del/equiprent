package bookings

import "testing"

func TestNormalizeCustomer(t *testing.T) {
	ind := &CustomerDetails{Name: "Ravi", Phone: "9800000000", Email: "r@x.in", GSTIN: "junk"}
	if k, _, err := normalizeCustomer("individual", ind); err != nil || k != "individual" {
		t.Fatalf("individual should need no company data: %v", err)
	}
	if _, _, err := normalizeCustomer("individual", &CustomerDetails{Name: "R", Phone: "123", Email: "r@x.in"}); err == nil {
		t.Fatal("bad phone accepted")
	}
	co := &CustomerDetails{CompanyName: "Acme", ContactName: "A", Phone: "9800000000", Email: "a@acme.in", Address: "1 Rd", City: "Pune", State: "MH", PinCode: "411001"}
	if _, _, err := normalizeCustomer("company", co); err != nil {
		t.Fatalf("valid company rejected: %v", err)
	}
	co.PinCode, co.GSTIN = "41", "BAD"
	if _, _, err := normalizeCustomer("company", co); err == nil {
		t.Fatal("bad pin/gstin accepted")
	}
	if _, _, err := normalizeCustomer("company", &CustomerDetails{Phone: "9800000000", Email: "a@b.in"}); err == nil {
		t.Fatal("company without name accepted")
	}
	if k, _, err := normalizeCustomer("", nil); err != nil || k != "individual" {
		t.Fatal("legacy request should default to individual")
	}
}
