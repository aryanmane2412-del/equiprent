package invoices

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/modules/bookings"
)

// A price whose taxes are configured per component: GST on hire and
// transport, a different (hypothetical) rate on fuel, and nothing on damage.
func samplePrice() bookings.Price {
	return bookings.Price{
		Version: 2,
		Lines:   []bookings.RentalLine{{Unit: "day", Count: 3, RatePaise: 1_500_000, Paise: 4_500_000}},
		Charges: []bookings.ChargeLine{
			{Component: bookings.CompRental, Label: "Rental", Paise: 4_500_000},
			{Component: bookings.CompDelivery, Label: "Delivery", Paise: 300_000},
			{Component: bookings.CompFuel, Label: "Fuel", Paise: 100_000},
			{Component: bookings.CompPlatformFee, Label: "Platform fee", Paise: 90_000},
			{Component: "tax", Label: "GST 18%", Paise: 880_200},
		},
		Terms: &bookings.FrozenTerms{Taxes: []bookings.TaxRule{
			{Code: "gst", Name: "GST", RateBps: 1800, AppliesTo: []string{"rental", "delivery", "platform_fee"}, SACCode: "997313"},
			{Code: "gst_fuel", Name: "GST on fuel", RateBps: 500, AppliesTo: []string{"fuel"}, SACCode: "996601"},
		}},
	}
}

func TestBuildLinesUsesConfiguredRules(t *testing.T) {
	b := BuildLines(samplePrice(), nil, true)
	if b.Basis != "estimate" || len(b.Lines) != 3 {
		t.Fatalf("lines = %+v", b.Lines)
	}
	rental, delivery, fuel := b.Lines[0], b.Lines[1], b.Lines[2]
	if rental.Quantity != 3 || rental.Unit != "day" || rental.GSTRateBps != 1800 || rental.SACCode != "997313" {
		t.Errorf("rental = %+v", rental)
	}
	if delivery.GSTRateBps != 1800 || fuel.GSTRateBps != 500 || fuel.SACCode != "996601" || fuel.TaxCode != "gst_fuel" {
		t.Errorf("delivery/fuel = %+v / %+v", delivery, fuel)
	}
	for _, l := range b.Lines {
		if strings.Contains(l.Description, "Platform") {
			t.Fatal("the platform fee is EQUIPRENT's supply, not the owner's")
		}
	}
	if !strings.Contains(strings.Join(b.Notes, " "), "platform fee") {
		t.Error("the excluded platform fee should be explained")
	}
}

func TestBuildLinesPrefersFinalCharges(t *testing.T) {
	final := &bookings.FinalCharges{Charges: []bookings.ChargeLine{
		{Component: bookings.CompRental, Label: "Rental", Detail: "4 days", Paise: 6_000_000},
		{Component: bookings.CompDamage, Label: "Damage", Paise: 50_000},
		{Component: bookings.CompLateReturn, Label: "Late return", Paise: 0},
	}}
	b := BuildLines(samplePrice(), final, true)
	if b.Basis != "final" || len(b.Lines) != 2 || b.Lines[0].TaxablePaise != 6_000_000 || b.Lines[0].Description != "Rental (4 days)" {
		t.Fatalf("final lines = %+v", b.Lines)
	}
	if b.Lines[1].GSTRateBps != 0 || b.Lines[1].TaxCode != "none" {
		t.Fatalf("damage has no configured tax here: %+v", b.Lines[1])
	}
}

func TestBuildLinesBillOfSupply(t *testing.T) {
	b := BuildLines(samplePrice(), nil, false)
	for _, l := range b.Lines {
		if l.GSTRateBps != 0 {
			t.Fatalf("unregistered supplier charged GST: %+v", l)
		}
	}
	if !strings.Contains(strings.Join(b.Notes, " "), "bill of supply") {
		t.Fatal("bill of supply should be explained")
	}
}

func TestSumSplitsPerLine(t *testing.T) {
	lines := BuildLines(samplePrice(), nil, true).Lines
	intra := Sum(lines, true)
	// 18% of 48,00,000 paise = 8,64,000; 5% of 1,00,000 = 5,000.
	if intra.Subtotal != 4_900_000 || intra.CGST+intra.SGST != 869_000 || intra.CGST != intra.SGST || intra.IGST != 0 {
		t.Fatalf("intra = %+v", intra)
	}
	inter := Sum(lines, false)
	if inter.IGST != 869_000 || inter.CGST != 0 || inter.Total() != 5_769_000 {
		t.Fatalf("inter = %+v", inter)
	}
}

