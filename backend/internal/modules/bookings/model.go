// Package bookings implements the rental lifecycle: price estimates,
// booking requests (with idempotency), owner responses, operator assignment,
// dispatch → on site → active → return → completion, cancellation with
// policy fees, and the timeline. The database enforces the transition graph
// and double-booking; this package adds who may do what.
package bookings

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Status values (booking_status enum).
const (
	StatusDraft                 = "draft"
	StatusPaymentProcessing     = "payment_processing"
	StatusExtensionRequested    = "extension_requested"
	StatusCancellationRequested = "cancellation_requested"
	StatusRefundPending         = "refund_pending"
	StatusRefunded              = "refunded"
	StatusRequested             = "requested"
	StatusQuoted                = "quoted"
	StatusAccepted              = "accepted"
	StatusPaymentPending        = "payment_pending"
	StatusConfirmed             = "confirmed"
	StatusOperatorAssigned      = "operator_assigned"
	StatusDispatched            = "dispatched"
	StatusOnSite                = "on_site"
	StatusActive                = "active"
	StatusReturnInitiated       = "return_initiated"
	StatusReturned              = "returned"
	StatusCompleted             = "completed"
	StatusClosed                = "closed"
	StatusRejected              = "rejected"
	StatusExpired               = "expired"
	StatusCancelled             = "cancelled"
	StatusDisputed              = "disputed"
)

// Side is a caller's relationship to a booking (bit set).
type Side uint8

const (
	SideCustomer Side = 1 << iota
	SideOwner
	SideOperator
	SideAdmin
)

// Has reports whether s includes any of want.
func (s Side) Has(want Side) bool { return s&want != 0 }

// Role is the audit role name for the side.
func (s Side) Role() string {
	switch {
	case s.Has(SideAdmin):
		return "admin"
	case s.Has(SideOwner):
		return "owner"
	case s.Has(SideOperator):
		return "operator"
	case s.Has(SideCustomer):
		return "customer"
	}
	return ""
}

// Action is a lifecycle step a party can take.
type Action string

const (
	ActionAccept         Action = "accept"
	ActionReject         Action = "reject"
	ActionCancel         Action = "cancel"
	ActionDispatch       Action = "dispatch"
	ActionArrive         Action = "arrive"
	ActionStart          Action = "start"
	ActionRequestReturn  Action = "request_return"
	ActionMarkReturned   Action = "mark_returned"
	ActionComplete       Action = "complete"
	ActionClose          Action = "close"
	ActionConfirmPayment Action = "confirm_payment"
	ActionResolveDispute Action = "resolve_dispute"
)

// Rule says from which statuses an action is allowed, where it leads and who
// may take it.
type Rule struct {
	From  []string
	To    string
	Sides Side
	Event string
}

// Rules is the lifecycle as seen by the API (subset of the DB graph in
// booking_status_transitions, which remains the final authority).
var Rules = map[Action]Rule{
	ActionAccept: {From: []string{StatusRequested, StatusQuoted}, To: StatusAccepted, Sides: SideOwner, Event: "booking.accepted"},
	ActionReject: {From: []string{StatusRequested, StatusQuoted}, To: StatusRejected, Sides: SideOwner | SideAdmin, Event: "booking.rejected"},
	ActionCancel: {From: []string{StatusDraft, StatusRequested, StatusQuoted, StatusAccepted, StatusPaymentPending, StatusPaymentProcessing,
		StatusConfirmed, StatusOperatorAssigned, StatusDispatched, StatusOnSite}, To: StatusCancelled, Sides: SideCustomer | SideOwner | SideAdmin, Event: "booking.cancelled"},
	ActionDispatch:       {From: []string{StatusConfirmed, StatusOperatorAssigned}, To: StatusDispatched, Sides: SideOwner | SideOperator, Event: "booking.dispatched"},
	ActionArrive:         {From: []string{StatusDispatched}, To: StatusOnSite, Sides: SideOwner | SideOperator, Event: "booking.on_site"},
	ActionStart:          {From: []string{StatusOnSite}, To: StatusActive, Sides: SideCustomer | SideOperator, Event: "booking.active"},
	ActionRequestReturn:  {From: []string{StatusActive}, To: StatusReturnInitiated, Sides: SideCustomer | SideOwner, Event: "booking.return_initiated"},
	ActionMarkReturned:   {From: []string{StatusReturnInitiated}, To: StatusReturned, Sides: SideOwner | SideOperator, Event: "booking.returned"},
	ActionComplete:       {From: []string{StatusReturned}, To: StatusCompleted, Sides: SideOwner | SideAdmin, Event: "booking.completed"},
	ActionClose:          {From: []string{StatusCompleted}, To: StatusClosed, Sides: SideAdmin, Event: ""},
	ActionConfirmPayment: {From: []string{StatusAccepted, StatusPaymentPending, StatusPaymentProcessing}, To: StatusConfirmed, Sides: SideAdmin, Event: "booking.confirmed"},
}

