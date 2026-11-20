// Package agreements generates the rental agreement for a booking, fixes
// its exact wording with a SHA-256 hash, collects each party's electronic
// signature (built in, or through an external e-signature provider), and
// stores the draft and executed PDFs in the private object store. Once both
// parties sign, the database freezes the agreement.
package agreements

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/events"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/modules/bookings"
	"github.com/equiprent/backend/internal/storage"
)

// Agreement is the agreement with its rendered terms and signing state.
type Agreement struct {
	ID                 uuid.UUID       `json:"id"`
	BookingID          uuid.UUID       `json:"booking_id"`
	Reference          string          `json:"reference"`
	TemplateVersion    string          `json:"template_version"`
	TermsSHA256        string          `json:"terms_sha256"`
	Terms              string          `json:"terms" doc:"Empty when the booking changed after the agreement was fixed (status outdated)"`
	Status             string          `json:"status" doc:"awaiting_signatures | partially_signed | executed | outdated | superseded"`
	Version            int32           `json:"version" doc:"1 for the first issue; a new version is issued when the booking's dates or price change"`
	Current            bool            `json:"current" doc:"false for a superseded version (kept for the record)"`
	SupersedeReason    *string         `json:"supersede_reason,omitempty"`
	LegalTerms         *LegalSummary   `json:"legal_terms,omitempty" doc:"The legal template the clauses come from (rental-v3 onwards)"`
	ConsentText        string          `json:"consent_text" doc:"Show this next to the acceptance checkbox; it is stored with the acceptance record"`
	Assurance          string          `json:"assurance" doc:"acceptance_record (typed name + consent, not a digital signature) or electronic_signature (external e-sign provider)"`
	AssuranceNote      string          `json:"assurance_note"`
	Owner              *Party          `json:"owner,omitempty"`
	Customer           *Party          `json:"customer,omitempty"`
	Signatures         []SignatureView `json:"signatures"`
	CustomerAcceptedAt *time.Time      `json:"customer_accepted_at,omitempty"`
	OwnerAcceptedAt    *time.Time      `json:"owner_accepted_at,omitempty"`
	FullyAccepted      bool            `json:"fully_accepted"`
	YourParty          string          `json:"your_party,omitempty" doc:"customer or owner: the side you sign for"`
	CanSign            bool            `json:"can_sign"`
	SignatureProvider  string          `json:"signature_provider" doc:"equiprent (typed name) or the external provider"`
	Documents          []DocumentInfo  `json:"documents"`
	CreatedAt          time.Time       `json:"created_at"`
}

