package bookings

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/events"
	"github.com/equiprent/backend/internal/httpx"
)

// fakeRepo keeps bookings in memory and applies the DB transition graph.
type fakeRepo struct {
	checklists          bool // HasChecklist answer
	bookings            map[uuid.UUID]*Booking
	events              []string
	created             []db.CreateBookingParams
	site                Site
	facts               QuoteFacts
	busy                []Busy
	final               []db.SetBookingFinalChargesParams
	trips               int
	operatorEligible    bool
	assignmentResponses []string
	paid                []Refundable // captured payments still refundable
	refunds             []db.CreateRefundParams
	exts                map[uuid.UUID]*Extension
}

func (f *fakeRepo) TransitionDue(ctx context.Context, id uuid.UUID, from string, version int32, to string, due *time.Time) (Transition, error) {
	t, err := f.Transition(ctx, id, from, version, to, nil)
	if err == nil {
		f.bookings[id].PaymentDueAt = due
	}
	return t, err
}
func (f *fakeRepo) UpdateDraft(_ context.Context, p db.UpdateDraftBookingParams) (Transition, error) {
	b := f.bookings[p.ID]
	if b.Status != StatusDraft || b.Version != p.Version {
		return Transition{}, apperror.ErrVersionMismatch
	}
	b.Version++
	b.EstimatedTotal, b.Price = p.EstimatedTotalPaise, p.PriceSnapshot
	return Transition{ID: b.ID, Status: b.Status, Version: b.Version}, nil
}
func (f *fakeRepo) Reprice(context.Context, db.RepriceBookingParams) (Transition, error) {
	return Transition{}, nil
}
func (f *fakeRepo) SetDue(_ context.Context, id uuid.UUID, version int32, resp, pay *time.Time) (Transition, error) {
	b := f.bookings[id]
	if b.Version != version {
		return Transition{}, apperror.ErrVersionMismatch
	}
	b.Version++
	b.OwnerResponseDueAt, b.PaymentDueAt = resp, pay
	return Transition{ID: id, Status: b.Status, Version: b.Version}, nil
}
func (f *fakeRepo) OverlappingRequests(context.Context, Booking) ([]Summary, error) { return nil, nil }
func (f *fakeRepo) EndTrips(context.Context, uuid.UUID) error                       { return nil }
func (f *fakeRepo) LockEquipment(context.Context, uuid.UUID) error                  { return nil }
func (f *fakeRepo) ConflictsExcept(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time) ([]Busy, error) {
	return f.busy, nil
}
func (f *fakeRepo) CreateExtension(_ context.Context, p db.CreateExtensionParams) (Extension, error) {
	x := &Extension{ID: uuid.New(), RequestedBy: p.RequestedBy, RequestedSide: p.RequestedSide, PreviousEnd: p.PreviousEnd,
		NewEnd: p.NewEnd, AmountPaise: p.AmountPaise, Status: "pending"}
	if f.exts == nil {
		f.exts = map[uuid.UUID]*Extension{}
	}
	f.exts[x.ID] = x
	return *x, nil
}
func (f *fakeRepo) Extension(_ context.Context, _, id uuid.UUID) (Extension, error) {
	if x, ok := f.exts[id]; ok {
		return *x, nil
	}
	return Extension{}, apperror.ErrNotFound
}
func (f *fakeRepo) Extensions(context.Context, uuid.UUID) ([]Extension, error) {
	var out []Extension
	for _, x := range f.exts {
		out = append(out, *x)
	}
	return out, nil
}
func (f *fakeRepo) RespondExtension(_ context.Context, id uuid.UUID, status string, _ uuid.NullUUID, _ *string) (Extension, error) {
	x := f.exts[id]
	x.Status = status
	return *x, nil
}
func (f *fakeRepo) ExtendPeriod(_ context.Context, id uuid.UUID, version int32, newEnd time.Time, amount int64) (Transition, error) {
	b := f.bookings[id]
	if b.Version != version {
		return Transition{}, apperror.ErrVersionMismatch
	}
	b.Version++
	b.Period.End, b.EstimatedTotal = newEnd, b.EstimatedTotal+amount
	return Transition{ID: id, Status: b.Status, Version: b.Version}, nil
}
func (f *fakeRepo) ApprovedExtensionsTotal(context.Context, uuid.UUID) (int64, error) { return 0, nil }
func (f *fakeRepo) PaymentTotals(context.Context, uuid.UUID) (int64, int64, error) {
	var paid int64
	for _, p := range f.paid {
		paid += p.RefundablePaise
	}
	return paid, 0, nil
}
func (f *fakeRepo) RefundablePayments(context.Context, uuid.UUID) ([]Refundable, error) {
	return slices.Clone(f.paid), nil
}
func (f *fakeRepo) CreateRefund(_ context.Context, p db.CreateRefundParams) (bool, error) {
	for _, r := range f.refunds {
		if r.IdempotencyKey == p.IdempotencyKey {
			return false, nil
		}
	}
	f.refunds = append(f.refunds, p)
	return true, nil
}
func (f *fakeRepo) Refunds(context.Context, uuid.UUID) ([]Refund, error) { return nil, nil }
func (f *fakeRepo) DefaultCancellationRules(context.Context) (json.RawMessage, error) {
	return nil, nil
}

