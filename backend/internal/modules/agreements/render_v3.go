package agreements

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/modules/bookings"
)

// TemplateV3 renders the terms from a legal template managed as data
// (legal_templates): the clauses are configuration, reviewed and versioned
// outside the code. The template body is copied into the agreement's
// snapshot, so the exact text can always be reproduced and re-hashed.
const TemplateV3 = "rental-v3"

// LegalBody is the clause content of a rental-agreement template.
type LegalBody struct {
	Preamble            string   `json:"preamble"`
	ConditionClauses    []string `json:"condition_clauses"`
	UseClauses          []string `json:"use_clauses"`
	DamageClauses       []string `json:"damage_clauses"`
	DepositClauses      []string `json:"deposit_clauses"`
	GeneralTerms        []string `json:"general_terms"`
	AcceptanceStatement string   `json:"acceptance_statement"`
	ConsentText         string   `json:"consent_text"`
}

// Validate reports what a template is missing before it can be published.
func (b LegalBody) Validate() []string {
	var miss []string
	check := func(name string, ok bool) {
		if !ok {
			miss = append(miss, name)
		}
	}
	check("preamble", strings.TrimSpace(b.Preamble) != "")
	check("condition_clauses", len(b.ConditionClauses) > 0)
	check("damage_clauses", len(b.DamageClauses) > 0)
	check("deposit_clauses", len(b.DepositClauses) > 0)
	check("general_terms", len(b.GeneralTerms) > 0)
	check("acceptance_statement", strings.TrimSpace(b.AcceptanceStatement) != "")
	check("consent_text", strings.TrimSpace(b.ConsentText) != "")
	for _, list := range [][]string{b.ConditionClauses, b.UseClauses, b.DamageClauses, b.DepositClauses, b.GeneralTerms} {
		for _, c := range list {
			if strings.TrimSpace(c) == "" {
				return append(miss, "empty clause")
			}
		}
	}
	return miss
}

// LegalTerms is the template an agreement was issued from.
type LegalTerms struct {
	TemplateID   string     `json:"template_id"`
	Version      string     `json:"version"`
	Title        string     `json:"title"`
	ReviewedBy   string     `json:"reviewed_by,omitempty"`
	ReviewedAt   *time.Time `json:"reviewed_at,omitempty"`
	ReviewRef    string     `json:"review_reference,omitempty"`
	Body         LegalBody  `json:"body"`
	Jurisdiction string     `json:"jurisdiction"`
}

// Reviewed reports whether a legal review was recorded for the template.
func (t LegalTerms) Reviewed() bool { return t.ReviewedBy != "" && t.ReviewedAt != nil }

// TermsFromTemplate converts a template row.
func TermsFromTemplate(t db.LegalTemplate) (LegalTerms, error) {
	var body LegalBody
	if err := json.Unmarshal(t.Body, &body); err != nil {
		return LegalTerms{}, err
	}
	out := LegalTerms{TemplateID: t.ID.String(), Version: t.Version, Title: t.Title, Body: body, Jurisdiction: t.Jurisdiction,
		ReviewedAt: t.ReviewedAt}
	if t.ReviewedByName != nil {
		out.ReviewedBy = *t.ReviewedByName
		if t.ReviewerCapacity != nil && *t.ReviewerCapacity != "" {
			out.ReviewedBy += " (" + *t.ReviewerCapacity + ")"
		}
	}
	if t.ReviewReference != nil {
		out.ReviewRef = *t.ReviewReference
	}
	return out, nil
}

// additional charge components (everything that is not the hire itself).
var additional = map[string]bool{
	bookings.CompDelivery: true, bookings.CompPickup: true, bookings.CompOperator: true, bookings.CompFuel: true,
	bookings.CompPlatformFee: true, bookings.CompLateReturn: true, bookings.CompDamage: true,
}

var conditionText = map[string]string{
	"excellent": "excellent", "good": "good", "fair": "fair (usable, with visible wear)", "needs_repair": "needs repair (as declared by the owner)",
}

