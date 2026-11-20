package invoices

import (
	"fmt"
	"strings"

	"github.com/equiprent/backend/internal/modules/bookings"
)

// Line building. GST is never a constant here: every line is taxed with the
// rules frozen in the booking's estimate (tax_rates as configured and
// reviewed when the booking was priced), matched by charge component. The
// platform fee is EQUIPRENT's own supply, so it is not on the owner's invoice.

// rulesOf returns the taxes that applied to the booking.
func rulesOf(p bookings.Price) []bookings.TaxRule {
	if p.Terms != nil && p.Terms.Taxes != nil {
		return p.Terms.Taxes
	}
	if p.GSTRateBps > 0 { // older estimates: one rate on every component
		return []bookings.TaxRule{{Code: "gst", Name: "GST", RateBps: p.GSTRateBps, AppliesTo: bookings.AllComponents}}
	}
	return nil
}

// lineTax combines the rules that apply to a component.
func lineTax(rules []bookings.TaxRule, component string) (rateBps int32, codes, sac string) {
	var cs []string
	for _, r := range rules {
		if r.Applies(component) {
			rateBps += int32(r.RateBps)
			cs = append(cs, r.Code)
			if sac == "" && r.SACCode != "" {
				sac = r.SACCode
			}
		}
	}
	if sac == "" {
		sac = SACRental
	}
	if len(cs) == 0 {
		return 0, "none", sac
	}
	return rateBps, strings.Join(cs, "_"), sac
}

// Built is the outcome of line building.
type Built struct {
	Lines []Line
	Basis string // final | estimate
	Notes []string
}

// BuildLines turns the booking's charges into invoice lines: the settled
// final charges when they exist, else the accepted estimate. registered=false
// (the owner has no GSTIN) produces a bill of supply without GST.
func BuildLines(p bookings.Price, final *bookings.FinalCharges, registered bool) Built {
	rules := rulesOf(p)
	out := Built{Basis: "estimate"}
	charges := p.Charges
	if final != nil && len(final.Charges) > 0 {
		charges, out.Basis = final.Charges, "final"
	}
	add := func(desc, component string, qty float64, unit string, rate, taxable int64) {
		if taxable <= 0 {
			return
		}
		bps, codes, sac := lineTax(rules, component)
		if !registered {
			bps, codes = 0, "none"
		}
		out.Lines = append(out.Lines, Line{
			LineNo: int16(len(out.Lines) + 1), Description: desc, SACCode: sac, Quantity: qty, Unit: unit,
			RatePaise: rate, TaxablePaise: taxable, GSTRateBps: bps, TaxCode: codes, Component: component,
		})
	}
	if len(charges) == 0 { // estimates priced before the itemised breakdown
		for _, l := range BuildRentalLines(p, 0) {
			add(l.Description, bookings.CompRental, l.Quantity, l.Unit, l.RatePaise, l.TaxablePaise)
		}
	}
	rentalIdx := 0
	for _, c := range charges {
		if c.Component == "tax" { // tax lines of the estimate: the invoice computes its own
			continue
		}
		if c.Component == bookings.CompPlatformFee {
			if c.Paise > 0 {
				out.Notes = append(out.Notes, fmt.Sprintf("The EQUIPRENT platform fee (%s) is EQUIPRENT's own service and is not part of this invoice.", rupees(c.Paise)))
			}
			continue
		}
		desc := c.Label
		if c.Detail != "" {
			desc += " (" + c.Detail + ")"
		}
		// Rental lines map one to one onto the booked unit lines.
		if c.Component == bookings.CompRental {
			k := rentalIdx
			rentalIdx++
			if k < len(p.Lines) && p.Lines[k].Paise == c.Paise {
				l := p.Lines[k]
				add("Equipment rental", bookings.CompRental, float64(l.Count), l.Unit, l.RatePaise, l.Paise)
				continue
			}
		}
		add(desc, c.Component, 1, "service", c.Paise, c.Paise)
	}
	if !registered {
		out.Notes = append(out.Notes, "The supplier has no GSTIN on EQUIPRENT, so this is a bill of supply and no GST is charged on it.")
	}
	if out.Basis == "estimate" {
		out.Notes = append(out.Notes, "Issued from the accepted estimate: no final charges were settled for this booking.")
	}
	return out
}

// Totals are the invoice header amounts.
type Totals struct {
	Subtotal, CGST, SGST, IGST int64
	SupplyType                 string
}

// Total is the invoice total.
func (t Totals) Total() int64 { return t.Subtotal + t.CGST + t.SGST + t.IGST }

// Sum splits each line's tax (CGST+SGST within the state, IGST across
// states) and adds them up.
func Sum(lines []Line, intra bool) Totals {
	t := Totals{SupplyType: "inter"}
	if intra {
		t.SupplyType = "intra"
	}
	for _, l := range lines {
		t.Subtotal += l.TaxablePaise
		x := SplitGST(l.TaxablePaise, int(l.GSTRateBps), intra)
		t.CGST, t.SGST, t.IGST = t.CGST+x.CGST, t.SGST+x.SGST, t.IGST+x.IGST
	}
	return t
}

// CreditTax works out the tax on a credit of `amount` taxable value against
// an invoice: proportional to the original, and the remaining tax exactly
// when the rest of the invoice is credited (no rounding drift).
func CreditTax(orig Invoice, creditedSubtotal, creditedTax, amount int64) Totals {
	t := Totals{Subtotal: amount, SupplyType: orig.SupplyType}
	origTax := orig.CGSTPaise + orig.SGSTPaise + orig.IGSTPaise
	if orig.SubtotalPaise <= 0 || origTax == 0 {
		return t
	}
	if amount == orig.SubtotalPaise-creditedSubtotal {
		rest := origTax - creditedTax
		if orig.SupplyType == "intra" {
			t.CGST = rest / 2
			t.SGST = t.CGST
		} else {
			t.IGST = rest
		}
		return t
	}
	if orig.SupplyType == "intra" {
		t.CGST = (amount*orig.CGSTPaise + orig.SubtotalPaise/2) / orig.SubtotalPaise
		t.SGST = t.CGST
	} else {
		t.IGST = (amount*orig.IGSTPaise + orig.SubtotalPaise/2) / orig.SubtotalPaise
	}
	return t
}
