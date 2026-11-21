package verification

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/events"
	"github.com/equiprent/backend/internal/httpx"
)

// Subject identifies whose verification a call is about: an organisation
// (owner) or a user (operator).
type Subject struct {
	Role   string // owner | operator
	OrgID  uuid.UUID
	UserID uuid.UUID
}

func (s Subject) org() uuid.NullUUID {
	if s.Role == ContextOwner {
		return uuid.NullUUID{UUID: s.OrgID, Valid: true}
	}
	return uuid.NullUUID{}
}

func (s Subject) user() uuid.NullUUID {
	if s.Role == ContextOperator {
		return uuid.NullUUID{UUID: s.UserID, Valid: true}
	}
	return uuid.NullUUID{}
}

// Repository is the persistence the verification service needs.
type Repository interface {
	// applications
	EnsureApplication(ctx context.Context, s Subject) (db.VerificationApplication, error)
	Application(ctx context.Context, id uuid.UUID) (db.GetApplicationRow, error)
	SubmitApplication(ctx context.Context, id, by uuid.UUID) (db.VerificationApplication, error)
	StartReview(ctx context.Context, id, reviewer uuid.UUID) (db.VerificationApplication, error)
	Decide(ctx context.Context, id, reviewer uuid.UUID, status string, reason *string) (db.VerificationApplication, error)
	ListApplications(ctx context.Context, status, role *string, k httpx.Keyset) ([]db.ListApplicationsRow, error)
	AddEvent(ctx context.Context, app uuid.UUID, actor uuid.UUID, action string, doc uuid.NullUUID, reason *string) error
	Events(ctx context.Context, app uuid.UUID) ([]db.ListVerificationEventsRow, error)

	// documents
	CreateDocument(ctx context.Context, p db.CreateDocumentParams) (db.KycRecord, error)
	Document(ctx context.Context, id uuid.UUID) (db.KycRecord, error)
	Documents(ctx context.Context, s Subject) ([]db.KycRecord, error)
	DeleteDocument(ctx context.Context, id uuid.UUID) (bool, error)
	ReviewDocument(ctx context.Context, id, reviewer uuid.UUID, status string, note *string) (db.KycRecord, error)
	ApproveSubmittedDocuments(ctx context.Context, s Subject, reviewer uuid.UUID) error
	AuditDocumentAccess(ctx context.Context, actor uuid.UUID, role string, doc uuid.UUID, reason string) error

	// owner
	Organization(ctx context.Context, id uuid.UUID) (db.Organization, error)
	Address(ctx context.Context, id uuid.UUID) (db.GetAddressRow, error)
	CreateAddress(ctx context.Context, org uuid.UUID, a Address) (uuid.UUID, error)
	UpdateBusiness(ctx context.Context, p db.UpdateOwnerBusinessParams) (db.Organization, error)
	SetOrgKYC(ctx context.Context, org uuid.UUID, status string) error
	Reopen(ctx context.Context, id uuid.UUID) (db.VerificationApplication, error)
	ReplaceBankAccount(ctx context.Context, p db.InsertBankAccountParams, link PayoutLink) (db.InsertBankAccountRow, error)
	BankAccount(ctx context.Context, org uuid.UUID) (*db.GetPrimaryBankAccountRow, error)

	// operator
	OperatorProfile(ctx context.Context, user uuid.UUID) (*db.OperatorProfile, error)
	UpsertOperator(ctx context.Context, p db.UpsertOperatorVerificationProfileParams, skills []string) error
	OperatorSkills(ctx context.Context, user uuid.UUID) ([]string, error)
	SetOperatorVerified(ctx context.Context, user uuid.UUID, verified bool) error
	CreateQualification(ctx context.Context, p db.CreateQualificationParams) (db.OperatorQualification, error)
	Qualifications(ctx context.Context, user uuid.UUID) ([]db.OperatorQualification, error)
	DeleteQualification(ctx context.Context, user, id uuid.UUID) (bool, error)

	// customer
	User(ctx context.Context, id uuid.UUID) (db.User, error)
	OperatorHomeAddress(ctx context.Context, userID uuid.UUID) (*Address, error)
	SaveOperatorHomeAddress(ctx context.Context, userID uuid.UUID, address Address) error
	RecordOperatorIdentityConsent(ctx context.Context, userID uuid.UUID) error
	SetAvatar(ctx context.Context, user uuid.UUID, key string) error
	AddressCount(ctx context.Context, org uuid.UUID) (int, error)

	// fleet
	FleetProfile(ctx context.Context, user uuid.UUID) (*db.GetFleetManagerProfileRow, error)
	FleetGrants(ctx context.Context, user, org uuid.UUID) ([]db.ListFleetGrantsRow, error)
	SetFleetAccess(ctx context.Context, user, org, by uuid.UUID, all bool, equipment []uuid.UUID) (int64, error)
	FleetManagers(ctx context.Context, org uuid.UUID) ([]db.ListOrgFleetManagersRow, error)
	HasOrgRole(ctx context.Context, user, org uuid.UUID, roles ...string) (bool, error)

	Emit(ctx context.Context, app uuid.UUID, eventType string, payload events.Payload) error
}

