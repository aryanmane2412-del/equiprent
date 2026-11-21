package bookings

import (
	"context"
	"crypto/rand"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/events"
	"github.com/equiprent/backend/internal/httpx"
)

const maxRentalDays = 180

// Service implements the booking use cases.
type Service struct {
	scope          EquipmentScope // fleet-manager machine scope (optional)
	repo           Repository
	tx             database.Transactor
	cfg            config.BookingConfig
	now            func() time.Time
	inspectionGate func(context.Context, uuid.UUID, string) (int32, error)
	// recordInspection stores the customer's own acknowledgement (see Act).
	recordInspection func(ctx context.Context, booking uuid.UUID, stage string, user uuid.UUID, role string) error
}

func (s *Service) WithInspectionRecorder(f func(context.Context, uuid.UUID, string, uuid.UUID, string) error) *Service {
	s.recordInspection = f
	return s
}

func (s *Service) WithInspectionGate(gate func(context.Context, uuid.UUID, string) (int32, error)) *Service {
	s.inspectionGate = gate
	return s
}

type DriverCompensation struct {
	ID                   uuid.UUID  `json:"id"`
	BookingID            uuid.UUID  `json:"booking_id"`
	OwnerOrgID           uuid.UUID  `json:"owner_organization_id"`
	DriverUserID         uuid.UUID  `json:"driver_user_id"`
	DriverName           string     `json:"driver_name"`
	DriverPhone          string     `json:"driver_phone,omitempty"`
	AgreedAmountPaise    int64      `json:"agreed_amount_paise"`
	Status               string     `json:"status"`
	TransactionReference *string    `json:"transaction_reference,omitempty"`
	PaidAt               *time.Time `json:"paid_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
}

type DriverCompensationEvent struct {
	Status               string    `json:"status"`
	AmountPaise          int64     `json:"amount_paise"`
	TransactionReference *string   `json:"transaction_reference,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
}

type DriverCompensationList struct {
	Data    []DriverCompensation      `json:"data"`
	History []DriverCompensationEvent `json:"history"`
}

type DriverCompensationRequest struct {
	AgreedAmountPaise int64 `json:"agreed_amount_paise" binding:"required,min=1,max=100000000000"`
}

type DriverCompensationPaymentRequest struct {
	TransactionReference string `json:"transaction_reference" binding:"required,min=1,max=120"`
}

func compensation(id, bookingID, orgID, driverID uuid.UUID, name, phone string, amount int64, status string, reference *string, paidAt *time.Time, createdAt time.Time) DriverCompensation {
	return DriverCompensation{ID: id, BookingID: bookingID, OwnerOrgID: orgID, DriverUserID: driverID, DriverName: name, DriverPhone: phone,
		AgreedAmountPaise: amount, Status: status, TransactionReference: reference, PaidAt: paidAt, CreatedAt: createdAt}
}

func (s *Service) RecordDriverCompensation(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID, amount int64) (DriverCompensation, error) {
	b, side, err := s.Access(ctx, p, bookingID)
	if err != nil {
		return DriverCompensation{}, err
	}
	if !side.Has(SideOwner) {
		return DriverCompensation{}, apperror.ErrForbidden
	}
	if b.Operator == nil || !b.WithOperator {
		return DriverCompensation{}, apperror.ErrBusinessRule.WithDetail("assign an operator to a wet-hire booking before agreeing driver payment")
	}
	return s.repo.DriverCompensation(ctx, b.OwnerOrgID, b.ID, amount, p.UserID)
}

func (s *Service) DriverCompensations(ctx context.Context, p *httpx.Principal, orgID uuid.UUID) ([]DriverCompensation, error) {
	if !p.HasOrgRole(orgID, httpx.RoleOwner) {
		return nil, apperror.ErrForbidden
	}
	return s.repo.DriverCompensations(ctx, orgID, 100)
}

func (s *Service) PayDriverCompensation(ctx context.Context, p *httpx.Principal, orgID, id uuid.UUID, reference string) (DriverCompensation, error) {
	if !p.HasOrgRole(orgID, httpx.RoleOwner) {
		return DriverCompensation{}, apperror.ErrForbidden
	}
	if s.inspectionGate != nil {
		row, err := s.repo.DriverCompensationByID(ctx, orgID, id)
		if err != nil {
			return DriverCompensation{}, err
		}
		if row.Status != "paid" {
			if n, e := s.inspectionGate(ctx, row.BookingID, "return"); e != nil {
				return DriverCompensation{}, e
			} else if n < 2 {
				return DriverCompensation{}, apperror.ErrBusinessRule.WithDetail("driver settlement requires completed return inspection acknowledgements")
			}
		}
	}
	return s.repo.PayDriverCompensation(ctx, orgID, id, strings.TrimSpace(reference))
}

// OperatorDriverCompensations returns only payments belonging to the signed-in driver.
func (s *Service) OperatorDriverCompensations(ctx context.Context, p *httpx.Principal) ([]DriverCompensation, error) {
	isOperator := false
	for _, role := range p.Roles {
		if role.Code == httpx.RoleOperator {
			isOperator = true
			break
		}
	}
	if !isOperator {
		return nil, apperror.ErrForbidden
	}
	return s.repo.OperatorDriverCompensations(ctx, p.UserID, 100)
}

func (s *Service) OperatorDriverCompensationHistory(ctx context.Context, p *httpx.Principal) ([]DriverCompensationEvent, error) {
	isOperator := false
	for _, role := range p.Roles {
		if role.Code == httpx.RoleOperator {
			isOperator = true
			break
		}
	}
	if !isOperator {
		return nil, apperror.ErrForbidden
	}
	return s.repo.OperatorDriverCompensationHistory(ctx, p.UserID, 300)
}

