package agreements

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/modules/bookings"
	"github.com/equiprent/backend/internal/pdfdoc"
)

// Template versions. The wording of a version must never change once
// agreements exist with it: their hashes are computed over the exact text.
const (
	TemplateV1 = "rental-v1" // early agreements, rendered from the live booking
	TemplateV2 = "rental-v2" // rendered from a snapshot stored with the agreement
	// TemplateVersion is the version new agreements use.
	TemplateVersion = TemplateV2
)

// Party is one side of the agreement as named in it.
type Party struct {
	LegalName    string `json:"legal_name"`
	DisplayName  string `json:"display_name"`
	GSTIN        string `json:"gstin,omitempty"`
	Address      string `json:"address,omitempty"`
	ContactName  string `json:"contact_name,omitempty"`
	ContactPhone string `json:"contact_phone,omitempty"`
}

// MachineInfo describes the equipment.
type MachineInfo struct {
	Title          string `json:"title"`
	Category       string `json:"category"`
	Make           string `json:"make"`
	Model          string `json:"model"`
	Year           int    `json:"year,omitempty"`
	RegistrationNo string `json:"registration_no,omitempty"`
	SerialNo       string `json:"serial_no,omitempty"`
	// rental-v3 onwards.
	ListingID string  `json:"listing_id,omitempty"`
	Condition string  `json:"condition,omitempty"`
	HourMeter float64 `json:"hour_meter,omitempty"`
}

// Line is one priced line.
type Line struct {
	Component string `json:"component,omitempty"`
	Label     string `json:"label"`
	Detail    string `json:"detail,omitempty"`
	Paise     int64  `json:"paise"`
}

