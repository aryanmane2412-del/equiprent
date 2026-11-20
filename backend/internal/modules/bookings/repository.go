package bookings

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/events"
	"github.com/equiprent/backend/internal/httpx"
)

// QuoteFacts are the listing facts needed to price and validate a request.
type QuoteFacts struct {
	EquipmentID          uuid.UUID
	OwnerOrgID           uuid.UUID
	EquipmentStatus      string
	InstantBook          bool
	CategoryID           uuid.UUID
	CityID               uuid.NullUUID
	CancellationPolicyID uuid.UUID
	HasSite              bool
	DistanceKm           float64
	Terms                Terms
}

// Site is a customer's saved work site.
type Site struct {
	OrgID       uuid.UUID
	Location    GeoPoint
	HasLocation bool
}

// Repository is the persistence the bookings service needs.
type Repository interface {
	// QuoteFacts loads a listing's terms with the plans valid on onDate (zero
	// = today) for the hire mode; site (optional) gives the distance.
	QuoteFacts(ctx context.Context, equipmentID uuid.UUID, withOperator bool, site *GeoPoint, onDate time.Time) (QuoteFacts, error)
	CustomerFeeBps(ctx context.Context, category uuid.UUID, city uuid.NullUUID, owner uuid.UUID) (int32, error)
	Taxes(ctx context.Context, onDate time.Time) ([]TaxRule, error)
	Conflicts(ctx context.Context, equipmentID uuid.UUID, from, to time.Time) ([]Busy, error)
	NextFree(ctx context.Context, equipmentID uuid.UUID, from time.Time, hours float64) (*time.Time, error)
	SetFinalCharges(ctx context.Context, p db.SetBookingFinalChargesParams) error
	TaxRateList(ctx context.Context) ([]TaxRate, error)
	SaveTaxRate(ctx context.Context, req TaxRateRequest) error
	ReviewTaxRate(ctx context.Context, id uuid.UUID, req TaxReviewRequest) error
	Site(ctx context.Context, addressID uuid.UUID) (Site, error)

	Create(ctx context.Context, p db.CreateBookingParams) (Transition, error)
	Get(ctx context.Context, id uuid.UUID) (Booking, error)
	Timeline(ctx context.Context, id uuid.UUID) ([]Event, error)
	// List pages bookings; equipment (non-nil) limits it to those machines.
	List(ctx context.Context, side string, org uuid.UUID, statuses []string, equipment []uuid.UUID, k httpx.Keyset) ([]Summary, error)
	// HasChecklist reports whether a handover/return checklist was submitted.
	HasChecklist(ctx context.Context, bookingID uuid.UUID, kind string) (bool, error)
	// ReplaceAssignment marks the booking's open assignment replaced.
	ReplaceAssignment(ctx context.Context, bookingID uuid.UUID) error
	// Restricted reports an active account restriction of a kind.
	Restricted(ctx context.Context, userID uuid.UUID, kind string) (bool, error)
	Transition(ctx context.Context, id uuid.UUID, from string, version int32, to string, reason *string) (Transition, error)
	// TransitionDue also sets the payment deadline (entering payment_pending).
	TransitionDue(ctx context.Context, id uuid.UUID, from string, version int32, to string, paymentDue *time.Time) (Transition, error)

	// drafts
	UpdateDraft(ctx context.Context, p db.UpdateDraftBookingParams) (Transition, error)
	Reprice(ctx context.Context, p db.RepriceBookingParams) (Transition, error)
	SetDue(ctx context.Context, id uuid.UUID, version int32, responseDue, paymentDue *time.Time) (Transition, error)
	// OverlappingRequests are open requests for the same machine and dates.
	OverlappingRequests(ctx context.Context, b Booking) ([]Summary, error)
	// LockEquipment takes the machine's row lock for the rest of the
	// transaction (serialises changes to who holds its calendar).
	LockEquipment(ctx context.Context, equipmentID uuid.UUID) error
	// ConflictsExcept is Conflicts ignoring one booking (its own slot).
	ConflictsExcept(ctx context.Context, equipmentID, bookingID uuid.UUID, from, to time.Time) ([]Busy, error)

	// extensions
	CreateExtension(ctx context.Context, p db.CreateExtensionParams) (Extension, error)
	Extension(ctx context.Context, bookingID, id uuid.UUID) (Extension, error)
	Extensions(ctx context.Context, bookingID uuid.UUID) ([]Extension, error)
	RespondExtension(ctx context.Context, id uuid.UUID, status string, by uuid.NullUUID, reason *string) (Extension, error)
	ExtendPeriod(ctx context.Context, id uuid.UUID, version int32, newEnd time.Time, amount int64) (Transition, error)
	ApprovedExtensionsTotal(ctx context.Context, bookingID uuid.UUID) (int64, error)

	// money
	PaymentTotals(ctx context.Context, bookingID uuid.UUID) (paid, refunded int64, err error)
	RefundablePayments(ctx context.Context, bookingID uuid.UUID) ([]Refundable, error)
	CreateRefund(ctx context.Context, p db.CreateRefundParams) (bool, error)
	Refunds(ctx context.Context, bookingID uuid.UUID) ([]Refund, error)
	DefaultCancellationRules(ctx context.Context) (json.RawMessage, error)

	IsOrgOperator(ctx context.Context, userID, orgID uuid.UUID) (bool, error)
	// OperatorVerified reports whether an admin approved the operator.
	OperatorVerified(ctx context.Context, userID uuid.UUID) (bool, error)
	OperatorEligibleForEquipment(ctx context.Context, userID, equipmentID uuid.UUID) (bool, error)
	Assign(ctx context.Context, bookingID, operatorID, assignedBy uuid.UUID) error
	DriverCompensation(ctx context.Context, ownerOrgID, bookingID uuid.UUID, amount int64, actor uuid.UUID) (DriverCompensation, error)
	DriverCompensations(ctx context.Context, ownerOrgID uuid.UUID, limit int32) ([]DriverCompensation, error)
	OperatorDriverCompensations(ctx context.Context, driverUserID uuid.UUID, limit int32) ([]DriverCompensation, error)
	OperatorDriverCompensationHistory(ctx context.Context, driverUserID uuid.UUID, limit int32) ([]DriverCompensationEvent, error)
	PayDriverCompensation(ctx context.Context, ownerOrgID, id uuid.UUID, reference string) (DriverCompensation, error)
	DriverCompensationByID(ctx context.Context, ownerOrgID, id uuid.UUID) (DriverCompensation, error)
	OperatorAssignment(ctx context.Context, bookingID, operatorID uuid.UUID) (uuid.UUID, error)
	RespondAssignment(ctx context.Context, assignmentID uuid.UUID, status string) error
	CompleteAssignments(ctx context.Context, bookingID uuid.UUID) error
	// EndTrips closes a booking's open trip (cancelled on the way).
	EndTrips(ctx context.Context, bookingID uuid.UUID) error
	StartTrip(ctx context.Context, bookingID, equipmentID uuid.UUID, operator uuid.NullUUID, kind string) error

	Emit(ctx context.Context, bookingID uuid.UUID, eventType string, p events.Payload) error
}