func NewService(repo Repository, tx database.Transactor, cfg config.BookingConfig) *Service {
	return &Service{repo: repo, tx: tx, cfg: cfg, now: time.Now}
}

// ---- pricing ---------------------------------------------------------------------

func (s *Service) validatePeriod(p Period) error {
	now := s.now()
	switch {
	case p.Start.Before(now.Add(-5 * time.Minute)):
		return apperror.ErrValidation.WithFields(apperror.FieldError{Field: "period.start", Code: "in_past", Message: "must be in the future"})
	case !p.End.After(p.Start):
		return apperror.ErrValidation.WithFields(apperror.FieldError{Field: "period.end", Code: "gtfield", Message: "must be after start"})
	case p.End.Sub(p.Start) > maxRentalDays*24*time.Hour:
		return apperror.ErrValidation.WithFields(apperror.FieldError{Field: "period.end", Code: "max", Message: "rentals are limited to 180 days"})
	}
	return nil
}

// ---- access ----------------------------------------------------------------------

// Access loads a booking and the caller's sides; non-parties get 404 (the
// booking's existence is not disclosed).
func (s *Service) Access(ctx context.Context, p *httpx.Principal, id uuid.UUID) (Booking, Side, error) {
	b, err := s.repo.Get(ctx, id)
	if err != nil {
		return Booking{}, 0, err
	}
	side := SidesFor(p, b)
	// A fleet manager acts for the owner only on machines in their scope.
	if side.Has(SideOwner) && s.scope != nil && !p.HasOrgRole(b.OwnerOrgID, httpx.RoleOwner) {
		if err := s.scope.CanManage(ctx, p, b.OwnerOrgID, b.EquipmentID); err != nil {
			side &^= SideOwner
		}
	}
	if side == 0 {
		return Booking{}, 0, apperror.ErrNotFound
	}
	return b, side, nil
}

// EquipmentScope limits fleet managers to the machines they manage
// (implemented by internal/access).
type EquipmentScope interface {
	CanManage(ctx context.Context, p *httpx.Principal, org, equipmentID uuid.UUID) error
	Scope(ctx context.Context, p *httpx.Principal, org uuid.UUID) (all bool, ids map[uuid.UUID]bool, err error)
}

// WithScope enables fleet-manager machine scope on bookings.
func (s *Service) WithScope(sc EquipmentScope) *Service { s.scope = sc; return s }