// PGRepository implements Repository on PostgreSQL.
type PGRepository struct {
	pool *pgxpool.Pool
	tx   database.Transactor
}

func NewRepository(pool *pgxpool.Pool, tx database.Transactor) *PGRepository {
	return &PGRepository{pool: pool, tx: tx}
}

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

func nullable(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: id != uuid.Nil} }

func ptr[T any](v T) *T { return &v }

func (r *PGRepository) EnsureApplication(ctx context.Context, s Subject) (db.VerificationApplication, error) {
	a, err := r.q(ctx).EnsureApplication(ctx, db.EnsureApplicationParams{Role: s.Role, OrganizationID: s.org(), UserID: s.user()})
	return a, database.MapError(err)
}

func (r *PGRepository) Application(ctx context.Context, id uuid.UUID) (db.GetApplicationRow, error) {
	a, err := r.q(ctx).GetApplication(ctx, id)
	return a, database.MapError(err)
}

func (r *PGRepository) SubmitApplication(ctx context.Context, id, by uuid.UUID) (db.VerificationApplication, error) {
	a, err := r.q(ctx).SubmitApplication(ctx, db.SubmitApplicationParams{ID: id, SubmittedBy: nullable(by)})
	return a, database.MapError(err)
}

func (r *PGRepository) StartReview(ctx context.Context, id, reviewer uuid.UUID) (db.VerificationApplication, error) {
	a, err := r.q(ctx).StartApplicationReview(ctx, db.StartApplicationReviewParams{ID: id, ReviewerID: nullable(reviewer)})
	return a, database.MapError(err)
}

func (r *PGRepository) Decide(ctx context.Context, id, reviewer uuid.UUID, status string, reason *string) (db.VerificationApplication, error) {
	a, err := r.q(ctx).DecideApplication(ctx, db.DecideApplicationParams{
		ID: id, ReviewerID: nullable(reviewer), Status: db.ApplicationStatus(status), RejectionReason: reason,
	})
	return a, database.MapError(err)
}

func (r *PGRepository) ListApplications(ctx context.Context, status, role *string, k httpx.Keyset) ([]db.ListApplicationsRow, error) {
	rows, err := r.q(ctx).ListApplications(ctx, db.ListApplicationsParams{Status: status, Role: role, After: k.After, ResultLimit: k.Fetch()})
	return rows, database.MapError(err)
}

func (r *PGRepository) AddEvent(ctx context.Context, app, actor uuid.UUID, action string, doc uuid.NullUUID, reason *string) error {
	return database.MapError(r.q(ctx).InsertVerificationEvent(ctx, db.InsertVerificationEventParams{
		ApplicationID: app, ActorUserID: nullable(actor), Action: action, DocumentID: doc, Reason: reason,
	}))
}

func (r *PGRepository) Events(ctx context.Context, app uuid.UUID) ([]db.ListVerificationEventsRow, error) {
	rows, err := r.q(ctx).ListVerificationEvents(ctx, app)
	return rows, database.MapError(err)
}

func (r *PGRepository) CreateDocument(ctx context.Context, p db.CreateDocumentParams) (db.KycRecord, error) {
	d, err := r.q(ctx).CreateDocument(ctx, p)
	return d, database.MapError(err)
}

func (r *PGRepository) Document(ctx context.Context, id uuid.UUID) (db.KycRecord, error) {
	d, err := r.q(ctx).GetDocument(ctx, id)
	return d, database.MapError(err)
}

func (r *PGRepository) Documents(ctx context.Context, s Subject) ([]db.KycRecord, error) {
	rows, err := r.q(ctx).ListSubjectDocuments(ctx, db.ListSubjectDocumentsParams{Context: ptr(s.Role), OrganizationID: s.org(), UserID: s.user()})
	return rows, database.MapError(err)
}

func (r *PGRepository) DeleteDocument(ctx context.Context, id uuid.UUID) (bool, error) {
	n, err := r.q(ctx).DeleteDocument(ctx, id)
	return n == 1, database.MapError(err)
}