// GeoPoint is a WGS84 coordinate.
type GeoPoint struct {
	Lat float64 `json:"lat" binding:"latitude"`
	Lng float64 `json:"lng" binding:"longitude"`
}

// Period is a half-open time range [start, end).
type Period struct {
	Start time.Time `json:"start" binding:"required"`
	End   time.Time `json:"end" binding:"required,gtfield=Start"`
}

// Booking is the full booking view.
type Booking struct {
	ID                 uuid.UUID       `json:"id"`
	Reference          string          `json:"reference"`
	Status             string          `json:"status"`
	Version            int32           `json:"version"`
	EquipmentID        uuid.UUID       `json:"equipment_id"`
	EquipmentTitle     string          `json:"equipment_title"`
	OwnerOrgID         uuid.UUID       `json:"owner_org_id"`
	OwnerName          string          `json:"owner_name"`
	CustomerOrgID      uuid.UUID       `json:"customer_org_id"`
	CustomerName       string          `json:"customer_name"`
	RequestedBy        uuid.UUID       `json:"requested_by"`
	SiteAddressID      uuid.UUID       `json:"site_address_id"`
	Site               GeoPoint        `json:"site"`
	SiteGeofenceM      int32           `json:"site_geofence_m"`
	Period             Period          `json:"period"`
	WithOperator       bool            `json:"with_operator"`
	PricingUnit        string          `json:"pricing_unit"`
	Price              json.RawMessage `json:"price"`
	EstimatedTotal     int64           `json:"estimated_total_paise"`
	FinalTotal         *int64          `json:"final_total_paise,omitempty"`
	FinalCharges       json.RawMessage `json:"final_charges,omitempty" swaggertype:"object" doc:"Settled after the rental (kind final); price is the estimate"`
	FinalizedAt        *time.Time      `json:"finalized_at,omitempty"`
	Delivery           bool            `json:"delivery"`
	Pickup             bool            `json:"pickup"`
	DepositPaise       int64           `json:"deposit_paise"`
	Currency           string          `json:"currency"`
	PaymentMode        string          `json:"payment_mode"`
	WorkDescription    string          `json:"work_description"`
	CancelReason       *string         `json:"cancel_reason,omitempty"`
	OwnerResponseDueAt *time.Time      `json:"owner_response_due_at,omitempty"`
	PaymentDueAt       *time.Time      `json:"payment_due_at,omitempty" doc:"Online bookings awaiting payment must pay by then or the dates are released"`
	ResumeStatus       *string         `json:"resume_status,omitempty" doc:"Where a pending extension/cancellation request returns if declined"`
	Timezone           string          `json:"timezone"`
	SiteLabel          string          `json:"site_label"`
	SiteAddress        string          `json:"site_address"`
	StatusChangedAt    time.Time       `json:"status_changed_at"`
	CreatedAt          time.Time       `json:"created_at"`
	Operator           *Assignment     `json:"operator,omitempty"`
	Timeline           []Event         `json:"timeline,omitempty"`

	// Filled on the detail view for the caller.
	Actions          []string            `json:"actions" doc:"What the caller may do now (names match the lifecycle endpoints)"`
	Customer         *CustomerInfo       `json:"customer,omitempty" doc:"Owner side: who is renting"`
	Money            *MoneySummary       `json:"money,omitempty"`
	PendingExtension *Extension          `json:"pending_extension,omitempty"`
	Availability     *AvailabilityResult `json:"availability,omitempty" doc:"Owner side, open requests: are the dates still free?"`
	Cancellation     *CancellationView   `json:"cancellation_policy,omitempty"`

	cancellationRules json.RawMessage
	requesterName     string
	requesterPhone    string
	customerType      string
	customerDetails   CustomerDetails
	customerKYC       string
	customerRentals   int32
}