// Refundable is a captured payment with what is left to refund.
type Refundable struct {
	PaymentID       uuid.UUID
	Purpose         string
	RefundablePaise int64
}

// PGRepository implements Repository on PostgreSQL.
type PGRepository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *PGRepository { return &PGRepository{pool: pool} }

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

func (r *PGRepository) QuoteFacts(ctx context.Context, equipmentID uuid.UUID, withOperator bool, site *GeoPoint, onDate time.Time) (QuoteFacts, error) {
	q := r.q(ctx)
	params := db.GetPricingTermsParams{EquipmentID: equipmentID}
	if site != nil {
		params.Lat, params.Lng = &site.Lat, &site.Lng
	}
	t, err := q.GetPricingTerms(ctx, params)
	if err != nil {
		return QuoteFacts{}, database.MapError(err)
	}
	if onDate.IsZero() {
		onDate = localDate(time.Now(), t.Timezone)
	}
	plans, err := q.ActivePricingPlans(ctx, db.ActivePricingPlansParams{EquipmentID: equipmentID, WithOperator: withOperator, OnDate: onDate})
	if err != nil {
		return QuoteFacts{}, database.MapError(err)
	}
	f := QuoteFacts{
		EquipmentID: t.EquipmentID, OwnerOrgID: t.OwnerOrgID, EquipmentStatus: string(t.EquipmentStatus), InstantBook: t.InstantBook,
		CategoryID: t.CategoryID, CityID: t.CityID, CancellationPolicyID: t.CancellationPolicyID, HasSite: site != nil, DistanceKm: t.DistanceKm,
		Terms: Terms{
			DeliveryFixedPaise: t.DeliveryFixedPaise, DeliveryPerKmPaise: t.DeliveryPerKmPaise, FreeKm: t.FreeKm,
			PickupFixedPaise: t.PickupFixedPaise, PickupPerKmPaise: t.PickupPerKmPaise, FuelPolicy: t.FuelPolicy,
			FuelRatePerHourPaise: t.FuelRatePerHourPaise, DepositPaise: t.SecurityDepositPaise, LateFeePerHourPaise: t.LateFeePerHourPaise,
			LateGraceMinutes: t.LateGraceMinutes, MinRentalHours: t.MinRentalHours, MaxRentalDays: t.MaxRentalDays,
			ServiceRadiusKm: t.ServiceRadiusKm, Timezone: t.Timezone, CancellationRules: t.CancellationRules,
		},
	}
	for _, pl := range plans {
		f.Terms.Plans = append(f.Terms.Plans, PlanRate{Unit: pl.Unit, RatePaise: pl.RatePaise, MinUnits: pl.MinUnits,
			IncludedHours: pl.IncludedHours, ExtraHourRatePaise: pl.ExtraHourRatePaise, OperatorBataPerDayPaise: pl.OperatorBataPerDayPaise})
	}
	return f, nil
}