func (f *fakeRepo) QuoteFacts(_ context.Context, _ uuid.UUID, _ bool, site *GeoPoint, _ time.Time) (QuoteFacts, error) {
	out := f.facts
	out.HasSite = site != nil
	return out, nil
}
func (f *fakeRepo) Taxes(context.Context, time.Time) ([]TaxRule, error) {
	return []TaxRule{{Code: "gst", Name: "GST", RateBps: 1800, AppliesTo: AllComponents}}, nil
}
func (f *fakeRepo) Conflicts(context.Context, uuid.UUID, time.Time, time.Time) ([]Busy, error) {
	return f.busy, nil
}
func (f *fakeRepo) NextFree(_ context.Context, _ uuid.UUID, from time.Time, _ float64) (*time.Time, error) {
	if len(f.busy) == 0 {
		return &from, nil
	}
	end := f.busy[len(f.busy)-1].End
	return &end, nil
}
func (f *fakeRepo) SetFinalCharges(_ context.Context, p db.SetBookingFinalChargesParams) error {
	b := f.bookings[p.ID]
	if b.FinalizedAt != nil || b.Version != p.Version {
		return apperror.ErrConflict
	}
	now := time.Now()
	b.FinalCharges, b.FinalTotal, b.FinalizedAt = p.FinalCharges, p.FinalTotalPaise, &now
	f.final = append(f.final, p)
	return nil
}
func (f *fakeRepo) TaxRateList(context.Context) ([]TaxRate, error)                   { return nil, nil }
func (f *fakeRepo) SaveTaxRate(context.Context, TaxRateRequest) error                { return nil }
func (f *fakeRepo) ReviewTaxRate(context.Context, uuid.UUID, TaxReviewRequest) error { return nil }
func (f *fakeRepo) CustomerFeeBps(context.Context, uuid.UUID, uuid.NullUUID, uuid.UUID) (int32, error) {
	return 200, nil
}
func (f *fakeRepo) Site(context.Context, uuid.UUID) (Site, error) { return f.site, nil }

func (f *fakeRepo) Create(_ context.Context, p db.CreateBookingParams) (Transition, error) {
	f.created = append(f.created, p)
	b := &Booking{ID: uuid.New(), Reference: p.Reference, Status: string(p.Status), Version: 1,
		CustomerOrgID: p.CustomerOrgID, OwnerOrgID: f.facts.OwnerOrgID, PaymentMode: string(p.PaymentMode),
		Price: p.PriceSnapshot, EstimatedTotal: p.EstimatedTotalPaise, Period: Period{Start: p.StartsAt, End: p.EndsAt},
		WithOperator: p.WithOperator}
	f.bookings[b.ID] = b
	return Transition{ID: b.ID, Status: b.Status, Version: 1}, nil
}

func (f *fakeRepo) Get(_ context.Context, id uuid.UUID) (Booking, error) {
	b, ok := f.bookings[id]
	if !ok {
		return Booking{}, apperror.ErrNotFound
	}
	return *b, nil
}

func (f *fakeRepo) Timeline(context.Context, uuid.UUID) ([]Event, error) { return nil, nil }
func (f *fakeRepo) HasChecklist(context.Context, uuid.UUID, string) (bool, error) {
	return f.checklists, nil
}
func (f *fakeRepo) ReplaceAssignment(context.Context, uuid.UUID) error          { return nil }
func (f *fakeRepo) Restricted(context.Context, uuid.UUID, string) (bool, error) { return false, nil }
func (f *fakeRepo) List(context.Context, string, uuid.UUID, []string, []uuid.UUID, httpx.Keyset) ([]Summary, error) {
	return nil, nil
}