// CustomerInfo is what an owner sees about the renter. The phone number is
// shared once the owner has accepted the booking.
type CustomerInfo struct {
	CustomerType     string `json:"customer_type"`
	CompanyName      string `json:"company_name,omitempty"`
	Email            string `json:"email,omitempty"`
	Address          string `json:"address,omitempty"`
	City             string `json:"city,omitempty"`
	State            string `json:"state,omitempty"`
	PinCode          string `json:"pin_code,omitempty"`
	GSTIN            string `json:"gstin,omitempty"`
	OrgName          string `json:"org_name"`
	ContactName      string `json:"contact_name"`
	Phone            string `json:"phone,omitempty"`
	KYCStatus        string `json:"kyc_status"`
	CompletedRentals int32  `json:"completed_rentals"`
}

// MoneySummary is the payment position of a booking.
type MoneySummary struct {
	PaymentMode     string     `json:"payment_mode"`
	AdvanceDuePaise int64      `json:"advance_due_paise" doc:"Paid online to confirm the booking"`
	DepositPaise    int64      `json:"deposit_paise"`
	PaidPaise       int64      `json:"paid_paise" doc:"Captured, all purposes"`
	RefundedPaise   int64      `json:"refunded_paise" doc:"Refunded or being refunded"`
	BalanceDuePaise int64      `json:"balance_due_paise" doc:"Total (final once settled) + deposit - net paid; never negative"`
	PaymentDueAt    *time.Time `json:"payment_due_at,omitempty"`
	Refunds         []Refund   `json:"refunds"`
}

// Refund is money going back to the customer.
type Refund struct {
	ID          uuid.UUID  `json:"id"`
	PaymentID   uuid.UUID  `json:"payment_id"`
	AmountPaise int64      `json:"amount_paise"`
	Reason      string     `json:"reason"`
	Status      string     `json:"status" doc:"pending, processed or failed"`
	ProcessedAt *time.Time `json:"processed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// CancellationView explains the booking's cancellation policy.
type CancellationView struct {
	Rules   []CancellationRule `json:"rules"`
	FeeNow  int64              `json:"fee_now_paise" doc:"What the customer would pay to cancel right now"`
	Summary string             `json:"summary"`
}

// Extension is a request to move a running booking's end.
type Extension struct {
	ID             uuid.UUID  `json:"id"`
	RequestedBy    uuid.UUID  `json:"requested_by"`
	RequestedSide  string     `json:"requested_side" doc:"customer or owner; the other side answers"`
	PreviousEnd    time.Time  `json:"previous_end"`
	NewEnd         time.Time  `json:"new_end"`
	AmountPaise    int64      `json:"amount_paise" doc:"Estimated charge for the extra period, taxes included"`
	Note           string     `json:"note"`
	Status         string     `json:"status" doc:"pending, approved, declined, withdrawn or expired"`
	ResponseReason *string    `json:"response_reason,omitempty"`
	RespondedAt    *time.Time `json:"responded_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type ExtensionList struct {
	Data []Extension `json:"data"`
}

// ExtensionRequest asks to extend a running booking.
type ExtensionRequest struct {
	NewEnd time.Time `json:"new_end" binding:"required" doc:"New end (RFC 3339), after the current end"`
	Note   string    `json:"note" binding:"max=500"`
	DryRun bool      `json:"dry_run" doc:"Only price it and check availability"`
}

// ExtensionQuote is the price and availability of a proposed extension.
type ExtensionQuote struct {
	PreviousEnd  time.Time          `json:"previous_end"`
	NewEnd       time.Time          `json:"new_end"`
	AmountPaise  int64              `json:"amount_paise"`
	Price        Price              `json:"price"`
	Availability AvailabilityResult `json:"availability"`
	Extension    *Extension         `json:"extension,omitempty" doc:"The created request (not on a dry run)"`
}

