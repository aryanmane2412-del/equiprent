package verification

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/events"
	"github.com/equiprent/backend/internal/fieldcrypt"
	"github.com/equiprent/backend/internal/filecheck"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/storage"
)

// Uploader is the part of the uploads service verification needs.
type Uploader interface {
	Claim(ctx context.Context, userID, uploadID uuid.UUID, purposes ...filecheck.Purpose) (db.Upload, error)
	SignedURL(key string, ttl time.Duration) storage.SignedURL
}

// Options configures the service.
type Options struct {
	URLTTL       time.Duration // document links
	AvatarURLTTL time.Duration
	Now          func() time.Time
}

// Service implements verification.
type Service struct {
	repo    Repository
	tx      database.Transactor
	uploads Uploader
	crypt   *fieldcrypt.Crypter
	payouts PayoutProvider
	opt     Options
}

func NewService(repo Repository, tx database.Transactor, uploads Uploader, crypt *fieldcrypt.Crypter, payouts PayoutProvider, opt Options) *Service {
	if opt.URLTTL <= 0 {
		opt.URLTTL = 5 * time.Minute
	}
	if opt.AvatarURLTTL <= 0 {
		opt.AvatarURLTTL = time.Hour
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &Service{repo: repo, tx: tx, uploads: uploads, crypt: crypt, payouts: payouts, opt: opt}
}

// Admin roles allowed to review verification.
var ReviewerRoles = []string{httpx.RoleAdminKYC}

var (
	panPattern   = regexp.MustCompile(`^[A-Z]{5}[0-9]{4}[A-Z]$`)
	gstinPattern = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$`)
	cinPattern   = regexp.MustCompile(`^[LU][0-9]{5}[A-Z]{2}[0-9]{4}[A-Z]{3}[0-9]{6}$`)
	ifscPattern  = regexp.MustCompile(`^[A-Z]{4}0[A-Z0-9]{6}$`)
	udyamPattern = regexp.MustCompile(`^UDYAM-[A-Z]{2}-[0-9]{2}-[0-9]{7}$`)
)

func field(name, code, msg string) *apperror.Error {
	return apperror.ErrValidation.WithFields(apperror.FieldError{Field: name, Code: code, Message: msg})
}

var errLocked = apperror.ErrInvalidTransition.WithDetail("verification is submitted or approved; it can only be changed after a rejection")

// ---- shared ------------------------------------------------------------------------------

func editable(status string) bool { return status == StatusDraft || status == StatusRejected }

func hasOperatorRole(p *httpx.Principal) bool {
	for _, role := range p.Roles {
		if role.Code == httpx.RoleOperator {
			return true
		}
	}
	return false
}

func toApplication(a db.VerificationApplication) Application {
	return Application{ID: a.ID, Role: a.Role, Status: string(a.Status), SubmittedAt: a.SubmittedAt, ReviewedAt: a.ReviewedAt, RejectionReason: a.RejectionReason}
}

func toDocument(d db.KycRecord) Document {
	return Document{
		ID: d.ID, DocType: string(d.DocType), Status: string(d.Status), NumberLast4: d.NumberLast4, ContentType: d.ContentType,
		ExpiresOn: dateString(d.ExpiresOn), ReviewNote: d.ReviewNote, ReviewedAt: d.ReviewedAt, CreatedAt: d.CreatedAt,
	}
}

func toDocuments(ds []db.KycRecord) []Document {
	out := make([]Document, 0, len(ds))
	for _, d := range ds {
		out = append(out, toDocument(d))
	}
	return out
}

// hasDoc reports whether a usable (not rejected, not expired) document of
// one of kinds exists.
func (s *Service) hasDoc(ds []db.KycRecord, kinds []string) bool {
	today := s.opt.Now().Truncate(24 * time.Hour)
	return slices.ContainsFunc(ds, func(d db.KycRecord) bool {
		if !slices.Contains(kinds, string(d.DocType)) {
			return false
		}
		if d.Status == db.VerificationStatusRejected || d.Status == db.VerificationStatusExpired {
			return false
		}
		return d.ExpiresOn == nil || !d.ExpiresOn.Before(today)
	})
}

func allDone(cs []Check) bool {
	return !slices.ContainsFunc(cs, func(c Check) bool { return !c.Done })
}

func missing(cs []Check) *apperror.Error {
	var fe []apperror.FieldError
	for _, c := range cs {
		if !c.Done {
			fe = append(fe, apperror.FieldError{Field: c.Key, Code: "incomplete", Message: c.Label})
		}
	}
	return apperror.ErrBusinessRule.WithDetail("complete every step before submitting").WithFields(fe...)
}

func parseDate(name string, v *string) (*time.Time, error) {
	if v == nil || *v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, *v)
	if err != nil {
		return nil, field(name, "date", "use YYYY-MM-DD")
	}
	return &t, nil
}

func trimPtr(v *string) *string {
	if v == nil {
		return nil
	}
	t := strings.ToUpper(strings.TrimSpace(*v))
	if t == "" {
		return nil
	}
	return &t
}

// addDocument stores a document for subject after claiming the upload.
func (s *Service) addDocument(ctx context.Context, actor uuid.UUID, subj Subject, req DocumentRequest, allowed []string, purposes ...filecheck.Purpose) (Document, error) {
	if !slices.Contains(allowed, req.DocType) {
		return Document{}, field("doc_type", "oneof", "this document type is not accepted here")
	}
	expires, err := parseDate("expires_on", req.ExpiresOn)
	if err != nil {
		return Document{}, err
	}
	if expires != nil && expires.Before(s.opt.Now().Truncate(24*time.Hour)) {
		return Document{}, field("expires_on", "expired", "this document has expired")
	}
	var out Document
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		app, err := s.repo.EnsureApplication(ctx, subj)
		if err != nil {
			return err
		}
		if !editable(string(app.Status)) {
			return errLocked
		}
		up, err := s.uploads.Claim(ctx, actor, req.UploadID, purposes...)
		if err != nil {
			return err
		}
		p := db.CreateDocumentParams{
			OrganizationID: subj.org(), UserID: subj.user(), DocType: db.KycDocumentType(req.DocType),
			DocumentObjectKey: &up.ObjectKey, DocumentSha256: up.Sha256, ContentType: &up.ContentType,
			UploadID: nullable(up.ID), Context: ptr(subj.Role), ExpiresOn: expires,
		}
		if req.DocumentNumber != nil && strings.TrimSpace(*req.DocumentNumber) != "" {
			sealed, err := s.crypt.Seal("kyc."+req.DocType, *req.DocumentNumber)
			if err != nil {
				return err
			}
			p.NumberEnc, p.NumberHash, p.NumberLast4 = sealed.Enc, sealed.Hash, &sealed.Last4
		}
		d, err := s.repo.CreateDocument(ctx, p)
		out = toDocument(d)
		return err
	})
	return out, err
}

// belongs reports whether document d is part of subject's verification.
func belongs(d db.KycRecord, subj Subject) bool {
	if d.Context == nil || *d.Context != subj.Role {
		return false
	}
	if subj.Role == ContextOwner {
		return d.OrganizationID.Valid && d.OrganizationID.UUID == subj.OrgID
	}
	return d.UserID.Valid && d.UserID.UUID == subj.UserID
}

func (s *Service) deleteDocument(ctx context.Context, subj Subject, id uuid.UUID) error {
	return s.tx.InTx(ctx, func(ctx context.Context) error {
		d, err := s.repo.Document(ctx, id)
		if err != nil || !belongs(d, subj) {
			return apperror.ErrNotFound
		}
		app, err := s.repo.EnsureApplication(ctx, subj)
		if err != nil {
			return err
		}
		if !editable(string(app.Status)) {
			return errLocked
		}
		if d.Status != db.VerificationStatusSubmitted && d.Status != db.VerificationStatusPending {
			// Reviewed documents stay as evidence in the review history.
			return apperror.ErrInvalidTransition.WithDetail("reviewed documents are kept in the review history; upload a replacement instead")
		}
		ok, err := s.repo.DeleteDocument(ctx, id)
		if err != nil {
			return err
		}
		if !ok {
			return apperror.ErrInvalidTransition.WithDetail("verified documents cannot be removed")
		}
		return nil
	})
}

func (s *Service) submit(ctx context.Context, actor uuid.UUID, subj Subject, checks func(context.Context) ([]Check, error)) (Application, error) {
	var out Application
	err := s.tx.InTxAs(ctx, database.Actor{UserID: actor, Role: subj.Role, Reason: "verification submitted"}, func(ctx context.Context) error {
		app, err := s.repo.EnsureApplication(ctx, subj)
		if err != nil {
			return err
		}
		if !editable(string(app.Status)) {
			return apperror.ErrInvalidTransition.WithDetail("already " + strings.ReplaceAll(string(app.Status), "_", " "))
		}
		cs, err := checks(ctx)
		if err != nil {
			return err
		}
		if !allDone(cs) {
			return missing(cs)
		}
		a, err := s.repo.SubmitApplication(ctx, app.ID, actor)
		if err != nil {
			return transition(err)
		}
		if subj.Role == ContextOwner {
			if err := s.repo.SetOrgKYC(ctx, subj.OrgID, string(db.VerificationStatusSubmitted)); err != nil {
				return err
			}
		}
		out = toApplication(a)
		if err := s.repo.AddEvent(ctx, a.ID, actor, "submitted", uuid.NullUUID{}, nil); err != nil {
			return err
		}
		what := "A driver submitted verification documents."
		if subj.Role == ContextOwner {
			what = "An equipment owner submitted business verification."
		}
		return s.repo.Emit(ctx, a.ID, events.VerificationSubmitted, events.Payload{ActorUserID: actor, Title: what})
	})
	return out, err
}

func transition(err error) error {
	if isNotFound(err) {
		return apperror.ErrInvalidTransition
	}
	return err
}

// ---- customer ----------------------------------------------------------------------------

// CustomerOrg is the organisation a customer buys for (the acting one, else
// the first where they are a customer).
func CustomerOrg(p *httpx.Principal) uuid.UUID {
	if p.HasOrgRole(p.ActingOrg, httpx.RoleCustomer) {
		return p.ActingOrg
	}
	for _, o := range p.Orgs() {
		if p.HasOrgRole(o, httpx.RoleCustomer) {
			return o
		}
	}
	return uuid.Nil
}

func (s *Service) Customer(ctx context.Context, p *httpx.Principal) (CustomerStatus, error) {
	u, err := s.repo.User(ctx, p.UserID)
	if err != nil {
		return CustomerStatus{}, err
	}
	addresses := 0
	if org := CustomerOrg(p); org != uuid.Nil {
		if addresses, err = s.repo.AddressCount(ctx, org); err != nil {
			return CustomerStatus{}, err
		}
	}
	out := CustomerStatus{Checks: []Check{
		{Key: "personal_details", Label: "Add your full name", Done: len(strings.TrimSpace(u.FullName)) >= 2},
		{Key: "mobile", Label: "Verify your mobile number", Done: u.PhoneVerifiedAt != nil},
		{Key: "email", Label: "Verify your email address", Done: u.Email != nil && u.EmailVerifiedAt != nil},
		{Key: "address", Label: "Add an address", Done: addresses > 0},
		{Key: "profile_image", Label: "Add a profile image", Done: u.AvatarObjectKey != nil},
	}}
	out.Complete = allDone(out.Checks)
	if u.AvatarObjectKey != nil {
		url := s.uploads.SignedURL(*u.AvatarObjectKey, s.opt.AvatarURLTTL)
		out.AvatarURL = &url
	}
	return out, nil
}

// AvatarURL returns a signed link to a user's avatar, if any.
func (s *Service) AvatarURL(key *string) *storage.SignedURL {
	if key == nil {
		return nil
	}
	u := s.uploads.SignedURL(*key, s.opt.AvatarURLTTL)
	return &u
}

func (s *Service) SetAvatar(ctx context.Context, p *httpx.Principal, req AvatarRequest) (CustomerStatus, error) {
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		up, err := s.uploads.Claim(ctx, p.UserID, req.UploadID, filecheck.Avatar)
		if err != nil {
			return err
		}
		return s.repo.SetAvatar(ctx, p.UserID, up.ObjectKey)
	})
	if err != nil {
		return CustomerStatus{}, err
	}
	return s.Customer(ctx, p)
}

// ---- owner -------------------------------------------------------------------------------

func ownerSubject(org uuid.UUID) Subject { return Subject{Role: ContextOwner, OrgID: org} }

func accountKind(t db.OrgType) string {
	if t == db.OrgTypeIndividual || t == db.OrgTypeProprietorship {
		return "individual"
	}
	return "company"
}

func (s *Service) Owner(ctx context.Context, org uuid.UUID) (OwnerVerification, error) {
	subj := ownerSubject(org)
	app, err := s.repo.EnsureApplication(ctx, subj)
	if err != nil {
		return OwnerVerification{}, err
	}
	return s.ownerView(ctx, org, app)
}

func (s *Service) ownerView(ctx context.Context, org uuid.UUID, app db.VerificationApplication) (OwnerVerification, error) {
	o, err := s.repo.Organization(ctx, org)
	if err != nil {
		return OwnerVerification{}, err
	}
	docs, err := s.repo.Documents(ctx, ownerSubject(org))
	if err != nil {
		return OwnerVerification{}, err
	}
	bank, err := s.repo.BankAccount(ctx, org)
	if err != nil {
		return OwnerVerification{}, err
	}
	v := OwnerVerification{
		OrganizationID: o.ID, AccountKind: accountKind(o.OrgType), OrgType: string(o.OrgType), LegalName: o.LegalName,
		BusinessName: o.DisplayName, PANLast4: o.PanLast4, GSTIN: o.Gstin, UdyamNumber: o.UdyamNumber, CIN: o.Cin,
		ContactName: o.ContactName, ContactPhone: o.ContactPhone, ContactEmail: o.ContactEmail,
		KYCStatus: string(o.KycStatus), Application: toApplication(app), Documents: toDocuments(docs),
		Editable: editable(string(app.Status)),
	}
	if o.BillingAddressID.Valid {
		a, err := s.repo.Address(ctx, o.BillingAddressID.UUID)
		if err != nil && !isNotFound(err) {
			return OwnerVerification{}, err
		}
		if err == nil {
			v.BusinessAddress = &Address{Label: a.Label, Line1: a.Line1, Line2: a.Line2, Locality: a.Locality, City: a.City, District: a.District, StateCode: a.StateCode, Pincode: a.Pincode}
		}
	}
	if bank != nil {
		v.BankAccount = &BankAccount{ID: bank.ID, AccountHolderName: bank.AccountHolderName, AccountLast4: bank.AccountLast4, IFSC: bank.Ifsc,
			VerificationStatus: string(bank.VerificationStatus), ProviderReference: bank.GatewayLinkedAccountID, VerifiedAt: bank.VerifiedAt}
	}
	company := v.AccountKind == "company"
	v.Checks = []Check{
		{Key: "business_details", Label: "Business and contact details", Done: o.PanLast4 != nil && o.ContactPhone != nil && o.ContactName != nil},
		{Key: "business_address", Label: "Business address", Done: v.BusinessAddress != nil},
		{Key: "identity_document", Label: "Identity document (PAN, passport, voter ID or driving licence)", Done: s.hasDoc(docs, OwnerIdentityDocs)},
		{Key: "business_document", Label: "Business document (GST, registration, Udyam, CIN or address proof)", Done: s.hasDoc(docs, OwnerBusinessDocs) || !company},
		{Key: "bank_account", Label: "Payout bank account verified by the payment provider", Done: bank != nil && bank.VerificationStatus == db.VerificationStatusVerified},
	}
	return v, nil
}

func (s *Service) SaveBusiness(ctx context.Context, sc httpx.Scope, req OwnerBusinessRequest) (OwnerVerification, error) {
	individualTypes := []string{"individual", "proprietorship"}
	if (req.AccountKind == "individual") != slices.Contains(individualTypes, req.OrgType) {
		return OwnerVerification{}, field("org_type", "mismatch", "individual accounts are individual or proprietorship; companies use a company type")
	}
	pan := fieldcrypt.Normalize(req.PAN)
	if !panPattern.MatchString(pan) {
		return OwnerVerification{}, field("pan", "format", "is not a valid PAN")
	}
	gstin, udyam, cin := trimPtr(req.GSTIN), trimPtr(req.UdyamNumber), trimPtr(req.CIN)
	var gstState *string
	if gstin != nil {
		if !gstinPattern.MatchString(*gstin) {
			return OwnerVerification{}, field("gstin", "format", "is not a valid GSTIN")
		}
		if (*gstin)[2:12] != pan {
			return OwnerVerification{}, field("gstin", "pan_mismatch", "does not belong to this PAN")
		}
		gstState = ptr((*gstin)[:2])
	}
	if udyam != nil && !udyamPattern.MatchString(*udyam) {
		return OwnerVerification{}, field("udyam_number", "format", "use the UDYAM-XX-00-0000000 format")
	}
	if cin != nil && !cinPattern.MatchString(*cin) {
		return OwnerVerification{}, field("cin", "format", "is not a valid CIN")
	}
	if (req.OrgType == "private_limited" || req.OrgType == "public_limited") && cin == nil {
		return OwnerVerification{}, field("cin", "required", "is required for limited companies")
	}
	sealed, err := s.crypt.Seal("org.pan", pan)
	if err != nil {
		return OwnerVerification{}, err
	}
	var app db.VerificationApplication
	err = s.tx.InTxAs(ctx, database.Actor{UserID: sc.P.UserID, Role: "owner", Reason: "business details"}, func(ctx context.Context) error {
		app, err = s.repo.EnsureApplication(ctx, ownerSubject(sc.Org))
		if err != nil {
			return err
		}
		if !editable(string(app.Status)) {
			return errLocked
		}
		addr, err := s.repo.CreateAddress(ctx, sc.Org, req.Address)
		if err != nil {
			return err
		}
		_, err = s.repo.UpdateBusiness(ctx, db.UpdateOwnerBusinessParams{
			ID: sc.Org, LegalName: strings.TrimSpace(req.LegalName), DisplayName: strings.TrimSpace(req.BusinessName), OrgType: db.OrgType(req.OrgType),
			Gstin: gstin, GstStateCode: gstState, PanEnc: sealed.Enc, PanHash: sealed.Hash, PanLast4: &sealed.Last4,
			UdyamNumber: udyam, Cin: cin, ContactName: ptr(strings.TrimSpace(req.ContactName)), ContactPhone: &req.ContactPhone,
			ContactEmail: strings.ToLower(strings.TrimSpace(req.ContactEmail)), BillingAddressID: nullable(addr),
		})
		return err
	})
	if err != nil {
		return OwnerVerification{}, err
	}
	return s.ownerView(ctx, sc.Org, app)
}

func (s *Service) AddOwnerDocument(ctx context.Context, sc httpx.Scope, req DocumentRequest) (Document, error) {
	allowed := slices.Concat(OwnerIdentityDocs, OwnerBusinessDocs)
	purposes := []filecheck.Purpose{filecheck.IdentityDocument, filecheck.BusinessDocument}
	return s.addDocument(ctx, sc.P.UserID, ownerSubject(sc.Org), req, allowed, purposes...)
}

func (s *Service) DeleteOwnerDocument(ctx context.Context, sc httpx.Scope, id uuid.UUID) error {
	return s.deleteDocument(ctx, ownerSubject(sc.Org), id)
}

// SetBankAccount verifies the account with the payment provider and makes it
// the payout account. Unverified accounts are not stored.
func (s *Service) SetBankAccount(ctx context.Context, sc httpx.Scope, req BankAccountRequest) (BankAccount, error) {
	ifsc := strings.ToUpper(strings.TrimSpace(req.IFSC))
	if !ifscPattern.MatchString(ifsc) {
		return BankAccount{}, field("ifsc", "format", "is not a valid IFSC")
	}
	app, err := s.repo.EnsureApplication(ctx, ownerSubject(sc.Org))
	if err != nil {
		return BankAccount{}, err
	}
	if !editable(string(app.Status)) {
		return BankAccount{}, errLocked
	}
	res, err := s.payouts.VerifyAccount(ctx, PayoutAccount{HolderName: req.AccountHolderName, AccountNumber: req.AccountNumber, IFSC: ifsc})
	if err != nil {
		return BankAccount{}, err
	}
	if !res.Verified {
		return BankAccount{}, apperror.ErrBusinessRule.WithDetail("the payment provider could not verify this account: " + res.Reason)
	}
	sealed, err := s.crypt.Seal("bank.account_number", req.AccountNumber)
	if err != nil {
		return BankAccount{}, err
	}
	now := s.opt.Now()
	var row db.InsertBankAccountRow
	err = s.tx.InTxAs(ctx, database.Actor{UserID: sc.P.UserID, Role: "owner", Reason: "payout account"}, func(ctx context.Context) error {
		row, err = s.repo.ReplaceBankAccount(ctx, db.InsertBankAccountParams{
			OrganizationID: sc.Org, AccountHolderName: strings.TrimSpace(req.AccountHolderName), AccountNumberEnc: sealed.Enc,
			AccountNumberHash: sealed.Hash, AccountLast4: sealed.Last4, Ifsc: ifsc, GatewayLinkedAccountID: &res.Reference,
			VerificationStatus: db.VerificationStatusVerified, VerifiedAt: &now,
		})
		if err != nil {
			return err
		}
		return s.repo.AddEvent(ctx, app.ID, sc.P.UserID, "bank_account_verified", uuid.NullUUID{}, nil)
	})
	if err != nil {
		return BankAccount{}, err
	}
	return BankAccount{ID: row.ID, AccountHolderName: row.AccountHolderName, AccountLast4: row.AccountLast4, IFSC: row.Ifsc,
		VerificationStatus: string(row.VerificationStatus), ProviderReference: row.GatewayLinkedAccountID, VerifiedAt: row.VerifiedAt}, nil
}

func (s *Service) SubmitOwner(ctx context.Context, sc httpx.Scope) (Application, error) {
	return s.submit(ctx, sc.P.UserID, ownerSubject(sc.Org), func(ctx context.Context) ([]Check, error) {
		app, err := s.repo.EnsureApplication(ctx, ownerSubject(sc.Org))
		if err != nil {
			return nil, err
		}
		v, err := s.ownerView(ctx, sc.Org, app)
		return v.Checks, err
	})
}

// ---- operator ----------------------------------------------------------------------------

func operatorSubject(user uuid.UUID) Subject { return Subject{Role: ContextOperator, UserID: user} }

func (s *Service) Operator(ctx context.Context, user uuid.UUID) (OperatorVerification, error) {
	app, err := s.repo.EnsureApplication(ctx, operatorSubject(user))
	if err != nil {
		return OperatorVerification{}, err
	}
	return s.operatorView(ctx, user, app)
}

func (s *Service) operatorView(ctx context.Context, user uuid.UUID, app db.VerificationApplication) (OperatorVerification, error) {
	u, err := s.repo.User(ctx, user)
	if err != nil {
		return OperatorVerification{}, err
	}
	prof, err := s.repo.OperatorProfile(ctx, user)
	if err != nil {
		return OperatorVerification{}, err
	}
	skills, err := s.repo.OperatorSkills(ctx, user)
	if err != nil {
		return OperatorVerification{}, err
	}
	quals, err := s.repo.Qualifications(ctx, user)
	if err != nil {
		return OperatorVerification{}, err
	}
	docs, err := s.repo.Documents(ctx, operatorSubject(user))
	if err != nil {
		return OperatorVerification{}, err
	}
	v := OperatorVerification{
		UserID: user, Skills: skills, Application: toApplication(app), Documents: toDocuments(docs),
		Qualifications: make([]Qualification, 0, len(quals)), Editable: editable(string(app.Status)),
	}
	v.Address, err = s.repo.OperatorHomeAddress(ctx, user)
	if err != nil {
		return OperatorVerification{}, err
	}
	if v.Skills == nil {
		v.Skills = []string{}
	}
	for _, q := range quals {
		item := Qualification{ID: q.ID, Title: q.Title, Issuer: q.Issuer, IssuedOn: dateString(q.IssuedOn), ValidUntil: dateString(q.ValidUntil)}
		if q.DocumentID.Valid {
			item.DocumentID = &q.DocumentID.UUID
		}
		v.Qualifications = append(v.Qualifications, item)
	}
	licenceValid := false
	if prof != nil {
		v.LicenceLast4, v.LicenceClass, v.LicenceExpiresOn = prof.LicenceLast4, prof.LicenceClass, dateString(prof.LicenceExpiresOn)
		v.ExperienceYears, v.OnDuty, v.Verified = prof.ExperienceYears, prof.OnDuty, prof.VerifiedAt != nil
		v.IdentityConsent = prof.IdentityConsentAt != nil
		var av Availability
		if len(prof.Availability) > 0 && json.Unmarshal(prof.Availability, &av) == nil && len(av.Days) > 0 {
			v.Availability = &av
		}
		licenceValid = len(prof.LicenceNumberEnc) > 0 && prof.LicenceLast4 != nil && prof.LicenceExpiresOn != nil && !prof.LicenceExpiresOn.Before(s.opt.Now())
	}
	v.Checks = []Check{
		{Key: "personal_details", Label: "Full name and verified mobile number", Done: len(strings.TrimSpace(u.FullName)) >= 2 && u.PhoneVerifiedAt != nil},
		{Key: "address", Label: "Home address", Done: v.Address != nil && strings.TrimSpace(v.Address.Line1) != ""},
		{Key: "licence_details", Label: "Valid licence details", Done: licenceValid},
		{Key: "experience", Label: "Experience and machines you can operate", Done: v.ExperienceYears != nil && len(skills) > 0},
		{Key: "availability", Label: "Availability", Done: v.Availability != nil},
		{Key: "licence_document", Label: "Licence document", Done: s.hasDoc(docs, OperatorLicenceDocs)},
		{Key: "pan_document", Label: "PAN or other approved identity document", Done: s.hasDoc(docs, OperatorIdentityDocs)},
		{Key: "privacy_consent", Label: "Consent to private identity-document review and retention", Done: v.IdentityConsent && s.hasDoc(docs, append(slices.Clone(OperatorLicenceDocs), OperatorIdentityDocs...))},
	}
	return v, nil
}

func (s *Service) SaveOperatorHomeAddress(ctx context.Context, p *httpx.Principal, req OperatorHomeAddressRequest) (OperatorVerification, error) {
	if !hasOperatorRole(p) {
		return OperatorVerification{}, apperror.ErrForbidden
	}
	if !req.PrivacyConsent {
		return OperatorVerification{}, field("privacy_consent", "required", "consent is required before saving identity documents for review")
	}
	if req.Line2 != nil {
		*req.Line2 = strings.TrimSpace(*req.Line2)
	}
	if req.Locality != nil {
		*req.Locality = strings.TrimSpace(*req.Locality)
	}
	if req.District != nil {
		*req.District = strings.TrimSpace(*req.District)
	}
	req.Line1, req.City, req.StateCode, req.Pincode = strings.TrimSpace(req.Line1), strings.TrimSpace(req.City), strings.ToUpper(strings.TrimSpace(req.StateCode)), strings.TrimSpace(req.Pincode)
	app, err := s.repo.EnsureApplication(ctx, operatorSubject(p.UserID))
	if err != nil {
		return OperatorVerification{}, err
	}
	if !editable(string(app.Status)) {
		return OperatorVerification{}, errLocked
	}
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "operator", Reason: "operator home address"}, func(ctx context.Context) error {
		if err := s.repo.SaveOperatorHomeAddress(ctx, p.UserID, Address{Label: "Home address", Line1: req.Line1, Line2: req.Line2,
			Locality: req.Locality, City: req.City, District: req.District, StateCode: req.StateCode, Pincode: req.Pincode}); err != nil {
			return err
		}
		return s.repo.RecordOperatorIdentityConsent(ctx, p.UserID)
	})
	if err != nil {
		return OperatorVerification{}, err
	}
	return s.operatorView(ctx, p.UserID, app)
}

func (s *Service) SaveOperatorProfile(ctx context.Context, p *httpx.Principal, req OperatorProfileRequest) (OperatorVerification, error) {
	expires, err := parseDate("licence_expires_on", &req.LicenceExpiresOn)
	if err != nil {
		return OperatorVerification{}, err
	}
	if expires.Before(s.opt.Now()) {
		return OperatorVerification{}, field("licence_expires_on", "expired", "this licence has expired")
	}
	if req.Availability.Start >= req.Availability.End {
		return OperatorVerification{}, field("availability.end", "order", "must be after the start time")
	}
	av := Availability{Start: req.Availability.Start, End: req.Availability.End}
	for _, d := range []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"} {
		if slices.Contains(req.Availability.Days, d) {
			av.Days = append(av.Days, d)
		}
	}
	avJSON, _ := json.Marshal(av)
	skills := slices.Compact(slices.Sorted(slices.Values(req.Skills)))
	sealed, err := s.crypt.Seal("operator.licence", req.LicenceNumber)
	if err != nil {
		return OperatorVerification{}, err
	}
	var app db.VerificationApplication
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "operator", Reason: "operator profile"}, func(ctx context.Context) error {
		app, err = s.repo.EnsureApplication(ctx, operatorSubject(p.UserID))
		if err != nil {
			return err
		}
		if !editable(string(app.Status)) {
			return errLocked
		}
		return s.repo.UpsertOperator(ctx, db.UpsertOperatorVerificationProfileParams{
			UserID: p.UserID, LicenceNumberEnc: sealed.Enc, LicenceNumberHash: sealed.Hash, LicenceLast4: &sealed.Last4,
			LicenceClass: ptr(strings.TrimSpace(req.LicenceClass)), LicenceExpiresOn: expires, ExperienceYears: &req.ExperienceYears,
			Availability: avJSON,
		}, skills)
	})
	if err != nil {
		return OperatorVerification{}, err
	}
	return s.operatorView(ctx, p.UserID, app)
}

func (s *Service) AddQualification(ctx context.Context, p *httpx.Principal, req QualificationRequest) (Qualification, error) {
	issued, err := parseDate("issued_on", req.IssuedOn)
	if err != nil {
		return Qualification{}, err
	}
	valid, err := parseDate("valid_until", req.ValidUntil)
	if err != nil {
		return Qualification{}, err
	}
	if issued != nil && valid != nil && valid.Before(*issued) {
		return Qualification{}, field("valid_until", "order", "must be after the issue date")
	}
	var out Qualification
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		var doc uuid.NullUUID
		if req.UploadID != nil {
			d, err := s.addDocument(ctx, p.UserID, operatorSubject(p.UserID),
				DocumentRequest{DocType: "operator_certificate", UploadID: *req.UploadID, ExpiresOn: req.ValidUntil},
				[]string{"operator_certificate"}, filecheck.OperatorDocument)
			if err != nil {
				return err
			}
			doc = nullable(d.ID)
		} else {
			app, err := s.repo.EnsureApplication(ctx, operatorSubject(p.UserID))
			if err != nil {
				return err
			}
			if !editable(string(app.Status)) {
				return errLocked
			}
		}
		q, err := s.repo.CreateQualification(ctx, db.CreateQualificationParams{
			OperatorUserID: p.UserID, Title: strings.TrimSpace(req.Title), Issuer: req.Issuer, IssuedOn: issued, ValidUntil: valid, DocumentID: doc,
		})
		if err != nil {
			return err
		}
		out = Qualification{ID: q.ID, Title: q.Title, Issuer: q.Issuer, IssuedOn: dateString(q.IssuedOn), ValidUntil: dateString(q.ValidUntil)}
		if q.DocumentID.Valid {
			out.DocumentID = &q.DocumentID.UUID
		}
		return nil
	})
	return out, err
}

func (s *Service) DeleteQualification(ctx context.Context, p *httpx.Principal, id uuid.UUID) error {
	return s.tx.InTx(ctx, func(ctx context.Context) error {
		app, err := s.repo.EnsureApplication(ctx, operatorSubject(p.UserID))
		if err != nil {
			return err
		}
		if !editable(string(app.Status)) {
			return errLocked
		}
		ok, err := s.repo.DeleteQualification(ctx, p.UserID, id)
		if err != nil {
			return err
		}
		if !ok {
			return apperror.ErrNotFound
		}
		return nil
	})
}

func (s *Service) AddOperatorDocument(ctx context.Context, p *httpx.Principal, req DocumentRequest) (Document, error) {
	allowed := slices.Concat(OperatorLicenceDocs, OperatorIdentityDocs)
	return s.addDocument(ctx, p.UserID, operatorSubject(p.UserID), req, allowed, filecheck.OperatorDocument, filecheck.IdentityDocument)
}

func (s *Service) DeleteOperatorDocument(ctx context.Context, p *httpx.Principal, id uuid.UUID) error {
	return s.deleteDocument(ctx, operatorSubject(p.UserID), id)
}

func (s *Service) SubmitOperator(ctx context.Context, p *httpx.Principal) (Application, error) {
	return s.submit(ctx, p.UserID, operatorSubject(p.UserID), func(ctx context.Context) ([]Check, error) {
		app, err := s.repo.EnsureApplication(ctx, operatorSubject(p.UserID))
		if err != nil {
			return nil, err
		}
		v, err := s.operatorView(ctx, p.UserID, app)
		return v.Checks, err
	})
}

// ---- fleet managers ----------------------------------------------------------------------

// FleetManager shows a fleet manager their company and authorised machines.
func (s *Service) FleetManager(ctx context.Context, p *httpx.Principal) (FleetManagerView, error) {
	org := uuid.Nil
	if p.HasOrgRole(p.ActingOrg, httpx.RoleFleetManager) {
		org = p.ActingOrg
	} else {
		for _, o := range p.Orgs() {
			if p.HasOrgRole(o, httpx.RoleFleetManager) {
				org = o
				break
			}
		}
	}
	if org == uuid.Nil {
		return FleetManagerView{}, apperror.ErrForbidden.WithDetail("you are not a fleet manager of any company")
	}
	o, err := s.repo.Organization(ctx, org)
	if err != nil {
		return FleetManagerView{}, err
	}
	v := FleetManagerView{
		OrganizationID: o.ID, OrganizationName: o.DisplayName, LegalName: o.LegalName,
		CompanyVerified: o.KycStatus == db.VerificationStatusVerified, ContactName: o.ContactName, ContactPhone: o.ContactPhone,
		ContactEmail: o.ContactEmail, Equipment: []GrantedEquipment{},
	}
	prof, err := s.repo.FleetProfile(ctx, p.UserID)
	if err != nil {
		return FleetManagerView{}, err
	}
	v.AllEquipment = prof != nil && prof.OrganizationID == org && prof.AllEquipment
	grants, err := s.repo.FleetGrants(ctx, p.UserID, org)
	if err != nil {
		return FleetManagerView{}, err
	}
	for _, g := range grants {
		v.Equipment = append(v.Equipment, GrantedEquipment{EquipmentID: g.EquipmentID, Title: g.Title, Status: string(g.Status), GrantedAt: g.GrantedAt})
	}
	return v, nil
}

func (s *Service) FleetManagers(ctx context.Context, org uuid.UUID) (FleetManagerList, error) {
	rows, err := s.repo.FleetManagers(ctx, org)
	if err != nil {
		return FleetManagerList{}, err
	}
	out := FleetManagerList{Data: make([]FleetManagerSummary, 0, len(rows))}
	for _, r := range rows {
		out.Data = append(out.Data, FleetManagerSummary(r))
	}
	return out, nil
}

// SetFleetAccess replaces what a fleet manager of the owner's organisation
// may manage. Only machines of that organisation can be granted.
func (s *Service) SetFleetAccess(ctx context.Context, sc httpx.Scope, user uuid.UUID, req FleetAccessRequest) (FleetManagerSummary, error) {
	ok, err := s.repo.HasOrgRole(ctx, user, sc.Org, httpx.RoleFleetManager)
	if err != nil {
		return FleetManagerSummary{}, err
	}
	if !ok {
		return FleetManagerSummary{}, apperror.ErrNotFound.WithDetail("no fleet manager with this id in your organisation")
	}
	ids := slices.Compact(slices.SortedFunc(slices.Values(req.EquipmentIDs), func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) }))
	if req.AllEquipment {
		ids = nil
	}
	if _, err := s.repo.SetFleetAccess(ctx, user, sc.Org, sc.P.UserID, req.AllEquipment, ids); err != nil {
		return FleetManagerSummary{}, err
	}
	list, err := s.FleetManagers(ctx, sc.Org)
	if err != nil {
		return FleetManagerSummary{}, err
	}
	for _, m := range list.Data {
		if m.UserID == user {
			return m, nil
		}
	}
	return FleetManagerSummary{}, apperror.ErrNotFound
}

// ---- document links ------------------------------------------------------------------------

// DocumentURL issues a short-lived signed link after an access check. Only
// the document's owner organisation (owner role), the operator it belongs to,
// or a KYC reviewer may obtain one; every link issued is audited.
func (s *Service) DocumentURL(ctx context.Context, p *httpx.Principal, id uuid.UUID) (storage.SignedURL, error) {
	d, err := s.repo.Document(ctx, id)
	if err != nil {
		return storage.SignedURL{}, apperror.ErrNotFound
	}
	role, allowed := "", false
	switch {
	case d.Context != nil && *d.Context == ContextOwner && d.OrganizationID.Valid && p.HasOrgRole(d.OrganizationID.UUID, httpx.RoleOwner):
		role, allowed = "owner", true
	case d.Context != nil && *d.Context == ContextOperator && d.UserID.Valid && d.UserID.UUID == p.UserID:
		role, allowed = "operator", true
	case p.HasPlatformRole(ReviewerRoles...):
		role, allowed = "admin", true
	}
	if !allowed || d.DocumentObjectKey == nil {
		return storage.SignedURL{}, apperror.ErrNotFound
	}
	if err := s.repo.AuditDocumentAccess(ctx, p.UserID, role, d.ID, "document link issued"); err != nil {
		return storage.SignedURL{}, err
	}
	return s.uploads.SignedURL(*d.DocumentObjectKey, s.opt.URLTTL), nil
}

// ---- admin review --------------------------------------------------------------------------

func (s *Service) Queue(ctx context.Context, q QueueQuery) (ApplicationPage, error) {
	k, err := httpx.PageQuery{Limit: q.Limit, Cursor: q.Cursor}.Keyset()
	if err != nil {
		return ApplicationPage{}, err
	}
	var status, role *string
	if q.Status != "" {
		status = &q.Status
	}
	if q.Role != "" {
		role = &q.Role
	}
	rows, err := s.repo.ListApplications(ctx, status, role, k)
	if err != nil {
		return ApplicationPage{}, err
	}
	items := make([]ApplicationSummary, 0, len(rows))
	for _, r := range rows {
		items = append(items, ApplicationSummary{ID: r.ID, Role: r.Role, Status: string(r.Status), SubjectName: r.SubjectName, SubmittedAt: r.SubmittedAt, ReviewedAt: r.ReviewedAt})
	}
	pg := httpx.Paginate(items, k, func(a ApplicationSummary) uuid.UUID { return a.ID })
	return ApplicationPage{Data: pg.Data, NextCursor: pg.NextCursor}, nil
}

func subjectOf(a db.GetApplicationRow) Subject {
	if a.Role == ContextOwner {
		return ownerSubject(a.OrganizationID.UUID)
	}
	return operatorSubject(a.UserID.UUID)
}

func plain(a db.GetApplicationRow) db.VerificationApplication {
	return db.VerificationApplication{ID: a.ID, Role: a.Role, UserID: a.UserID, OrganizationID: a.OrganizationID, Status: a.Status,
		SubmittedBy: a.SubmittedBy, SubmittedAt: a.SubmittedAt, ReviewerID: a.ReviewerID, ReviewedAt: a.ReviewedAt,
		RejectionReason: a.RejectionReason, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}

// load fetches an application that is visible to reviewers (not a draft).
func (s *Service) load(ctx context.Context, id uuid.UUID) (db.GetApplicationRow, error) {
	a, err := s.repo.Application(ctx, id)
	if err != nil || a.Status == db.ApplicationStatusDraft {
		return db.GetApplicationRow{}, apperror.ErrNotFound
	}
	return a, nil
}

// selfReview blocks reviewers from deciding on their own application.
func (s *Service) selfReview(ctx context.Context, p *httpx.Principal, a db.GetApplicationRow) error {
	if a.Role == ContextOperator && a.UserID.UUID == p.UserID {
		return apperror.ErrForbidden.WithDetail("you cannot review your own verification")
	}
	if a.Role == ContextOwner && p.IsMember(a.OrganizationID.UUID) {
		return apperror.ErrForbidden.WithDetail("you cannot review your own organisation")
	}
	return nil
}

// Detail returns an application with documents (each with a fresh signed
// link, audited) and its history.
func (s *Service) Detail(ctx context.Context, p *httpx.Principal, id uuid.UUID) (ApplicationDetail, error) {
	a, err := s.load(ctx, id)
	if err != nil {
		return ApplicationDetail{}, err
	}
	out := ApplicationDetail{Application: toApplication(plain(a)), SubjectName: a.SubjectName}
	switch {
	case a.Role == ContextOperator && a.UserID.Valid:
		out.AccountUserID = &a.UserID.UUID
	case a.SubmittedBy.Valid:
		out.AccountUserID = &a.SubmittedBy.UUID
	}
	var docs []Document
	switch a.Role {
	case ContextOwner:
		v, err := s.ownerView(ctx, a.OrganizationID.UUID, plain(a))
		if err != nil {
			return ApplicationDetail{}, err
		}
		out.Owner, docs = &v, v.Documents
	default:
		v, err := s.operatorView(ctx, a.UserID.UUID, plain(a))
		if err != nil {
			return ApplicationDetail{}, err
		}
		out.Operator, docs = &v, v.Documents
	}
	for i := range docs {
		u, err := s.DocumentURL(ctx, p, docs[i].ID)
		if err != nil && !errors.Is(err, apperror.ErrNotFound) {
			return ApplicationDetail{}, err
		}
		if err == nil {
			docs[i].Download = &u
		}
	}
	evs, err := s.repo.Events(ctx, id)
	if err != nil {
		return ApplicationDetail{}, err
	}
	out.History = make([]HistoryEntry, 0, len(evs))
	for _, e := range evs {
		out.History = append(out.History, HistoryEntry{Action: e.Action, ActorID: e.ActorUserID, ActorName: e.ActorName, DocumentID: e.DocumentID, Reason: e.Reason, At: e.CreatedAt})
	}
	return out, nil
}

func (s *Service) reviewTx(ctx context.Context, p *httpx.Principal, reason string, fn func(ctx context.Context) error) error {
	return s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "admin", Reason: reason}, fn)
}

// StartReview claims a submitted application for review.
func (s *Service) StartReview(ctx context.Context, p *httpx.Principal, id uuid.UUID) (Application, error) {
	var out Application
	err := s.reviewTx(ctx, p, "verification review started", func(ctx context.Context) error {
		a, err := s.load(ctx, id)
		if err != nil {
			return err
		}
		if err := s.selfReview(ctx, p, a); err != nil {
			return err
		}
		r, err := s.repo.StartReview(ctx, id, p.UserID)
		if err != nil {
			return transition(err)
		}
		out = toApplication(r)
		return s.repo.AddEvent(ctx, id, p.UserID, "review_started", uuid.NullUUID{}, nil)
	})
	return out, err
}

// ReviewDocument approves or rejects one document of an application under
// review. Rejections need a reason, which the applicant sees.
func (s *Service) ReviewDocument(ctx context.Context, p *httpx.Principal, appID, docID uuid.UUID, req DocumentReviewRequest) (Document, error) {
	reason := strings.TrimSpace(req.Reason)
	if req.Decision == "reject" && len(reason) < 5 {
		return Document{}, field("reason", "required", "explain why the document is rejected (at least 5 characters)")
	}
	var out Document
	err := s.reviewTx(ctx, p, "verification document review", func(ctx context.Context) error {
		a, err := s.load(ctx, appID)
		if err != nil {
			return err
		}
		if err := s.selfReview(ctx, p, a); err != nil {
			return err
		}
		if a.Status != db.ApplicationStatusSubmitted && a.Status != db.ApplicationStatusUnderReview {
			return apperror.ErrInvalidTransition.WithDetail("documents can only be reviewed while the application is pending")
		}
		d, err := s.repo.Document(ctx, docID)
		if err != nil || !belongs(d, subjectOf(a)) {
			return apperror.ErrNotFound
		}
		if a.Status == db.ApplicationStatusSubmitted {
			if _, err := s.repo.StartReview(ctx, appID, p.UserID); err != nil {
				return transition(err)
			}
			if err := s.repo.AddEvent(ctx, appID, p.UserID, "review_started", uuid.NullUUID{}, nil); err != nil {
				return err
			}
		}
		status, action := db.VerificationStatusVerified, "document_approved"
		var note *string
		if req.Decision == "reject" {
			status, action, note = db.VerificationStatusRejected, "document_rejected", &reason
		}
		r, err := s.repo.ReviewDocument(ctx, docID, p.UserID, string(status), note)
		if err != nil {
			return err
		}
		out = toDocument(r)
		return s.repo.AddEvent(ctx, appID, p.UserID, action, nullable(docID), note)
	})
	return out, err
}

func (s *Service) checksFor(ctx context.Context, a db.GetApplicationRow) ([]Check, error) {
	if a.Role == ContextOwner {
		v, err := s.ownerView(ctx, a.OrganizationID.UUID, plain(a))
		return v.Checks, err
	}
	v, err := s.operatorView(ctx, a.UserID.UUID, plain(a))
	return v.Checks, err
}

func (s *Service) payload(a db.GetApplicationRow, actor uuid.UUID, title string) events.Payload {
	p := events.Payload{ActorUserID: actor, Title: title, Extra: map[string]any{"role": a.Role, "status": string(a.Status)}}
	if a.Role == ContextOwner {
		p.OwnerOrgID = a.OrganizationID.UUID
	} else {
		p.UserIDs = []uuid.UUID{a.UserID.UUID}
	}
	return p
}

// Approve verifies an application: pending documents are approved, the
// organisation's KYC (owners) or the operator's profile becomes verified.
func (s *Service) Approve(ctx context.Context, p *httpx.Principal, id uuid.UUID, req DecisionRequest) (Application, error) {
	var out Application
	err := s.reviewTx(ctx, p, "verification approved", func(ctx context.Context) error {
		a, err := s.load(ctx, id)
		if err != nil {
			return err
		}
		if err := s.selfReview(ctx, p, a); err != nil {
			return err
		}
		if a.Status != db.ApplicationStatusSubmitted && a.Status != db.ApplicationStatusUnderReview {
			return apperror.ErrInvalidTransition.WithDetail("only pending applications can be approved")
		}
		cs, err := s.checksFor(ctx, a)
		if err != nil {
			return err
		}
		if !allDone(cs) {
			return missing(cs).WithDetail("the application is incomplete (a required document may have been rejected); reject it with a reason instead")
		}
		subj := subjectOf(a)
		if err := s.repo.ApproveSubmittedDocuments(ctx, subj, p.UserID); err != nil {
			return err
		}
		r, err := s.repo.Decide(ctx, id, p.UserID, StatusApproved, nil)
		if err != nil {
			return transition(err)
		}
		if subj.Role == ContextOwner {
			err = s.repo.SetOrgKYC(ctx, subj.OrgID, string(db.VerificationStatusVerified))
		} else {
			err = s.repo.SetOperatorVerified(ctx, subj.UserID, true)
		}
		if err != nil {
			return err
		}
		var note *string
		if t := strings.TrimSpace(req.Reason); t != "" {
			note = &t
		}
		if err := s.repo.AddEvent(ctx, id, p.UserID, "approved", uuid.NullUUID{}, note); err != nil {
			return err
		}
		out = toApplication(r)
		a.Status = r.Status
		return s.repo.Emit(ctx, id, events.VerificationApproved, s.payload(a, p.UserID, "Your verification is approved."))
	})
	return out, err
}

// Reject rejects a pending application, or revokes an approved one. The
// reason is required and shown to the applicant, who may fix and resubmit.
func (s *Service) Reject(ctx context.Context, p *httpx.Principal, id uuid.UUID, req DecisionRequest) (Application, error) {
	reason := strings.TrimSpace(req.Reason)
	if len(reason) < 5 {
		return Application{}, field("reason", "required", "explain why (at least 5 characters); the applicant sees this")
	}
	var out Application
	err := s.reviewTx(ctx, p, "verification rejected", func(ctx context.Context) error {
		a, err := s.load(ctx, id)
		if err != nil {
			return err
		}
		if err := s.selfReview(ctx, p, a); err != nil {
			return err
		}
		action := "rejected"
		switch a.Status {
		case db.ApplicationStatusSubmitted, db.ApplicationStatusUnderReview:
		case db.ApplicationStatusApproved:
			action = "revoked"
		default:
			return apperror.ErrInvalidTransition.WithDetail("this application is already rejected")
		}
		r, err := s.repo.Decide(ctx, id, p.UserID, StatusRejected, &reason)
		if err != nil {
			return transition(err)
		}
		subj := subjectOf(a)
		if subj.Role == ContextOwner {
			err = s.repo.SetOrgKYC(ctx, subj.OrgID, string(db.VerificationStatusRejected))
		} else {
			err = s.repo.SetOperatorVerified(ctx, subj.UserID, false)
		}
		if err != nil {
			return err
		}
		if err := s.repo.AddEvent(ctx, id, p.UserID, action, uuid.NullUUID{}, &reason); err != nil {
			return err
		}
		out = toApplication(r)
		a.Status = r.Status
		return s.repo.Emit(ctx, id, events.VerificationRejected, s.payload(a, p.UserID, "Verification "+action+": "+reason))
	})
	return out, err
}

// ReopenOwner lets the owner edit business details that were already submitted or approved. The
// application returns to draft and the organisation to pending: nothing edited is treated as
// verified until it is submitted again and approved by a reviewer.
func (s *Service) ReopenOwner(ctx context.Context, sc httpx.Scope) (OwnerVerification, error) {
	subj := ownerSubject(sc.Org)
	err := s.tx.InTxAs(ctx, database.Actor{UserID: sc.P.UserID, Role: subj.Role, Reason: "verification reopened for editing"}, func(ctx context.Context) error {
		app, err := s.repo.EnsureApplication(ctx, subj)
		if err != nil {
			return err
		}
		if editable(string(app.Status)) {
			return nil
		}
		a, err := s.repo.Reopen(ctx, app.ID)
		if err != nil {
			return transition(err)
		}
		if err := s.repo.SetOrgKYC(ctx, sc.Org, string(db.VerificationStatusPending)); err != nil {
			return err
		}
		return s.repo.AddEvent(ctx, a.ID, sc.P.UserID, "reopened", uuid.NullUUID{}, nil)
	})
	if err != nil {
		return OwnerVerification{}, err
	}
	return s.Owner(ctx, sc.Org)
}