// visibleEquipment is nil when p sees every machine of org, else the ids
// (possibly empty) they manage.
func (s *Service) visibleEquipment(ctx context.Context, p *httpx.Principal, org uuid.UUID) ([]uuid.UUID, error) {
	if s.scope == nil || p == nil || p.HasOrgRole(org, httpx.RoleOwner) {
		return nil, nil
	}
	all, ids, err := s.scope.Scope(ctx, p, org)
	if err != nil || all {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	return out, nil
}

// SidesFor works out the caller's relationship to a booking. When the
// caller acts for one of the two organisations (X-Org-Id), only that side
// counts, which disambiguates users who belong to both.
func SidesFor(p *httpx.Principal, b Booking) Side {
	var side Side
	if p.HasOrgRole(b.CustomerOrgID, httpx.RoleCustomer) {
		side |= SideCustomer
	}
	if p.HasOrgRole(b.OwnerOrgID, httpx.RoleOwner, httpx.RoleFleetManager) {
		side |= SideOwner
	}
	if b.Operator != nil && b.Operator.UserID == p.UserID {
		side |= SideOperator
	}
	if p.HasPlatformRole(httpx.RoleAdminOps, httpx.RoleAdminSupport, httpx.RoleAdminFinance) {
		side |= SideAdmin
	}
	switch p.ActingOrg {
	case b.CustomerOrgID:
		side &^= SideOwner
	case b.OwnerOrgID:
		side &^= SideCustomer
	}
	if b.Status == StatusDraft { // a draft is the customer's own business
		side &^= SideOwner | SideOperator
	}
	return side
}

// ---- queries ---------------------------------------------------------------------

// Get returns a booking with its status history, payment position and the
// actions open to the caller (parties only).
func (s *Service) Get(ctx context.Context, p *httpx.Principal, id uuid.UUID) (Booking, error) {
	b, side, err := s.Access(ctx, p, id)
	if err != nil {
		return Booking{}, err
	}
	if b.Timeline, err = s.repo.Timeline(ctx, id); err != nil {
		return Booking{}, err
	}
	if err := s.decorate(ctx, &b, side); err != nil {
		return Booking{}, err
	}
	return b, nil
}

// Status groups for lists. "action" depends on the side: requests and
// customer decisions for owners, drafts and payments for customers.
var (
	ongoingStatuses = []string{StatusConfirmed, StatusOperatorAssigned, StatusDispatched, StatusOnSite, StatusActive,
		StatusExtensionRequested, StatusReturnInitiated, StatusReturned, StatusCancellationRequested, StatusDisputed}
	pastStatuses        = []string{StatusCompleted, StatusClosed, StatusRejected, StatusExpired, StatusCancelled, StatusRefundPending, StatusRefunded}
	ownerActionStatuses = []string{StatusRequested, StatusQuoted, StatusCancellationRequested, StatusExtensionRequested,
		StatusReturnInitiated, StatusReturned, StatusConfirmed}
	customerActionStatuses = []string{StatusDraft, StatusAccepted, StatusPaymentPending, StatusPaymentProcessing,
		StatusOnSite, StatusExtensionRequested}
)

// List pages an organisation's bookings from one side ("customer"/"owner").
// On the owner side a fleet manager sees only their machines' bookings.
func (s *Service) List(ctx context.Context, p *httpx.Principal, side string, org uuid.UUID, q ListQuery) (SummaryPage, error) {
	var equipment []uuid.UUID
	if side == "owner" {
		ids, err := s.visibleEquipment(ctx, p, org)
		if err != nil {
			return SummaryPage{}, err
		}
		if ids != nil && len(ids) == 0 {
			return SummaryPage{Data: []Summary{}}, nil
		}
		equipment = ids
	}
	k, err := httpx.PageQuery{Limit: q.Limit, Cursor: q.Cursor}.Keyset()
	if err != nil {
		return SummaryPage{}, err
	}
	var statuses []string
	switch {
	case q.Status != "":
		statuses = []string{q.Status}
	case q.Group == "ongoing":
		statuses = ongoingStatuses
	case q.Group == "past":
		statuses = pastStatuses
	case q.Group == "action" && side == "owner":
		statuses = ownerActionStatuses
	case q.Group == "action":
		statuses = customerActionStatuses
	}
	rows, err := s.repo.List(ctx, side, org, statuses, equipment, k)
	if err != nil {
		return SummaryPage{}, err
	}
	page := httpx.Paginate(rows, k, func(b Summary) uuid.UUID { return b.ID })
	return SummaryPage{Data: page.Data, NextCursor: page.NextCursor}, nil
}

// ---- create ----------------------------------------------------------------------

// prepared is a validated, priced booking request.
type prepared struct {
	params db.CreateBookingParams
	facts  QuoteFacts
	price  Price
	period Period
}

// prepare validates a request for the caller's organisation and prices it
// on the server from the listing's stored terms.
func (s *Service) prepare(ctx context.Context, p *httpx.Principal, org uuid.UUID, req CreateRequest) (prepared, error) {
	unknown := apperror.ErrValidation.WithFields(apperror.FieldError{Field: "site_address_id", Code: "not_found", Message: "unknown address"})
	site, err := s.repo.Site(ctx, req.SiteAddressID)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return prepared{}, unknown
		}
		return prepared{}, err
	}
	if site.OrgID != org {
		return prepared{}, unknown
	}
	if !site.HasLocation {
		return prepared{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "site_address_id", Code: "no_location", Message: "the site needs a map location"})
	}
	facts, err := s.repo.QuoteFacts(ctx, req.EquipmentID, req.WithOperator, &site.Location, time.Time{})
	if err != nil {
		return prepared{}, err
	}
	if facts.EquipmentStatus != "active" {
		return prepared{}, apperror.ErrBusinessRule.WithDetail("this equipment is not available for booking")
	}
	if facts.OwnerOrgID == org {
		return prepared{}, apperror.ErrBusinessRule.WithDetail("you cannot book your own equipment")
	}
	period, err := resolve(Slot{Period: req.Period, Dates: req.Dates}, facts.Terms.Timezone)
	if err != nil {
		return prepared{}, err
	}
	if err := s.validatePeriod(period); err != nil {
		return prepared{}, err
	}
	if facts, err = s.repo.QuoteFacts(ctx, req.EquipmentID, req.WithOperator, &site.Location, localDate(period.Start, facts.Terms.Timezone)); err != nil {
		return prepared{}, err
	}
	choice := Choice{Unit: req.PricingUnit, WithOperator: req.WithOperator, Delivery: boolOr(req.Delivery, true),
		Pickup: boolOr(req.Pickup, true), ExpectedHours: req.ExpectedHours}
	price, err := s.quote(ctx, facts, period, choice)
	if err != nil {
		return prepared{}, err
	}
	unit := req.PricingUnit
	if unit == UnitAuto {
		unit = price.Lines[0].Unit // the column records the main unit; the snapshot has every line
	}
	mode := req.PaymentMode
	if mode == "" {
		mode = "online"
	}
	params := db.CreateBookingParams{
		EquipmentID: req.EquipmentID, CustomerOrgID: org, RequestedBy: p.UserID, SiteAddressID: req.SiteAddressID,
		SiteLat: site.Location.Lat, SiteLng: site.Location.Lng, StartsAt: period.Start, EndsAt: period.End,
		WithOperator: req.WithOperator, PricingUnit: db.PriceUnit(unit), PriceSnapshot: snapshot(price),
		Delivery: choice.Delivery, Pickup: choice.Pickup,
		EstimatedTotalPaise: price.EstimatedTotalPaise, DepositPaise: price.DepositPaise,
		PaymentMode: db.PaymentMode(mode), WorkDescription: strings.TrimSpace(req.WorkDescription),
		CustomerType: CustomerIndividual, CustomerDetails: []byte("{}"),
	}
	if facts.CancellationPolicyID != uuid.Nil {
		params.CancellationPolicyID = uuid.NullUUID{UUID: facts.CancellationPolicyID, Valid: true}
	}
	return prepared{params: params, facts: facts, price: price, period: period}, nil
}

// entry works out where a submitted booking starts: instant-book listings
// skip the owner (confirmed when paid offline, else awaiting payment); the
// others wait for the owner until the response window ends.
func (s *Service) entry(instant bool, mode string, start time.Time) (status, event string, responseDue, paymentDue *time.Time) {
	if !instant {
		d := s.now().Add(s.cfg.OwnerResponseWindow)
		return StatusRequested, events.BookingRequested, &d, nil
	}
	if mode != "online" {
		return StatusConfirmed, events.BookingConfirmed, nil, nil
	}
	return StatusPaymentPending, events.BookingAccepted, nil, s.paymentDue(start)
}