// Policy is the platform's booking, payment and cancellation policy.
type Policy struct {
	AdvanceBps               int                `json:"advance_bps" doc:"Share of the estimate paid online to confirm (5000 = 50%)"`
	PaymentWindowMinutes     int                `json:"payment_window_minutes" doc:"Time to pay once the owner accepts"`
	OwnerResponseMinutes     int                `json:"owner_response_minutes" doc:"Time the owner has to answer a request"`
	ProcessingTimeoutMinutes int                `json:"processing_timeout_minutes" doc:"An unfinished checkout returns to awaiting payment after this"`
	DefaultCancellation      []CancellationRule `json:"default_cancellation_rules"`
	FreeCancellation         []string           `json:"free_cancellation_statuses" doc:"The customer cancels without a fee in these statuses"`
	ApprovalNeeded           []string           `json:"cancellation_needs_owner_approval" doc:"Customer cancellations in these statuses need the owner's approval"`
	Extendable               []string           `json:"extendable_statuses"`
}

// Assignment is the operator on the job.
type Assignment struct {
	ID     uuid.UUID `json:"id"`
	UserID uuid.UUID `json:"user_id"`
	Name   string    `json:"name"`
	Phone  string    `json:"phone,omitempty"`
	Status string    `json:"status"`
}

// Event is one timeline entry.
type Event struct {
	From      *string       `json:"from,omitempty"`
	To        string        `json:"to"`
	ActorID   uuid.NullUUID `json:"actor_user_id"`
	ActorRole *string       `json:"actor_role,omitempty"`
	Reason    *string       `json:"reason,omitempty"`
	At        time.Time     `json:"at"`
}