// Duration states a rental period in days and hours.
func Duration(start, end time.Time) string {
	h := int64(end.Sub(start).Round(time.Minute).Minutes()) / 60
	m := int64(end.Sub(start).Round(time.Minute).Minutes()) % 60
	days, hours := h/24, h%24
	var parts []string
	if days > 0 {
		parts = append(parts, plural(days, "day"))
	}
	if hours > 0 {
		parts = append(parts, plural(hours, "hour"))
	}
	if m > 0 {
		parts = append(parts, plural(m, "minute"))
	}
	if len(parts) == 0 {
		return "0 hours"
	}
	return strings.Join(parts, " ") + fmt.Sprintf(" (%d hours in total)", h)
}

func plural(n int64, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// BuildSnapshotV3 is BuildSnapshot plus the v3 facts (condition, equipment
// id, legal template).
func BuildSnapshotV3(c db.AgreementContextRow, terms LegalTerms) Snapshot {
	s := BuildSnapshot(c)
	s.Machine.ListingID = c.EquipmentID.String()
	s.Machine.Condition = c.EquipmentCondition
	s.Machine.HourMeter = c.CurrentHmr
	s.Legal = &terms
	// Components for the rental / additional split.
	var price bookings.Price
	if json.Unmarshal(c.PriceSnapshot, &price) == nil && len(price.Charges) == len(s.Charges) {
		for i, l := range price.Charges {
			s.Charges[i].Component = l.Component
		}
	}
	for _, l := range s.Charges {
		switch l.Component {
		case bookings.CompDelivery:
			s.DeliveryPaise += l.Paise
		case bookings.CompPickup:
			s.PickupPaise += l.Paise
		}
	}
	return s
}

// RenderV3 produces the exact agreement text from a snapshot with legal terms.
func RenderV3(s Snapshot) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	lt := s.Legal
	if lt == nil {
		lt = &LegalTerms{}
	}
	clauses := func(list []string) {
		for _, c := range list {
			w("- %s\n", strings.TrimSpace(c))
		}
	}

	w("EQUIPMENT RENTAL AGREEMENT\n")
	w("Booking reference: %s. Format: %s. Terms: %s.\n\n", s.Reference, TemplateV3, orDash(lt.Version))
	w("%s\n\n", strings.TrimSpace(lt.Body.Preamble))

	w("1. PARTIES\n")
	w("%s", partyBlock("Owner (lessor)", s.Owner))
	w("%s\n", partyBlock("Customer (lessee)", s.Customer))

	w("2. EQUIPMENT AND IDENTIFICATION\n")
	w("Machine: %s\n", s.Machine.Title)
	w("Make and model: %s %s (%s)\n", s.Machine.Make, s.Machine.Model, orDash(s.Machine.Category))
	if s.Machine.Year > 0 {
		w("Year of manufacture: %d\n", s.Machine.Year)
	}
	w("Registration number: %s\n", orDash(s.Machine.RegistrationNo))
	w("Serial / chassis number: %s\n", orDash(s.Machine.SerialNo))
	w("EQUIPRENT listing ID: %s\n", orDash(s.Machine.ListingID))
	if s.WithOperator {
		w("Hire mode: with operator (wet hire). The owner provides a licensed operator.\n")
	} else {
		w("Hire mode: without operator (dry hire). The customer provides a competent, licensed operator.\n")
	}
	if t, ok := fuelText[s.FuelPolicy]; ok {
		w("Fuel: %s.\n", t)
	}

	w("\n3. RENTAL PERIOD AND DURATION\n")
	w("Start: %s\n", localTime(s.Start, s.TimeZone))
	w("End: %s\n", localTime(s.End, s.TimeZone))
	w("(UTC %s to %s)\n", s.Start.Format(time.RFC3339), s.End.Format(time.RFC3339))
	w("Duration: %s\n", Duration(s.Start, s.End))
	w("Pricing unit: %s\n", s.PricingUnit)

	w("\n4. SITE AND DELIVERY\n")
	w("Work site: %s\n", orDash(s.Site))
	w("Work to be done: %s\n", orDash(s.Work))
	if s.Delivery {
		w("Delivery: the owner delivers the equipment to the site (%s).\n", INR(s.DeliveryPaise))
	} else {
		w("Delivery: the customer collects the equipment; no delivery charge.\n")
	}
	if s.Pickup {
		w("Return: the owner collects the equipment from the site (%s).\n", INR(s.PickupPaise))
	} else {
		w("Return: the customer returns the equipment to the owner; no pickup charge.\n")
	}

	w("\n5. CHARGES\n")
	w("Rental charges:\n")
	var extra []Line
	for _, l := range s.Charges {
		if l.Component == "tax" { // listed with the taxes below
			continue
		}
		if additional[l.Component] {
			extra = append(extra, l)
			continue
		}
		w("%s", chargeLine(l))
	}
	if len(extra) > 0 {
		w("Additional charges:\n")
		for _, l := range extra {
			w("%s", chargeLine(l))
		}
	}
	w("Subtotal before taxes: %s\n", INR(s.SubtotalPaise))
	if len(s.Taxes) == 0 {
		w("Taxes: none charged.\n")
	}
	for _, t := range s.Taxes {
		w("- %s: %s\n", t.Label, INR(t.Paise))
	}
	w("Estimated total including taxes: %s\n", INR(s.TotalPaise))
	if s.AdvancePaise > 0 {
		w("Advance due on confirmation: %s\n", INR(s.AdvancePaise))
	}
	if s.PaymentMode == "offline" {
		w("Payment: directly between the parties (offline), as agreed.\n")
	} else {
		w("Payment: online through EQUIPRENT.\n")
	}
	for _, n := range s.PriceNotes {
		w("Note: %s\n", n)
	}
	w("The final amount is settled after the rental from the actual period, hour-meter readings and any agreed damage, at the rates above.\n")
	if s.LateFeePerHourPaise > 0 {
		w("Late return: %s per hour after a grace period of %d minutes.\n", INR(s.LateFeePerHourPaise), s.LateGraceMinutes)
	}

	w("\n6. SECURITY DEPOSIT\n")
	w("Refundable security deposit: %s\n", INR(s.DepositPaise))
	clauses(lt.Body.DepositClauses)

	w("\n7. EQUIPMENT CONDITION\n")
	cond := conditionText[s.Machine.Condition]
	if cond == "" {
		cond = orDash(s.Machine.Condition)
	}
	w("Condition declared by the owner when booked: %s.\n", cond)
	if s.Machine.HourMeter > 0 {
		w("Hour meter when booked: %.1f hours (the handover checklist records the reading at handover).\n", s.Machine.HourMeter)
	}
	clauses(lt.Body.ConditionClauses)

	w("\n8. CANCELLATION POLICY\n")
	if s.CancellationPolicy != "" {
		w("Policy: %s\n", s.CancellationPolicy)
	}
	clauses(s.CancellationTerms)

	w("\n9. DAMAGE RESPONSIBILITY\n")
	clauses(lt.Body.DamageClauses)

	n := 10
	if len(lt.Body.UseClauses) > 0 {
		w("\n%d. USE OF THE EQUIPMENT\n", n)
		clauses(lt.Body.UseClauses)
		n++
	}
	w("\n%d. TERMS AND CONDITIONS\n", n)
	clauses(lt.Body.GeneralTerms)
	n++

	w("\n%d. ACCEPTANCE AND SIGNATURE\n", n)
	w("%s\n", strings.TrimSpace(lt.Body.AcceptanceStatement))
	w("\nTerms version: %s (%s, jurisdiction %s).\n", orDash(lt.Version), orDash(lt.Title), orDash(lt.Jurisdiction))
	if lt.Reviewed() {
		w("Legal review of these terms: %s on %s", lt.ReviewedBy, lt.ReviewedAt.UTC().Format("2006-01-02"))
		if lt.ReviewRef != "" {
			w(" (ref. %s)", lt.ReviewRef)
		}
		w(".\n")
	} else {
		w("Legal review of these terms: not recorded. This wording is a working draft.\n")
	}
	return b.String()
}

func chargeLine(l Line) string {
	if l.Detail != "" {
		return fmt.Sprintf("- %s (%s): %s\n", l.Label, l.Detail, INR(l.Paise))
	}
	return fmt.Sprintf("- %s: %s\n", l.Label, INR(l.Paise))
}

// Render produces the text for a snapshot in its template version.
func Render(templateVersion string, s Snapshot) string {
	if templateVersion == TemplateV3 {
		return RenderV3(s)
	}
	return RenderV2(s)
}
