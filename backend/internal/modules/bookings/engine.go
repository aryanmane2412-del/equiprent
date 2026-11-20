package bookings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // IANA zones on every platform (Windows has none built in)

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/events"
	"github.com/equiprent/backend/internal/httpx"
)

// ---- requests and responses -------------------------------------------------------------

// DateRange picks rental dates in the listing's local time zone. Dates are
// inclusive: 12 to 14 March from 08:00 is 72 hours, ending 15 March 08:00,
// unless end_time is given (then it ends on end_date at end_time).
type DateRange struct {
	StartDate string `json:"start_date" form:"start_date" binding:"required,datetime=2006-01-02"`
	EndDate   string `json:"end_date" form:"end_date" binding:"required,datetime=2006-01-02"`
	StartTime string `json:"start_time" form:"start_time" binding:"omitempty,datetime=15:04" doc:"Default 08:00"`
	EndTime   string `json:"end_time" form:"end_time" binding:"omitempty,datetime=15:04"`
}

// Slot is either an exact period (RFC 3339 instants, any offset) or local
// dates. Exactly one must be given.
type Slot struct {
	Period *Period    `json:"period,omitempty"`
	Dates  *DateRange `json:"dates,omitempty"`
}

// Busy is a conflicting range.
type Busy struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Kind  string    `json:"kind" doc:"booked or blocked"`
}

// AvailabilityResult answers "is this machine free then?".
type AvailabilityResult struct {
	Available          bool       `json:"available"`
	Period             PeriodView `json:"period"`
	Conflicts          []Busy     `json:"conflicts"`
	RuleProblem        string     `json:"rule_problem,omitempty" doc:"Minimum/maximum duration not met"`
	NextAvailableStart *time.Time `json:"next_available_start,omitempty" doc:"Earliest start for the same duration"`
	MinRentalHours     int32      `json:"min_rental_hours,omitempty"`
	MaxRentalDays      int32      `json:"max_rental_days,omitempty"`
}

// EstimateResponse is the estimate plus availability for the same period.
type EstimateResponse struct {
	Price
	Availability AvailabilityResult `json:"availability"`
}

// AvailabilityQuery is GET /equipment/:id/availability/check.
type AvailabilityQuery struct {
	Start     string `form:"start" binding:"omitempty" doc:"RFC 3339 instant (or use start_date)"`
	End       string `form:"end" binding:"omitempty"`
	StartDate string `form:"start_date" binding:"omitempty,datetime=2006-01-02"`
	EndDate   string `form:"end_date" binding:"omitempty,datetime=2006-01-02"`
	StartTime string `form:"start_time" binding:"omitempty,datetime=15:04"`
	EndTime   string `form:"end_time" binding:"omitempty,datetime=15:04"`
}

func (q AvailabilityQuery) slot() (Slot, error) {
	if q.StartDate != "" || q.EndDate != "" {
		return Slot{Dates: &DateRange{StartDate: q.StartDate, EndDate: q.EndDate, StartTime: q.StartTime, EndTime: q.EndTime}}, nil
	}
	s, err1 := time.Parse(time.RFC3339, q.Start)
	e, err2 := time.Parse(time.RFC3339, q.End)
	if err1 != nil || err2 != nil {
		return Slot{}, field("start", "required", "give start and end (RFC 3339) or start_date and end_date")
	}
	return Slot{Period: &Period{Start: s, End: e}}, nil
}

// FinalChargesRequest records what actually happened (owner, after return).
type FinalChargesRequest struct {
	ActualStart    time.Time `json:"actual_start" binding:"required"`
	ActualEnd      time.Time `json:"actual_end" binding:"required,gtfield=ActualStart"`
	OperatingHours float64   `json:"operating_hours" binding:"min=0,max=10000" doc:"Hour-meter hours worked (0 = not recorded)"`
	DamagePaise    int64     `json:"damage_paise" binding:"min=0,max=100000000"`
	DamageNote     string    `json:"damage_note" binding:"max=500"`
}

// FinalChargesResult is the settlement (dry run or saved).
type FinalChargesResult struct {
	FinalCharges
	BookingID uuid.UUID `json:"booking_id"`
	DryRun    bool      `json:"dry_run"`
	Saved     bool      `json:"saved"`
}