// Summary is a booking row in lists.
type Summary struct {
	ID             uuid.UUID  `json:"id"`
	Reference      string     `json:"reference"`
	Status         string     `json:"status"`
	Version        int32      `json:"version"`
	EquipmentID    uuid.UUID  `json:"equipment_id"`
	EquipmentTitle string     `json:"equipment_title"`
	OwnerOrgID     uuid.UUID  `json:"owner_org_id"`
	CustomerOrgID  uuid.UUID  `json:"customer_org_id"`
	Period         Period     `json:"period"`
	EstimatedTotal int64      `json:"estimated_total_paise"`
	FinalTotal     *int64     `json:"final_total_paise,omitempty"`
	Currency       string     `json:"currency"`
	OwnerName      string     `json:"owner_name"`
	CustomerName   string     `json:"customer_name"`
	WithOperator   bool       `json:"with_operator"`
	PaymentDueAt   *time.Time `json:"payment_due_at,omitempty"`
	ResponseDueAt  *time.Time `json:"owner_response_due_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// Requests ------------------------------------------------------------------------

// EstimateRequest prices a hypothetical booking.
type EstimateRequest struct {
	Site          *GeoPoint  `json:"site" doc:"Work site; required for delivery or pickup"`
	Period        *Period    `json:"period,omitempty" doc:"Exact start and end (RFC 3339, any offset)"`
	Dates         *DateRange `json:"dates,omitempty" doc:"Or local dates in the listing's time zone"`
	PricingUnit   string     `json:"pricing_unit" binding:"required,oneof=hour shift day week month auto" doc:"auto = cheapest mix for any duration"`
	WithOperator  bool       `json:"with_operator"`
	Delivery      *bool      `json:"delivery" doc:"Owner delivers to site (default true)"`
	Pickup        *bool      `json:"pickup" doc:"Owner collects from site (default true)"`
	ExpectedHours float64    `json:"expected_hours" binding:"omitempty,min=0,max=10000" doc:"Working hours (extra hours and fuel)"`
}

func (r EstimateRequest) choice() Choice {
	return Choice{Unit: r.PricingUnit, WithOperator: r.WithOperator, Delivery: boolOr(r.Delivery, true), Pickup: boolOr(r.Pickup, true),
		ExpectedHours: r.ExpectedHours}
}

// CreateRequest books a machine.
type CreateRequest struct {
	EquipmentID     uuid.UUID        `json:"equipment_id" binding:"required"`
	SiteAddressID   uuid.UUID        `json:"site_address_id" binding:"required"`
	Period          *Period          `json:"period,omitempty"`
	Dates           *DateRange       `json:"dates,omitempty" doc:"Or local dates in the listing's time zone"`
	PricingUnit     string           `json:"pricing_unit" binding:"required,oneof=hour shift day week month auto"`
	WithOperator    bool             `json:"with_operator"`
	Delivery        *bool            `json:"delivery" doc:"Owner delivers to site (default true)"`
	Pickup          *bool            `json:"pickup" doc:"Owner collects from site (default true)"`
	ExpectedHours   float64          `json:"expected_hours" binding:"omitempty,min=0,max=10000"`
	WorkDescription string           `json:"work_description" binding:"max=2000"`
	PaymentMode     string           `json:"payment_mode" binding:"omitempty,oneof=online offline"`
	Draft           bool             `json:"draft" doc:"Save as a draft (does not hold the dates); submit it later"`
	CustomerType    string           `json:"customer_type" binding:"omitempty,oneof=individual company" doc:"individual (personal use) or company (business use)"`
	Customer        *CustomerDetails `json:"customer,omitempty" doc:"Contact and business details for the chosen customer type"`
}

// DraftRequest replaces a draft's choices (PATCH /bookings/:id).
type DraftRequest struct {
	SiteAddressID   uuid.UUID  `json:"site_address_id" binding:"required"`
	Period          *Period    `json:"period,omitempty"`
	Dates           *DateRange `json:"dates,omitempty"`
	PricingUnit     string     `json:"pricing_unit" binding:"required,oneof=hour shift day week month auto"`
	WithOperator    bool       `json:"with_operator"`
	Delivery        *bool      `json:"delivery"`
	Pickup          *bool      `json:"pickup"`
	ExpectedHours   float64    `json:"expected_hours" binding:"omitempty,min=0,max=10000"`
	WorkDescription string     `json:"work_description" binding:"max=2000"`
	PaymentMode     string     `json:"payment_mode" binding:"omitempty,oneof=online offline"`
	Version         int32      `json:"version" binding:"min=0" doc:"The version you edited (optional)"`
}

func (d DraftRequest) create() CreateRequest {
	return CreateRequest{SiteAddressID: d.SiteAddressID, Period: d.Period, Dates: d.Dates, PricingUnit: d.PricingUnit,
		WithOperator: d.WithOperator, Delivery: d.Delivery, Pickup: d.Pickup, ExpectedHours: d.ExpectedHours,
		WorkDescription: d.WorkDescription, PaymentMode: d.PaymentMode}
}

// ActionRequest carries an optional reason (required to cancel or reject).
// Version, when given, is the version the client saw: a booking that changed
// meanwhile is refused with VERSION_MISMATCH instead of being acted on.
type ActionRequest struct {
	Reason  string `json:"reason" binding:"max=500"`
	Version int32  `json:"version" binding:"min=0"`
}

// CancelQuery previews the fee with dry_run=true.
type CancelQuery struct {
	DryRun bool `form:"dry_run"`
}

// CancelResult describes a cancellation (or its preview).
type CancelResult struct {
	BookingID        uuid.UUID `json:"booking_id"`
	Status           string    `json:"status" doc:"cancelled, refund_pending, or cancellation_requested when the owner must approve"`
	Version          int32     `json:"version"`
	FeePaise         int64     `json:"fee_paise" doc:"Cancellation fee under the booking's policy"`
	RefundPaise      int64     `json:"refund_paise" doc:"Paid money going back to the customer"`
	RequiresApproval bool      `json:"requires_approval" doc:"The machine has left the yard: the owner approves the cancellation"`
	DryRun           bool      `json:"dry_run"`
}

// AssignOperatorRequest puts an operator on a job.
type AssignOperatorRequest struct {
	OperatorUserID uuid.UUID `json:"operator_user_id" binding:"required"`
}

// ListQuery filters booking lists.
type ListQuery struct {
	Status string `form:"status" binding:"omitempty,oneof=draft requested quoted accepted payment_pending payment_processing confirmed operator_assigned dispatched on_site active extension_requested return_initiated returned completed closed rejected expired cancelled disputed cancellation_requested refund_pending refunded"`
	Group  string `form:"group" binding:"omitempty,oneof=action ongoing past" doc:"action = waiting for the caller; ongoing = in progress; past = finished (rental history)"`
	Limit  int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Cursor string `form:"cursor"`
}

// SummaryPage is a page of bookings.
type SummaryPage struct {
	Data       []Summary `json:"data"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

// Transition is the result of a lifecycle action.
type Transition struct {
	ID      uuid.UUID `json:"id"`
	Status  string    `json:"status"`
	Version int32     `json:"version"`
}
