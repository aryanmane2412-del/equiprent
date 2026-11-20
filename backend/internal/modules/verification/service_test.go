package verification

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/fieldcrypt"
	"github.com/equiprent/backend/internal/httpx"
)

// newTestService has no repository: the tests exercise rules that are
// decided before any data access.
func newTestService(t *testing.T) *Service {
	t.Helper()
	c, err := fieldcrypt.New("ZGV2LW9ubHktZmllbGQta2V5LWV4YWN0bHktMzItYnk=", "test-hash-key")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	return NewService(nil, nil, nil, c, SandboxPayouts{}, Options{Now: func() time.Time { return now }})
}

func fieldOf(err error) string {
	var ae *apperror.Error
	if errors.As(err, &ae) && len(ae.Fields) > 0 {
		return ae.Fields[0].Field
	}
	return ""
}

func validBusiness() OwnerBusinessRequest {
	return OwnerBusinessRequest{
		AccountKind: "company", OrgType: "private_limited", LegalName: "Patil Earthmovers Pvt Ltd", BusinessName: "Patil Earthmovers",
		PAN: "ABCDE1234F", GSTIN: ptr("27ABCDE1234F1Z5"), CIN: ptr("U45200MH2015PTC123456"),
		ContactName: "Ravi", ContactPhone: "+919800000000", ContactEmail: "ravi@example.com",
		Address: Address{Line1: "Plot 7", City: "Pune", StateCode: "27", Pincode: "411026"},
	}
}

func TestBusinessDetailsValidation(t *testing.T) {
	s := newTestService(t)
	sc := httpx.Scope{P: &httpx.Principal{UserID: uuid.New()}, Org: uuid.New()}
	cases := map[string]struct {
		mut   func(*OwnerBusinessRequest)
		field string
	}{
		"individual with company type": {func(r *OwnerBusinessRequest) { r.AccountKind = "individual" }, "org_type"},
		"company with individual type": {func(r *OwnerBusinessRequest) { r.OrgType = "proprietorship" }, "org_type"},
		"malformed PAN":                {func(r *OwnerBusinessRequest) { r.PAN = "1234567890" }, "pan"},
		"malformed GSTIN":              {func(r *OwnerBusinessRequest) { r.GSTIN = ptr("27ABCDE1234F1X5") }, "gstin"},
		"GSTIN of another PAN":         {func(r *OwnerBusinessRequest) { r.GSTIN = ptr("27ZZZZZ9999Z1Z5") }, "gstin"},
		"limited company without CIN":  {func(r *OwnerBusinessRequest) { r.CIN = nil }, "cin"},
		"malformed CIN":                {func(r *OwnerBusinessRequest) { r.CIN = ptr("X45200MH2015PTC123456") }, "cin"},
		"malformed Udyam":              {func(r *OwnerBusinessRequest) { r.UdyamNumber = ptr("UDYAM-123") }, "udyam_number"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := validBusiness()
			tc.mut(&req)
			_, err := s.SaveBusiness(context.Background(), sc, req)
			if !errors.Is(err, apperror.ErrValidation) || fieldOf(err) != tc.field {
				t.Fatalf("err = %v (field %q), want validation on %s", err, fieldOf(err), tc.field)
			}
		})
	}
}

func TestOperatorProfileValidation(t *testing.T) {
	s := newTestService(t)
	p := &httpx.Principal{UserID: uuid.New()}
	base := OperatorProfileRequest{
		LicenceNumber: "MH1220150012345", LicenceClass: "HTV", LicenceExpiresOn: "2030-01-01", ExperienceYears: 5,
		Skills: []string{"excavator"}, Availability: Availability{Days: []string{"mon"}, Start: "08:00", End: "18:00"},
	}
	expired := base
	expired.LicenceExpiresOn = "2026-11-12"
	if _, err := s.SaveOperatorProfile(context.Background(), p, expired); fieldOf(err) != "licence_expires_on" {
		t.Fatalf("expired licence: %v", err)
	}
	backwards := base
	backwards.Availability.Start, backwards.Availability.End = "18:00", "08:00"
	if _, err := s.SaveOperatorProfile(context.Background(), p, backwards); fieldOf(err) != "availability.end" {
		t.Fatalf("availability order: %v", err)
	}
}