// dbGraph mirrors booking_status_transitions (the parts these tests use).
var dbGraph = map[string][]string{
	StatusDraft:                 {StatusRequested, StatusPaymentPending, StatusConfirmed, StatusCancelled},
	StatusRequested:             {StatusAccepted, StatusRejected, StatusExpired, StatusCancelled},
	StatusAccepted:              {StatusPaymentPending, StatusConfirmed, StatusCancelled, StatusRefundPending},
	StatusPaymentPending:        {StatusPaymentProcessing, StatusConfirmed, StatusExpired, StatusCancelled, StatusRefundPending},
	StatusPaymentProcessing:     {StatusPaymentPending, StatusConfirmed, StatusCancelled, StatusExpired, StatusRefundPending},
	StatusConfirmed:             {StatusOperatorAssigned, StatusDispatched, StatusCancelled, StatusRefundPending},
	StatusDispatched:            {StatusOnSite, StatusCancelled, StatusRefundPending, StatusCancellationRequested},
	StatusOnSite:                {StatusActive, StatusCancelled, StatusRefundPending, StatusCancellationRequested},
	StatusCancellationRequested: {StatusDispatched, StatusOnSite, StatusCancelled, StatusRefundPending},
	StatusActive:                {StatusReturnInitiated, StatusDisputed, StatusExtensionRequested},
	StatusExtensionRequested:    {StatusActive},
	StatusCancelled:             {StatusRefundPending},
	StatusExpired:               {StatusRefundPending},
}

func (f *fakeRepo) Transition(_ context.Context, id uuid.UUID, from string, version int32, to string, reason *string) (Transition, error) {
	b := f.bookings[id]
	if b.Status != from || b.Version != version {
		return Transition{}, apperror.ErrVersionMismatch
	}
	if !slices.Contains(dbGraph[from], to) {
		return Transition{}, apperror.ErrInvalidTransition
	}
	if b.ResumeStatus != nil && to != StatusCancelled && to != StatusRefundPending && to != *b.ResumeStatus {
		return Transition{}, apperror.ErrInvalidTransition
	}
	b.ResumeStatus = nil
	if to == StatusExtensionRequested || to == StatusCancellationRequested {
		rs := from
		b.ResumeStatus = &rs
	}
	b.Status, b.Version, b.CancelReason = to, b.Version+1, reason
	return Transition{ID: id, Status: to, Version: b.Version}, nil
}