func (r *PGRepository) Taxes(ctx context.Context, onDate time.Time) ([]TaxRule, error) {
	rows, err := r.q(ctx).ActiveTaxRates(ctx, onDate)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]TaxRule, 0, len(rows))
	for _, t := range rows {
		out = append(out, TaxRule{Code: t.Code, Name: t.Name, RateBps: int(t.RateBps), AppliesTo: t.AppliesTo, SACCode: t.SacCode})
	}
	return out, nil
}

func (r *PGRepository) Conflicts(ctx context.Context, equipmentID uuid.UUID, from, to time.Time) ([]Busy, error) {
	rows, err := r.q(ctx).SlotConflicts(ctx, db.SlotConflictsParams{EquipmentID: equipmentID, FromAt: from, ToAt: to})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Busy, 0, len(rows))
	for _, b := range rows {
		out = append(out, Busy{Start: b.StartsAt, End: b.EndsAt, Kind: b.Kind})
	}
	return out, nil
}

func (r *PGRepository) NextFree(ctx context.Context, equipmentID uuid.UUID, from time.Time, hours float64) (*time.Time, error) {
	t, err := r.q(ctx).NextFreeStart(ctx, db.NextFreeStartParams{EquipmentID: equipmentID, FromAt: from, Hours: hours})
	if err != nil {
		if errors.Is(database.MapError(err), apperror.ErrNotFound) {
			return nil, nil
		}
		return nil, database.MapError(err)
	}
	return &t, nil
}

func (r *PGRepository) SetFinalCharges(ctx context.Context, p db.SetBookingFinalChargesParams) error {
	if _, err := r.q(ctx).SetBookingFinalCharges(ctx, p); err != nil {
		err = database.MapError(err)
		if errors.Is(err, apperror.ErrNotFound) {
			return apperror.ErrConflict.WithDetail("the booking changed or was already settled; reload and try again")
		}
		return err
	}
	return nil
}

func (r *PGRepository) TaxRateList(ctx context.Context) ([]TaxRate, error) {
	rows, err := r.q(ctx).ListTaxRates(ctx)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]TaxRate, 0, len(rows))
	for _, t := range rows {
		x := TaxRate{ID: t.ID, Code: t.Code, Name: t.Name, RateBps: t.RateBps, AppliesTo: t.AppliesTo,
			ValidFrom: t.ValidFrom.Format("2006-01-02"), IsActive: t.IsActive, UpdatedAt: t.UpdatedAt,
			SACCode: t.SacCode, LegalBasis: t.LegalBasis, ReviewedByName: t.ReviewedByName, ReviewedAt: t.ReviewedAt}
		if t.ValidUntil != nil {
			v := t.ValidUntil.Format("2006-01-02")
			x.ValidUntil = &v
		}
		out = append(out, x)
	}
	return out, nil
}

func (r *PGRepository) SaveTaxRate(ctx context.Context, req TaxRateRequest) error {
	from, _ := time.Parse("2006-01-02", req.ValidFrom)
	var until *time.Time
	if req.ValidUntil != nil {
		u, _ := time.Parse("2006-01-02", *req.ValidUntil)
		until = &u
	}
	_, err := r.q(ctx).UpsertTaxRate(ctx, db.UpsertTaxRateParams{Code: req.Code, Name: req.Name, RateBps: req.RateBps,
		AppliesTo: req.AppliesTo, ValidFrom: from, ValidUntil: until, IsActive: boolOr(req.IsActive, true),
		SacCode: sacOr(req.SACCode), LegalBasis: req.LegalBasis})
	return database.MapError(err)
}

func sacOr(s string) string {
	if s == "" {
		return "997313"
	}
	return s
}

// ReviewTaxRate records who checked a rule against current GST notifications.
func (r *PGRepository) ReviewTaxRate(ctx context.Context, id uuid.UUID, req TaxReviewRequest) error {
	_, err := r.q(ctx).ReviewTaxRate(ctx, db.ReviewTaxRateParams{ID: id, ReviewedByName: &req.ReviewedByName, LegalBasis: req.LegalBasis})
	return database.MapError(err)
}