// LegalSummary names the legal template behind an agreement.
type LegalSummary struct {
	Version    string     `json:"version"`
	Title      string     `json:"title"`
	Reviewed   bool       `json:"reviewed" doc:"false = no legal review was recorded for this wording"`
	ReviewedBy string     `json:"reviewed_by,omitempty"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
}

// DocumentInfo describes a stored PDF (download through /document).
type DocumentInfo struct {
	Kind      string    `json:"kind" doc:"draft | executed | provider_signed"`
	SHA256    string    `json:"sha256"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

// SignRequest signs for the caller's side. The hash proves which text they read.
type SignRequest struct {
	TermsSHA256 string  `json:"terms_sha256" binding:"required,len=64,hexadecimal"`
	SignerName  string  `json:"signer_name" binding:"required,min=2,max=120" doc:"Full name typed by the signer"`
	Designation *string `json:"designation" binding:"omitempty,max=80" doc:"e.g. Director, Site engineer"`
	Consent     bool    `json:"consent" binding:"required" doc:"Must be true: the signer ticked the consent_text checkbox"`
}

// AcceptRequest is the older acceptance call (signs with the account name).
type AcceptRequest struct {
	TermsSHA256 string `json:"terms_sha256" binding:"required,len=64,hexadecimal"`
}

// SignResult is the agreement after signing, or where to finish signing
// when an external provider is used.
type SignResult struct {
	Agreement Agreement          `json:"agreement"`
	Redirect  *Redirect          `json:"redirect,omitempty"`
	Sandbox   *SandboxCompletion `json:"sandbox,omitempty" doc:"Development provider only: POST body to webhook_path with X-ESign-Signature"`
}

// Redirect sends the signer to the provider.
type Redirect struct {
	Provider  string    `json:"provider"`
	URL       string    `json:"url"`
	SessionID uuid.UUID `json:"session_id"`
}

// DocumentLink is a short-lived signed download link.
type DocumentLink struct {
	Kind      string    `json:"kind"`
	FileName  string    `json:"file_name"`
	SHA256    string    `json:"sha256"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// WebhookAck answers a provider callback.
type WebhookAck struct {
	Status string `json:"status"`
}

// ---- repository --------------------------------------------------------------------

type Repository interface {
	Get(ctx context.Context, bookingID uuid.UUID) (db.RentalAgreement, error)
	Context(ctx context.Context, bookingID uuid.UUID) (db.AgreementContextRow, error)
	CreateVersion(ctx context.Context, p db.CreateRentalAgreementVersionParams) (uuid.UUID, error)
	Supersede(ctx context.Context, id uuid.UUID, reason string) (bool, error)
	Versions(ctx context.Context, bookingID uuid.UUID) ([]db.ListRentalAgreementVersionsRow, error)
	Version(ctx context.Context, bookingID uuid.UUID, version int32) (db.RentalAgreement, error)
	PublishedTemplate(ctx context.Context) (db.LegalTemplate, error)
	History(ctx context.Context, bookingID uuid.UUID, limit int32) ([]db.BookingDocumentHistoryRow, error)
	Templates() TemplateStore
	Signatures(ctx context.Context, agreementID uuid.UUID) ([]db.AgreementSignature, error)
	InsertSignature(ctx context.Context, p db.CreateAgreementSignatureParams) error
	AcceptCustomer(ctx context.Context, agreementID, userID uuid.UUID, ip *netip.Addr) (bool, error)
	AcceptOwner(ctx context.Context, agreementID, userID uuid.UUID) (bool, error)
	UserName(ctx context.Context, userID uuid.UUID) (string, error)
	Documents(ctx context.Context, agreementID uuid.UUID) ([]DocumentInfo, error)
	Document(ctx context.Context, agreementID uuid.UUID, kind string) (db.GetAgreementDocumentRow, error)
	AddDocument(ctx context.Context, p db.CreateAgreementDocumentParams) error
	CreateSession(ctx context.Context, p db.CreateSigningSessionParams) (db.AgreementSigningSession, error)
	SessionByReference(ctx context.Context, provider, ref string) (db.AgreementSigningSession, error)
	FinishSession(ctx context.Context, id uuid.UUID, status string) error
	AgreementByID(ctx context.Context, id uuid.UUID) (db.RentalAgreement, error)
	Emit(ctx context.Context, bookingID uuid.UUID, eventType string, p events.Payload) error
	Audit(ctx context.Context, p db.InsertAuditLogParams) error
}

type PGRepository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *PGRepository { return &PGRepository{pool: pool} }

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

func (r *PGRepository) Get(ctx context.Context, bookingID uuid.UUID) (db.RentalAgreement, error) {
	a, err := r.q(ctx).GetRentalAgreement(ctx, bookingID)
	return a, database.MapError(err)
}

func (r *PGRepository) AgreementByID(ctx context.Context, id uuid.UUID) (db.RentalAgreement, error) {
	a, err := r.q(ctx).GetRentalAgreementByID(ctx, id)
	return a, database.MapError(err)
}

func (r *PGRepository) Context(ctx context.Context, bookingID uuid.UUID) (db.AgreementContextRow, error) {
	c, err := r.q(ctx).AgreementContext(ctx, bookingID)
	return c, database.MapError(err)
}

func (r *PGRepository) CreateVersion(ctx context.Context, p db.CreateRentalAgreementVersionParams) (uuid.UUID, error) {
	id, err := r.q(ctx).CreateRentalAgreementVersion(ctx, p)
	return id, database.MapError(err)
}

func (r *PGRepository) Supersede(ctx context.Context, id uuid.UUID, reason string) (bool, error) {
	n, err := r.q(ctx).SupersedeRentalAgreement(ctx, db.SupersedeRentalAgreementParams{ID: id, Reason: &reason})
	return n == 1, database.MapError(err)
}

func (r *PGRepository) Versions(ctx context.Context, bookingID uuid.UUID) ([]db.ListRentalAgreementVersionsRow, error) {
	rows, err := r.q(ctx).ListRentalAgreementVersions(ctx, bookingID)
	return rows, database.MapError(err)
}

func (r *PGRepository) Version(ctx context.Context, bookingID uuid.UUID, version int32) (db.RentalAgreement, error) {
	a, err := r.q(ctx).GetRentalAgreementVersion(ctx, db.GetRentalAgreementVersionParams{BookingID: bookingID, Version: version})
	return a, database.MapError(err)
}

func (r *PGRepository) PublishedTemplate(ctx context.Context) (db.LegalTemplate, error) {
	t, err := r.q(ctx).GetPublishedLegalTemplate(ctx, "rental_agreement")
	return t, database.MapError(err)
}

func (r *PGRepository) History(ctx context.Context, bookingID uuid.UUID, limit int32) ([]db.BookingDocumentHistoryRow, error) {
	rows, err := r.q(ctx).BookingDocumentHistory(ctx, db.BookingDocumentHistoryParams{BookingID: bookingID, Lim: limit})
	return rows, database.MapError(err)
}

func (r *PGRepository) Templates() TemplateStore { return pgTemplates{r} }

func (r *PGRepository) Signatures(ctx context.Context, id uuid.UUID) ([]db.AgreementSignature, error) {
	rows, err := r.q(ctx).ListAgreementSignatures(ctx, id)
	return rows, database.MapError(err)
}

func (r *PGRepository) InsertSignature(ctx context.Context, p db.CreateAgreementSignatureParams) error {
	_, err := r.q(ctx).CreateAgreementSignature(ctx, p)
	return database.MapError(err)
}

func (r *PGRepository) AcceptCustomer(ctx context.Context, agreementID, userID uuid.UUID, ip *netip.Addr) (bool, error) {
	n, err := r.q(ctx).AcceptAgreementVersionAsCustomer(ctx, db.AcceptAgreementVersionAsCustomerParams{
		ID: agreementID, UserID: uuid.NullUUID{UUID: userID, Valid: true}, Ip: ip,
	})
	return n == 1, database.MapError(err)
}

func (r *PGRepository) AcceptOwner(ctx context.Context, agreementID, userID uuid.UUID) (bool, error) {
	n, err := r.q(ctx).AcceptAgreementVersionAsOwner(ctx, db.AcceptAgreementVersionAsOwnerParams{ID: agreementID, UserID: uuid.NullUUID{UUID: userID, Valid: true}})
	return n == 1, database.MapError(err)
}

func (r *PGRepository) UserName(ctx context.Context, userID uuid.UUID) (string, error) {
	u, err := r.q(ctx).GetUserByID(ctx, userID)
	if err != nil {
		return "", database.MapError(err)
	}
	return strings.TrimSpace(u.FullName), nil
}

func (r *PGRepository) Documents(ctx context.Context, id uuid.UUID) ([]DocumentInfo, error) {
	rows, err := r.q(ctx).ListAgreementDocuments(ctx, id)
	out := make([]DocumentInfo, 0, len(rows))
	for _, d := range rows {
		out = append(out, DocumentInfo{Kind: d.Kind, SHA256: hex.EncodeToString(d.Sha256), SizeBytes: d.SizeBytes, CreatedAt: d.CreatedAt})
	}
	return out, database.MapError(err)
}

func (r *PGRepository) Document(ctx context.Context, id uuid.UUID, kind string) (db.GetAgreementDocumentRow, error) {
	d, err := r.q(ctx).GetAgreementDocument(ctx, db.GetAgreementDocumentParams{AgreementID: id, Kind: kind})
	return d, database.MapError(err)
}

func (r *PGRepository) AddDocument(ctx context.Context, p db.CreateAgreementDocumentParams) error {
	return database.MapError(r.q(ctx).CreateAgreementDocument(ctx, p))
}

func (r *PGRepository) CreateSession(ctx context.Context, p db.CreateSigningSessionParams) (db.AgreementSigningSession, error) {
	s, err := r.q(ctx).CreateSigningSession(ctx, p)
	return s, database.MapError(err)
}

func (r *PGRepository) SessionByReference(ctx context.Context, provider, ref string) (db.AgreementSigningSession, error) {
	s, err := r.q(ctx).GetSigningSessionByReference(ctx, db.GetSigningSessionByReferenceParams{Provider: provider, ProviderReference: ref})
	return s, database.MapError(err)
}

func (r *PGRepository) FinishSession(ctx context.Context, id uuid.UUID, status string) error {
	return database.MapError(r.q(ctx).FinishSigningSession(ctx, db.FinishSigningSessionParams{ID: id, Status: status}))
}

func (r *PGRepository) Emit(ctx context.Context, bookingID uuid.UUID, eventType string, p events.Payload) error {
	return database.MapError(events.Emit(ctx, r.q(ctx), "booking", bookingID, eventType, p))
}

func (r *PGRepository) Audit(ctx context.Context, p db.InsertAuditLogParams) error {
	return database.MapError(r.q(ctx).InsertAuditLog(ctx, p))
}

// ---- service -----------------------------------------------------------------------

// Files stores generated PDFs and mints download links (the uploads module).
type Files interface {
	StoreGenerated(ctx context.Context, userID uuid.UUID, purpose, contentType, ext, fileName string, data []byte) (db.Upload, error)
	SignedURL(key string, ttl time.Duration) storage.SignedURL
}

// Booking statuses in which an agreement can be generated and signed: from
// the owner's acceptance until the rental ends.
var signable = map[string]bool{
	bookings.StatusAccepted: true, bookings.StatusPaymentPending: true, "payment_processing": true, bookings.StatusConfirmed: true,
	bookings.StatusOperatorAssigned: true, bookings.StatusDispatched: true, bookings.StatusOnSite: true,
	bookings.StatusActive: true, "extension_requested": true, "cancellation_requested": true,
}

type Service struct {
	// requireReviewed refuses to issue agreements from a template with no
	// recorded legal review (staging and production).
	requireReviewed bool
	repo            Repository
	bookings        *bookings.Service
	files           Files
	provider        Provider
	tx              database.Transactor
	urlTTL          time.Duration
	log             *slog.Logger
}

func NewService(repo Repository, bk *bookings.Service, files Files, provider Provider, tx database.Transactor, urlTTL time.Duration, log *slog.Logger) *Service {
	if provider == nil {
		provider = Builtin{}
	}
	if urlTTL <= 0 {
		urlTTL = 5 * time.Minute
	}
	return &Service{repo: repo, bookings: bk, files: files, provider: provider, tx: tx, urlTTL: urlTTL, log: log}
}

// RequireReviewedTemplates makes agreement issue depend on a recorded legal
// review of the published template.
func (s *Service) RequireReviewedTemplates(on bool) *Service { s.requireReviewed = on; return s }

// party is the side the caller signs for; owner-side fleet managers may read
// but not sign.
func party(p *httpx.Principal, b bookings.Booking, side bookings.Side) string {
	switch {
	case side.Has(bookings.SideCustomer) && p.HasOrgRole(b.CustomerOrgID, httpx.RoleCustomer):
		return "customer"
	case side.Has(bookings.SideOwner) && p.HasOrgRole(b.OwnerOrgID, httpx.RoleOwner, httpx.RoleOrgAdmin):
		return "owner"
	}
	return ""
}

// loaded is an agreement version with its text worked out.
type loaded struct {
	row      db.RentalAgreement
	terms    string
	snapshot *Snapshot
	outdated bool // the booking moved on: the text no longer matches it
}

func executed(a db.RentalAgreement) bool {
	return a.CustomerAcceptedAt != nil && a.OwnerAcceptedAt != nil
}

// interpret renders a stored version and checks it against the booking.
func interpret(a db.RentalAgreement, b bookings.Booking) (loaded, error) {
	out := loaded{row: a}
	var err error
	switch a.TemplateVersion {
	case TemplateV1:
		out.terms, err = RenderV1(b)
		if err != nil {
			return loaded{}, err
		}
	default:
		var snap Snapshot
		if err := json.Unmarshal(a.Snapshot, &snap); err != nil {
			return loaded{}, err
		}
		out.snapshot = &snap
		out.terms = Render(a.TemplateVersion, snap)
		if !b.Period.Start.Equal(snap.Start) || !b.Period.End.Equal(snap.End) || b.EstimatedTotal != snap.TotalPaise {
			out.outdated = true
		}
	}
	if !bytes.Equal(Hash(out.terms), a.TermsSha256) {
		out.outdated = true
	}
	if out.outdated && a.TemplateVersion == TemplateV1 && !executed(a) {
		out.terms = ""
	}
	return out, nil
}

// load returns the current agreement version, issuing the first one on
// first access by a party and a new version when the booking's dates or
// price changed (before acceptance: re-issue; after it: an amendment).
// Superseded versions stay in the history, unchanged.
func (s *Service) load(ctx context.Context, b bookings.Booking) (loaded, error) {
	a, err := s.repo.Get(ctx, b.ID)
	if errors.Is(err, apperror.ErrNotFound) {
		if !signable[b.Status] {
			return loaded{}, apperror.ErrInvalidTransition.WithDetail("the agreement is issued once the owner accepts the booking")
		}
		return s.issue(ctx, b, nil, "")
	}
	if err != nil {
		return loaded{}, err
	}
	l, err := interpret(a, b)
	if err != nil {
		return loaded{}, err
	}
	if !l.outdated || !signable[b.Status] {
		if l.outdated && executed(a) {
			l.outdated = false // the accepted text stands until an amendment can be issued
		}
		return l, nil
	}
	reason := "re-issued: the booking's dates or price changed before both parties accepted"
	if executed(a) {
		reason = "amended: the booking's dates or price changed after both parties accepted"
	}
	return s.issue(ctx, b, &a, reason)
}

// loadVersion returns a specific version (0 = current).
func (s *Service) loadVersion(ctx context.Context, b bookings.Booking, version int32) (loaded, error) {
	if version <= 0 {
		return s.load(ctx, b)
	}
	a, err := s.repo.Version(ctx, b.ID, version)
	if err != nil {
		return loaded{}, err
	}
	l, err := interpret(a, b)
	if err == nil && (a.SupersededAt != nil || executed(a)) {
		l.outdated = false // historical text is shown as it was
	}
	return l, err
}

// issue creates a new current version from the published legal template,
// superseding prev (if any) in the same transaction.
func (s *Service) issue(ctx context.Context, b bookings.Booking, prev *db.RentalAgreement, reason string) (loaded, error) {
	tmpl, err := s.repo.PublishedTemplate(ctx)
	if errors.Is(err, apperror.ErrNotFound) {
		return loaded{}, apperror.ErrServiceUnavailable.WithDetail("no rental agreement terms are published yet")
	}
	if err != nil {
		return loaded{}, err
	}
	terms, err := TermsFromTemplate(tmpl)
	if err != nil {
		return loaded{}, err
	}
	if s.requireReviewed && !terms.Reviewed() {
		return loaded{}, apperror.ErrServiceUnavailable.WithDetail("the published agreement terms have no recorded legal review")
	}
	c, err := s.repo.Context(ctx, b.ID)
	if err != nil {
		return loaded{}, err
	}
	snap := BuildSnapshotV3(c, terms)
	raw, err := json.Marshal(snap)
	if err != nil {
		return loaded{}, err
	}
	err = s.tx.InTxAs(ctx, database.Actor{Role: "system", Reason: "agreement issued"}, func(ctx context.Context) error {
		if prev != nil {
			ok, err := s.repo.Supersede(ctx, prev.ID, reason)
			if err != nil {
				return err
			}
			if !ok {
				return apperror.ErrConflict
			}
		}
		_, err := s.repo.CreateVersion(ctx, db.CreateRentalAgreementVersionParams{
			BookingID: b.ID, TemplateVersion: TemplateV3, TermsSha256: Hash(RenderV3(snap)), Snapshot: raw,
			LegalTemplateID: uuid.NullUUID{UUID: tmpl.ID, Valid: true},
		})
		return err
	})
	if err != nil && !errors.Is(err, apperror.ErrConflict) { // conflict: a concurrent request issued it
		return loaded{}, err
	}
	a, err := s.repo.Get(ctx, b.ID)
	if err != nil {
		return loaded{}, err
	}
	return interpret(a, b)
}

func (s *Service) view(ctx context.Context, p *httpx.Principal, b bookings.Booking, side bookings.Side, l loaded) (Agreement, error) {
	a := l.row
	sigs, err := s.repo.Signatures(ctx, a.ID)
	if err != nil {
		return Agreement{}, err
	}
	docs, err := s.repo.Documents(ctx, a.ID)
	if err != nil {
		return Agreement{}, err
	}
	out := Agreement{
		ID: a.ID, BookingID: a.BookingID, Reference: b.Reference, TemplateVersion: a.TemplateVersion,
		TermsSHA256: hex.EncodeToString(a.TermsSha256), Terms: l.terms, Signatures: make([]SignatureView, 0, len(sigs)),
		CustomerAcceptedAt: a.CustomerAcceptedAt, OwnerAcceptedAt: a.OwnerAcceptedAt,
		FullyAccepted: a.CustomerAcceptedAt != nil && a.OwnerAcceptedAt != nil, YourParty: party(p, b, side),
		SignatureProvider: s.provider.Name(), Documents: docs, CreatedAt: a.CreatedAt,
		Version: a.Version, Current: a.SupersededAt == nil, SupersedeReason: a.SupersedeReason,
		ConsentText: consentText(l), Assurance: s.assurance(), AssuranceNote: assuranceNote(s.assurance()),
	}
	if l.snapshot != nil {
		out.Owner, out.Customer = &l.snapshot.Owner, &l.snapshot.Customer
		if lt := l.snapshot.Legal; lt != nil {
			out.LegalTerms = &LegalSummary{Version: lt.Version, Title: lt.Title, Reviewed: lt.Reviewed(), ReviewedBy: lt.ReviewedBy, ReviewedAt: lt.ReviewedAt}
		}
	}
	for _, sg := range sigs {
		out.Signatures = append(out.Signatures, toView(sg))
	}
	switch {
	case a.SupersededAt != nil:
		out.Status = "superseded"
	case out.FullyAccepted:
		out.Status = "executed"
	case l.outdated:
		out.Status = "outdated"
	case a.CustomerAcceptedAt != nil || a.OwnerAcceptedAt != nil:
		out.Status = "partially_signed"
	default:
		out.Status = "awaiting_signatures"
	}
	mine := (out.YourParty == "customer" && a.CustomerAcceptedAt != nil) || (out.YourParty == "owner" && a.OwnerAcceptedAt != nil)
	out.CanSign = out.YourParty != "" && !mine && !l.outdated && !out.FullyAccepted && signable[b.Status] && out.Current
	return out, nil
}

// defaultConsent is shown for agreements issued before consent text was
// part of the legal template.
const defaultConsent = "I have read this agreement and accept it on behalf of my organisation. I understand that my acceptance is recorded electronically with my name, the time, my IP address and a fingerprint of this text."

func consentText(l loaded) string {
	if l.snapshot != nil && l.snapshot.Legal != nil && strings.TrimSpace(l.snapshot.Legal.Body.ConsentText) != "" {
		return l.snapshot.Legal.Body.ConsentText
	}
	return defaultConsent
}

// assurance is what a party's action amounts to with the configured provider.
func (s *Service) assurance() string {
	if s.provider.External() {
		return "electronic_signature"
	}
	return "acceptance_record"
}

func assuranceNote(a string) string {
	if a == "electronic_signature" {
		return "Signed through an external e-signature provider; the provider's own certificate is the signature evidence."
	}
	return "A typed name with an explicit consent checkbox, recorded with the time, IP address, device and the text fingerprint. This is an acceptance record, not a digital signature or Aadhaar eSign under the Information Technology Act, 2000."
}

func toView(sg db.AgreementSignature) SignatureView {
	v := SignatureView{Party: sg.Party, SignerName: sg.SignerName, Designation: sg.SignerDesignation, Method: sg.Method,
		Provider: sg.Provider, SignedAt: sg.SignedAt, IP: maskIP(sg.Ip), Kind: "acceptance_record"}
	if sg.Method == "provider" {
		v.Kind = "electronic_signature"
	}
	if sg.ConsentText != nil {
		v.ConsentText = *sg.ConsentText
	}
	if sg.UserAgent != nil {
		v.UserAgent = trimUA(*sg.UserAgent)
	}
	return v
}

func trimUA(ua string) string {
	if len(ua) > 120 {
		return ua[:120]
	}
	return ua
}

// Get returns the agreement, creating it on first access by a party.
func (s *Service) Get(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID) (Agreement, error) {
	b, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return Agreement{}, err
	}
	l, err := s.load(ctx, b)
	if err != nil {
		return Agreement{}, err
	}
	return s.view(ctx, p, b, side, l)
}

// SignMeta is request metadata recorded with a signature.
type SignMeta struct {
	IP        string
	UserAgent string
}

// Sign records the caller's signature (built-in provider) or starts a
// signing session at the external provider.
func (s *Service) Sign(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID, req SignRequest, meta SignMeta) (SignResult, error) {
	b, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return SignResult{}, err
	}
	l, err := s.load(ctx, b)
	if err != nil {
		return SignResult{}, err
	}
	who := party(p, b, side)
	switch {
	case who == "":
		return SignResult{}, apperror.ErrForbidden.WithDetail("only the customer or the owner (not a fleet manager or operator) signs the agreement")
	case !req.Consent:
		return SignResult{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "consent", Code: "required", Message: "agree to sign electronically"})
	case l.outdated || hex.EncodeToString(l.row.TermsSha256) != strings.ToLower(req.TermsSHA256):
		return SignResult{}, apperror.ErrConflict.WithDetail("the agreement text changed; reload and read it again")
	case l.row.CustomerAcceptedAt != nil && l.row.OwnerAcceptedAt != nil:
		return SignResult{}, apperror.ErrConflict.WithDetail("the agreement is already signed by both parties")
	case (who == "customer" && l.row.CustomerAcceptedAt != nil) || (who == "owner" && l.row.OwnerAcceptedAt != nil):
		return SignResult{}, apperror.ErrConflict.WithDetail("your side has already signed")
	case !signable[b.Status]:
		return SignResult{}, apperror.ErrInvalidTransition.WithDetail("the booking is " + b.Status + "; the agreement can no longer be signed")
	}
	name := strings.Join(strings.Fields(req.SignerName), " ")
	var ip *netip.Addr
	if parsed, err := netip.ParseAddr(meta.IP); err == nil {
		ip = &parsed
	}

	if s.provider.External() {
		return s.beginExternal(ctx, p, b, side, l, who, name)
	}
	ua := trimUA(meta.UserAgent)
	consent := consentText(l)
	sig := db.CreateAgreementSignatureParams{
		AgreementID: l.row.ID, Party: who, SignerUserID: p.UserID, SignerName: name, SignerDesignation: req.Designation,
		Method: "typed_name", Provider: "equiprent", TermsSha256: l.row.TermsSha256, Ip: ip, UserAgent: &ua, ConsentText: &consent,
	}
	if err := s.record(ctx, b, sig, ip); err != nil {
		return SignResult{}, err
	}
	a, err := s.Get(ctx, p, bookingID)
	if err != nil {
		return SignResult{}, err
	}
	if a.FullyAccepted {
		s.storeExecuted(ctx, p.UserID, b)
	}
	return SignResult{Agreement: a}, nil
}

// record writes the signature and the acceptance, and announces it — in one
// transaction.
func (s *Service) record(ctx context.Context, b bookings.Booking, sig db.CreateAgreementSignatureParams, ip *netip.Addr) error {
	return s.tx.InTxAs(ctx, database.Actor{UserID: sig.SignerUserID, Role: sig.Party, Reason: "agreement signed"}, func(ctx context.Context) error {
		if err := s.repo.InsertSignature(ctx, sig); err != nil {
			if errors.Is(err, apperror.ErrConflict) {
				return apperror.ErrConflict.WithDetail("your side has already signed")
			}
			return err
		}
		var ok bool
		var err error
		if sig.Party == "customer" {
			ok, err = s.repo.AcceptCustomer(ctx, sig.AgreementID, sig.SignerUserID, ip)
		} else {
			ok, err = s.repo.AcceptOwner(ctx, sig.AgreementID, sig.SignerUserID)
		}
		if err != nil {
			return err
		}
		if !ok {
			return apperror.ErrConflict.WithDetail("your side has already accepted, or this version was superseded")
		}
		a, err := s.repo.AgreementByID(ctx, sig.AgreementID)
		if err != nil {
			return err
		}
		pl := bookings.EventPayload(b, sig.SignerUserID)
		pl.Extra = map[string]any{"booking_id": b.ID, "actor_name": sig.SignerName, "party": sig.Party, "version": a.Version}
		if err := s.repo.Emit(ctx, b.ID, events.AgreementSigned, pl); err != nil {
			return err
		}
		if a.CustomerAcceptedAt != nil && a.OwnerAcceptedAt != nil {
			pl := bookings.EventPayload(b, uuid.Nil)
			pl.Extra = map[string]any{"booking_id": b.ID}
			return s.repo.Emit(ctx, b.ID, events.AgreementExecuted, pl)
		}
		return nil
	})
}

func (s *Service) beginExternal(ctx context.Context, p *httpx.Principal, b bookings.Booking, side bookings.Side, l loaded, who, name string) (SignResult, error) {
	pdf := BuildPDF(PDFInput{AgreementID: l.row.ID.String(), Reference: b.Reference, Template: l.row.TemplateVersion, Terms: l.terms,
		TermsSHA256: l.row.TermsSha256, CreatedAt: l.row.CreatedAt, Version: l.row.Version})
	sessionID := uuid.Must(uuid.NewV7())
	res, err := s.provider.Begin(ctx, BeginRequest{SessionID: sessionID, Reference: b.Reference, Party: who, SignerName: name, Document: pdf})
	if err != nil {
		return SignResult{}, apperror.New(502, "ESIGN_UNAVAILABLE", "The e-signature service is unavailable").WithDetail(err.Error())
	}
	sess, err := s.repo.CreateSession(ctx, db.CreateSigningSessionParams{
		AgreementID: l.row.ID, Party: who, SignerUserID: p.UserID, SignerName: name, Provider: s.provider.Name(),
		ProviderReference: res.ProviderReference, TermsSha256: l.row.TermsSha256,
	})
	if err != nil {
		return SignResult{}, err
	}
	a, err := s.view(ctx, p, b, side, l)
	if err != nil {
		return SignResult{}, err
	}
	return SignResult{Agreement: a, Redirect: &Redirect{Provider: s.provider.Name(), URL: res.RedirectURL, SessionID: sess.ID}, Sandbox: res.Sandbox}, nil
}

// Webhook completes an external signing session.
func (s *Service) Webhook(ctx context.Context, provider string, header map[string][]string, body []byte) (WebhookAck, error) {
	if !s.provider.External() || provider != s.provider.Name() {
		return WebhookAck{}, apperror.ErrNotFound
	}
	ev, err := s.provider.Webhook(header, body)
	if err != nil {
		return WebhookAck{}, apperror.New(401, "WEBHOOK_SIGNATURE_INVALID", "The webhook signature is not valid")
	}
	var signedNow bool
	var b bookings.Booking
	var signerID uuid.UUID
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		sess, err := s.repo.SessionByReference(ctx, provider, ev.ProviderReference)
		if err != nil {
			return err
		}
		if sess.Status != "pending" {
			return nil // redelivery
		}
		if ev.Status != "signed" {
			return s.repo.FinishSession(ctx, sess.ID, ev.Status)
		}
		a, err := s.repo.AgreementByID(ctx, sess.AgreementID)
		if err != nil {
			return err
		}
		if a.SupersededAt != nil {
			return s.repo.FinishSession(ctx, sess.ID, "expired") // a newer version replaced the one being signed
		}
		b, err = s.bookings.Load(ctx, a.BookingID)
		if err != nil {
			return err
		}
		ref := ev.ProviderReference
		if err := s.record(ctx, b, db.CreateAgreementSignatureParams{
			AgreementID: sess.AgreementID, Party: sess.Party, SignerUserID: sess.SignerUserID, SignerName: sess.SignerName,
			Method: "provider", Provider: provider, ProviderReference: &ref, TermsSha256: sess.TermsSha256, ConsentText: &providerConsent,
		}, nil); err != nil {
			return err
		}
		signedNow, signerID = true, sess.SignerUserID
		return s.repo.FinishSession(ctx, sess.ID, "completed")
	})
	if errors.Is(err, apperror.ErrNotFound) {
		return WebhookAck{Status: "ignored"}, nil
	}
	if err != nil {
		return WebhookAck{}, err
	}
	if signedNow && len(ev.SignedDocument) > 0 {
		s.storeDocument(ctx, signerID, b, "provider_signed", ev.SignedDocument)
	}
	return WebhookAck{Status: "processed"}, nil
}

var providerConsent = "Signed at the external e-signature provider."

// Accept is the older acceptance call: a typed-name signature in the
// account holder's name.
func (s *Service) Accept(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID, req AcceptRequest, meta SignMeta) (Agreement, error) {
	name, err := s.repo.UserName(ctx, p.UserID)
	if err != nil {
		return Agreement{}, err
	}
	if len(strings.TrimSpace(name)) < 2 {
		name = "Account holder"
	}
	res, err := s.Sign(ctx, p, bookingID, SignRequest{TermsSHA256: req.TermsSHA256, SignerName: name, Consent: true}, meta)
	return res.Agreement, err
}

func fileName(ref, kind string, version int32) string {
	v := ""
	if version > 1 {
		v = fmt.Sprintf("-v%d", version)
	}
	if kind == "draft" {
		return "agreement-" + ref + v + "-draft.pdf"
	}
	return "agreement-" + ref + v + ".pdf"
}

// storeDocument saves a PDF and records it; a concurrent writer wins quietly.
func (s *Service) storeDocument(ctx context.Context, userID uuid.UUID, b bookings.Booking, kind string, pdf []byte) {
	a, err := s.repo.Get(ctx, b.ID)
	if err == nil {
		_, err = s.saveDocument(ctx, userID, a.ID, b.Reference, kind, a.Version, pdf)
	}
	if err != nil {
		s.log.WarnContext(ctx, "storing agreement document failed", "booking_id", b.ID, "kind", kind, "error", err)
	}
}

func (s *Service) saveDocument(ctx context.Context, userID, agreementID uuid.UUID, ref, kind string, version int32, pdf []byte) (db.GetAgreementDocumentRow, error) {
	up, err := s.files.StoreGenerated(ctx, userID, "agreement_document", "application/pdf", "pdf", fileName(ref, kind, version), pdf)
	if err != nil {
		return db.GetAgreementDocumentRow{}, err
	}
	if err := s.repo.AddDocument(ctx, db.CreateAgreementDocumentParams{AgreementID: agreementID, Kind: kind, UploadID: up.ID, Sha256: up.Sha256}); err != nil {
		return db.GetAgreementDocumentRow{}, err
	}
	return s.repo.Document(ctx, agreementID, kind)
}

// storeExecuted keeps the signed copy as soon as both parties have signed.
func (s *Service) storeExecuted(ctx context.Context, userID uuid.UUID, b bookings.Booking) {
	l, err := s.load(ctx, b)
	if err != nil {
		s.log.WarnContext(ctx, "loading executed agreement failed", "booking_id", b.ID, "error", err)
		return
	}
	if _, err := s.Document(ctx, userID, b, l); err != nil {
		s.log.WarnContext(ctx, "storing executed agreement failed", "booking_id", b.ID, "error", err)
	}
}

// Document returns the stored PDF for the agreement's state, generating it
// on first request: the executed copy once both signed, else the draft.
func (s *Service) Document(ctx context.Context, userID uuid.UUID, b bookings.Booking, l loaded) (db.GetAgreementDocumentRow, error) {
	executed := executed(l.row)
	kind := "draft"
	if executed {
		kind = "executed"
	}
	doc, err := s.repo.Document(ctx, l.row.ID, kind)
	if err == nil || !errors.Is(err, apperror.ErrNotFound) {
		return doc, err
	}
	if l.terms == "" {
		return doc, apperror.ErrConflict.WithDetail("the agreement is outdated; no document can be produced")
	}
	in := PDFInput{AgreementID: l.row.ID.String(), Reference: b.Reference, Template: l.row.TemplateVersion, Terms: l.terms,
		TermsSHA256: l.row.TermsSha256, CreatedAt: l.row.CreatedAt, Executed: executed, Version: l.row.Version,
		Superseded: l.row.SupersededAt != nil}
	if executed {
		sigs, err := s.repo.Signatures(ctx, l.row.ID)
		if err != nil {
			return doc, err
		}
		for _, sg := range sigs {
			in.Signatures = append(in.Signatures, toView(sg))
		}
		// Agreements accepted before signatures were recorded show the acceptance.
		if len(in.Signatures) == 0 {
			in.Signatures = append(in.Signatures,
				SignatureView{Party: "customer", SignerName: "Customer (click-through acceptance)", Method: "typed_name", SignedAt: *l.row.CustomerAcceptedAt, IP: maskIP(l.row.CustomerAcceptIp)},
				SignatureView{Party: "owner", SignerName: "Owner (click-through acceptance)", Method: "typed_name", SignedAt: *l.row.OwnerAcceptedAt, IP: "-"})
		}
		last := in.Signatures[len(in.Signatures)-1].SignedAt
		in.CreatedAt = last // deterministic: the executed copy is dated by the last signature
	}
	return s.saveDocument(ctx, userID, l.row.ID, b.Reference, kind, l.row.Version, BuildPDF(in))
}

// DocumentLink issues a short-lived download link for the agreement PDF.
func (s *Service) DocumentLink(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID, version int32) (DocumentLink, error) {
	b, _, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return DocumentLink{}, err
	}
	l, err := s.loadVersion(ctx, b, version)
	if err != nil {
		return DocumentLink{}, err
	}
	doc, err := s.Document(ctx, p.UserID, b, l)
	if err != nil {
		return DocumentLink{}, err
	}
	link := s.files.SignedURL(doc.ObjectKey, s.urlTTL)
	reqID := httpx.RequestIDFromContext(ctx)
	reason := fmt.Sprintf("agreement v%d %s download", l.row.Version, doc.Kind)
	if err := s.repo.Audit(ctx, db.InsertAuditLogParams{
		ActorUserID: uuid.NullUUID{UUID: p.UserID, Valid: true}, Action: "document.url_issued", EntityType: "rental_agreement",
		EntityID: uuid.NullUUID{UUID: l.row.ID, Valid: true}, Reason: &reason, RequestID: &reqID,
	}); err != nil {
		return DocumentLink{}, err
	}
	return DocumentLink{Kind: doc.Kind, FileName: fileName(b.Reference, doc.Kind, l.row.Version), SHA256: hex.EncodeToString(doc.Sha256),
		URL: link.URL, ExpiresAt: link.ExpiresAt}, nil
}

// ---- handler -----------------------------------------------------------------------

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Name() string { return "agreements" }

func meta(c *gin.Context) SignMeta {
	return SignMeta{IP: c.ClientIP(), UserAgent: c.GetHeader("User-Agent")}
}

func (h *Handler) Routes(r *httpx.Router) {
	r = r.Tag("agreements")
	a := r.Authed()
	a.GET("/bookings/:id/agreement", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Get(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Rental agreement (generated on first view)", Response: Agreement{},
		Description: "Names both parties, the equipment, the rental dates, the price breakdown, the cancellation policy and the terms. terms_sha256 fingerprints the exact text."})
	a.POST("/bookings/:id/agreement/sign", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req SignRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Sign(c.Request.Context(), p, id, req, meta(c))
	}), httpx.Doc{Summary: "Accept (or sign) the agreement for your side", Request: SignRequest{}, Response: SignResult{},
		Description: "Built-in provider: stores an acceptance record (typed name + consent checkbox) bound to terms_sha256 with time, IP and device; this is not a digital signature. External e-sign provider: returns a redirect to sign there."})
	a.POST("/bookings/:id/agreement/accept", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req AcceptRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Accept(c.Request.Context(), p, id, req, meta(c))
	}), httpx.Doc{Summary: "Accept the agreement for your side (signs in your account name)", Request: AcceptRequest{}, Response: Agreement{}})
	a.GET("/bookings/:id/agreement/document", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.DocumentLink(c.Request.Context(), p, id, 0)
	}), httpx.Doc{Summary: "Download link for the agreement PDF", Response: DocumentLink{},
		Description: "The executed copy once both parties signed, else a watermarked draft. PDFs are stored privately; the link is signed and short-lived, and its issue is audited."})
	h.versionRoutes(a)
	r.POST("/webhooks/esign/:provider", func(c *gin.Context) {
		body, err := c.GetRawData()
		if err != nil {
			httpx.Fail(c, httpx.BindError(err))
			return
		}
		ack, err := h.svc.Webhook(c.Request.Context(), c.Param("provider"), c.Request.Header, body)
		httpx.Reply(c, 200, ack, err)
	}, httpx.Doc{Summary: "E-signature provider webhook (HMAC-signed)", Response: WebhookAck{}})
}