func TestReasonsRequired(t *testing.T) {
	s := newTestService(t)
	admin := &httpx.Principal{UserID: uuid.New()}
	if _, err := s.Reject(context.Background(), admin, uuid.New(), DecisionRequest{Reason: "  no "}); fieldOf(err) != "reason" {
		t.Fatalf("reject without reason: %v", err)
	}
	if _, err := s.ReviewDocument(context.Background(), admin, uuid.New(), uuid.New(), DocumentReviewRequest{Decision: "reject"}); fieldOf(err) != "reason" {
		t.Fatalf("document reject without reason: %v", err)
	}
}

func TestBankAccountFormat(t *testing.T) {
	s := newTestService(t)
	sc := httpx.Scope{P: &httpx.Principal{UserID: uuid.New()}, Org: uuid.New()}
	_, err := s.SetBankAccount(context.Background(), sc, BankAccountRequest{AccountHolderName: "A B", AccountNumber: "123456789", ConfirmAccountNumber: "123456789", IFSC: "HDFC1234567"})
	if fieldOf(err) != "ifsc" {
		t.Fatalf("bad IFSC: %v", err)
	}
}

func TestSandboxPayouts(t *testing.T) {
	ok, err := SandboxPayouts{}.VerifyAccount(context.Background(), PayoutAccount{AccountNumber: "50100123456789"})
	if err != nil || !ok.Verified || !strings.HasPrefix(ok.Reference, "fa_sbx_") {
		t.Fatalf("verify = %+v %v", ok, err)
	}
	bad, _ := SandboxPayouts{}.VerifyAccount(context.Background(), PayoutAccount{AccountNumber: "50100123450000"})
	if bad.Verified || bad.Reason == "" {
		t.Fatalf("failing account verified: %+v", bad)
	}
	if _, err := (UnconfiguredPayouts{}).VerifyAccount(context.Background(), PayoutAccount{}); !errors.Is(err, apperror.ErrServiceUnavailable) {
		t.Fatalf("unconfigured provider must refuse, got %v", err)
	}
}

func TestDocumentRules(t *testing.T) {
	s := newTestService(t)
	org, user := uuid.New(), uuid.New()
	owner, operator := ptr(ContextOwner), ptr(ContextOperator)
	doc := db.KycRecord{Context: owner, OrganizationID: uuid.NullUUID{UUID: org, Valid: true}}
	if !belongs(doc, ownerSubject(org)) || belongs(doc, ownerSubject(uuid.New())) || belongs(doc, operatorSubject(user)) {
		t.Fatal("owner document ownership")
	}
	opDoc := db.KycRecord{Context: operator, UserID: uuid.NullUUID{UUID: user, Valid: true}}
	if !belongs(opDoc, operatorSubject(user)) || belongs(opDoc, operatorSubject(uuid.New())) {
		t.Fatal("operator document ownership")
	}

	yesterday := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	docs := []db.KycRecord{
		{DocType: "pan", Status: db.VerificationStatusRejected},
		{DocType: "passport", Status: db.VerificationStatusSubmitted, ExpiresOn: &yesterday},
	}
	if s.hasDoc(docs, OwnerIdentityDocs) {
		t.Fatal("rejected and expired documents must not satisfy a check")
	}
	docs = append(docs, db.KycRecord{DocType: "voter_id", Status: db.VerificationStatusSubmitted})
	if !s.hasDoc(docs, OwnerIdentityDocs) || s.hasDoc(docs, OwnerBusinessDocs) {
		t.Fatal("document kinds")
	}

	for st, want := range map[string]bool{StatusDraft: true, StatusRejected: true, StatusSubmitted: false, StatusUnderReview: false, StatusApproved: false} {
		if editable(st) != want {
			t.Errorf("editable(%s) = %v", st, !want)
		}
	}
}