func (r *PGRepository) CustomerFeeBps(ctx context.Context, category uuid.UUID, city uuid.NullUUID, owner uuid.UUID) (int32, error) {
	row, err := r.q(ctx).GetCommissionRule(ctx, db.GetCommissionRuleParams{
		CategoryID: uuid.NullUUID{UUID: category, Valid: true}, CityID: city, OwnerOrgID: uuid.NullUUID{UUID: owner, Valid: true},
	})
	if err != nil {
		if errors.Is(database.MapError(err), apperror.ErrNotFound) {
			return 0, nil
		}
		return 0, database.MapError(err)
	}
	return row.CustomerFeeBps, nil
}

func (r *PGRepository) Site(ctx context.Context, addressID uuid.UUID) (Site, error) {
	a, err := r.q(ctx).GetAddress(ctx, addressID)
	if err != nil {
		return Site{}, database.MapError(err)
	}
	return Site{OrgID: a.OrganizationID, Location: GeoPoint{Lat: a.Lat, Lng: a.Lng}, HasLocation: a.HasLocation}, nil
}

func (r *PGRepository) Create(ctx context.Context, p db.CreateBookingParams) (Transition, error) {
	row, err := r.q(ctx).CreateBooking(ctx, p)
	if err != nil {
		return Transition{}, database.MapError(err)
	}
	return Transition{ID: row.ID, Status: string(row.Status), Version: row.Version}, nil
}

func (r *PGRepository) Get(ctx context.Context, id uuid.UUID) (Booking, error) {
	q := r.q(ctx)
	b, err := q.GetBookingDetail(ctx, id)
	if err != nil {
		return Booking{}, database.MapError(err)
	}
	out := Booking{
		ID: b.ID, Reference: b.Reference, Status: string(b.Status), Version: b.Version,
		EquipmentID: b.EquipmentID, EquipmentTitle: b.EquipmentTitle, OwnerOrgID: b.OwnerOrgID, OwnerName: b.OwnerName,
		CustomerOrgID: b.CustomerOrgID, CustomerName: b.CustomerName, RequestedBy: b.RequestedBy,
		SiteAddressID: b.SiteAddressID, Site: GeoPoint{Lat: b.SiteLat, Lng: b.SiteLng}, SiteGeofenceM: b.SiteGeofenceM,
		Period: Period{Start: b.StartsAt, End: b.EndsAt}, WithOperator: b.WithOperator, PricingUnit: string(b.PricingUnit),
		Price: b.PriceSnapshot, EstimatedTotal: b.EstimatedTotalPaise, FinalTotal: b.FinalTotalPaise,
		DepositPaise: b.DepositPaise, Currency: b.Currency, PaymentMode: string(b.PaymentMode),
		WorkDescription: b.WorkDescription, CancelReason: b.CancelReason, OwnerResponseDueAt: b.OwnerResponseDueAt,
		StatusChangedAt: b.StatusChangedAt, CreatedAt: b.CreatedAt, cancellationRules: b.CancellationRules,
		FinalCharges: b.FinalCharges, FinalizedAt: b.FinalizedAt, Delivery: b.Delivery, Pickup: b.Pickup,
		PaymentDueAt: b.PaymentDueAt, Timezone: b.EquipmentTimezone, SiteLabel: b.SiteLabel, SiteAddress: b.SiteAddress,
		Actions: []string{}, requesterName: b.RequesterName, requesterPhone: b.RequesterPhone,
		customerType: b.CustomerType, customerKYC: b.CustomerKycStatus, customerRentals: b.CustomerCompletedRentals,
	}
	_ = json.Unmarshal(b.CustomerDetails, &out.customerDetails)
	if b.ResumeStatus.Valid {
		rs := string(b.ResumeStatus.BookingStatus)
		out.ResumeStatus = &rs
	}
	if a, err := q.GetActiveAssignment(ctx, id); err == nil {
		out.Operator = &Assignment{ID: a.ID, UserID: a.OperatorUserID, Name: a.OperatorName, Phone: a.OperatorPhone, Status: string(a.Status)}
	} else if mapped := database.MapError(err); !errors.Is(mapped, apperror.ErrNotFound) {
		return Booking{}, mapped
	}
	return out, nil
}

func (r *PGRepository) Timeline(ctx context.Context, id uuid.UUID) ([]Event, error) {
	rows, err := r.q(ctx).ListBookingEvents(ctx, id)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Event, 0, len(rows))
	for _, e := range rows {
		ev := Event{To: string(e.ToStatus), ActorID: e.ActorUserID, ActorRole: e.ActorRole, Reason: e.Reason, At: e.CreatedAt}
		if e.FromStatus.Valid {
			from := string(e.FromStatus.BookingStatus)
			ev.From = &from
		}
		out = append(out, ev)
	}
	return out, nil
}