func TestCreditTax(t *testing.T) {
	orig := Invoice{SupplyType: "intra", SubtotalPaise: 1_000_001, CGSTPaise: 90_000, SGSTPaise: 90_000, TotalPaise: 1_180_001}
	part := CreditTax(orig, 0, 0, 500_000)
	if part.CGST != part.SGST || part.CGST != 45_000 {
		t.Fatalf("part = %+v", part)
	}
	// Crediting the rest returns exactly the remaining tax.
	rest := CreditTax(orig, 500_000, part.CGST+part.SGST, 500_001)
	if rest.CGST+rest.SGST+part.CGST+part.SGST != 180_000 {
		t.Fatalf("rest = %+v", rest)
	}
	none := CreditTax(Invoice{SupplyType: "inter", SubtotalPaise: 1000}, 0, 0, 500)
	if none.Total() != 500 {
		t.Fatalf("bill of supply credit = %+v", none)
	}
}

func TestInvoicePDFHasTheRequiredDetails(t *testing.T) {
	issued := time.Date(2026, 11, 14, 6, 0, 0, 0, time.UTC)
	captured := issued.Add(-72 * time.Hour)
	inv := Invoice{
		ID: uuid.New(), Number: "ER/2627/00042", Title: "Tax invoice", Type: "rental", Status: "issued", PlaceOfSupply: "27",
		SupplyType: "intra", SACCode: "997313", SubtotalPaise: 4_900_000, CGSTPaise: 434_500, SGSTPaise: 434_500, TotalPaise: 5_769_000,
		IssuedAt: &issued, CreditedPaise: 118_000, NetPaise: 5_651_000,
		Lines: BuildLines(samplePrice(), nil, true).Lines,
		Details: &Details{
			BookingReference: "ER-2611-4X7K",
			Supplier:         Party{LegalName: "Patil Earthmovers LLP", GSTIN: "27ABCDE1234F1Z5", Address: "Pune", StateCode: "27"},
			Recipient:        Party{LegalName: "Shree Builders", Address: "Wakad, Pune"},
			Equipment:        Equipment{Title: "JCB 3DX", Make: "JCB", Model: "3DX", RegistrationNo: "MH12KX4471"},
			Rental:           Rental{Start: captured, End: issued, TimeZone: "Asia/Kolkata", Site: "Wakad site"},
			Basis:            "final",
			Payments:         []PaymentRef{{Purpose: "advance", AmountPaise: 2_000_000, ReceiptNumber: "RCPT-2026-0000007", GatewayPaymentID: "pay_sbx_1", CapturedAt: &captured}},
			Notes:            []string{},
		},
		Adjustments: []Adjustment{{Kind: "credit_note", Reference: "CN/2627/00001", AmountPaise: 118_000, Status: "issued", Reason: "Short delivery"}},
	}
	pdf := BuildPDF(inv)
	for _, want := range []string{"%PDF-", "Tax invoice", "ER/2627/00042", "Patil Earthmovers LLP", "27ABCDE1234F1Z5", "Shree Builders",
		"JCB 3DX", "MH12KX4471", "Rental period", "SAC 997313", "GST 18%", "GST 5%", "CGST", "SGST", "Rs. 57,690.00",
		"RCPT-2026-0000007", "pay_sbx_1", "CN/2627/00001", "Rs. 56,510.00"} {
		if !bytes.Contains(pdf, []byte(want)) {
			t.Errorf("invoice PDF missing %q", want)
		}
	}
	if !bytes.Equal(pdf, BuildPDF(inv)) {
		t.Fatal("invoice PDF must be deterministic")
	}
	inv.Title, inv.CGSTPaise, inv.SGSTPaise = "Bill of supply", 0, 0
	if bos := BuildPDF(inv); bytes.Contains(bos, []byte("CGST")) || !bytes.Contains(bos, []byte("Bill of supply")) {
		t.Fatal("bill of supply shows no GST")
	}
}