func (r *PGRepository) ReviewDocument(ctx context.Context, id, reviewer uuid.UUID, status string, note *string) (db.KycRecord, error) {
	d, err := r.q(ctx).ReviewDocument(ctx, db.ReviewDocumentParams{ID: id, ReviewedBy: nullable(reviewer), Status: db.VerificationStatus(status), ReviewNote: note})
	return d, database.MapError(err)
}

func (r *PGRepository) ApproveSubmittedDocuments(ctx context.Context, s Subject, reviewer uuid.UUID) error {
	return database.MapError(r.q(ctx).ApproveSubmittedDocuments(ctx, db.ApproveSubmittedDocumentsParams{
		ReviewedBy: nullable(reviewer), Context: ptr(s.Role), OrganizationID: s.org(), UserID: s.user(),
	}))
}

func (r *PGRepository) AuditDocumentAccess(ctx context.Context, actor uuid.UUID, role string, doc uuid.UUID, reason string) error {
	return database.MapError(r.q(ctx).InsertDocumentAccessAudit(ctx, db.InsertDocumentAccessAuditParams{
		ActorUserID: nullable(actor), ActorRole: &role, EntityID: nullable(doc), Reason: &reason,
		RequestID: ptr(httpx.RequestIDFromContext(ctx)),
	}))
}

func (r *PGRepository) Organization(ctx context.Context, id uuid.UUID) (db.Organization, error) {
	o, err := r.q(ctx).GetOrganization(ctx, id)
	return o, database.MapError(err)
}

func (r *PGRepository) Address(ctx context.Context, id uuid.UUID) (db.GetAddressRow, error) {
	a, err := r.q(ctx).GetAddress(ctx, id)
	return a, database.MapError(err)
}