func (r *PGRepository) HasChecklist(ctx context.Context, bookingID uuid.UUID, kind string) (bool, error) {
	ok, err := r.q(ctx).HasChecklist(ctx, db.HasChecklistParams{BookingID: bookingID, Kind: kind})
	return ok, database.MapError(err)
}

func (r *PGRepository) ReplaceAssignment(ctx context.Context, bookingID uuid.UUID) error {
	_, err := r.q(ctx).ReplaceAssignment(ctx, bookingID)
	return database.MapError(err)
}

func (r *PGRepository) Restricted(ctx context.Context, userID uuid.UUID, kind string) (bool, error) {
	ok, err := r.q(ctx).IsRestricted(ctx, db.IsRestrictedParams{UserID: userID, Kind: kind})
	return ok, database.MapError(err)
}

func (r *PGRepository) List(ctx context.Context, side string, org uuid.UUID, statuses []string, equipment []uuid.UUID, k httpx.Keyset) ([]Summary, error) {
	rows, err := r.q(ctx).ListBookingsForOrg(ctx, db.ListBookingsForOrgParams{
		Side: side, OrgID: org, Statuses: statuses, EquipmentIds: equipment, After: k.After, ResultLimit: k.Fetch(),
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Summary, 0, len(rows))
	for _, b := range rows {
		out = append(out, Summary{
			ID: b.ID, Reference: b.Reference, Status: string(b.Status), Version: b.Version,
			EquipmentID: b.EquipmentID, EquipmentTitle: b.EquipmentTitle, OwnerOrgID: b.OwnerOrgID, CustomerOrgID: b.CustomerOrgID,
			Period: Period{Start: b.StartsAt, End: b.EndsAt}, EstimatedTotal: b.EstimatedTotalPaise, Currency: b.Currency,
			CreatedAt: b.CreatedAt, FinalTotal: b.FinalTotalPaise, OwnerName: b.OwnerName, CustomerName: b.CustomerName,
			WithOperator: b.WithOperator, PaymentDueAt: b.PaymentDueAt, ResponseDueAt: b.OwnerResponseDueAt,
		})
	}
	return out, nil
}

// Transition is a compare-and-set on status and version; losing a race (or a
// stale version) yields VERSION_MISMATCH.
func (r *PGRepository) Transition(ctx context.Context, id uuid.UUID, from string, version int32, to string, reason *string) (Transition, error) {
	return r.transition(ctx, db.TransitionBookingParams{
		ID: id, FromStatus: db.BookingStatus(from), Version: version, ToStatus: db.BookingStatus(to), CancelReason: reason,
	})
}

func (r *PGRepository) TransitionDue(ctx context.Context, id uuid.UUID, from string, version int32, to string, paymentDue *time.Time) (Transition, error) {
	return r.transition(ctx, db.TransitionBookingParams{
		ID: id, FromStatus: db.BookingStatus(from), Version: version, ToStatus: db.BookingStatus(to), PaymentDueAt: paymentDue,
	})
}

func (r *PGRepository) transition(ctx context.Context, p db.TransitionBookingParams) (Transition, error) {
	row, err := r.q(ctx).TransitionBooking(ctx, p)
	if err != nil {
		return Transition{}, casError(err)
	}
	return Transition{ID: row.ID, Status: string(row.Status), Version: row.Version}, nil
}

// casError maps "no row matched the expected status/version" to
// VERSION_MISMATCH.
func casError(err error) error {
	err = database.MapError(err)
	if errors.Is(err, apperror.ErrNotFound) {
		return apperror.ErrVersionMismatch.WithDetail("the booking changed meanwhile; reload and retry")
	}
	return err
}

func (r *PGRepository) UpdateDraft(ctx context.Context, p db.UpdateDraftBookingParams) (Transition, error) {
	row, err := r.q(ctx).UpdateDraftBooking(ctx, p)
	if err != nil {
		return Transition{}, casError(err)
	}
	return Transition{ID: row.ID, Status: string(row.Status), Version: row.Version}, nil
}

func (r *PGRepository) Reprice(ctx context.Context, p db.RepriceBookingParams) (Transition, error) {
	row, err := r.q(ctx).RepriceBooking(ctx, p)
	if err != nil {
		return Transition{}, casError(err)
	}
	return Transition{ID: row.ID, Status: string(row.Status), Version: row.Version}, nil
}

func (r *PGRepository) SetDue(ctx context.Context, id uuid.UUID, version int32, responseDue, paymentDue *time.Time) (Transition, error) {
	row, err := r.q(ctx).SetBookingResponseDue(ctx, db.SetBookingResponseDueParams{
		ID: id, Version: version, OwnerResponseDueAt: responseDue, PaymentDueAt: paymentDue,
	})
	if err != nil {
		return Transition{}, casError(err)
	}
	return Transition{ID: row.ID, Status: string(row.Status), Version: row.Version}, nil
}

func (r *PGRepository) OverlappingRequests(ctx context.Context, b Booking) ([]Summary, error) {
	rows, err := r.q(ctx).OverlappingRequests(ctx, db.OverlappingRequestsParams{
		EquipmentID: b.EquipmentID, ID: b.ID, StartsAt: b.Period.Start, EndsAt: b.Period.End,
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Summary, 0, len(rows))
	for _, x := range rows {
		out = append(out, Summary{ID: x.ID, Reference: x.Reference, Status: x.Status, Version: x.Version,
			CustomerOrgID: x.CustomerOrgID, OwnerOrgID: x.OwnerOrgID, EquipmentID: b.EquipmentID, EquipmentTitle: b.EquipmentTitle})
	}
	return out, nil
}

func (r *PGRepository) LockEquipment(ctx context.Context, equipmentID uuid.UUID) error {
	_, err := r.q(ctx).LockEquipmentForBooking(ctx, equipmentID)
	return database.MapError(err)
}

func (r *PGRepository) ConflictsExcept(ctx context.Context, equipmentID, bookingID uuid.UUID, from, to time.Time) ([]Busy, error) {
	rows, err := r.q(ctx).SlotConflictsExcept(ctx, db.SlotConflictsExceptParams{EquipmentID: equipmentID, BookingID: bookingID, FromAt: from, ToAt: to})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Busy, 0, len(rows))
	for _, b := range rows {
		out = append(out, Busy{Start: b.StartsAt, End: b.EndsAt, Kind: b.Kind})
	}
	return out, nil
}

func toExtension(x db.BookingExtension) Extension {
	return Extension{ID: x.ID, RequestedBy: x.RequestedBy, RequestedSide: x.RequestedSide, PreviousEnd: x.PreviousEnd,
		NewEnd: x.NewEnd, AmountPaise: x.AmountPaise, Note: x.Note, Status: string(x.Status), ResponseReason: x.ResponseReason,
		RespondedAt: x.RespondedAt, CreatedAt: x.CreatedAt}
}

func (r *PGRepository) CreateExtension(ctx context.Context, p db.CreateExtensionParams) (Extension, error) {
	x, err := r.q(ctx).CreateExtension(ctx, p)
	if err != nil {
		err = database.MapError(err)
		if errors.Is(err, apperror.ErrConflict) {
			return Extension{}, apperror.ErrConflict.WithDetail("an extension request is already open")
		}
		return Extension{}, err
	}
	return toExtension(x), nil
}

func (r *PGRepository) Extension(ctx context.Context, bookingID, id uuid.UUID) (Extension, error) {
	x, err := r.q(ctx).GetExtension(ctx, db.GetExtensionParams{ID: id, BookingID: bookingID})
	if err != nil {
		return Extension{}, database.MapError(err)
	}
	return toExtension(x), nil
}

func (r *PGRepository) Extensions(ctx context.Context, bookingID uuid.UUID) ([]Extension, error) {
	rows, err := r.q(ctx).ListExtensions(ctx, bookingID)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Extension, 0, len(rows))
	for _, x := range rows {
		out = append(out, toExtension(x))
	}
	return out, nil
}

