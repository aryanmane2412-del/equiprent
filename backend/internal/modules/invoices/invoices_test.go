package invoices

import (
	"testing"
	"time"

	"github.com/equiprent/backend/internal/modules/bookings"
)

func TestFinancialYear(t *testing.T) {
	cases := map[string]string{
		"2026-11-12T10:00:00Z": "2627",
		"2027-03-31T18:00:00Z": "2627", // still 31 Mar in IST (23:30)
		"2027-03-31T18:31:00Z": "2728", // 1 Apr 00:01 IST
		"2026-04-01T00:00:00Z": "2627",
		"2099-12-31T00:00:00Z": "9900",
	}
	for in, want := range cases {
		ts, _ := time.Parse(time.RFC3339, in)
		if got := FinancialYear(ts); got != want {
			t.Errorf("FinancialYear(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestSplitGST(t *testing.T) {
	intra := SplitGST(4_950_000, 1800, true)
	if intra.SupplyType != "intra" || intra.CGST != 445_500 || intra.SGST != intra.CGST || intra.IGST != 0 {
		t.Fatalf("intra = %+v", intra)
	}
	inter := SplitGST(4_950_000, 1800, false)
	if inter.SupplyType != "inter" || inter.IGST != 891_000 || inter.CGST+inter.SGST != 0 {
		t.Fatalf("inter = %+v", inter)
	}
	// Odd amounts: each half rounds, totals stay consistent (CHECK cgst = sgst).
	odd := SplitGST(101, 1800, true)
	if odd.CGST != odd.SGST {
		t.Fatalf("odd = %+v", odd)
	}
}

func TestBuildRentalLines(t *testing.T) {
	lines := BuildRentalLines(bookings.Price{
		Unit: "day", Units: 3, RatePaise: 1_500_000, RentalPaise: 4_500_000 + 2*110_000, ExtraHours: 2,
		MobilisationPaise: 400_000, OperatorBataPaise: 150_000, Days: 3,
	}, 1800)
	if len(lines) != 4 {
		t.Fatalf("lines = %+v", lines)
	}
	var sum int64
	for i, l := range lines {
		if l.LineNo != int16(i+1) || l.SACCode != SACRental || l.GSTRateBps != 1800 {
			t.Errorf("line %d = %+v", i, l)
		}
		sum += l.TaxablePaise
	}
	if sum != 4_500_000+220_000+400_000+150_000 {
		t.Fatalf("taxable sum = %d", sum)
	}
	if lines[1].Description != "Extra hours" || lines[1].RatePaise != 110_000 {
		t.Fatalf("extra hours line = %+v", lines[1])
	}
}