// paymentDue is when an online booking must be paid: the payment window,
// but never after the rental starts (with at least 15 minutes to pay).
func (s *Service) paymentDue(start time.Time) *time.Time {
	now := s.now()
	due := now.Add(s.cfg.PaymentWindow)
	if start.Before(due) {
		due = start
	}
	if floor := now.Add(15 * time.Minute); due.Before(floor) {
		due = floor
	}
	return &due
}

// Create books a machine for the caller's organisation, or saves a draft.
// Submitted bookings fail early on taken dates; the exclusion constraint is
// the final guard once a booking holds the calendar.
func (s *Service) Create(ctx context.Context, p *httpx.Principal, org uuid.UUID, req CreateRequest) (Booking, error) {
	if blocked, err := s.repo.Restricted(ctx, p.UserID, "new_bookings"); err != nil {
		return Booking{}, err
	} else if blocked {
		return Booking{}, apperror.ErrForbidden.WithDetail("your account cannot make new bookings (restricted by EQUIPRENT)")
	}
	pr, err := s.prepare(ctx, p, org, req)
	if err != nil {
		return Booking{}, err
	}
	params := pr.params
	if !req.Draft || req.CustomerType != "" || req.Customer != nil {
		kind, raw, err := normalizeCustomer(req.CustomerType, req.Customer)
		if err != nil {
			return Booking{}, err
		}
		params.CustomerType, params.CustomerDetails = kind, raw
	}
	var event string
	if req.Draft {
		params.Status = StatusDraft
	} else {
		if err := s.ensureFree(ctx, req.EquipmentID, uuid.Nil, pr.period); err != nil {
			return Booking{}, err
		}
		var status string
		status, event, params.OwnerResponseDueAt, params.PaymentDueAt = s.entry(pr.facts.InstantBook, string(params.PaymentMode), pr.period.Start)
		params.Status = db.BookingStatus(status)
	}

	var id uuid.UUID
	for attempt := 0; ; attempt++ {
		params.Reference = NewReference(s.now())
		err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: httpx.RoleCustomer}, func(ctx context.Context) error {
			t, err := s.repo.Create(ctx, params)
			if err != nil {
				return err
			}
			id = t.ID
			if event == "" {
				return nil
			}
			return s.repo.Emit(ctx, t.ID, event, events.Payload{
				Reference: params.Reference, CustomerOrgID: org, OwnerOrgID: pr.facts.OwnerOrgID, ActorUserID: p.UserID,
			})
		})
		if err == nil || attempt == 4 || !isReferenceClash(err) {
			break
		}
	}
	if err != nil {
		return Booking{}, err
	}
	return s.Get(ctx, p, id)
}

// ensureFree fails with BOOKING_OVERLAP when held bookings (other than
// self) or owner blocks cover part of the period.
func (s *Service) ensureFree(ctx context.Context, equipmentID, self uuid.UUID, p Period) error {
	busy, err := s.repo.ConflictsExcept(ctx, equipmentID, self, p.Start, p.End)
	if err != nil {
		return err
	}
	if len(busy) > 0 {
		return apperror.ErrBookingOverlap.WithDetail("the machine is booked or blocked for part of this period")
	}
	return nil
}

func isReferenceClash(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.ConstraintName == "bookings_reference_key"
}

const refAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I

// NewReference returns a human booking reference like ER-2611-4X7K
// (year+month, then 4 random characters).
func NewReference(now time.Time) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = refAlphabet[int(b[i])%len(refAlphabet)]
	}
	return "ER-" + now.UTC().Format("0601") + "-" + string(b)
}

// ---- lifecycle -------------------------------------------------------------------

// checkVersion refuses to act on a booking that changed since the client
// loaded it (when the client sent the version it saw).
func checkVersion(b Booking, want int32) error {
	if want != 0 && want != b.Version {
		return apperror.ErrVersionMismatch.WithDetail("the booking changed since you loaded it; reload and retry")
	}
	return nil
}

