// Package settlements turns finished bookings into owner settlements and pays
// them out. All money is integer paise; rates are basis points read from
// commission_rules and tax_rules, never hard-coded.
package settlements

import "errors"

// Rates are the basis-point rates in force for one settlement.
type Rates struct {
	CommissionBps int
	GSTBps        int // GST on commission
	TCSBps        int // GST TCS s.52; applied only when the owner is GST-registered
	TDSBps        int // 194-O (the no-PAN rate is chosen by the caller)
}

// Input is what the customer paid and how it splits.
type Input struct {
	PaidPaise        int64 // captured, excluding deposits
	RefundedPaise    int64 // processed refunds on those payments
	EstTotalPaise    int64 // booking estimate the split is proportioned from
	EstRentalPaise   int64
	EstDeliveryPaise int64
	EstFeePaise      int64 // platform (convenience) fee, before its GST
	EstFeeTaxPaise   int64
	DepositDeducted  int64 // approved damage deductions already credited to the owner
	OwnerGST         bool  // owner registered for GST (TCS applies)
	Rates            Rates
}

// Result mirrors the settlements row. Net = Gross - Commission - GST - TCS - TDS.
type Result struct {
	Customer, Rental, Delivery, OwnerTax, Fee, FeeTax, Base int64
	Gross, Commission, CommissionGST, TCS, TDS, Net         int64
}

func bps(v int64, r int) int64 { return (v*int64(r) + 5000) / 10000 }

// prop returns v * part / total rounded half up (0 when total is 0).
func prop(v, part, total int64) int64 {
	if total <= 0 {
		return 0
	}
	return (v*part*2 + total) / (2 * total)
}

var ErrInvalid = errors.New("settlement inputs are inconsistent")

// Compute splits a booking's net customer payment. The platform fee and its GST
// are carved out first; the rest belongs to the owner (supply value plus the owner's GST).
func Compute(in Input) (Result, error) {
	if in.PaidPaise < 0 || in.RefundedPaise < 0 || in.RefundedPaise > in.PaidPaise || in.DepositDeducted < 0 {
		return Result{}, ErrInvalid
	}
	var r Result
	r.Customer = in.PaidPaise - in.RefundedPaise
	r.Fee = min(prop(r.Customer, in.EstFeePaise, in.EstTotalPaise), r.Customer)
	r.FeeTax = min(prop(r.Customer, in.EstFeeTaxPaise, in.EstTotalPaise), r.Customer-r.Fee)
	ownerShare := r.Customer - r.Fee - r.FeeTax
	supply := in.EstRentalPaise + in.EstDeliveryPaise
	r.Rental = prop(ownerShare, in.EstRentalPaise, supply+max(0, in.EstTotalPaise-in.EstFeePaise-in.EstFeeTaxPaise-supply))
	r.Delivery = prop(ownerShare, in.EstDeliveryPaise, supply+max(0, in.EstTotalPaise-in.EstFeePaise-in.EstFeeTaxPaise-supply))
	r.OwnerTax = max(0, ownerShare-r.Rental-r.Delivery)
	r.Base = r.Rental
	r.Gross = ownerShare + in.DepositDeducted
	r.Commission = bps(r.Base, in.Rates.CommissionBps)
	r.CommissionGST = bps(r.Commission, in.Rates.GSTBps)
	taxable := r.Rental + r.Delivery
	if in.OwnerGST {
		r.TCS = bps(taxable, in.Rates.TCSBps)
	}
	r.TDS = bps(taxable, in.Rates.TDSBps)
	r.Net = r.Gross - r.Commission - r.CommissionGST - r.TCS - r.TDS
	if r.Net < 0 {
		return Result{}, ErrInvalid
	}
	return r, nil
}
