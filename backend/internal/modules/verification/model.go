// Package verification runs user and business verification:
//
//   - Customers complete a profile checklist (name, verified mobile and email,
//     address, profile image). No manual review.
//   - Equipment owners (individual or company) submit business details,
//     contact details, a business address, identity and business documents
//     and a bank account verified by the payout provider; admins review.
//   - Operators submit licence details (encrypted), experience, skills,
//     availability, qualifications and documents; admins review.
//   - Fleet managers see their company and the machines an owner authorised.
//   - Admins review documents, approve or reject (reason recorded), revoke,
//     and every step is kept in an append-only history plus the audit log.
//
// Documents live in the private store; they are only readable through
// short-lived signed URLs issued after an access check, and every issued URL
// is audited.
package verification

import (
	"time"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/storage"
)

// Application statuses.
const (
	StatusDraft       = "draft"
	StatusSubmitted   = "submitted"
	StatusUnderReview = "under_review"
	StatusApproved    = "approved"
	StatusRejected    = "rejected"
)

// Contexts (whose verification a document belongs to).
const (
	ContextOwner    = "owner"
	ContextOperator = "operator"
)

// Document kinds per use.
var (
	OwnerIdentityDocs   = []string{"pan", "passport", "voter_id", "driving_licence"}
	OwnerBusinessDocs   = []string{"gst_certificate", "business_registration", "udyam", "cin", "address_proof"}
	OperatorLicenceDocs = []string{"driving_licence"}
	// Aadhaar images are not accepted for storage. Aadhaar ID numbers and
	// unmasked Aadhaar copies are sensitive identity data; use PAN or another
	// approved identity document instead.
	OperatorIdentityDocs = []string{"pan", "passport", "voter_id", "operator_certificate"}
)

// Check is one item of a verification checklist.
type Check struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Done  bool   `json:"done"`
}

// Document is an uploaded verification document (never with a public URL).
type Document struct {
	ID          uuid.UUID  `json:"id"`
	DocType     string     `json:"doc_type"`
	Status      string     `json:"status" doc:"submitted, verified or rejected"`
	NumberLast4 *string    `json:"number_last4,omitempty"`
	ContentType *string    `json:"content_type,omitempty"`
	ExpiresOn   *string    `json:"expires_on,omitempty"`
	ReviewNote  *string    `json:"review_note,omitempty"`
	ReviewedAt  *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	// Download is only filled for admins reviewing an application.
	Download *storage.SignedURL `json:"download,omitempty"`
}

// Application is the review state of an owner or operator.
type Application struct {
	ID              uuid.UUID  `json:"id"`
	Role            string     `json:"role"`
	Status          string     `json:"status"`
	SubmittedAt     *time.Time `json:"submitted_at,omitempty"`
	ReviewedAt      *time.Time `json:"reviewed_at,omitempty"`
	RejectionReason *string    `json:"rejection_reason,omitempty"`
}

// Address is a postal address.
type Address struct {
	Label     string     `json:"label" binding:"omitempty,max=60"`
	Line1     string     `json:"line1" binding:"required,min=3,max=200"`
	Line2     *string    `json:"line2" binding:"omitempty,max=200"`
	Locality  *string    `json:"locality" binding:"omitempty,max=100"`
	City      string     `json:"city" binding:"required,max=100"`
	District  *string    `json:"district" binding:"omitempty,max=100"`
	StateCode string     `json:"state_code" binding:"required,statecode"`
	Pincode   string     `json:"pincode" binding:"required,pincode"`
	ConsentAt *time.Time `json:"-"`
}

// ---- customer ------------------------------------------------------------------------

type CustomerStatus struct {
	Complete  bool               `json:"complete"`
	Checks    []Check            `json:"checks"`
	AvatarURL *storage.SignedURL `json:"avatar_url,omitempty"`
}

type AvatarRequest struct {
	UploadID uuid.UUID `json:"upload_id" binding:"required"`
}

// ---- owner ------------------------------------------------------------------------------

type OwnerBusinessRequest struct {
	AccountKind  string  `json:"account_kind" binding:"required,oneof=individual company"`
	OrgType      string  `json:"org_type" binding:"required,oneof=individual proprietorship partnership llp private_limited public_limited other" doc:"individual/proprietorship for individuals"`
	LegalName    string  `json:"legal_name" binding:"required,min=2,max=200"`
	BusinessName string  `json:"business_name" binding:"required,min=2,max=100"`
	PAN          string  `json:"pan" binding:"required,len=10" doc:"Stored encrypted; only the last 4 characters are shown"`
	GSTIN        *string `json:"gstin" binding:"omitempty,len=15"`
	UdyamNumber  *string `json:"udyam_number" binding:"omitempty,max=19"`
	CIN          *string `json:"cin" binding:"omitempty,len=21"`
	ContactName  string  `json:"contact_name" binding:"required,min=2,max=100"`
	ContactPhone string  `json:"contact_phone" binding:"required,e164"`
	ContactEmail string  `json:"contact_email" binding:"required,email,max=254"`
	Address      Address `json:"business_address" binding:"required"`
}