// TaxRate is an admin view of a configurable tax.
type TaxRate struct {
	ID         uuid.UUID `json:"id"`
	Code       string    `json:"code"`
	Name       string    `json:"name"`
	RateBps    int32     `json:"rate_bps"`
	AppliesTo  []string  `json:"applies_to"`
	ValidFrom  string    `json:"valid_from"`
	ValidUntil *string   `json:"valid_until,omitempty"`
	IsActive   bool      `json:"is_active"`
	UpdatedAt  time.Time `json:"updated_at"`
	SACCode    string    `json:"sac_code"`
	LegalBasis *string   `json:"legal_basis,omitempty" doc:"Notification / entry the rate is based on"`
	// A rule changed after its review needs a new review (saving clears it).
	ReviewedByName *string    `json:"reviewed_by_name,omitempty"`
	ReviewedAt     *time.Time `json:"reviewed_at,omitempty"`
}

// TaxReviewRequest records the compliance review of a tax rule.
type TaxReviewRequest struct {
	ReviewedByName string  `json:"reviewed_by_name" binding:"required,min=2,max=120" doc:"e.g. the chartered accountant who checked it"`
	LegalBasis     *string `json:"legal_basis" binding:"omitempty,max=300"`
}

type TaxRateList struct {
	Data []TaxRate `json:"data"`
}

// TaxRateRequest creates or replaces a tax for a validity start date.
type TaxRateRequest struct {
	Code       string   `json:"code" binding:"required,min=2,max=30"`
	Name       string   `json:"name" binding:"required,min=2,max=60"`
	RateBps    int32    `json:"rate_bps" binding:"min=0,max=10000"`
	AppliesTo  []string `json:"applies_to" binding:"required,min=1,dive,oneof=rental extra_hours delivery pickup operator fuel platform_fee late_return damage"`
	ValidFrom  string   `json:"valid_from" binding:"required,datetime=2006-01-02"`
	ValidUntil *string  `json:"valid_until" binding:"omitempty,datetime=2006-01-02"`
	IsActive   *bool    `json:"is_active"`
	SACCode    string   `json:"sac_code" binding:"omitempty,len=6,numeric,startswith=99" doc:"Defaults to 997313"`
	LegalBasis *string  `json:"legal_basis" binding:"omitempty,max=300"`
}

func field(name, code, msg string) error {
	return apperror.ErrValidation.WithFields(apperror.FieldError{Field: name, Code: code, Message: msg})
}

// ---- time zones ---------------------------------------------------------------------------

func location(tz string) *time.Location {
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc
	}
	loc, _ := time.LoadLocation("Asia/Kolkata")
	return loc
}

// resolve turns a slot into an exact UTC period using the listing's zone.
func resolve(s Slot, tz string) (Period, error) {
	switch {
	case s.Period != nil && s.Dates != nil:
		return Period{}, field("period", "exclusive", "give either period or dates, not both")
	case s.Period != nil:
		if s.Period.Start.IsZero() || s.Period.End.IsZero() {
			return Period{}, field("period", "required", "start and end are required")
		}
		return Period{Start: s.Period.Start.UTC(), End: s.Period.End.UTC()}, nil
	case s.Dates != nil:
		loc := location(tz)
		d := s.Dates
		st := d.StartTime
		if st == "" {
			st = "08:00"
		}
		start, err := time.ParseInLocation("2006-01-02 15:04", d.StartDate+" "+st, loc)
		if err != nil {
			return Period{}, field("dates.start_date", "datetime", "use YYYY-MM-DD and HH:MM")
		}
		var end time.Time
		if d.EndTime != "" {
			end, err = time.ParseInLocation("2006-01-02 15:04", d.EndDate+" "+d.EndTime, loc)
		} else {
			end, err = time.ParseInLocation("2006-01-02 15:04", d.EndDate+" "+st, loc)
			end = end.AddDate(0, 0, 1) // inclusive end date, calendar-aware
		}
		if err != nil {
			return Period{}, field("dates.end_date", "datetime", "use YYYY-MM-DD and HH:MM")
		}
		return Period{Start: start.UTC(), End: end.UTC()}, nil
	}
	return Period{}, field("period", "required", "choose the rental dates")
}

