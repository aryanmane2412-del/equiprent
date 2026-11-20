package agreements

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/modules/bookings"
)

func sampleBooking() bookings.Booking {
	start := time.Date(2026, 11, 20, 2, 30, 0, 0, time.UTC)
	return bookings.Booking{
		Reference: "ER-2611-4X7K", OwnerName: "Patil Earthmovers", CustomerName: "Shree Builders",
		EquipmentTitle: "EX200", WithOperator: true, Period: bookings.Period{Start: start, End: start.Add(72 * time.Hour)},
		PricingUnit: "day", EstimatedTotal: 3885504, DepositPaise: 1000000,
	}
}

// Agreements created with rental-v1 must keep producing the same text and
// hash, or they could no longer be verified or signed.
func TestRenderV1Unchanged(t *testing.T) {
	text, err := RenderV1(sampleBooking())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"(rental-v1)", "ER-2611-4X7K", "Patil Earthmovers", "Shree Builders", "INR 38855.04", "INR 10000.00",
		"with operator", "2026-11-20T02:30:00Z", "breakdown hours are not billed"} {
		if !strings.Contains(text, want) {
			t.Errorf("v1 terms missing %q", want)
		}
	}
}

func sampleContext() db.AgreementContextRow {
	gst, phone, reg := "27ABCDE1234F1Z5", "+919800000001", "MH12AB1234"
	year := int16(2021)
	late := int64(150000)
	price, _ := json.Marshal(bookings.Price{
		SubtotalPaise: 3300000, GSTPaise: 594000, AdvanceDuePaise: 1947000, Notes: []string{"Diesel billed at actuals"},
		Charges: []bookings.ChargeLine{{Label: "Rental", Detail: "3 days × ₹10,000.00", Paise: 3000000}, {Label: "Delivery to site", Paise: 300000}},
		Taxes:   []bookings.TaxLine{{Name: "CGST", RateBps: 900, Paise: 297000}, {Name: "SGST", RateBps: 900, Paise: 297000}},
	})
	return db.AgreementContextRow{
		Reference: "ER-2611-4X7K", WithOperator: true, PricingUnit: "day",
		StartsAt: time.Date(2026, 11, 20, 2, 30, 0, 0, time.UTC), EndsAt: time.Date(2026, 11, 23, 2, 30, 0, 0, time.UTC),
		PriceSnapshot: price, EstimatedTotalPaise: 3894000, DepositPaise: 1000000, PaymentMode: "online",
		WorkDescription: "Foundation excavation", Delivery: true, Pickup: true,
		OwnerLegalName: "Patil Earthmovers Pvt Ltd", OwnerDisplayName: "Patil Earthmovers", OwnerGstin: &gst, OwnerContactPhone: &phone,
		OwnerAddress: "Plot 4, MIDC, Pune 411019", CustomerLegalName: "Shree Builders LLP", CustomerDisplayName: "Shree Builders",
		SiteAddress: "Hinjewadi site: Plot 12, Pune 411057", EquipmentTitle: "Tata Hitachi EX200", RegistrationNo: &reg,
		ManufactureYear: &year, EquipmentTimezone: "Asia/Kolkata", FuelPolicy: "customer", LateFeePerHourPaise: &late, LateGraceMinutes: 30,
		ModelName: "EX200", MakeName: "Tata Hitachi", CategoryName: "Excavator", CancellationPolicyName: "Standard",
		CancellationRules: json.RawMessage(`[{"hours_before": 0, "fee_bps": 2500}, {"hours_before": 24, "fee_bps": 0}]`),
	}
}

func TestRenderV2CoversEveryRequiredDetail(t *testing.T) {
	snap := BuildSnapshot(sampleContext())
	text := RenderV2(snap)
	for _, want := range []string{
		// parties
		"Owner (lessor): Patil Earthmovers Pvt Ltd (trading as Patil Earthmovers)", "GSTIN: 27ABCDE1234F1Z5", "Customer (lessee): Shree Builders LLP",
		// equipment
		"Machine: Tata Hitachi EX200", "Make and model: Tata Hitachi EX200 (Excavator)", "Registration number: MH12AB1234", "Year of manufacture: 2021",
		"with operator (wet hire)", "fuel is supplied by the customer",
		// dates in IST and UTC
		"From: Fri 20 Nov 2026, 08:00 IST", "To: Mon 23 Nov 2026, 08:00 IST", "2026-11-20T02:30:00Z",
		// pricing and terms
		"Rental (3 days × ₹10,000.00): ₹30,000.00", "CGST @ 9%: ₹2,970.00", "Estimated total including taxes: ₹38,940.00",
		"Refundable security deposit: ₹10,000.00", "Advance due on confirmation: ₹19,470.00", "Late return: ₹1,500.00 per hour",
		"Cancelling at least 24 hours before the start: no fee.", "Cancelling after that, up to the start: a fee of 25% of the estimated total.",
		"6. TERMS AND CONDITIONS", "governed by the laws of India", "7. ELECTRONIC SIGNATURE",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("terms missing %q", want)
		}
	}
	// Rendering from the stored snapshot gives the same text (and hash).
	raw, _ := json.Marshal(snap)
	var back Snapshot
	if err := json.Unmarshal(raw, &back); err != nil || RenderV2(back) != text {
		t.Fatal("snapshot round trip changed the text")
	}
	changed := snap
	changed.TotalPaise++
	if bytes.Equal(Hash(RenderV2(changed)), Hash(text)) {
		t.Fatal("a price change must change the hash")
	}
}