// Act performs a lifecycle action as the caller.
func (s *Service) Act(ctx context.Context, p *httpx.Principal, id uuid.UUID, action Action, req ActionRequest) (Transition, error) {
	reason := req.Reason
	if action == ActionCancel {
		res, err := s.Cancel(ctx, p, id, req, false)
		return Transition{ID: res.BookingID, Status: res.Status, Version: res.Version}, err
	}
	b, side, err := s.Access(ctx, p, id)
	if err != nil {
		return Transition{}, err
	}
	if err := checkVersion(b, req.Version); err != nil {
		return Transition{}, err
	}
	rule, ok := Rules[action]
	if !ok {
		return Transition{}, apperror.ErrNotFound
	}
	allowed := side & rule.Sides
	if allowed == 0 {
		return Transition{}, apperror.ErrForbidden.WithDetail("you cannot " + strings.ReplaceAll(string(action), "_", " ") + " this booking")
	}
	if !slices.Contains(rule.From, b.Status) {
		return Transition{}, apperror.ErrInvalidTransition.WithDetail("the booking is " + b.Status)
	}
	// Confirming the handover / releasing the machine is the customer's acknowledgement of that stage's inspection.
	if s.recordInspection != nil && allowed.Has(SideCustomer) && (action == ActionStart || action == ActionRequestReturn) {
		stage := "handover"
		if action == ActionRequestReturn {
			stage = "return"
		}
		if err := s.recordInspection(ctx, b.ID, stage, p.UserID, "customer"); err != nil {
			return Transition{}, err
		}
	}
	if action == ActionDispatch {
		ok, err := s.repo.HasChecklist(ctx, b.ID, "handover")
		if err != nil {
			return Transition{}, err
		}
		if !ok {
			return Transition{}, apperror.ErrBusinessRule.WithDetail("complete the pre-delivery inspection before dispatch")
		}
	}
	// Operators work from evidence: before photos and hour meter at
	// handover, after photos at return.
	if allowed == SideOperator {
		need := map[Action]string{ActionStart: "handover", ActionMarkReturned: "return"}[action]
		if need != "" {
			ok, err := s.repo.HasChecklist(ctx, b.ID, need)
			if err != nil {
				return Transition{}, err
			}
			if !ok {
				return Transition{}, apperror.ErrBusinessRule.WithDetail("submit the " + need + " checklist with photos first")
			}
		}
	}
	if action == ActionStart && s.inspectionGate != nil {
		n, err := s.inspectionGate(ctx, b.ID, "handover")
		if err != nil {
			return Transition{}, err
		}
		if n < 2 {
			return Transition{}, apperror.ErrBusinessRule.WithDetail("customer and equipment-side inspection acknowledgements are required before work starts")
		}
	}
	if action == ActionMarkReturned && s.inspectionGate != nil {
		n, err := s.inspectionGate(ctx, b.ID, "return")
		if err != nil {
			return Transition{}, err
		}
		if n < 2 {
			return Transition{}, apperror.ErrBusinessRule.WithDetail("customer and equipment-side return acknowledgements are required")
		}
	}
	if action == ActionReject && strings.TrimSpace(reason) == "" {
		return Transition{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "reason", Code: "required", Message: "tell the customer why"})
	}
	if action == ActionAccept {
		// Confirm availability before holding the slot (the exclusion
		// constraint decides races between two acceptances).
		if err := s.ensureFree(ctx, b.EquipmentID, b.ID, b.Period); err != nil {
			return Transition{}, err
		}
	}

	var out Transition
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: allowed.Role(), Reason: reason}, func(ctx context.Context) error {
		if action == ActionComplete && b.PaymentMode == "online" {
			if b.FinalizedAt == nil || b.FinalTotal == nil {
				return apperror.ErrBusinessRule.WithDetail("settle the final bill before completing an online booking")
			}
			paid, refunded, err := s.repo.PaymentTotals(ctx, b.ID)
			if err != nil {
				return err
			}
			if max(*b.FinalTotal+b.DepositPaise-(paid-refunded), 0) > 0 {
				return apperror.ErrBusinessRule.WithDetail("the customer must pay the final balance before the booking can be completed")
			}
		}
		if action == ActionAccept {
			// Machine first, then booking rows: concurrent acceptances queue
			// here; the loser then finds its request changed or rejected.
			if err := s.repo.LockEquipment(ctx, b.EquipmentID); err != nil {
				return err
			}
		}
		t, err := s.repo.Transition(ctx, b.ID, b.Status, b.Version, rule.To, nil)
		if err != nil {
			return err
		}
		if err := s.afterTransition(ctx, p, b, action, &t); err != nil {
			return err
		}
		out = t
		if rule.Event == "" {
			return nil
		}
		return s.repo.Emit(ctx, b.ID, rule.Event, payload(b, p.UserID))
	})
	return out, err
}

// afterTransition applies the side effects of an action in the same
// transaction.
func (s *Service) afterTransition(ctx context.Context, p *httpx.Principal, b Booking, action Action, t *Transition) error {
	switch action {
	case ActionAccept:
		// Offline/credit bookings are confirmed at once; online ones wait for
		// the advance payment until the payment deadline.
		next, event, due := StatusPaymentPending, "", s.paymentDue(b.Period.Start)
		if b.PaymentMode != "online" {
			next, event, due = StatusConfirmed, events.BookingConfirmed, nil
		}
		nt, err := s.repo.TransitionDue(ctx, b.ID, t.Status, t.Version, next, due)
		if err != nil {
			return err
		}
		*t = nt
		if event != "" {
			if err := s.repo.Emit(ctx, b.ID, event, payload(b, p.UserID)); err != nil {
				return err
			}
		}
		return s.rejectOverlapping(ctx, p, b)
	case ActionDispatch:
		operator := uuid.NullUUID{}
		if b.Operator != nil {
			operator = uuid.NullUUID{UUID: b.Operator.UserID, Valid: true}
		}
		return s.repo.StartTrip(ctx, b.ID, b.EquipmentID, operator, "mobilisation")
	case ActionArrive:
		// The mobilisation trip ends on site (geofence or manual), so the
		// machine's next dispatch is not refused for an open trip.
		return s.repo.EndTrips(ctx, b.ID)
	case ActionMarkReturned:
		if err := s.repo.CompleteAssignments(ctx, b.ID); err != nil {
			return err
		}
		// Whatever journey is still open (e.g. the trip back) ends with the return.
		return s.repo.EndTrips(ctx, b.ID)
	}
	return nil
}

// rejectOverlapping declines the open requests that can no longer be
// accepted because b now holds their dates.
func (s *Service) rejectOverlapping(ctx context.Context, p *httpx.Principal, b Booking) error {
	others, err := s.repo.OverlappingRequests(ctx, b)
	if err != nil {
		return err
	}
	reason := "These dates were booked by another customer."
	for _, o := range others {
		if _, err := s.repo.Transition(ctx, o.ID, o.Status, o.Version, StatusRejected, &reason); err != nil {
			return err
		}
		pl := events.Payload{Reference: o.Reference, CustomerOrgID: o.CustomerOrgID, OwnerOrgID: o.OwnerOrgID,
			ActorUserID: p.UserID, Title: b.EquipmentTitle, Extra: map[string]any{"reason": reason}}
		if err := s.repo.Emit(ctx, o.ID, events.BookingRejected, pl); err != nil {
			return err
		}
	}
	return nil
}