func view(p Period, tz string) PeriodView {
	loc := location(tz)
	return PeriodView{
		Start: p.Start.UTC(), End: p.End.UTC(), Timezone: loc.String(),
		StartLocal: p.Start.In(loc).Format(time.RFC3339), EndLocal: p.End.In(loc).Format(time.RFC3339),
		Hours: math.Round(p.End.Sub(p.Start).Hours()*100) / 100,
	}
}

// localDate is the calendar date of t in the listing's zone (plan and tax
// validity are by date).
func localDate(t time.Time, tz string) time.Time {
	y, m, d := t.In(location(tz)).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// ---- options ------------------------------------------------------------------------------

// Choice is what the customer selected (shared by estimate and booking).
type Choice struct {
	Unit          string
	WithOperator  bool
	Delivery      bool
	Pickup        bool
	ExpectedHours float64
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func pricingErr(err error) error {
	switch {
	case errors.Is(err, ErrNoPlan):
		return apperror.ErrBusinessRule.WithDetail(err.Error())
	case errors.Is(err, ErrTooShort), errors.Is(err, ErrTooLong):
		return apperror.ErrBusinessRule.WithDetail(err.Error())
	case errors.Is(err, ErrOutsideArea):
		return apperror.ErrBusinessRule.WithDetail(err.Error())
	case err != nil:
		return apperror.ErrValidation.WithDetail(err.Error())
	}
	return nil
}

// quote prices a period for one listing: everything is read and computed
// on the server.
func (s *Service) quote(ctx context.Context, f QuoteFacts, period Period, c Choice) (Price, error) {
	if (c.Delivery || c.Pickup) && !f.HasSite {
		return Price{}, field("site", "required", "choose a site for delivery or pickup, or collect and return the machine yourself")
	}
	fee, err := s.repo.CustomerFeeBps(ctx, f.CategoryID, f.CityID, f.OwnerOrgID)
	if err != nil {
		return Price{}, err
	}
	taxes, err := s.repo.Taxes(ctx, localDate(period.Start, f.Terms.Timezone))
	if err != nil {
		return Price{}, err
	}
	if len(taxes) == 0 && s.cfg.GSTRateBps > 0 { // no table rows: platform GST setting
		taxes = []TaxRule{{Code: "gst", Name: "GST", RateBps: s.cfg.GSTRateBps, AppliesTo: AllComponents}}
	}
	p, err := Estimate(f.Terms, Options{
		Unit: c.Unit, WithOperator: c.WithOperator, Delivery: c.Delivery, Pickup: c.Pickup, DistanceKm: f.DistanceKm,
		Start: period.Start, End: period.End, ExpectedHours: c.ExpectedHours,
	}, Commercials{CustomerFeeBps: fee, AdvanceBps: s.cfg.AdvanceBps, DefaultDepositPaise: s.cfg.DefaultDepositPaise, Taxes: taxes})
	if err != nil {
		return Price{}, pricingErr(err)
	}
	pv := view(period, f.Terms.Timezone)
	p.Period = &pv
	p.ExpectedHours = c.ExpectedHours
	if !c.Delivery {
		p.Notes = append(p.Notes, "You collect the machine from the owner's yard.")
	}
	if !c.Pickup {
		p.Notes = append(p.Notes, "You return the machine to the owner's yard.")
	}
	return p, nil
}

// availability checks a period against held bookings, owner blocks and the
// owner's duration rules.
func (s *Service) availability(ctx context.Context, f QuoteFacts, p Period) (AvailabilityResult, error) {
	res := AvailabilityResult{Period: view(p, f.Terms.Timezone), Conflicts: []Busy{},
		MinRentalHours: f.Terms.MinRentalHours, MaxRentalDays: f.Terms.MaxRentalDays}
	h := p.End.Sub(p.Start).Hours()
	maxDays := f.Terms.MaxRentalDays
	if maxDays == 0 {
		maxDays = maxRentalDays
	}
	switch {
	case f.Terms.MinRentalHours > 0 && h+1e-9 < float64(f.Terms.MinRentalHours):
		res.RuleProblem = fmt.Sprintf("The owner's minimum rental is %s.", hoursLabel(f.Terms.MinRentalHours))
	case h > float64(maxDays)*24+1e-9:
		res.RuleProblem = fmt.Sprintf("The longest rental for this machine is %d days.", maxDays)
	}
	busy, err := s.repo.Conflicts(ctx, f.EquipmentID, p.Start, p.End)
	if err != nil {
		return res, err
	}
	res.Conflicts = busy
	res.Available = len(busy) == 0 && res.RuleProblem == ""
	if len(busy) > 0 {
		next, err := s.repo.NextFree(ctx, f.EquipmentID, p.Start, h)
		if err != nil {
			return res, err
		}
		res.NextAvailableStart = next
	}
	return res, nil
}

func hoursLabel(h int32) string {
	if h%24 == 0 {
		d := h / 24
		if d == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", d)
	}
	if h == 1 {
		return "1 hour"
	}
	return fmt.Sprintf("%d hours", h)
}

// Estimate prices a hypothetical rental (public) and reports availability.
func (s *Service) Estimate(ctx context.Context, equipmentID uuid.UUID, req EstimateRequest) (EstimateResponse, error) {
	f, err := s.repo.QuoteFacts(ctx, equipmentID, req.WithOperator, req.Site, time.Time{})
	if err != nil {
		return EstimateResponse{}, err
	}
	if f.EquipmentStatus != "active" {
		return EstimateResponse{}, apperror.ErrNotFound
	}
	period, err := resolve(Slot{Period: req.Period, Dates: req.Dates}, f.Terms.Timezone)
	if err != nil {
		return EstimateResponse{}, err
	}
	if err := s.validatePeriod(period); err != nil {
		return EstimateResponse{}, err
	}
	// Plans are chosen by the start date in the listing's zone.
	if f, err = s.repo.QuoteFacts(ctx, equipmentID, req.WithOperator, req.Site, localDate(period.Start, f.Terms.Timezone)); err != nil {
		return EstimateResponse{}, err
	}
	price, err := s.quote(ctx, f, period, req.choice())
	if err != nil {
		return EstimateResponse{}, err
	}
	avail, err := s.availability(ctx, f, period)
	if err != nil {
		return EstimateResponse{}, err
	}
	return EstimateResponse{Price: price, Availability: avail}, nil
}

// CheckAvailability answers whether a machine is free for a period.
func (s *Service) CheckAvailability(ctx context.Context, equipmentID uuid.UUID, q AvailabilityQuery) (AvailabilityResult, error) {
	slot, err := q.slot()
	if err != nil {
		return AvailabilityResult{}, err
	}
	f, err := s.repo.QuoteFacts(ctx, equipmentID, true, nil, time.Time{})
	if err != nil {
		return AvailabilityResult{}, err
	}
	if f.EquipmentStatus != "active" {
		return AvailabilityResult{}, apperror.ErrNotFound
	}
	period, err := resolve(slot, f.Terms.Timezone)
	if err != nil {
		return AvailabilityResult{}, err
	}
	if err := s.validatePeriod(period); err != nil {
		return AvailabilityResult{}, err
	}
	return s.availability(ctx, f, period)
}

// ---- final charges -------------------------------------------------------------------------

// Statuses in which the owner may settle final charges.
var finalizable = []string{StatusActive, StatusReturnInitiated, StatusReturned, StatusCompleted}

// SettleFinal computes (dry run) or saves the final charges of a booking from
// the actual period, hour-meter reading and damage. Rates are those frozen
// in the booking's estimate. Saving is once only and checks the version.
func (s *Service) SettleFinal(ctx context.Context, p *httpx.Principal, id uuid.UUID, req FinalChargesRequest, dryRun bool) (FinalChargesResult, error) {
	b, side, err := s.Access(ctx, p, id)
	if err != nil {
		return FinalChargesResult{}, err
	}
	if !side.Has(SideOwner) {
		return FinalChargesResult{}, apperror.ErrForbidden.WithDetail("only the owner settles final charges")
	}
	if !slices.Contains(finalizable, b.Status) {
		return FinalChargesResult{}, apperror.ErrInvalidTransition.WithDetail("final charges are settled once the rental has started; this booking is " + b.Status)
	}
	if req.DamagePaise > 0 && b.Status != StatusReturned && b.Status != StatusCompleted && b.Status != StatusClosed {
		return FinalChargesResult{}, apperror.ErrBusinessRule.WithDetail("damage charges can only be included after return and require an authorized admin decision")
	}
	if req.DamagePaise > 0 {
		return FinalChargesResult{}, apperror.ErrBusinessRule.WithDetail("damage charges require an authorized admin fine-policy decision; submit a damage report for investigation")
	}
	if b.FinalizedAt != nil && !dryRun {
		return FinalChargesResult{}, apperror.ErrConflict.WithDetail("final charges were already settled")
	}
	if req.ActualEnd.After(s.now().Add(10 * time.Minute)) {
		return FinalChargesResult{}, field("actual_end", "future", "cannot be in the future")
	}
	var est Price
	if err := json.Unmarshal(b.Price, &est); err != nil {
		return FinalChargesResult{}, err
	}
	fc, err := Finalize(est, b.Period.Start, b.Period.End, Actuals{
		ActualStart: req.ActualStart, ActualEnd: req.ActualEnd, OperatingHours: req.OperatingHours,
		DamagePaise: req.DamagePaise, DamageNote: strings.TrimSpace(req.DamageNote),
	})
	if err != nil {
		return FinalChargesResult{}, apperror.ErrBusinessRule.WithDetail(err.Error())
	}
	// Approved extensions were priced (taxes included) when agreed.
	ext, err := s.repo.ApprovedExtensionsTotal(ctx, b.ID)
	if err != nil {
		return FinalChargesResult{}, err
	}
	if ext > 0 {
		fc.Charges = append(fc.Charges, ChargeLine{Component: "extension", Label: "Extensions", Paise: ext, Detail: "as agreed, taxes included"})
		fc.FinalTotalPaise += ext
		fc.EstimatedTotalPaise = b.EstimatedTotal
		fc.DifferencePaise = fc.FinalTotalPaise - b.EstimatedTotal
	}
	out := FinalChargesResult{FinalCharges: fc, BookingID: b.ID, DryRun: dryRun}
	if dryRun {
		return out, nil
	}
	raw, err := json.Marshal(fc)
	if err != nil {
		return FinalChargesResult{}, err
	}
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "owner", Reason: "final charges"}, func(ctx context.Context) error {
		if err := s.repo.SetFinalCharges(ctx, db.SetBookingFinalChargesParams{
			ID: b.ID, Version: b.Version, FinalCharges: raw, FinalTotalPaise: &fc.FinalTotalPaise,
			ActualStart: req.ActualStart.UTC(), ActualEnd: req.ActualEnd.UTC(),
		}); err != nil {
			return err
		}
		pl := payload(b, p.UserID)
		pl.Extra = map[string]any{"booking_id": b.ID, "final_total_paise": fc.FinalTotalPaise}
		return s.repo.Emit(ctx, b.ID, events.BookingFinalCharges, pl)
	})
	if err != nil {
		return FinalChargesResult{}, err
	}
	out.Saved = true
	return out, nil
}