func TestBuildPDF(t *testing.T) {
	snap := BuildSnapshot(sampleContext())
	terms := RenderV2(snap)
	in := PDFInput{AgreementID: uuid.NewString(), Reference: snap.Reference, Terms: terms, TermsSHA256: Hash(terms),
		CreatedAt: time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)}
	draft := BuildPDF(in)
	if !bytes.HasPrefix(draft, []byte("%PDF-")) || !bytes.Contains(draft, []byte("(DRAFT)")) || bytes.Contains(draft, []byte("SIGNATURE CERTIFICATE")) {
		t.Fatal("draft PDF")
	}
	if !bytes.Contains(draft, []byte("Rs.38,940.00")) {
		t.Fatal("the rupee sign should be spelled Rs. in the PDF")
	}
	ip := netip.MustParseAddr("203.0.113.77")
	in.Executed = true
	in.Signatures = []SignatureView{
		{Party: "customer", SignerName: "Asha Rao", Method: "typed_name", SignedAt: in.CreatedAt, IP: maskIP(&ip)},
		{Party: "owner", SignerName: "Ravi Patil", Method: "provider", Provider: "sandbox", SignedAt: in.CreatedAt.Add(time.Hour), IP: "-"},
	}
	exec := BuildPDF(in)
	for _, want := range []string{"ACCEPTANCE RECORD", "Asha Rao", "Ravi Patil", "203.0.113.x", "Typed-name acceptance", "Electronic signature through"} {
		if !bytes.Contains(exec, []byte(want)) {
			t.Errorf("executed PDF missing %q", want)
		}
	}
	if bytes.Contains(exec, []byte("203.0.113.77")) || bytes.Contains(exec, []byte("(DRAFT)")) {
		t.Fatal("executed PDF must mask IPs and carry no watermark")
	}
	if !bytes.Equal(exec, BuildPDF(in)) {
		t.Fatal("PDF output must be deterministic")
	}
}

func TestPartyRules(t *testing.T) {
	cust, own := uuid.New(), uuid.New()
	b := bookings.Booking{CustomerOrgID: cust, OwnerOrgID: own}
	p := func(org uuid.UUID, role string) *httpx.Principal {
		return &httpx.Principal{UserID: uuid.New(), Roles: []httpx.Role{{OrgID: org, Code: role}}}
	}
	cases := []struct {
		p    *httpx.Principal
		side bookings.Side
		want string
	}{
		{p(cust, httpx.RoleCustomer), bookings.SideCustomer, "customer"},
		{p(cust, httpx.RoleOrgAdmin), bookings.SideCustomer, "customer"},
		{p(own, httpx.RoleOwner), bookings.SideOwner, "owner"},
		{p(own, httpx.RoleFleetManager), bookings.SideOwner, ""},
		{p(own, httpx.RoleOperator), bookings.SideOperator, ""},
	}
	for i, c := range cases {
		if got := party(c.p, b, c.side); got != c.want {
			t.Errorf("case %d: party = %q, want %q", i, got, c.want)
		}
	}
}

func TestSandboxProvider(t *testing.T) {
	sb := Sandbox{Secret: "test-secret"}
	res, err := sb.Begin(t.Context(), BeginRequest{SessionID: uuid.New()})
	if err != nil || !strings.HasPrefix(res.ProviderReference, "sbx_") || res.Sandbox == nil {
		t.Fatalf("begin = %+v %v", res, err)
	}
	h := http.Header{}
	h.Set("X-ESign-Signature", res.Sandbox.Signature)
	ev, err := sb.Webhook(h, res.Sandbox.Body)
	if err != nil || ev.Status != "signed" || ev.ProviderReference != res.ProviderReference {
		t.Fatalf("webhook = %+v %v", ev, err)
	}
	tampered := bytes.Replace(res.Sandbox.Body, []byte("signed"), []byte("expired"), 1)
	if _, err := sb.Webhook(h, tampered); err == nil {
		t.Fatal("a tampered body must be refused")
	}
	if _, err := (Builtin{}).Webhook(h, res.Sandbox.Body); err == nil {
		t.Fatal("the built-in provider has no webhooks")
	}
}

func TestINRAndCancellation(t *testing.T) {
	for paise, want := range map[int64]string{0: "₹0.00", 5: "₹0.05", 3885504: "₹38,855.04", 12345678901: "₹12,34,56,789.01"} {
		if got := INR(paise); got != want {
			t.Errorf("INR(%d) = %s, want %s", paise, got, want)
		}
	}
	if got := cancellationTerms(nil); len(got) != 1 {
		t.Fatalf("no rules = %v", got)
	}
	if got := maskIP(nil); got != "-" {
		t.Fatal(got)
	}
	v6 := netip.MustParseAddr("2001:db8:1234:5678::1")
	if got := maskIP(&v6); got != "2001:db8:1234::/48" {
		t.Fatalf("v6 mask = %s", got)
	}
}
