package settlements

import "testing"

func TestComputeDocsExample(t *testing.T) {
	// ₹25,000 rental + 18% GST + ₹500 fee incl. GST => customer pays ₹30,000 (docs/DATABASE_DESIGN §8.2).
	r, err := Compute(Input{PaidPaise: 3000000, EstTotalPaise: 3000000, EstRentalPaise: 2500000,
		EstFeePaise: 42373, EstFeeTaxPaise: 7627, OwnerGST: true,
		Rates: Rates{CommissionBps: 1000, GSTBps: 1800, TCSBps: 50, TDSBps: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Commission != 250000 || r.CommissionGST != 45000 {
		t.Fatalf("commission %d gst %d", r.Commission, r.CommissionGST)
	}
	if r.Gross+r.Fee+r.FeeTax != 3000000 {
		t.Fatalf("split does not add up: %+v", r)
	}
	if r.Net != r.Gross-r.Commission-r.CommissionGST-r.TCS-r.TDS {
		t.Fatalf("net %+v", r)
	}
	if r.TCS == 0 || r.TDS == 0 {
		t.Fatalf("expected withholding: %+v", r)
	}
}

func TestNoGSTNoTCS(t *testing.T) {
	r, _ := Compute(Input{PaidPaise: 100000, EstTotalPaise: 100000, EstRentalPaise: 100000, Rates: Rates{TCSBps: 50, TDSBps: 10}})
	if r.TCS != 0 || r.TDS != 100 {
		t.Fatalf("%+v", r)
	}
}

func TestRefundReducesGross(t *testing.T) {
	r, _ := Compute(Input{PaidPaise: 100000, RefundedPaise: 40000, EstTotalPaise: 100000, EstRentalPaise: 100000, Rates: Rates{CommissionBps: 1000}})
	if r.Gross != 60000 || r.Commission != 6000 {
		t.Fatalf("%+v", r)
	}
}

func TestInvalid(t *testing.T) {
	if _, err := Compute(Input{PaidPaise: 10, RefundedPaise: 20}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := Compute(Input{PaidPaise: 100, EstTotalPaise: 100, EstRentalPaise: 100, Rates: Rates{CommissionBps: 5000, GSTBps: 5000, TDSBps: 9000}}); err == nil {
		t.Fatal("negative net must fail")
	}
}