func (r *PGRepository) RespondExtension(ctx context.Context, id uuid.UUID, status string, by uuid.NullUUID, reason *string) (Extension, error) {
	x, err := r.q(ctx).RespondExtension(ctx, db.RespondExtensionParams{ID: id, Status: db.ExtensionStatus(status), RespondedBy: by, ResponseReason: reason})
	if err != nil {
		err = database.MapError(err)
		if errors.Is(err, apperror.ErrNotFound) {
			return Extension{}, apperror.ErrInvalidTransition.WithDetail("this extension request was already answered")
		}
		return Extension{}, err
	}
	return toExtension(x), nil
}

func (r *PGRepository) ExtendPeriod(ctx context.Context, id uuid.UUID, version int32, newEnd time.Time, amount int64) (Transition, error) {
	q := r.q(ctx)
	row, err := q.ExtendBookingPeriod(ctx, db.ExtendBookingPeriodParams{ID: id, Version: version, NewEnd: newEnd, AmountPaise: amount})
	if err != nil {
		return Transition{}, casError(err)
	}
	if err := q.ExtendAssignmentPeriod(ctx, db.ExtendAssignmentPeriodParams{BookingID: id, NewEnd: newEnd}); err != nil {
		return Transition{}, database.MapError(err)
	}
	return Transition{ID: row.ID, Status: string(row.Status), Version: row.Version}, nil
}

func (r *PGRepository) ApprovedExtensionsTotal(ctx context.Context, bookingID uuid.UUID) (int64, error) {
	n, err := r.q(ctx).SumApprovedExtensions(ctx, bookingID)
	return n, database.MapError(err)
}

