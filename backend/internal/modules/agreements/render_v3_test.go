package agreements

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/db"
)

func sampleTemplate(reviewed bool) db.LegalTemplate {
	body, _ := json.Marshal(LegalBody{
		Preamble:            "Agreement on EQUIPRENT between the parties below.",
		ConditionClauses:    []string{"CONDITION-CLAUSE handover checklist binds."},
		UseClauses:          []string{"USE-CLAUSE site only."},
		DamageClauses:       []string{"DAMAGE-CLAUSE customer pays beyond wear."},
		DepositClauses:      []string{"DEPOSIT-CLAUSE refunded after return."},
		GeneralTerms:        []string{"GENERAL-CLAUSE laws of India."},
		AcceptanceStatement: "ACCEPTANCE-STATEMENT not a digital signature.",
		ConsentText:         "I accept.",
	})
	t := db.LegalTemplate{ID: uuid.New(), Kind: "rental_agreement", Version: "rental-terms-test.1", Title: "Test terms",
		Jurisdiction: "IN", Body: body, Status: "published"}
	if reviewed {
		name, cap := "A. Advocate", "Advocate"
		at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
		t.ReviewedByName, t.ReviewerCapacity, t.ReviewedAt = &name, &cap, &at
	}
	return t
}

func v3Snapshot(t *testing.T, reviewed bool) Snapshot {
	t.Helper()
	terms, err := TermsFromTemplate(sampleTemplate(reviewed))
	if err != nil {
		t.Fatal(err)
	}
	c := sampleContext()
	c.EquipmentCondition, c.CurrentHmr, c.EquipmentID = "good", 4210.5, uuid.MustParse("01900000-0000-7000-8000-000000000001")
	return BuildSnapshotV3(c, terms)
}

// Every element the agreement must state is in the v3 text, and the clauses
// come from the template (configuration), not from code.
func TestRenderV3CoversEveryRequiredSection(t *testing.T) {
	snap := v3Snapshot(t, false)
	text := RenderV3(snap)
	for _, want := range []string{
		"1. PARTIES", "Owner (lessor): " + snap.Owner.LegalName, "Customer (lessee): " + snap.Customer.LegalName,
		"2. EQUIPMENT AND IDENTIFICATION", "Registration number:", "Serial / chassis number:", "EQUIPRENT listing ID: 01900000",
		"3. RENTAL PERIOD AND DURATION", "Start:", "End:", "Duration: 3 days (72 hours in total)",
		"4. SITE AND DELIVERY", "Delivery:", "Return:",
		"5. CHARGES", "Rental charges:", "Estimated total including taxes:",
		"6. SECURITY DEPOSIT", "DEPOSIT-CLAUSE",
		"7. EQUIPMENT CONDITION", "Condition declared by the owner when booked: good", "Hour meter when booked: 4210.5", "CONDITION-CLAUSE",
		"8. CANCELLATION POLICY", "9. DAMAGE RESPONSIBILITY", "DAMAGE-CLAUSE",
		"USE OF THE EQUIPMENT", "USE-CLAUSE", "TERMS AND CONDITIONS", "GENERAL-CLAUSE",
		"ACCEPTANCE AND SIGNATURE", "ACCEPTANCE-STATEMENT", "Terms version: rental-terms-test.1",
		"Legal review of these terms: not recorded",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("v3 text missing %q", want)
		}
	}
	if strings.Contains(text, "signs this agreement electronically") {
		t.Fatal("v3 must not describe an acceptance as an electronic signature")
	}
	reviewed := RenderV3(v3Snapshot(t, true))
	if !strings.Contains(reviewed, "Legal review of these terms: A. Advocate (Advocate) on 2026-11-01") {
		t.Fatalf("reviewed template should be credited:\n%s", reviewed[len(reviewed)-300:])
	}
}

// The snapshot carries the template body, so the text (and its hash)
// reproduces exactly after a JSON round trip, as when loaded from the DB.
func TestRenderV3IsReproducible(t *testing.T) {
	snap := v3Snapshot(t, true)
	raw, _ := json.Marshal(snap)
	var back Snapshot
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(Hash(RenderV3(snap)), Hash(Render(TemplateV3, back))) {
		t.Fatal("v3 text must reproduce from the stored snapshot")
	}
	changed := back
	legal := *back.Legal // deep enough: replace the clause slice
	legal.Body.DamageClauses = []string{"other wording"}
	changed.Legal = &legal
	if bytes.Equal(Hash(RenderV3(changed)), Hash(RenderV3(back))) {
		t.Fatal("changing a clause must change the fingerprint")
	}
}

func TestLegalBodyValidate(t *testing.T) {
	var full LegalBody
	_ = json.Unmarshal(sampleTemplate(false).Body, &full)
	if miss := full.Validate(); len(miss) != 0 {
		t.Fatalf("complete body reported missing %v", miss)
	}
	empty := LegalBody{GeneralTerms: []string{" "}}
	miss := strings.Join(empty.Validate(), ",")
	for _, want := range []string{"preamble", "damage_clauses", "consent_text", "empty clause"} {
		if !strings.Contains(miss, want) {
			t.Errorf("missing %q in %s", want, miss)
		}
	}
}

func TestDuration(t *testing.T) {
	start := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	cases := map[time.Duration]string{
		72 * time.Hour:                "3 days (72 hours in total)",
		26*time.Hour + 30*time.Minute: "1 day 2 hours 30 minutes (26 hours in total)",
		5 * time.Hour:                 "5 hours (5 hours in total)",
	}
	for d, want := range cases {
		if got := Duration(start, start.Add(d)); got != want {
			t.Errorf("Duration(%v) = %q, want %q", d, got, want)
		}
	}
}

// Superseded and draft copies are watermarked; the version is in the title.
func TestBuildPDFVersions(t *testing.T) {
	snap := v3Snapshot(t, false)
	terms := RenderV3(snap)
	in := PDFInput{AgreementID: uuid.NewString(), Reference: snap.Reference, Terms: terms, TermsSHA256: Hash(terms),
		CreatedAt: time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC), Version: 2, Superseded: true}
	pdf := BuildPDF(in)
	if !bytes.Contains(pdf, []byte("(SUPERSEDED)")) || !bytes.Contains(pdf, []byte("version 2")) {
		t.Fatal("superseded PDF should carry the watermark and version")
	}
	if !bytes.Contains(pdf, []byte("7. EQUIPMENT CONDITION")) {
		t.Fatal("v3 sections should be in the PDF")
	}
}