// Customers cancel these without a fee (the owner has not committed yet).
var freeCancel = []string{StatusDraft, StatusRequested, StatusQuoted}

// Once the machine has left the yard, a customer's cancellation needs the
// owner's approval.
var approvalCancel = []string{StatusDispatched, StatusOnSite}

// cancelPreview works out the fee and refund of a cancellation by the
// given side right now.
func (s *Service) cancelPreview(ctx context.Context, b Booking, bySide Side) (fee, refund int64, err error) {
	if bySide == SideCustomer && !slices.Contains(freeCancel, b.Status) {
		fee = CancellationFee(b.cancellationRules, b.EstimatedTotal, s.now(), b.Period.Start)
	}
	pays, err := s.repo.RefundablePayments(ctx, b.ID)
	if err != nil {
		return 0, 0, err
	}
	var held int64
	for _, x := range pays {
		held += x.RefundablePaise
	}
	return fee, max(held-fee, 0), nil
}

// Cancel cancels (or previews cancelling) a booking. Customers pay the
// policy fee once the owner has accepted, out of what they paid; owner and
// admin cancellations refund everything. A customer cancelling after
// dispatch asks the owner (cancellation_requested).
func (s *Service) Cancel(ctx context.Context, p *httpx.Principal, id uuid.UUID, req ActionRequest, dryRun bool) (CancelResult, error) {
	b, side, err := s.Access(ctx, p, id)
	if err != nil {
		return CancelResult{}, err
	}
	rule := Rules[ActionCancel]
	allowed := side & rule.Sides
	if allowed == 0 {
		return CancelResult{}, apperror.ErrForbidden
	}
	if b.Status == StatusCancellationRequested {
		return CancelResult{}, apperror.ErrInvalidTransition.WithDetail("a cancellation is already waiting for the owner")
	}
	if !slices.Contains(rule.From, b.Status) {
		return CancelResult{}, apperror.ErrInvalidTransition.WithDetail("a booking that is " + b.Status + " cannot be cancelled")
	}
	if err := checkVersion(b, req.Version); err != nil {
		return CancelResult{}, err
	}
	byCustomer := allowed == SideCustomer
	fee, refund, err := s.cancelPreview(ctx, b, allowed)
	if err != nil {
		return CancelResult{}, err
	}
	res := CancelResult{BookingID: b.ID, Status: b.Status, Version: b.Version, FeePaise: fee, RefundPaise: refund, DryRun: dryRun,
		RequiresApproval: byCustomer && slices.Contains(approvalCancel, b.Status)}
	if dryRun {
		return res, nil
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return CancelResult{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "reason", Code: "required", Message: "a reason is required"})
	}
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: allowed.Role(), Reason: reason}, func(ctx context.Context) error {
		if res.RequiresApproval {
			t, err := s.repo.Transition(ctx, b.ID, b.Status, b.Version, StatusCancellationRequested, nil)
			if err != nil {
				return err
			}
			res.Status, res.Version = t.Status, t.Version
			pl := payload(b, p.UserID)
			pl.Extra = map[string]any{"reason": reason, "fee_paise": fee}
			return s.repo.Emit(ctx, b.ID, events.BookingCancellationRequested, pl)
		}
		t, refunded, err := s.cancelNow(ctx, p.UserID, b, b.Status, b.Version, fee, reason, allowed.Role())
		if err != nil {
			return err
		}
		res.Status, res.Version, res.RefundPaise = t.Status, t.Version, refunded
		return nil
	})
	return res, err
}

// cancelNow ends a booking: refunds what was paid (keeping the fee),
// releases the operator and announces it. With money to return the booking
// goes to refund_pending, else cancelled. Runs inside the caller's tx.
func (s *Service) cancelNow(ctx context.Context, actor uuid.UUID, b Booking, from string, version int32, fee int64, reason, by string) (Transition, int64, error) {
	refunded, err := s.startRefunds(ctx, b, fee, "Cancellation: "+reason, actor, "cancel")
	if err != nil {
		return Transition{}, 0, err
	}
	to := StatusCancelled
	if refunded > 0 {
		to = StatusRefundPending
	}
	t, err := s.repo.Transition(ctx, b.ID, from, version, to, &reason)
	if err != nil {
		return Transition{}, 0, err
	}
	if err := s.repo.CompleteAssignments(ctx, b.ID); err != nil {
		return Transition{}, 0, err
	}
	if err := s.repo.EndTrips(ctx, b.ID); err != nil {
		return Transition{}, 0, err
	}
	pl := payload(b, actor)
	pl.Extra = map[string]any{"fee_paise": fee, "refund_paise": refunded, "reason": reason, "cancelled_by": by}
	return t, refunded, s.repo.Emit(ctx, b.ID, events.BookingCancelled, pl)
}