type DocumentRequest struct {
	DocType        string    `json:"doc_type" binding:"required,oneof=pan passport voter_id driving_licence gst_certificate business_registration udyam cin address_proof operator_certificate"`
	UploadID       uuid.UUID `json:"upload_id" binding:"required"`
	DocumentNumber *string   `json:"document_number" binding:"omitempty,min=4,max=30" doc:"Optional; stored encrypted"`
	ExpiresOn      *string   `json:"expires_on" binding:"omitempty,datetime=2006-01-02"`
}

type BankAccountRequest struct {
	AccountHolderName    string `json:"account_holder_name" binding:"required,min=2,max=100"`
	AccountNumber        string `json:"account_number" binding:"required,numeric,min=9,max=18"`
	ConfirmAccountNumber string `json:"confirm_account_number" binding:"required,eqfield=AccountNumber"`
	IFSC                 string `json:"ifsc" binding:"required,len=11"`
}

type BankAccount struct {
	ID                 uuid.UUID  `json:"id"`
	AccountHolderName  string     `json:"account_holder_name"`
	AccountLast4       string     `json:"account_last4"`
	IFSC               string     `json:"ifsc"`
	VerificationStatus string     `json:"verification_status"`
	ProviderReference  *string    `json:"provider_reference,omitempty"`
	VerifiedAt         *time.Time `json:"verified_at,omitempty"`
}

type OwnerVerification struct {
	OrganizationID  uuid.UUID    `json:"organization_id"`
	AccountKind     string       `json:"account_kind"`
	OrgType         string       `json:"org_type"`
	LegalName       string       `json:"legal_name"`
	BusinessName    string       `json:"business_name"`
	PANLast4        *string      `json:"pan_last4,omitempty"`
	GSTIN           *string      `json:"gstin,omitempty"`
	UdyamNumber     *string      `json:"udyam_number,omitempty"`
	CIN             *string      `json:"cin,omitempty"`
	ContactName     *string      `json:"contact_name,omitempty"`
	ContactPhone    *string      `json:"contact_phone,omitempty"`
	ContactEmail    *string      `json:"contact_email,omitempty"`
	BusinessAddress *Address     `json:"business_address,omitempty"`
	KYCStatus       string       `json:"kyc_status"`
	Application     Application  `json:"application"`
	Documents       []Document   `json:"documents"`
	BankAccount     *BankAccount `json:"bank_account,omitempty"`
	Checks          []Check      `json:"checks"`
	Editable        bool         `json:"editable"`
}

// ---- operator --------------------------------------------------------------------------

type Availability struct {
	Days  []string `json:"days" binding:"required,min=1,max=7,dive,oneof=mon tue wed thu fri sat sun"`
	Start string   `json:"start" binding:"required,datetime=15:04"`
	End   string   `json:"end" binding:"required,datetime=15:04"`
}

type OperatorProfileRequest struct {
	LicenceNumber    string       `json:"licence_number" binding:"required,min=8,max=20" doc:"Stored encrypted"`
	LicenceClass     string       `json:"licence_class" binding:"required,min=2,max=40"`
	LicenceExpiresOn string       `json:"licence_expires_on" binding:"required,datetime=2006-01-02"`
	ExperienceYears  int16        `json:"experience_years" binding:"min=0,max=60"`
	Skills           []string     `json:"skills" binding:"required,min=1,max=20,dive,max=60" doc:"Equipment category slugs"`
	Availability     Availability `json:"availability" binding:"required"`
}

type OperatorHomeAddressRequest struct {
	Line1          string  `json:"line1" binding:"required,min=3,max=200"`
	Line2          *string `json:"line2" binding:"omitempty,max=200"`
	Locality       *string `json:"locality" binding:"omitempty,max=100"`
	City           string  `json:"city" binding:"required,max=100"`
	District       *string `json:"district" binding:"omitempty,max=100"`
	StateCode      string  `json:"state_code" binding:"required,statecode"`
	Pincode        string  `json:"pincode" binding:"required,pincode"`
	PrivacyConsent bool    `json:"privacy_consent" binding:"required"`
}

type QualificationRequest struct {
	Title      string     `json:"title" binding:"required,min=2,max=120"`
	Issuer     *string    `json:"issuer" binding:"omitempty,max=120"`
	IssuedOn   *string    `json:"issued_on" binding:"omitempty,datetime=2006-01-02"`
	ValidUntil *string    `json:"valid_until" binding:"omitempty,datetime=2006-01-02"`
	UploadID   *uuid.UUID `json:"upload_id" doc:"Optional certificate upload (purpose operator_document)"`
}