func (r *PGRepository) PaymentTotals(ctx context.Context, bookingID uuid.UUID) (paid, refunded int64, err error) {
	rows, err := r.q(ctx).BookingPaymentTotals(ctx, bookingID)
	if err != nil {
		return 0, 0, database.MapError(err)
	}
	for _, x := range rows {
		paid += x.CapturedPaise
		refunded += x.RefundedPaise
	}
	return paid, refunded, nil
}

func (r *PGRepository) RefundablePayments(ctx context.Context, bookingID uuid.UUID) ([]Refundable, error) {
	rows, err := r.q(ctx).RefundablePayments(ctx, bookingID)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Refundable, 0, len(rows))
	for _, x := range rows {
		if x.RefundablePaise > 0 {
			out = append(out, Refundable{PaymentID: x.ID, Purpose: x.Purpose, RefundablePaise: x.RefundablePaise})
		}
	}
	return out, nil
}

// CreateRefund records a refund; created=false when the idempotency key was
// already used (the refund exists).
func (r *PGRepository) CreateRefund(ctx context.Context, p db.CreateRefundParams) (bool, error) {
	_, err := r.q(ctx).CreateRefund(ctx, p)
	if err != nil {
		err = database.MapError(err)
		if errors.Is(err, apperror.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *PGRepository) Refunds(ctx context.Context, bookingID uuid.UUID) ([]Refund, error) {
	rows, err := r.q(ctx).ListBookingRefunds(ctx, bookingID)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Refund, 0, len(rows))
	for _, x := range rows {
		out = append(out, Refund{ID: x.ID, PaymentID: x.PaymentID, AmountPaise: x.AmountPaise, Reason: x.Reason, Status: x.Status,
			ProcessedAt: x.ProcessedAt, CreatedAt: x.CreatedAt})
	}
	return out, nil
}

func (r *PGRepository) DefaultCancellationRules(ctx context.Context) (json.RawMessage, error) {
	row, err := r.q(ctx).GetDefaultCancellationPolicy(ctx)
	if err != nil {
		if errors.Is(database.MapError(err), apperror.ErrNotFound) {
			return nil, nil
		}
		return nil, database.MapError(err)
	}
	return row.Rules, nil
}

func (r *PGRepository) OperatorVerified(ctx context.Context, userID uuid.UUID) (bool, error) {
	ok, err := r.q(ctx).OperatorVerified(ctx, userID)
	return ok, database.MapError(err)
}

func (r *PGRepository) OperatorEligibleForEquipment(ctx context.Context, userID, equipmentID uuid.UUID) (bool, error) {
	ok, err := r.q(ctx).OperatorEligibleForEquipment(ctx, db.OperatorEligibleForEquipmentParams{OperatorUserID: userID, EquipmentID: equipmentID})
	return ok, database.MapError(err)
}

func (r *PGRepository) IsOrgOperator(ctx context.Context, userID, orgID uuid.UUID) (bool, error) {
	ok, err := r.q(ctx).IsOrgOperator(ctx, db.IsOrgOperatorParams{UserID: userID, OrganizationID: uuid.NullUUID{UUID: orgID, Valid: true}})
	return ok, database.MapError(err)
}

func (r *PGRepository) Assign(ctx context.Context, bookingID, operatorID, assignedBy uuid.UUID) error {
	_, err := r.q(ctx).AssignOperator(ctx, db.AssignOperatorParams{BookingID: bookingID, OperatorUserID: operatorID, AssignedBy: assignedBy})
	return database.MapError(err)
}

func (r *PGRepository) DriverCompensation(ctx context.Context, orgID, bookingID uuid.UUID, amount int64, actor uuid.UUID) (DriverCompensation, error) {
	row, err := r.q(ctx).RecordDriverCompensation(ctx, db.RecordDriverCompensationParams{BookingID: bookingID, OwnerOrgID: orgID, AgreedAmountPaise: amount, RecordedBy: actor})
	if err != nil {
		return DriverCompensation{}, database.MapError(err)
	}
	return compensation(row.ID, row.BookingID, row.OwnerOrgID, row.DriverUserID, "", "", row.AgreedAmountPaise, string(row.Status), row.TransactionReference, row.PaidAt, row.CreatedAt), nil
}

func (r *PGRepository) DriverCompensations(ctx context.Context, orgID uuid.UUID, limit int32) ([]DriverCompensation, error) {
	rows, err := r.q(ctx).ListDriverCompensations(ctx, db.ListDriverCompensationsParams{OwnerOrgID: orgID, ResultLimit: limit})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]DriverCompensation, 0, len(rows))
	for _, row := range rows {
		out = append(out, compensation(row.ID, row.BookingID, row.OwnerOrgID, row.DriverUserID, row.DriverName, row.DriverPhone, row.AgreedAmountPaise, string(row.Status), row.TransactionReference, row.PaidAt, row.CreatedAt))
	}
	return out, nil
}