// Snapshot is everything the agreement states, frozen when it is generated.
type Snapshot struct {
	Reference    string      `json:"reference"`
	Owner        Party       `json:"owner"`
	Customer     Party       `json:"customer"`
	Machine      MachineInfo `json:"machine"`
	WithOperator bool        `json:"with_operator"`
	Site         string      `json:"site"`
	Start        time.Time   `json:"start"`
	End          time.Time   `json:"end"`
	TimeZone     string      `json:"time_zone"`
	PricingUnit  string      `json:"pricing_unit"`
	Delivery     bool        `json:"delivery"`
	Pickup       bool        `json:"pickup"`
	Work         string      `json:"work,omitempty"`
	FuelPolicy   string      `json:"fuel_policy"`
	PaymentMode  string      `json:"payment_mode"`

	Charges       []Line   `json:"charges"`
	Taxes         []Line   `json:"taxes"`
	SubtotalPaise int64    `json:"subtotal_paise"`
	TaxPaise      int64    `json:"tax_paise"`
	TotalPaise    int64    `json:"total_paise"`
	DepositPaise  int64    `json:"deposit_paise"`
	AdvancePaise  int64    `json:"advance_paise"`
	PriceNotes    []string `json:"price_notes,omitempty"`

	LateFeePerHourPaise int64    `json:"late_fee_per_hour_paise,omitempty"`
	LateGraceMinutes    int32    `json:"late_grace_minutes,omitempty"`
	CancellationPolicy  string   `json:"cancellation_policy,omitempty"`
	CancellationTerms   []string `json:"cancellation_terms"`

	// rental-v3 onwards.
	DeliveryPaise int64       `json:"delivery_paise,omitempty"`
	PickupPaise   int64       `json:"pickup_paise,omitempty"`
	Legal         *LegalTerms `json:"legal,omitempty"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// BuildSnapshot collects the agreement's facts from the database row.
func BuildSnapshot(c db.AgreementContextRow) Snapshot {
	var price bookings.Price
	_ = json.Unmarshal(c.PriceSnapshot, &price)
	s := Snapshot{
		Reference: c.Reference,
		Owner: Party{LegalName: c.OwnerLegalName, DisplayName: c.OwnerDisplayName, GSTIN: deref(c.OwnerGstin), Address: c.OwnerAddress,
			ContactName: deref(c.OwnerContactName), ContactPhone: deref(c.OwnerContactPhone)},
		Customer: Party{LegalName: c.CustomerLegalName, DisplayName: c.CustomerDisplayName, GSTIN: deref(c.CustomerGstin),
			Address: c.CustomerAddress, ContactName: deref(c.CustomerContactName), ContactPhone: deref(c.CustomerContactPhone)},
		Machine: MachineInfo{Title: c.EquipmentTitle, Category: c.CategoryName, Make: c.MakeName, Model: c.ModelName,
			RegistrationNo: deref(c.RegistrationNo), SerialNo: deref(c.SerialNo)},
		WithOperator: c.WithOperator, Site: c.SiteAddress, Start: c.StartsAt.UTC(), End: c.EndsAt.UTC(),
		TimeZone: c.EquipmentTimezone, PricingUnit: c.PricingUnit, Delivery: c.Delivery, Pickup: c.Pickup,
		Work: strings.TrimSpace(c.WorkDescription), FuelPolicy: c.FuelPolicy, PaymentMode: c.PaymentMode,
		SubtotalPaise: price.SubtotalPaise, TaxPaise: price.GSTPaise, TotalPaise: c.EstimatedTotalPaise,
		DepositPaise: c.DepositPaise, AdvancePaise: price.AdvanceDuePaise, PriceNotes: price.Notes,
		LateGraceMinutes: c.LateGraceMinutes, CancellationPolicy: c.CancellationPolicyName,
	}
	if c.ManufactureYear != nil {
		s.Machine.Year = int(*c.ManufactureYear)
	}
	if c.LateFeePerHourPaise != nil {
		s.LateFeePerHourPaise = *c.LateFeePerHourPaise
	}
	for _, l := range price.Charges {
		s.Charges = append(s.Charges, Line{Label: l.Label, Detail: l.Detail, Paise: l.Paise})
	}
	if len(s.Charges) == 0 && price.RentalPaise > 0 { // older snapshots without a breakdown
		s.Charges = append(s.Charges, Line{Label: "Rental", Paise: price.RentalPaise})
		for _, l := range []Line{{Label: "Mobilisation", Paise: price.MobilisationPaise}, {Label: "Operator", Paise: price.OperatorBataPaise},
			{Label: "Fuel", Paise: price.FuelPaise}, {Label: "Platform fee", Paise: price.PlatformFeePaise}} {
			if l.Paise > 0 {
				s.Charges = append(s.Charges, l)
			}
		}
	}
	for _, t := range price.Taxes {
		s.Taxes = append(s.Taxes, Line{Label: fmt.Sprintf("%s @ %s%%", t.Name, pct(t.RateBps)), Paise: t.Paise})
	}
	if s.SubtotalPaise == 0 {
		s.SubtotalPaise = s.TotalPaise - s.TaxPaise
	}
	rules := c.CancellationRules
	if price.Terms != nil && len(price.Terms.CancellationRules) > 0 {
		rules = price.Terms.CancellationRules // the rules frozen with the booking win
	}
	s.CancellationTerms = cancellationTerms(rules)
	return s
}

func pct(bps int) string {
	if bps%100 == 0 {
		return fmt.Sprint(bps / 100)
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", float64(bps)/100), "0"), ".")
}

// cancellationTerms states each cancellation rule in words.
func cancellationTerms(raw json.RawMessage) []string {
	var rules []bookings.CancellationRule
	if json.Unmarshal(raw, &rules) != nil || len(rules) == 0 {
		return []string{"Cancellation fees follow the policy attached to the booking on EQUIPRENT."}
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].HoursBefore > rules[j].HoursBefore })
	var out []string
	for _, r := range rules {
		when := fmt.Sprintf("at least %s hours before the start", strings.TrimSuffix(fmt.Sprintf("%g", r.HoursBefore), ".0"))
		if r.HoursBefore <= 0 {
			when = "after that, up to the start"
		}
		fee := "no fee"
		if r.FeeBps > 0 {
			fee = "a fee of " + pct(r.FeeBps) + "% of the estimated total"
		}
		out = append(out, fmt.Sprintf("Cancelling %s: %s.", when, fee))
	}
	return out
}

// INR formats paise with Indian digit grouping (₹1,23,456.50).
func INR(paise int64) string {
	neg := paise < 0
	if neg {
		paise = -paise
	}
	whole, frac := paise/100, paise%100
	d := fmt.Sprint(whole)
	if len(d) > 3 {
		head, tail := d[:len(d)-3], d[len(d)-3:]
		var groups []string
		for len(head) > 2 {
			groups = append([]string{head[len(head)-2:]}, groups...)
			head = head[:len(head)-2]
		}
		d = strings.Join(append([]string{head}, groups...), ",") + "," + tail
	}
	out := fmt.Sprintf("₹%s.%02d", d, frac)
	if neg {
		return "-" + out
	}
	return out
}

func localTime(t time.Time, tz string) string {
	loc, err := time.LoadLocation(tz)
	if err != nil || tz == "" {
		loc = time.FixedZone("IST", 5*3600+1800)
	}
	return t.In(loc).Format("Mon 02 Jan 2006, 15:04 MST")
}

var fuelText = map[string]string{
	"owner":    "fuel is supplied by the owner and charged as stated above",
	"customer": "fuel is supplied by the customer",
	"shared":   "fuel is shared between the parties as agreed on EQUIPRENT",
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func partyBlock(role string, p Party) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s", role, p.LegalName)
	if p.DisplayName != "" && p.DisplayName != p.LegalName {
		fmt.Fprintf(&b, " (trading as %s)", p.DisplayName)
	}
	b.WriteString("\n")
	if p.GSTIN != "" {
		fmt.Fprintf(&b, "   GSTIN: %s\n", p.GSTIN)
	}
	if p.Address != "" {
		fmt.Fprintf(&b, "   Address: %s\n", p.Address)
	}
	if p.ContactName != "" || p.ContactPhone != "" {
		fmt.Fprintf(&b, "   Contact: %s\n", strings.TrimSpace(p.ContactName+" "+p.ContactPhone))
	}
	return b.String()
}

// RenderV2 produces the exact agreement text from a snapshot.
func RenderV2(s Snapshot) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("EQUIPMENT RENTAL AGREEMENT\n")
	w("Booking reference: %s. Template: %s.\n\n", s.Reference, TemplateV2)
	w("This agreement is made on the EQUIPRENT platform between the owner and the customer named below for the rental of the equipment described in section 2.\n\n")

	w("1. PARTIES\n")
	w("%s", partyBlock("Owner (lessor)", s.Owner))
	w("%s\n", partyBlock("Customer (lessee)", s.Customer))

	w("2. EQUIPMENT\n")
	w("Machine: %s\n", s.Machine.Title)
	w("Make and model: %s %s (%s)\n", s.Machine.Make, s.Machine.Model, orDash(s.Machine.Category))
	if s.Machine.Year > 0 {
		w("Year of manufacture: %d\n", s.Machine.Year)
	}
	w("Registration number: %s\n", orDash(s.Machine.RegistrationNo))
	if s.Machine.SerialNo != "" {
		w("Serial number: %s\n", s.Machine.SerialNo)
	}
	if s.WithOperator {
		w("Hire mode: with operator (wet hire). The owner provides a licensed operator.\n")
	} else {
		w("Hire mode: without operator (dry hire). The customer provides a competent, licensed operator.\n")
	}
	if t, ok := fuelText[s.FuelPolicy]; ok {
		w("Fuel: %s.\n", t)
	}
	w("\n3. RENTAL PERIOD AND SITE\n")
	w("From: %s\n", localTime(s.Start, s.TimeZone))
	w("To: %s\n", localTime(s.End, s.TimeZone))
	w("(UTC %s to %s)\n", s.Start.Format(time.RFC3339), s.End.Format(time.RFC3339))
	w("Work site: %s\n", orDash(s.Site))
	w("Delivery to site by owner: %s. Pickup from site by owner: %s.\n", yesNo(s.Delivery), yesNo(s.Pickup))
	w("Work to be done: %s\n", orDash(s.Work))

	w("\n4. CHARGES\n")
	w("Pricing unit: %s\n", s.PricingUnit)
	for _, l := range s.Charges {
		if l.Detail != "" {
			w("- %s (%s): %s\n", l.Label, l.Detail, INR(l.Paise))
		} else {
			w("- %s: %s\n", l.Label, INR(l.Paise))
		}
	}
	w("Subtotal before taxes: %s\n", INR(s.SubtotalPaise))
	for _, t := range s.Taxes {
		w("- %s: %s\n", t.Label, INR(t.Paise))
	}
	w("Estimated total including taxes: %s\n", INR(s.TotalPaise))
	if s.AdvancePaise > 0 {
		w("Advance due on confirmation: %s\n", INR(s.AdvancePaise))
	}
	w("Refundable security deposit: %s\n", INR(s.DepositPaise))
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

	w("\n5. CANCELLATION\n")
	if s.CancellationPolicy != "" {
		w("Policy: %s\n", s.CancellationPolicy)
	}
	for _, t := range s.CancellationTerms {
		w("- %s\n", t)
	}

	w("\n6. TERMS AND CONDITIONS\n")
	for i, t := range standardTerms {
		w("%d. %s\n", i+1, t)
	}

	w("\n7. ELECTRONIC SIGNATURE\n")
	w("Each party signs this agreement electronically on EQUIPRENT. A signature records the signer's name, the time, the IP address and the SHA-256 fingerprint of this exact text, and binds the signer's organisation. The agreement takes effect when both parties have signed.\n")
	return b.String()
}

var standardTerms = []string{
	"The owner hands over the equipment in safe working condition, with valid registration, insurance and fitness documents where the law requires them.",
	"The customer uses the equipment only at the work site above, for the work described, within its rated capacity and in line with applicable safety rules.",
	"Working hours are measured by the hour meter (HMR) and recorded in daily logs. Logs not disputed within 48 hours are treated as approved.",
	"Breakdowns are reported immediately on EQUIPRENT. Hours lost to a breakdown that is not the customer's fault are not billed.",
	"Routine maintenance during the rental is the owner's responsibility; the customer allows reasonable access for it.",
	"The customer is responsible for loss of or damage to the equipment beyond normal wear while it is in the customer's care, assessed at return against the handover checklist and photographs.",
	"The equipment may not be sublet, moved to another site or used by anyone other than the agreed operator without the owner's written consent.",
	"Each party complies with the laws that apply to it, including labour, safety and environmental rules at the site.",
	"Neither party is liable for delays caused by events beyond its reasonable control, such as natural disasters, strikes or government orders.",
	"Disputes are first raised on EQUIPRENT. Payments held by the platform are released according to its terms and the outcome of the dispute.",
	"This agreement is governed by the laws of India.",
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// ---- v1 (legacy) ----------------------------------------------------------------------

var termsTemplateV1 = template.Must(template.New("terms").Parse(`EQUIPMENT RENTAL AGREEMENT ({{.Version}})
Booking reference: {{.Reference}}

Owner (lessor): {{.OwnerName}}
Customer (lessee): {{.CustomerName}}
Equipment: {{.Equipment}}
Hire mode: {{if .WithOperator}}with operator (wet hire){{else}}without operator (dry hire){{end}}
Rental period (UTC): {{.Start}} to {{.End}}
Pricing unit: {{.Unit}}
Estimated total (incl. GST): INR {{.Total}}
Security deposit: INR {{.Deposit}}

1. The owner provides the equipment in working condition with valid registration, insurance and fitness documents.
2. The customer uses the equipment only at the agreed site and for the described work: {{.Work}}
3. Hours are measured by the hour meter (HMR) and recorded in daily logs; logs not disputed within 48 hours are approved.
4. Breakdowns are reported immediately; breakdown hours are not billed.
5. Cancellation fees follow the cancellation policy attached to the booking.
6. Damage beyond normal wear is assessed at return against the handover checklist and photographs.
7. Disputes are first raised on the EQUIPRENT platform; payments are released per its terms.
`))

// RenderV1 reproduces the original wording from the live booking (kept so
// agreements created with rental-v1 still verify).
func RenderV1(b bookings.Booking) (string, error) {
	data := map[string]any{
		"Version": TemplateV1, "Reference": b.Reference, "OwnerName": b.OwnerName, "CustomerName": b.CustomerName,
		"Equipment": b.EquipmentTitle, "WithOperator": b.WithOperator,
		"Start": b.Period.Start.UTC().Format(time.RFC3339), "End": b.Period.End.UTC().Format(time.RFC3339),
		"Unit": b.PricingUnit, "Total": plainRupees(b.EstimatedTotal), "Deposit": plainRupees(b.DepositPaise),
		"Work": orDash(b.WorkDescription),
	}
	var buf bytes.Buffer
	err := termsTemplateV1.Execute(&buf, data)
	return buf.String(), err
}

func plainRupees(paise int64) string { return fmt.Sprintf("%d.%02d", paise/100, paise%100) }

// Hash is the SHA-256 of the exact text.
func Hash(text string) []byte {
	sum := sha256.Sum256([]byte(text))
	return sum[:]
}

// ---- PDF ---------------------------------------------------------------------------------

// SignatureView is a recorded signature as shown to the parties.
type SignatureView struct {
	Party       string    `json:"party"`
	SignerName  string    `json:"signer_name"`
	Designation *string   `json:"designation,omitempty"`
	Method      string    `json:"method" doc:"typed_name or provider"`
	Kind        string    `json:"kind" doc:"acceptance_record (typed name + consent; not a digital signature) or electronic_signature (external provider)"`
	Provider    string    `json:"provider"`
	ConsentText string    `json:"consent_text,omitempty"`
	SignedAt    time.Time `json:"signed_at"`
	IP          string    `json:"-"`
	UserAgent   string    `json:"-"`
}

// maskIP keeps the network part of an address (privacy in the PDF).
func maskIP(ip *netip.Addr) string {
	if ip == nil || !ip.IsValid() {
		return "-"
	}
	if ip.Is4() {
		b := ip.As4()
		return fmt.Sprintf("%d.%d.%d.x", b[0], b[1], b[2])
	}
	p, err := ip.Prefix(48)
	if err != nil {
		return "-"
	}
	return p.String()
}

// PDFInput is what a PDF rendering needs.
type PDFInput struct {
	AgreementID string
	Reference   string
	Template    string
	Terms       string
	TermsSHA256 []byte
	CreatedAt   time.Time
	Executed    bool
	Version     int32
	Superseded  bool
	Signatures  []SignatureView
}

// BuildPDF renders the agreement as a PDF. Drafts carry a watermark and no
// signature record; the executed copy ends with the signature certificate.
func BuildPDF(in PDFInput) []byte {
	hash := hex.EncodeToString(in.TermsSHA256)
	ver := ""
	if in.Version > 0 {
		ver = fmt.Sprintf(" (version %d)", in.Version)
	}
	d := &pdfdoc.Doc{
		Title: "Rental agreement " + in.Reference + ver, Author: "EQUIPRENT", Subject: "Equipment rental agreement",
		Created: in.CreatedAt, Header: "EQUIPRENT - Equipment rental agreement " + in.Reference + ver,
		Footer: "Agreement " + in.AgreementID + "   |   SHA-256 " + hash[:16] + "...",
	}
	switch {
	case in.Superseded:
		d.Watermark = "SUPERSEDED"
	case !in.Executed:
		d.Watermark = "DRAFT"
	}
	lines := strings.Split(strings.TrimRight(in.Terms, "\n"), "\n")
	for i, l := range lines {
		switch {
		case i == 0:
			d.Heading(l, 16)
		case isSectionHeading(l):
			d.Space(6)
			d.Heading(l, 11)
		case strings.HasPrefix(l, "   "):
			d.ParaStyled(strings.TrimSpace(l), 9, false, 0.25)
		case strings.TrimSpace(l) == "":
			d.Space(4)
		default:
			d.Para(l)
		}
	}
	d.Space(10)
	d.Rule()
	if !in.Executed {
		d.ParaStyled("Draft for review. This copy has not been accepted by both parties.", 10, true, 0)
		d.Small("Fingerprint of the text (SHA-256): " + hash)
		return d.Bytes()
	}
	allProvider := len(in.Signatures) > 0
	for _, s := range in.Signatures {
		if s.Method != "provider" {
			allProvider = false
		}
	}
	if allProvider {
		d.Heading("SIGNATURE RECORD", 11)
		d.Small("Both parties signed the text above through an external e-signature provider; the provider's certificate is the signature evidence. Fingerprint of the text (SHA-256): " + hash)
	} else {
		d.Heading("ACCEPTANCE RECORD", 11)
		d.Small("Both parties accepted the text above on EQUIPRENT. A typed-name acceptance with a consent checkbox is evidence of agreement; it is not a digital signature or Aadhaar eSign under the Information Technology Act, 2000. Fingerprint of the text (SHA-256): " + hash)
	}
	for _, s := range in.Signatures {
		d.Space(6)
		role := "Owner (lessor)"
		if s.Party == "customer" {
			role = "Customer (lessee)"
		}
		name := s.SignerName
		if s.Designation != nil && *s.Designation != "" {
			name += ", " + *s.Designation
		}
		d.Field(role, name)
		d.Field("Recorded at", s.SignedAt.In(time.FixedZone("IST", 5*3600+1800)).Format("02 Jan 2006 15:04:05 MST")+" ("+s.SignedAt.UTC().Format(time.RFC3339)+")")
		method := "Typed-name acceptance with consent checkbox (acceptance record)"
		if s.Method == "provider" {
			method = "Electronic signature through provider: " + s.Provider
		}
		d.Field("Method", method)
		if s.ConsentText != "" {
			d.Field("Consent", s.ConsentText)
		}
		d.Field("IP address", s.IP)
		if s.UserAgent != "" {
			d.Field("Device", s.UserAgent)
		}
	}
	return d.Bytes()
}

func isSectionHeading(l string) bool {
	if len(l) < 4 || l[0] < '1' || l[0] > '9' || l[1] != '.' || l[2] != ' ' {
		return false
	}
	rest := l[3:]
	return rest == strings.ToUpper(rest)
}