func (f *fakeRepo) IsOrgOperator(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return true, nil
}
func (f *fakeRepo) OperatorVerified(context.Context, uuid.UUID) (bool, error) { return true, nil }
func (f *fakeRepo) OperatorEligibleForEquipment(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return f.operatorEligible, nil
}
func (f *fakeRepo) Assign(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error { return nil }
func (f *fakeRepo) DriverCompensation(_ context.Context, orgID, bookingID uuid.UUID, amount int64, _ uuid.UUID) (DriverCompensation, error) {
	return DriverCompensation{ID: uuid.New(), OwnerOrgID: orgID, BookingID: bookingID, AgreedAmountPaise: amount, Status: "agreed"}, nil
}
func (f *fakeRepo) DriverCompensations(context.Context, uuid.UUID, int32) ([]DriverCompensation, error) {
	return []DriverCompensation{}, nil
}
func (f *fakeRepo) OperatorDriverCompensations(context.Context, uuid.UUID, int32) ([]DriverCompensation, error) {
	return []DriverCompensation{}, nil
}
func (f *fakeRepo) OperatorDriverCompensationHistory(context.Context, uuid.UUID, int32) ([]DriverCompensationEvent, error) {
	return []DriverCompensationEvent{}, nil
}
func (f *fakeRepo) PayDriverCompensation(_ context.Context, orgID, id uuid.UUID, reference string) (DriverCompensation, error) {
	return DriverCompensation{ID: id, OwnerOrgID: orgID, Status: "paid", TransactionReference: &reference}, nil
}
func (f *fakeRepo) DriverCompensationByID(context.Context, uuid.UUID, uuid.UUID) (DriverCompensation, error) {
	return DriverCompensation{}, apperror.ErrNotFound
}
func (f *fakeRepo) OperatorAssignment(context.Context, uuid.UUID, uuid.UUID) (uuid.UUID, error) {
	return uuid.New(), nil
}
func (f *fakeRepo) RespondAssignment(_ context.Context, _ uuid.UUID, status string) error {
	f.assignmentResponses = append(f.assignmentResponses, status)
	return nil
}
func (f *fakeRepo) CompleteAssignments(context.Context, uuid.UUID) error { return nil }
func (f *fakeRepo) StartTrip(context.Context, uuid.UUID, uuid.UUID, uuid.NullUUID, string) error {
	f.trips++
	return nil
}
func (f *fakeRepo) Emit(_ context.Context, _ uuid.UUID, t string, _ events.Payload) error {
	f.events = append(f.events, t)
	return nil
}

var (
	customerOrg = uuid.New()
	ownerOrg    = uuid.New()
	customer    = &httpx.Principal{UserID: uuid.New(), Roles: []httpx.Role{{Code: "customer", OrgID: customerOrg}}, ActingOrg: customerOrg}
	owner       = &httpx.Principal{UserID: uuid.New(), Roles: []httpx.Role{{Code: "owner", OrgID: ownerOrg}}, ActingOrg: ownerOrg}
	stranger    = &httpx.Principal{UserID: uuid.New(), Roles: []httpx.Role{{Code: "customer", OrgID: uuid.New()}}}
)

func newSvc() (*Service, *fakeRepo) {
	repo := &fakeRepo{
		bookings:         map[uuid.UUID]*Booking{},
		operatorEligible: true,
		site:             Site{OrgID: customerOrg, HasLocation: true, Location: GeoPoint{Lat: 18.5, Lng: 73.8}},
		facts: QuoteFacts{OwnerOrgID: ownerOrg, EquipmentStatus: "active", DistanceKm: 10,
			Terms: Terms{Plans: []PlanRate{{Unit: "day", RatePaise: 1_000_000, MinUnits: 1}}, ServiceRadiusKm: 50, Timezone: "Asia/Kolkata", LateGraceMinutes: 60}},
	}
	svc := NewService(repo, database.NoopTransactor{}, config.BookingConfig{OwnerResponseWindow: 2 * time.Hour, AdvanceBps: 5000, GSTRateBps: 1800})
	return svc, repo
}

func TestDriverCannotAcceptAssignmentAfterEligibilityExpires(t *testing.T) {
	svc, repo := newSvc()
	driverID := uuid.New()
	driver := &httpx.Principal{UserID: driverID, Roles: []httpx.Role{{Code: httpx.RoleOperator, OrgID: ownerOrg}}}
	bookingID := uuid.New()
	repo.bookings[bookingID] = &Booking{ID: bookingID, Status: StatusOperatorAssigned, Version: 1, EquipmentID: uuid.New()}
	repo.operatorEligible = false

	if _, err := svc.RespondToJob(context.Background(), driver, bookingID, true); !errors.Is(err, apperror.ErrBusinessRule) {
		t.Fatalf("accept after eligibility expired error = %v, want business rule", err)
	}
	if len(repo.assignmentResponses) != 0 {
		t.Fatalf("assignment response was recorded despite ineligibility: %v", repo.assignmentResponses)
	}
}

func period() *Period {
	start := time.Now().Add(48 * time.Hour)
	return &Period{Start: start, End: start.Add(72 * time.Hour)}
}

func book(t *testing.T, svc *Service, mode string) Booking {
	t.Helper()
	b, err := svc.Create(context.Background(), customer, customerOrg, CreateRequest{
		EquipmentID: uuid.New(), SiteAddressID: uuid.New(), Period: period(), PricingUnit: "day", PaymentMode: mode,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return b
}

func TestCreateRequestWaitsForOwner(t *testing.T) {
	svc, repo := newSvc()
	b := book(t, svc, "")
	p := repo.created[0]
	if b.Status != StatusRequested || p.OwnerResponseDueAt == nil || string(p.PaymentMode) != "online" {
		t.Fatalf("booking=%+v params=%+v", b, p)
	}
	var price Price
	_ = json.Unmarshal(p.PriceSnapshot, &price)
	if price.RentalPaise != 3_000_000 || price.PlatformFeePaise != 60_000 || p.EstimatedTotalPaise != price.EstimatedTotalPaise {
		t.Fatalf("price = %+v", price)
	}
	if !slices.Equal(repo.events, []string{events.BookingRequested}) {
		t.Fatalf("events = %v", repo.events)
	}
}

func TestInstantBookSkipsOwnerResponse(t *testing.T) {
	svc, repo := newSvc()
	repo.facts.InstantBook = true
	if b := book(t, svc, "offline"); b.Status != StatusConfirmed {
		t.Fatalf("offline instant = %s", b.Status)
	}
	if b := book(t, svc, "online"); b.Status != StatusPaymentPending {
		t.Fatalf("online instant = %s", b.Status)
	}
}

func TestCreateRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(*fakeRepo, *CreateRequest){
		"past start":       func(_ *fakeRepo, r *CreateRequest) { r.Period.Start = time.Now().Add(-time.Hour) },
		"too long":         func(_ *fakeRepo, r *CreateRequest) { r.Period.End = r.Period.Start.Add(200 * 24 * time.Hour) },
		"foreign site":     func(f *fakeRepo, _ *CreateRequest) { f.site.OrgID = uuid.New() },
		"site without pin": func(f *fakeRepo, _ *CreateRequest) { f.site.HasLocation = false },
		"paused listing":   func(f *fakeRepo, _ *CreateRequest) { f.facts.EquipmentStatus = "paused" },
		"own equipment":    func(f *fakeRepo, _ *CreateRequest) { f.facts.OwnerOrgID = customerOrg },
		"out of radius":    func(f *fakeRepo, _ *CreateRequest) { f.facts.DistanceKm = 80 },
		"slot taken": func(f *fakeRepo, _ *CreateRequest) {
			f.busy = []Busy{{Start: time.Now(), End: time.Now().Add(100 * time.Hour), Kind: "booked"}}
		},
		"no period":      func(_ *fakeRepo, r *CreateRequest) { r.Period = nil },
		"too short":      func(f *fakeRepo, _ *CreateRequest) { f.facts.Terms.MinRentalHours = 96 },
		"over owner max": func(f *fakeRepo, _ *CreateRequest) { f.facts.Terms.MaxRentalDays = 2 },
		"no hourly plan": func(_ *fakeRepo, r *CreateRequest) { r.PricingUnit = "hour" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			svc, repo := newSvc()
			req := CreateRequest{EquipmentID: uuid.New(), SiteAddressID: uuid.New(), Period: period(), PricingUnit: "day"}
			mutate(repo, &req)
			_, err := svc.Create(ctx, customer, customerOrg, req)
			if err == nil || apperror.From(err).Status >= 500 {
				t.Fatalf("want 4xx, got %v", err)
			}
		})
	}
}

func TestOwnerAcceptMovesToPaymentOrConfirmation(t *testing.T) {
	ctx := context.Background()
	for mode, want := range map[string]string{"online": StatusPaymentPending, "offline": StatusConfirmed} {
		svc, _ := newSvc()
		b := book(t, svc, mode)
		tr, err := svc.Act(ctx, owner, b.ID, ActionAccept, ActionRequest{})
		if err != nil || tr.Status != want {
			t.Fatalf("%s accept: %+v %v", mode, tr, err)
		}
	}
}

func TestLifecyclePermissions(t *testing.T) {
	ctx := context.Background()
	svc, _ := newSvc()
	b := book(t, svc, "offline")

	if _, err := svc.Act(ctx, customer, b.ID, ActionAccept, ActionRequest{}); !errors.Is(err, apperror.ErrForbidden) {
		t.Fatalf("customer accepting: %v", err)
	}
	if _, err := svc.Act(ctx, stranger, b.ID, ActionAccept, ActionRequest{}); !errors.Is(err, apperror.ErrNotFound) {
		t.Fatalf("stranger must not learn the booking exists: %v", err)
	}
	if _, err := svc.Act(ctx, owner, b.ID, ActionReject, ActionRequest{Reason: " "}); !errors.Is(err, apperror.ErrValidation) {
		t.Fatalf("reject without reason: %v", err)
	}
	if _, err := svc.Act(ctx, owner, b.ID, ActionComplete, ActionRequest{}); !errors.Is(err, apperror.ErrInvalidTransition) {
		t.Fatalf("complete a requested booking: %v", err)
	}
}

func TestDispatchStartsMobilisationTrip(t *testing.T) {
	ctx := context.Background()
	svc, repo := newSvc()
	repo.checklists = true // pre-delivery inspection completed
	b := book(t, svc, "offline")
	if _, err := svc.Act(ctx, owner, b.ID, ActionAccept, ActionRequest{}); err != nil {
		t.Fatal(err)
	}
	if tr, err := svc.Act(ctx, owner, b.ID, ActionDispatch, ActionRequest{}); err != nil || tr.Status != StatusDispatched || repo.trips != 1 {
		t.Fatalf("dispatch: %+v %v trips=%d", tr, err, repo.trips)
	}
}

func TestCustomerCancellationFeeAfterAcceptance(t *testing.T) {
	ctx := context.Background()
	svc, repo := newSvc()
	b := book(t, svc, "offline")

	// Before acceptance: free.
	if res, _ := svc.Cancel(ctx, customer, b.ID, ActionRequest{}, true); res.FeePaise != 0 {
		t.Fatalf("pre-acceptance fee = %d", res.FeePaise)
	}
	if _, err := svc.Act(ctx, owner, b.ID, ActionAccept, ActionRequest{}); err != nil {
		t.Fatal(err)
	}
	stored := repo.bookings[b.ID]
	stored.EstimatedTotal = 1_000_000
	stored.Period.Start = time.Now().Add(2 * time.Hour) // inside the 24 h window
	stored.cancellationRules = json.RawMessage(`[{"hours_before":24,"fee_bps":0},{"hours_before":0,"fee_bps":2500}]`)

	preview, err := svc.Cancel(ctx, customer, b.ID, ActionRequest{}, true)
	if err != nil || preview.FeePaise != 250_000 || !preview.DryRun || preview.Status != StatusConfirmed {
		t.Fatalf("preview = %+v %v", preview, err)
	}
	if _, err := svc.Cancel(ctx, customer, b.ID, ActionRequest{}, false); !errors.Is(err, apperror.ErrValidation) {
		t.Fatalf("cancel without reason: %v", err)
	}
	res, err := svc.Cancel(ctx, customer, b.ID, ActionRequest{Reason: "site not ready"}, false)
	if err != nil || res.Status != StatusCancelled || res.FeePaise != 250_000 {
		t.Fatalf("cancel = %+v %v", res, err)
	}
	// The owner cancelling costs the customer nothing.
	svc2, repo2 := newSvc()
	b2 := book(t, svc2, "offline")
	_, _ = svc2.Act(ctx, owner, b2.ID, ActionAccept, ActionRequest{})
	repo2.bookings[b2.ID].cancellationRules = stored.cancellationRules
	repo2.bookings[b2.ID].EstimatedTotal = 1_000_000
	if res, _ := svc2.Cancel(ctx, owner, b2.ID, ActionRequest{}, true); res.FeePaise != 0 {
		t.Fatalf("owner cancellation fee = %d", res.FeePaise)
	}
}

func TestSidesForRespectsActingOrg(t *testing.T) {
	both := &httpx.Principal{UserID: uuid.New(), Roles: []httpx.Role{
		{Code: "customer", OrgID: customerOrg}, {Code: "owner", OrgID: ownerOrg},
	}}
	b := Booking{CustomerOrgID: customerOrg, OwnerOrgID: ownerOrg}
	if s := SidesFor(both, b); !s.Has(SideCustomer) || !s.Has(SideOwner) {
		t.Fatalf("no acting org: %b", s)
	}
	both.ActingOrg = ownerOrg
	if s := SidesFor(both, b); s.Has(SideCustomer) || !s.Has(SideOwner) {
		t.Fatalf("acting as owner: %b", s)
	}
	op := &httpx.Principal{UserID: uuid.New()}
	b.Operator = &Assignment{UserID: op.UserID}
	if s := SidesFor(op, b); s != SideOperator {
		t.Fatalf("operator: %b", s)
	}
}

func TestNewReferenceFormat(t *testing.T) {
	ref := NewReference(time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC))
	if len(ref) != 12 || ref[:8] != "ER-2611-" {
		t.Fatalf("reference = %s", ref)
	}
}