func (r *PGRepository) OperatorDriverCompensations(ctx context.Context, driverID uuid.UUID, limit int32) ([]DriverCompensation, error) {
	rows, err := r.q(ctx).ListOperatorDriverCompensations(ctx, db.ListOperatorDriverCompensationsParams{DriverUserID: driverID, ResultLimit: limit})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]DriverCompensation, 0, len(rows))
	for _, row := range rows {
		out = append(out, compensation(row.ID, row.BookingID, row.OwnerOrgID, row.DriverUserID, "", "", row.AgreedAmountPaise, string(row.Status), row.TransactionReference, row.PaidAt, row.CreatedAt))
	}
	return out, nil
}

func (r *PGRepository) OperatorDriverCompensationHistory(ctx context.Context, driverID uuid.UUID, limit int32) ([]DriverCompensationEvent, error) {
	rows, err := r.q(ctx).DriverCompensationHistoryForOperator(ctx, db.DriverCompensationHistoryForOperatorParams{DriverUserID: driverID, ResultLimit: limit})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]DriverCompensationEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, DriverCompensationEvent{Status: string(row.Status), AmountPaise: row.AmountPaise, TransactionReference: row.TransactionReference, CreatedAt: row.CreatedAt})
	}
	return out, nil
}

func (r *PGRepository) PayDriverCompensation(ctx context.Context, orgID, id uuid.UUID, reference string) (DriverCompensation, error) {
	row, err := r.q(ctx).PayDriverCompensation(ctx, db.PayDriverCompensationParams{ID: id, OwnerOrgID: orgID, TransactionReference: &reference})
	if err != nil {
		return DriverCompensation{}, database.MapError(err)
	}
	return compensation(row.ID, row.BookingID, row.OwnerOrgID, row.DriverUserID, "", "", row.AgreedAmountPaise, string(row.Status), row.TransactionReference, row.PaidAt, row.CreatedAt), nil
}

func (r *PGRepository) DriverCompensationByID(ctx context.Context, orgID, id uuid.UUID) (DriverCompensation, error) {
	row, err := r.q(ctx).GetDriverCompensationByID(ctx, db.GetDriverCompensationByIDParams{ID: id, OwnerOrgID: orgID})
	if err != nil {
		return DriverCompensation{}, database.MapError(err)
	}
	return compensation(row.ID, row.BookingID, row.OwnerOrgID, row.DriverUserID, row.DriverName, row.DriverPhone, row.AgreedAmountPaise, string(row.Status), row.TransactionReference, row.PaidAt, row.CreatedAt), nil
}

func (r *PGRepository) OperatorAssignment(ctx context.Context, bookingID, operatorID uuid.UUID) (uuid.UUID, error) {
	row, err := r.q(ctx).GetOperatorAssignment(ctx, db.GetOperatorAssignmentParams{BookingID: bookingID, OperatorUserID: operatorID})
	return row.ID, database.MapError(err)
}

func (r *PGRepository) RespondAssignment(ctx context.Context, assignmentID uuid.UUID, status string) error {
	_, err := r.q(ctx).RespondToAssignment(ctx, db.RespondToAssignmentParams{ID: assignmentID, Status: db.AssignmentStatus(status)})
	if errors.Is(database.MapError(err), apperror.ErrNotFound) {
		return apperror.ErrInvalidTransition.WithDetail("this assignment was already answered")
	}
	return database.MapError(err)
}

func (r *PGRepository) CompleteAssignments(ctx context.Context, bookingID uuid.UUID) error {
	return database.MapError(r.q(ctx).CompleteAssignments(ctx, bookingID))
}

func (r *PGRepository) EndTrips(ctx context.Context, bookingID uuid.UUID) error {
	return database.MapError(r.q(ctx).EndOpenTripsForBooking(ctx, bookingID))
}

func (r *PGRepository) StartTrip(ctx context.Context, bookingID, equipmentID uuid.UUID, operator uuid.NullUUID, kind string) error {
	_, err := r.q(ctx).StartTrip(ctx, db.StartTripParams{
		BookingID: bookingID, EquipmentID: equipmentID, OperatorUserID: operator, Kind: db.TripKind(kind), StartedAt: time.Now(),
	})
	return database.MapError(err)
}

func (r *PGRepository) Emit(ctx context.Context, bookingID uuid.UUID, eventType string, p events.Payload) error {
	return database.MapError(events.Emit(ctx, r.q(ctx), "booking", bookingID, eventType, p))
}

// snapshot marshals the price for storage.
func snapshot(p Price) json.RawMessage {
	b, _ := json.Marshal(p)
	return b
}