func (r *PGRepository) OperatorHomeAddress(ctx context.Context, userID uuid.UUID) (*Address, error) {
	var out Address
	var line1, line2, locality, city, district, stateCode, pincode sql.NullString
	var consent *time.Time
	err := r.pool.QueryRow(ctx, `SELECT home_address_line1, home_address_line2, home_address_locality, home_address_city, home_address_district, home_address_state_code, home_address_pincode, identity_consent_at FROM operator_profiles WHERE user_id=$1`, userID).
		Scan(&line1, &line2, &locality, &city, &district, &stateCode, &pincode, &consent)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, database.MapError(err)
	}
	// Operator profiles may exist before the driver has submitted an address.
	// Nullable database fields must be represented as an absent address, not a
	// server error while scanning NULL into Go strings.
	if !line1.Valid || strings.TrimSpace(line1.String) == "" {
		return nil, nil
	}
	out.Line1 = line1.String
	out.Line2 = nullableString(line2)
	out.Locality = nullableString(locality)
	out.City = city.String
	out.District = nullableString(district)
	out.StateCode = stateCode.String
	out.Pincode = pincode.String
	out.Label = "Home address"
	if consent != nil {
		out.ConsentAt = consent
	}
	return &out, nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func (r *PGRepository) SaveOperatorHomeAddress(ctx context.Context, userID uuid.UUID, a Address) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO operator_profiles(user_id,home_address_line1,home_address_line2,home_address_locality,home_address_city,home_address_district,home_address_state_code,home_address_pincode,identity_consent_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,now()) ON CONFLICT(user_id) DO UPDATE SET home_address_line1=EXCLUDED.home_address_line1,home_address_line2=EXCLUDED.home_address_line2,home_address_locality=EXCLUDED.home_address_locality,home_address_city=EXCLUDED.home_address_city,home_address_district=EXCLUDED.home_address_district,home_address_state_code=EXCLUDED.home_address_state_code,home_address_pincode=EXCLUDED.home_address_pincode,identity_consent_at=now()`, userID, strings.TrimSpace(a.Line1), a.Line2, a.Locality, strings.TrimSpace(a.City), a.District, strings.ToUpper(strings.TrimSpace(a.StateCode)), strings.TrimSpace(a.Pincode))
	return database.MapError(err)
}

func (r *PGRepository) RecordOperatorIdentityConsent(ctx context.Context, userID uuid.UUID) error {
	return nil // Consent is recorded atomically with the driver's home address submission.
}

func (r *PGRepository) CreateAddress(ctx context.Context, org uuid.UUID, a Address) (uuid.UUID, error) {
	label := a.Label
	if label == "" {
		label = "Business address"
	}
	row, err := r.q(ctx).InsertAddress(ctx, db.InsertAddressParams{
		OrganizationID: org, Label: label, Line1: a.Line1, Line2: a.Line2, Locality: a.Locality, City: a.City,
		District: a.District, StateCode: a.StateCode, Pincode: a.Pincode,
	})
	return row.ID, database.MapError(err)
}

func (r *PGRepository) UpdateBusiness(ctx context.Context, p db.UpdateOwnerBusinessParams) (db.Organization, error) {
	o, err := r.q(ctx).UpdateOwnerBusiness(ctx, p)
	return o, database.MapError(err)
}

func (r *PGRepository) SetOrgKYC(ctx context.Context, org uuid.UUID, status string) error {
	return database.MapError(r.q(ctx).SetOrgKYCStatus(ctx, db.SetOrgKYCStatusParams{ID: org, KycStatus: db.VerificationStatus(status)}))
}

// PayoutLink says how the verified account is paid: which provider verified it,
// in which mode, and the provider's fund-account reference payouts go to.
type PayoutLink struct {
	Provider  string // razorpay | simulated
	Mode      string // live | test | simulated
	AccountID string
}

// ReplaceBankAccount saves the verified account and, in the same transaction,
// makes it the owner's payout account. The payout account is what payouts are
// sent to; without it a verified owner would never be paid.
func (r *PGRepository) ReplaceBankAccount(ctx context.Context, p db.InsertBankAccountParams, link PayoutLink) (db.InsertBankAccountRow, error) {
	var out db.InsertBankAccountRow
	err := r.tx.InTx(ctx, func(ctx context.Context) error {
		q := r.q(ctx)
		if err := q.RetireBankAccounts(ctx, p.OrganizationID); err != nil {
			return err
		}
		row, err := q.InsertBankAccount(ctx, p)
		if err != nil {
			return err
		}
		out = row
		_, err = q.UpsertPayoutLinkedAccount(ctx, db.UpsertPayoutLinkedAccountParams{
			OwnerOrgID: p.OrganizationID, Provider: link.Provider, Mode: link.Mode, AccountID: link.AccountID, BankAccountID: row.ID,
		})
		return err
	})
	return out, database.MapError(err)
}

func (r *PGRepository) BankAccount(ctx context.Context, org uuid.UUID) (*db.GetPrimaryBankAccountRow, error) {
	b, err := r.q(ctx).GetPrimaryBankAccount(ctx, org)
	if err != nil {
		return nil, notFoundIsNil(err)
	}
	return &b, nil
}

func (r *PGRepository) OperatorProfile(ctx context.Context, user uuid.UUID) (*db.OperatorProfile, error) {
	p, err := r.q(ctx).GetOperatorProfile(ctx, user)
	if err != nil {
		return nil, notFoundIsNil(err)
	}
	return &p, nil
}

func (r *PGRepository) UpsertOperator(ctx context.Context, p db.UpsertOperatorVerificationProfileParams, skills []string) error {
	return database.MapError(r.tx.InTx(ctx, func(ctx context.Context) error {
		q := r.q(ctx)
		if _, err := q.UpsertOperatorVerificationProfile(ctx, p); err != nil {
			return err
		}
		if err := q.DeleteOperatorSkills(ctx, p.UserID); err != nil {
			return err
		}
		if err := q.AddOperatorSkills(ctx, db.AddOperatorSkillsParams{OperatorUserID: p.UserID, CategorySlugs: skills}); err != nil {
			return err
		}
		got, err := q.ListOperatorSkills(ctx, p.UserID)
		if err != nil {
			return err
		}
		if len(got) != len(skills) {
			return apperror.ErrValidation.WithFields(apperror.FieldError{Field: "skills", Code: "unknown", Message: "includes unknown equipment categories"})
		}
		return nil
	}))
}

func (r *PGRepository) OperatorSkills(ctx context.Context, user uuid.UUID) ([]string, error) {
	s, err := r.q(ctx).ListOperatorSkills(ctx, user)
	return s, database.MapError(err)
}

func (r *PGRepository) SetOperatorVerified(ctx context.Context, user uuid.UUID, verified bool) error {
	return database.MapError(r.q(ctx).SetOperatorVerified(ctx, db.SetOperatorVerifiedParams{UserID: user, Verified: verified}))
}

func (r *PGRepository) CreateQualification(ctx context.Context, p db.CreateQualificationParams) (db.OperatorQualification, error) {
	q, err := r.q(ctx).CreateQualification(ctx, p)
	return q, database.MapError(err)
}

func (r *PGRepository) Qualifications(ctx context.Context, user uuid.UUID) ([]db.OperatorQualification, error) {
	rows, err := r.q(ctx).ListQualifications(ctx, user)
	return rows, database.MapError(err)
}

func (r *PGRepository) DeleteQualification(ctx context.Context, user, id uuid.UUID) (bool, error) {
	n, err := r.q(ctx).DeleteQualification(ctx, db.DeleteQualificationParams{ID: id, OperatorUserID: user})
	return n == 1, database.MapError(err)
}

func (r *PGRepository) User(ctx context.Context, id uuid.UUID) (db.User, error) {
	u, err := r.q(ctx).GetUserByID(ctx, id)
	return u, database.MapError(err)
}

func (r *PGRepository) SetAvatar(ctx context.Context, user uuid.UUID, key string) error {
	return database.MapError(r.q(ctx).SetUserAvatar(ctx, db.SetUserAvatarParams{ID: user, AvatarObjectKey: &key}))
}

func (r *PGRepository) AddressCount(ctx context.Context, org uuid.UUID) (int, error) {
	n, err := r.q(ctx).CountOrgAddresses(ctx, org)
	return int(n), database.MapError(err)
}

func (r *PGRepository) FleetProfile(ctx context.Context, user uuid.UUID) (*db.GetFleetManagerProfileRow, error) {
	p, err := r.q(ctx).GetFleetManagerProfile(ctx, user)
	if err != nil {
		return nil, notFoundIsNil(err)
	}
	return &p, nil
}

func (r *PGRepository) FleetGrants(ctx context.Context, user, org uuid.UUID) ([]db.ListFleetGrantsRow, error) {
	rows, err := r.q(ctx).ListFleetGrants(ctx, db.ListFleetGrantsParams{UserID: user, OrganizationID: org})
	return rows, database.MapError(err)
}

func (r *PGRepository) SetFleetAccess(ctx context.Context, user, org, by uuid.UUID, all bool, equipment []uuid.UUID) (int64, error) {
	var granted int64
	err := r.tx.InTxAs(ctx, database.Actor{UserID: by, Role: "owner", Reason: "fleet manager access"}, func(ctx context.Context) error {
		q := r.q(ctx)
		if err := q.EnsureFleetManagerProfile(ctx, db.EnsureFleetManagerProfileParams{UserID: user, OrganizationID: org}); err != nil {
			return err
		}
		if err := q.SetFleetManagerAccess(ctx, db.SetFleetManagerAccessParams{UserID: user, OrganizationID: org, AllEquipment: all}); err != nil {
			return err
		}
		if err := q.DeleteFleetGrants(ctx, db.DeleteFleetGrantsParams{UserID: user, OrganizationID: org}); err != nil {
			return err
		}
		if len(equipment) == 0 {
			return nil
		}
		n, err := q.AddFleetGrants(ctx, db.AddFleetGrantsParams{UserID: user, OrganizationID: org, EquipmentIds: equipment, GrantedBy: nullable(by)})
		if err != nil {
			return err
		}
		if n != int64(len(equipment)) {
			// Some ids are not machines of this organisation: grant nothing.
			return apperror.ErrValidation.WithFields(apperror.FieldError{Field: "equipment_ids", Code: "unknown", Message: "includes machines that are not in this organisation"})
		}
		granted = n
		return nil
	})
	return granted, database.MapError(err)
}

func (r *PGRepository) FleetManagers(ctx context.Context, org uuid.UUID) ([]db.ListOrgFleetManagersRow, error) {
	rows, err := r.q(ctx).ListOrgFleetManagers(ctx, org)
	return rows, database.MapError(err)
}

func (r *PGRepository) HasOrgRole(ctx context.Context, user, org uuid.UUID, roles ...string) (bool, error) {
	ok, err := r.q(ctx).UserHasOrgRole(ctx, db.UserHasOrgRoleParams{UserID: user, OrganizationID: nullable(org), Roles: roles})
	return ok, database.MapError(err)
}

func (r *PGRepository) Emit(ctx context.Context, app uuid.UUID, eventType string, payload events.Payload) error {
	return database.MapError(events.Emit(ctx, r.q(ctx), "verification", app, eventType, payload))
}

func notFoundIsNil(err error) error {
	mapped := database.MapError(err)
	if isNotFound(mapped) {
		return nil
	}
	return mapped
}

func dateString(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.DateOnly)
	return &s
}

func isNotFound(err error) bool { return errors.Is(err, apperror.ErrNotFound) }

func (r *PGRepository) Reopen(ctx context.Context, id uuid.UUID) (db.VerificationApplication, error) {
	a, err := r.q(ctx).ReopenApplication(ctx, id)
	return a, database.MapError(err)
}