type Qualification struct {
	ID         uuid.UUID  `json:"id"`
	Title      string     `json:"title"`
	Issuer     *string    `json:"issuer,omitempty"`
	IssuedOn   *string    `json:"issued_on,omitempty"`
	ValidUntil *string    `json:"valid_until,omitempty"`
	DocumentID *uuid.UUID `json:"document_id,omitempty"`
}

type OperatorVerification struct {
	UserID           uuid.UUID       `json:"user_id"`
	Address          *Address        `json:"address,omitempty"`
	IdentityConsent  bool            `json:"identity_consent"`
	LicenceLast4     *string         `json:"licence_last4,omitempty"`
	LicenceClass     *string         `json:"licence_class,omitempty"`
	LicenceExpiresOn *string         `json:"licence_expires_on,omitempty"`
	ExperienceYears  *int16          `json:"experience_years,omitempty"`
	Skills           []string        `json:"skills"`
	Availability     *Availability   `json:"availability,omitempty"`
	OnDuty           bool            `json:"on_duty"`
	Verified         bool            `json:"verified"`
	Application      Application     `json:"application"`
	Qualifications   []Qualification `json:"qualifications"`
	Documents        []Document      `json:"documents"`
	Checks           []Check         `json:"checks"`
	Editable         bool            `json:"editable"`
}

// ---- fleet manager -----------------------------------------------------------------------

type GrantedEquipment struct {
	EquipmentID uuid.UUID `json:"equipment_id"`
	Title       string    `json:"title"`
	Status      string    `json:"status"`
	GrantedAt   time.Time `json:"granted_at"`
}

type FleetManagerView struct {
	OrganizationID   uuid.UUID          `json:"organization_id"`
	OrganizationName string             `json:"organization_name"`
	LegalName        string             `json:"legal_name"`
	CompanyVerified  bool               `json:"company_verified"`
	ContactName      *string            `json:"contact_name,omitempty"`
	ContactPhone     *string            `json:"contact_phone,omitempty"`
	ContactEmail     *string            `json:"contact_email,omitempty"`
	AllEquipment     bool               `json:"all_equipment"`
	Equipment        []GrantedEquipment `json:"equipment"`
}

type FleetAccessRequest struct {
	AllEquipment bool        `json:"all_equipment"`
	EquipmentIDs []uuid.UUID `json:"equipment_ids" binding:"max=500"`
}

type FleetManagerSummary struct {
	UserID       uuid.UUID `json:"user_id"`
	FullName     string    `json:"full_name"`
	Phone        string    `json:"phone"`
	AllEquipment bool      `json:"all_equipment"`
	GrantedCount int32     `json:"granted_count"`
}

type FleetManagerList struct {
	Data []FleetManagerSummary `json:"data"`
}

// ---- admin --------------------------------------------------------------------------------

type QueueQuery struct {
	Status string `form:"status" binding:"omitempty,oneof=submitted under_review approved rejected"`
	Role   string `form:"role" binding:"omitempty,oneof=owner operator"`
	Limit  int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Cursor string `form:"cursor"`
}

type ApplicationSummary struct {
	ID          uuid.UUID  `json:"id"`
	Role        string     `json:"role"`
	Status      string     `json:"status"`
	SubjectName string     `json:"subject_name"`
	SubmittedAt *time.Time `json:"submitted_at,omitempty"`
	ReviewedAt  *time.Time `json:"reviewed_at,omitempty"`
}

type ApplicationPage struct {
	Data       []ApplicationSummary `json:"data"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

type HistoryEntry struct {
	Action     string        `json:"action"`
	ActorID    uuid.NullUUID `json:"actor_user_id"`
	ActorName  string        `json:"actor_name,omitempty"`
	DocumentID uuid.NullUUID `json:"document_id"`
	Reason     *string       `json:"reason,omitempty"`
	At         time.Time     `json:"at"`
}

type ApplicationDetail struct {
	Application
	SubjectName string `json:"subject_name"`
	// AccountUserID is the applicant's account (the operator, or the owner
	// user who submitted), e.g. for suspension.
	AccountUserID *uuid.UUID            `json:"account_user_id,omitempty"`
	Owner         *OwnerVerification    `json:"owner,omitempty"`
	Operator      *OperatorVerification `json:"operator,omitempty"`
	History       []HistoryEntry        `json:"history"`
}

type DocumentReviewRequest struct {
	Decision string `json:"decision" binding:"required,oneof=approve reject"`
	Reason   string `json:"reason" binding:"max=500" doc:"Required to reject"`
}

type DecisionRequest struct {
	Reason string `json:"reason" binding:"max=1000" doc:"Required to reject or revoke"`
}

type DocumentURLResponse = storage.SignedURL