// ---- taxes (admin) -------------------------------------------------------------------------

func (s *Service) TaxRates(ctx context.Context) (TaxRateList, error) {
	rows, err := s.repo.TaxRateList(ctx)
	return TaxRateList{Data: rows}, err
}

func (s *Service) SaveTaxRate(ctx context.Context, p *httpx.Principal, req TaxRateRequest) (TaxRateList, error) {
	code := strings.ToLower(strings.TrimSpace(req.Code))
	for _, r := range code {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return TaxRateList{}, field("code", "format", "use lower-case letters, digits and _")
		}
	}
	if req.ValidUntil != nil && *req.ValidUntil < req.ValidFrom {
		return TaxRateList{}, field("valid_until", "gte", "must be on or after valid_from")
	}
	req.Code = code
	err := s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "admin_finance", Reason: "tax rate " + code}, func(ctx context.Context) error {
		return s.repo.SaveTaxRate(ctx, req)
	})
	if err != nil {
		return TaxRateList{}, err
	}
	return s.TaxRates(ctx)
}

// ReviewTaxRate records the review of a tax rule.
func (s *Service) ReviewTaxRate(ctx context.Context, p *httpx.Principal, id uuid.UUID, req TaxReviewRequest) (TaxRateList, error) {
	err := s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "admin_finance", Reason: "tax rule reviewed"}, func(ctx context.Context) error {
		return s.repo.ReviewTaxRate(ctx, id, req)
	})
	if err != nil {
		return TaxRateList{}, err
	}
	return s.TaxRates(ctx)
}