// startRefunds queues refunds of every captured payment, keeping `retain`
// (the cancellation fee) out of rental payments first, then deposits.
// Refund rows carry an idempotency key per booking, purpose tag and
// payment, so repeating the call never refunds twice. Returns the amount
// queued now.
func (s *Service) startRefunds(ctx context.Context, b Booking, retain int64, reason string, actor uuid.UUID, tag string) (int64, error) {
	pays, err := s.repo.RefundablePayments(ctx, b.ID)
	if err != nil {
		return 0, err
	}
	slices.SortStableFunc(pays, func(x, y Refundable) int { // deposits last: the fee comes out of rent first
		return boolInt(x.Purpose == "deposit") - boolInt(y.Purpose == "deposit")
	})
	by := uuid.NullUUID{UUID: actor, Valid: actor != uuid.Nil}
	var total int64
	for _, x := range pays {
		keep := min(retain, x.RefundablePaise)
		retain -= keep
		amount := x.RefundablePaise - keep
		if amount <= 0 {
			continue
		}
		created, err := s.repo.CreateRefund(ctx, db.CreateRefundParams{
			PaymentID: x.PaymentID, BookingID: b.ID, AmountPaise: amount, Reason: reason, InitiatedBy: by,
			IdempotencyKey: "booking:" + b.ID.String() + ":" + tag + ":" + x.PaymentID.String(),
		})
		if err != nil {
			return 0, err
		}
		if created {
			total += amount
		}
	}
	return total, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// AssignOperator puts one of the owner's operators on a confirmed booking.
func (s *Service) AssignOperator(ctx context.Context, p *httpx.Principal, id, operatorID uuid.UUID) (Transition, error) {
	b, side, err := s.Access(ctx, p, id)
	if err != nil {
		return Transition{}, err
	}
	if !side.Has(SideOwner) {
		return Transition{}, apperror.ErrForbidden
	}
	reassign := b.Status == StatusOperatorAssigned
	if b.Status != StatusConfirmed && !reassign {
		return Transition{}, apperror.ErrInvalidTransition.WithDetail("operators are assigned to confirmed bookings; this one is " + b.Status)
	}
	if reassign && b.Operator != nil && b.Operator.UserID == operatorID {
		return Transition{}, apperror.ErrConflict.WithDetail("this operator is already assigned")
	}
	if !b.WithOperator {
		return Transition{}, apperror.ErrBusinessRule.WithDetail("this is a dry-hire booking")
	}
	ok, err := s.repo.IsOrgOperator(ctx, operatorID, b.OwnerOrgID)
	if err != nil {
		return Transition{}, err
	}
	if !ok {
		return Transition{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "operator_user_id", Code: "not_operator", Message: "is not an operator of your organisation"})
	}
	verified, err := s.repo.OperatorVerified(ctx, operatorID)
	if err != nil {
		return Transition{}, err
	}
	if !verified {
		return Transition{}, apperror.ErrKYCRequired.WithDetail("this operator has not completed verification")
	}
	eligible, err := s.repo.OperatorEligibleForEquipment(ctx, operatorID, b.EquipmentID)
	if err != nil {
		return Transition{}, err
	}
	if !eligible {
		return Transition{}, apperror.ErrBusinessRule.WithDetail("this operator is not verified for the equipment category, or has an expired driving licence")
	}
	if blocked, err := s.repo.Restricted(ctx, operatorID, "operator_assignments"); err != nil {
		return Transition{}, err
	} else if blocked {
		return Transition{}, apperror.ErrBusinessRule.WithDetail("this operator cannot take new assignments (restricted by EQUIPRENT)")
	}
	var out Transition
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "owner"}, func(ctx context.Context) error {
		if reassign {
			if err := s.repo.ReplaceAssignment(ctx, b.ID); err != nil {
				return err
			}
			if b.Operator != nil {
				old := payload(b, p.UserID)
				old.UserIDs = []uuid.UUID{b.Operator.UserID}
				if err := s.repo.Emit(ctx, b.ID, events.BookingJobUnassigned, old); err != nil {
					return err
				}
			}
		}
		if err := s.repo.Assign(ctx, b.ID, operatorID, p.UserID); err != nil {
			if errors.Is(err, apperror.ErrConflict) {
				return apperror.ErrConflict.WithDetail("this operator already has a job that overlaps these dates")
			}
			return err
		}
		if reassign {
			out = Transition{ID: b.ID, Status: b.Status, Version: b.Version}
		} else {
			t, err := s.repo.Transition(ctx, b.ID, b.Status, b.Version, StatusOperatorAssigned, nil)
			if err != nil {
				return err
			}
			out = t
		}
		pl := payload(b, p.UserID)
		if err := s.repo.Emit(ctx, b.ID, events.BookingOperatorAssigned, pl); err != nil { // customer
			return err
		}
		pl.UserIDs = []uuid.UUID{operatorID}
		return s.repo.Emit(ctx, b.ID, events.BookingJobAssigned, pl) // the driver
	})
	return out, err
}

// RespondToJob lets the assigned operator accept or decline. Declining
// returns the booking to confirmed so the owner can assign someone else.
func (s *Service) RespondToJob(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID, accept bool) (Transition, error) {
	assignment, err := s.repo.OperatorAssignment(ctx, bookingID, p.UserID)
	if err != nil {
		return Transition{}, err
	}
	b, err := s.repo.Get(ctx, bookingID)
	if err != nil {
		return Transition{}, err
	}
	if accept {
		verified, err := s.repo.OperatorVerified(ctx, p.UserID)
		if err != nil {
			return Transition{}, err
		}
		if !verified {
			return Transition{}, apperror.ErrKYCRequired.WithDetail("driver verification must be approved before accepting assignments")
		}
		eligible, err := s.repo.OperatorEligibleForEquipment(ctx, p.UserID, b.EquipmentID)
		if err != nil {
			return Transition{}, err
		}
		if !eligible {
			return Transition{}, apperror.ErrBusinessRule.WithDetail("this driver is not currently eligible for this equipment category or has an expired licence")
		}
		if blocked, err := s.repo.Restricted(ctx, p.UserID, "operator_assignments"); err != nil {
			return Transition{}, err
		} else if blocked {
			return Transition{}, apperror.ErrBusinessRule.WithDetail("this driver cannot accept assignments (restricted by EQUIPRENT)")
		}
	}
	out := Transition{ID: b.ID, Status: b.Status, Version: b.Version}
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "operator"}, func(ctx context.Context) error {
		pl := payload(b, p.UserID)
		if accept {
			if err := s.repo.RespondAssignment(ctx, assignment, "accepted"); err != nil {
				return err
			}
			return s.repo.Emit(ctx, b.ID, events.BookingOperatorAccepted, pl)
		}
		if err := s.repo.RespondAssignment(ctx, assignment, "declined"); err != nil {
			return err
		}
		if b.Status == StatusOperatorAssigned {
			t, err := s.repo.Transition(ctx, b.ID, b.Status, b.Version, StatusConfirmed, nil)
			if err != nil {
				return err
			}
			out = t
		}
		return s.repo.Emit(ctx, b.ID, events.BookingOperatorDeclined, pl)
	})
	return out, err
}

// Statuses in which the customer can (still) pay the advance.
var awaitingPayment = []string{StatusAccepted, StatusPaymentPending, StatusPaymentProcessing}

// Statuses after which money that arrives late is returned.
var deadStatuses = []string{StatusCancelled, StatusExpired, StatusRejected, StatusRefundPending, StatusRefunded}

// CanPayAdvance reports whether the advance may be paid now.
func CanPayAdvance(status string) bool { return slices.Contains(awaitingPayment, status) }

// PaymentStarted marks a checkout in progress (payment_pending →
// payment_processing). Called by the payments module in its transaction.
func (s *Service) PaymentStarted(ctx context.Context, bookingID uuid.UUID) error {
	b, err := s.repo.Get(ctx, bookingID)
	if err != nil {
		return err
	}
	if b.Status != StatusPaymentPending {
		return nil
	}
	_, err = s.repo.Transition(ctx, b.ID, b.Status, b.Version, StatusPaymentProcessing, nil)
	return err
}

// PaymentFailed returns a booking whose checkout failed to awaiting
// payment (the deadline still applies).
func (s *Service) PaymentFailed(ctx context.Context, bookingID uuid.UUID) error {
	b, err := s.repo.Get(ctx, bookingID)
	if err != nil {
		return err
	}
	if b.Status != StatusPaymentProcessing {
		return nil
	}
	_, err = s.repo.Transition(ctx, b.ID, b.Status, b.Version, StatusPaymentPending, nil)
	return err
}

// ConfirmPaid confirms a booking once its advance is captured. A payment
// that lands after the booking was cancelled, expired or rejected (a late
// webhook) is refunded in full. Called by the payments module inside its
// transaction (system actor).
func (s *Service) ConfirmPaid(ctx context.Context, bookingID uuid.UUID) error {
	b, err := s.repo.Get(ctx, bookingID)
	if err != nil {
		return err
	}
	switch {
	case slices.Contains(awaitingPayment, b.Status):
		if _, err := s.repo.Transition(ctx, b.ID, b.Status, b.Version, StatusConfirmed, nil); err != nil {
			return err
		}
		return s.repo.Emit(ctx, b.ID, events.BookingConfirmed, payload(b, uuid.Nil))
	case slices.Contains(deadStatuses, b.Status):
		return s.RefundLatePayment(ctx, b)
	}
	return nil // already confirmed: nothing to do
}

// RefundLatePayment returns money captured for a booking that is no longer
// going ahead, moving it to refund_pending.
func (s *Service) RefundLatePayment(ctx context.Context, b Booking) error {
	queued, err := s.startRefunds(ctx, b, 0, "Payment received after the booking ended", uuid.Nil, "late")
	if err != nil || queued == 0 {
		return err
	}
	if b.Status != StatusRefundPending {
		if _, err := s.repo.Transition(ctx, b.ID, b.Status, b.Version, StatusRefundPending, nil); err != nil {
			return err
		}
	}
	pl := payload(b, uuid.Nil)
	pl.Extra = map[string]any{"refund_paise": queued, "amount_paise": queued}
	return s.repo.Emit(ctx, b.ID, events.BookingRefundPending, pl)
}

// MarkDisputed moves a running or finished booking to disputed (used by the
// disputes module inside its transaction). Other statuses are left alone.
func (s *Service) MarkDisputed(ctx context.Context, b Booking) error {
	if b.Status != StatusActive && b.Status != StatusReturned && b.Status != StatusCompleted {
		return nil
	}
	_, err := s.repo.Transition(ctx, b.ID, b.Status, b.Version, StatusDisputed, nil)
	return err
}

// ResumeAfterDispute returns a disputed booking to the state it was in when the
// dispute was opened (return, inspection and payment requirements still apply from
// there). Called by the disputes module once no dispute on the booking is open.
func (s *Service) ResumeAfterDispute(ctx context.Context, bookingID uuid.UUID) error {
	b, err := s.repo.Get(ctx, bookingID)
	if err != nil {
		return err
	}
	if b.Status != StatusDisputed {
		return nil
	}
	to := StatusCompleted // bookings disputed before the previous state was recorded
	if b.ResumeStatus != nil {
		to = *b.ResumeStatus
	}
	_, err = s.repo.Transition(ctx, b.ID, b.Status, b.Version, to, nil)
	return err
}

// Load returns a booking for system use (no access check).
func (s *Service) Load(ctx context.Context, id uuid.UUID) (Booking, error) {
	return s.repo.Get(ctx, id)
}

// EventPayload is the outbox payload naming both parties of a booking.
func EventPayload(b Booking, actor uuid.UUID) events.Payload { return payload(b, actor) }

func payload(b Booking, actor uuid.UUID) events.Payload {
	return events.Payload{
		Reference: b.Reference, CustomerOrgID: b.CustomerOrgID, OwnerOrgID: b.OwnerOrgID,
		ActorUserID: actor, Title: b.EquipmentTitle,
	}
}
