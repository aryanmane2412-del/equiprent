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
	"github.com/equiprent/backend/internal/events"
)

var policy25 = json.RawMessage(`[{"hours_before":24,"fee_bps":0},{"hours_before":0,"fee_bps":2500}]`)

func TestDraftDoesNotNotifyAndSubmitsToOwner(t *testing.T) {
	ctx := context.Background()
	svc, repo := newSvc()
	b, err := svc.Create(ctx, customer, customerOrg, CreateRequest{
		EquipmentID: uuid.New(), SiteAddressID: uuid.New(), Period: period(), PricingUnit: "day", Draft: true, ExpectedHours: 30,
	})
	if err != nil || b.Status != StatusDraft || len(repo.events) != 0 {
		t.Fatalf("draft: %+v %v events=%v", b.Status, err, repo.events)
	}
	if !slices.Contains(b.Actions, "submit") || slices.Contains(b.Actions, "pay") {
		t.Fatalf("draft actions = %v", b.Actions)
	}
	// The owner does not see a customer's draft at all.
	if _, err := svc.Get(ctx, owner, b.ID); !errors.Is(err, apperror.ErrNotFound) {
		t.Fatalf("owner sees draft: %v", err)
	}
	// A stale version is refused.
	if _, err := svc.Submit(ctx, customer, b.ID, b.Version+5); !errors.Is(err, apperror.ErrVersionMismatch) {
		t.Fatalf("stale submit: %v", err)
	}
	// Dates taken meanwhile: submitting fails with an overlap.
	repo.busy = []Busy{{Start: time.Now(), End: time.Now().Add(100 * time.Hour), Kind: "booked"}}
	if _, err := svc.Submit(ctx, customer, b.ID, 0); !errors.Is(err, apperror.ErrBookingOverlap) {
		t.Fatalf("submit on taken dates: %v", err)
	}
	repo.busy = nil
	sub, err := svc.Submit(ctx, customer, b.ID, 0)
	if err != nil || sub.Status != StatusRequested || sub.OwnerResponseDueAt == nil {
		t.Fatalf("submit = %+v %v", sub.Status, err)
	}
	if !slices.Equal(repo.events, []string{events.BookingRequested}) {
		t.Fatalf("events = %v", repo.events)
	}
	var price Price
	_ = json.Unmarshal(repo.bookings[b.ID].Price, &price)
	if price.ExpectedHours != 30 {
		t.Fatalf("expected hours lost on submit: %+v", price.ExpectedHours)
	}
}

func TestAcceptSetsPaymentDeadline(t *testing.T) {
	ctx := context.Background()
	svc, repo := newSvc()
	svc.cfg.PaymentWindow = 24 * time.Hour
	b := book(t, svc, "online")
	if _, err := svc.Act(ctx, owner, b.ID, ActionAccept, ActionRequest{}); err != nil {
		t.Fatal(err)
	}
	got := repo.bookings[b.ID]
	if got.Status != StatusPaymentPending || got.PaymentDueAt == nil {
		t.Fatalf("after accept: %s due=%v", got.Status, got.PaymentDueAt)
	}
	// The rental starts in 48 h, so the 24 h window applies.
	if d := time.Until(*got.PaymentDueAt); d < 23*time.Hour || d > 25*time.Hour {
		t.Fatalf("due in %v", d)
	}
	// A start sooner than the window pulls the deadline in (never below 15 min).
	if due := svc.paymentDue(time.Now().Add(3 * time.Hour)); time.Until(*due) > 3*time.Hour {
		t.Fatalf("due after start: %v", due)
	}
	if due := svc.paymentDue(time.Now().Add(time.Minute)); time.Until(*due) < 14*time.Minute {
		t.Fatalf("due too soon: %v", due)
	}
}

func TestPaymentProcessingRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc, repo := newSvc()
	b := book(t, svc, "online")
	_, _ = svc.Act(ctx, owner, b.ID, ActionAccept, ActionRequest{})
	if err := svc.PaymentStarted(ctx, b.ID); err != nil || repo.bookings[b.ID].Status != StatusPaymentProcessing {
		t.Fatalf("started: %v %s", err, repo.bookings[b.ID].Status)
	}
	if err := svc.PaymentFailed(ctx, b.ID); err != nil || repo.bookings[b.ID].Status != StatusPaymentPending {
		t.Fatalf("failed: %v %s", err, repo.bookings[b.ID].Status)
	}
	_ = svc.PaymentStarted(ctx, b.ID)
	if err := svc.ConfirmPaid(ctx, b.ID); err != nil || repo.bookings[b.ID].Status != StatusConfirmed {
		t.Fatalf("captured: %v %s", err, repo.bookings[b.ID].Status)
	}
	// A second capture (webhook after the callback) changes nothing.
	if err := svc.ConfirmPaid(ctx, b.ID); err != nil || repo.bookings[b.ID].Status != StatusConfirmed {
		t.Fatalf("duplicate capture: %v %s", err, repo.bookings[b.ID].Status)
	}
}

func TestLatePaymentOnCancelledBookingIsRefunded(t *testing.T) {
	ctx := context.Background()
	svc, repo := newSvc()
	b := book(t, svc, "online")
	_, _ = svc.Act(ctx, owner, b.ID, ActionAccept, ActionRequest{})
	if _, err := svc.Cancel(ctx, customer, b.ID, ActionRequest{Reason: "changed plans"}, false); err != nil {
		t.Fatal(err)
	}
	if repo.bookings[b.ID].Status != StatusCancelled {
		t.Fatalf("nothing paid: %s", repo.bookings[b.ID].Status)
	}
	repo.paid = []Refundable{{PaymentID: uuid.New(), Purpose: "advance", RefundablePaise: 500_000}}
	if err := svc.ConfirmPaid(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if repo.bookings[b.ID].Status != StatusRefundPending || len(repo.refunds) != 1 || repo.refunds[0].AmountPaise != 500_000 {
		t.Fatalf("late capture: %s refunds=%+v", repo.bookings[b.ID].Status, repo.refunds)
	}
}

func TestCancellingAPaidBookingRefundsMinusFee(t *testing.T) {
	ctx := context.Background()
	svc, repo := newSvc()
	b := book(t, svc, "online")
	_, _ = svc.Act(ctx, owner, b.ID, ActionAccept, ActionRequest{})
	_ = svc.ConfirmPaid(ctx, b.ID)
	stored := repo.bookings[b.ID]
	stored.EstimatedTotal, stored.cancellationRules = 1_000_000, policy25
	stored.Period.Start = time.Now().Add(2 * time.Hour)
	deposit, advance := uuid.New(), uuid.New()
	repo.paid = []Refundable{{PaymentID: deposit, Purpose: "deposit", RefundablePaise: 200_000}, {PaymentID: advance, Purpose: "advance", RefundablePaise: 500_000}}

	preview, err := svc.Cancel(ctx, customer, b.ID, ActionRequest{}, true)
	if err != nil || preview.FeePaise != 250_000 || preview.RefundPaise != 450_000 || preview.RequiresApproval {
		t.Fatalf("preview = %+v %v", preview, err)
	}
	res, err := svc.Cancel(ctx, customer, b.ID, ActionRequest{Reason: "site flooded"}, false)
	if err != nil || res.Status != StatusRefundPending || res.RefundPaise != 450_000 {
		t.Fatalf("cancel = %+v %v", res, err)
	}
	// The fee comes out of the advance; the deposit goes back in full.
	byPayment := map[uuid.UUID]int64{}
	for _, r := range repo.refunds {
		byPayment[r.PaymentID] = r.AmountPaise
	}
	if byPayment[deposit] != 200_000 || byPayment[advance] != 250_000 {
		t.Fatalf("refunds = %v", byPayment)
	}
	// Retrying the same refunds creates nothing new (idempotency keys).
	if n, _ := svc.startRefunds(ctx, *stored, 250_000, "again", uuid.Nil, "cancel"); n != 0 {
		t.Fatalf("refunded twice: %d", n)
	}
}

func TestCancellationAfterDispatchNeedsOwner(t *testing.T) {
	ctx := context.Background()
	for _, decision := range []string{"approve", "decline", "withdraw"} {
		t.Run(decision, func(t *testing.T) {
			svc, repo := newSvc()
			repo.checklists = true // pre-delivery inspection completed
			b := book(t, svc, "offline")
			_, _ = svc.Act(ctx, owner, b.ID, ActionAccept, ActionRequest{})
			if _, err := svc.Act(ctx, owner, b.ID, ActionDispatch, ActionRequest{}); err != nil {
				t.Fatal(err)
			}
			res, err := svc.Cancel(ctx, customer, b.ID, ActionRequest{Reason: "no longer needed"}, false)
			if err != nil || res.Status != StatusCancellationRequested || !res.RequiresApproval {
				t.Fatalf("request = %+v %v", res, err)
			}
			if _, err := svc.Cancel(ctx, customer, b.ID, ActionRequest{Reason: "again"}, false); !errors.Is(err, apperror.ErrInvalidTransition) {
				t.Fatalf("double request: %v", err)
			}
			got, _ := svc.Get(ctx, owner, b.ID)
			if !slices.Contains(got.Actions, "approve_cancellation") {
				t.Fatalf("owner actions = %v", got.Actions)
			}
			// The customer cannot approve their own request.
			if _, err := svc.RespondToCancellation(ctx, customer, b.ID, "approve", ActionRequest{}); !errors.Is(err, apperror.ErrForbidden) {
				t.Fatalf("customer approving: %v", err)
			}
			who := owner
			if decision == "withdraw" {
				who = customer
			}
			if decision == "decline" {
				if _, err := svc.RespondToCancellation(ctx, owner, b.ID, "decline", ActionRequest{}); !errors.Is(err, apperror.ErrValidation) {
					t.Fatalf("decline without reason: %v", err)
				}
			}
			tr, err := svc.RespondToCancellation(ctx, who, b.ID, decision, ActionRequest{Reason: "the machine is already there"})
			want := map[string]string{"approve": StatusCancelled, "decline": StatusDispatched, "withdraw": StatusDispatched}[decision]
			if err != nil || tr.Status != want || repo.bookings[b.ID].Status != want {
				t.Fatalf("%s: %+v %v", decision, tr, err)
			}
		})
	}
}

func TestExtensionRequestAndApproval(t *testing.T) {
	ctx := context.Background()
	svc, repo := newSvc()
	b := book(t, svc, "offline")
	stored := repo.bookings[b.ID]
	stored.Status = StatusConfirmed
	end := stored.Period.End
	newEnd := end.Add(48 * time.Hour)

	if _, err := svc.RequestExtension(ctx, customer, b.ID, ExtensionRequest{NewEnd: newEnd}); !errors.Is(err, apperror.ErrInvalidTransition) {
		t.Fatalf("extending a confirmed booking: %v", err)
	}
	stored.Status = StatusActive
	if _, err := svc.RequestExtension(ctx, customer, b.ID, ExtensionRequest{NewEnd: end.Add(-time.Hour)}); !errors.Is(err, apperror.ErrValidation) {
		t.Fatalf("earlier end: %v", err)
	}
	q, err := svc.RequestExtension(ctx, customer, b.ID, ExtensionRequest{NewEnd: newEnd, DryRun: true})
	if err != nil || q.Extension != nil || q.AmountPaise <= 0 || !q.Availability.Available || stored.Status != StatusActive {
		t.Fatalf("dry run = %+v %v", q, err)
	}
	if q.Price.DeliveryPaise != 0 || q.Price.DepositPaise != 0 {
		t.Fatalf("an extension charges no delivery or deposit: %+v", q.Price)
	}
	// Taken dates block the request.
	repo.busy = []Busy{{Start: end, End: newEnd, Kind: "booked"}}
	if _, err := svc.RequestExtension(ctx, customer, b.ID, ExtensionRequest{NewEnd: newEnd}); !errors.Is(err, apperror.ErrBookingOverlap) {
		t.Fatalf("overlap: %v", err)
	}
	repo.busy = nil
	q, err = svc.RequestExtension(ctx, customer, b.ID, ExtensionRequest{NewEnd: newEnd, Note: "rain delays"})
	if err != nil || q.Extension == nil || stored.Status != StatusExtensionRequested {
		t.Fatalf("request = %+v %v", q, err)
	}
	// The requester cannot approve; the owner can.
	if _, err := svc.RespondToExtension(ctx, customer, b.ID, q.Extension.ID, "approve", ActionRequest{}); !errors.Is(err, apperror.ErrForbidden) {
		t.Fatalf("self-approval: %v", err)
	}
	ownerView, _ := svc.Get(ctx, owner, b.ID)
	if ownerView.PendingExtension == nil || !slices.Contains(ownerView.Actions, "approve_extension") {
		t.Fatalf("owner view: %+v %v", ownerView.PendingExtension, ownerView.Actions)
	}
	before := stored.EstimatedTotal
	x, err := svc.RespondToExtension(ctx, owner, b.ID, q.Extension.ID, "approve", ActionRequest{})
	if err != nil || x.Status != "approved" || stored.Status != StatusActive || !stored.Period.End.Equal(newEnd) ||
		stored.EstimatedTotal != before+q.AmountPaise {
		t.Fatalf("approve: %+v %v status=%s end=%v", x, err, stored.Status, stored.Period.End)
	}
	if !slices.Contains(repo.events, events.BookingExtensionApproved) {
		t.Fatalf("events = %v", repo.events)
	}
}

func TestActionsPerSide(t *testing.T) {
	b := Booking{Status: StatusPaymentPending, PaymentMode: "online", WithOperator: true}
	if a := actionsFor(b, SideCustomer, nil, nil); !slices.Contains(a, "pay") || !slices.Contains(a, "cancel") || slices.Contains(a, "accept") {
		t.Fatalf("customer awaiting payment: %v", a)
	}
	b.Status = StatusRequested
	if a := actionsFor(b, SideOwner, nil, nil); !slices.Equal(a, []string{"accept", "reject"}) {
		t.Fatalf("owner on request: %v", a)
	}
	b.Status = StatusConfirmed
	if a := actionsFor(b, SideOwner, nil, nil); !slices.Contains(a, "assign_operator") || !slices.Contains(a, "dispatch") {
		t.Fatalf("owner on confirmed: %v", a)
	}
	b.Status = StatusDispatched
	if a := actionsFor(b, SideCustomer, nil, nil); !slices.Contains(a, "request_cancellation") || slices.Contains(a, "cancel") {
		t.Fatalf("customer after dispatch: %v", a)
	}
	b.Status = StatusOnSite
	if a := actionsFor(b, SideOperator, nil, nil); !slices.Equal(a, []string{"start"}) {
		t.Fatalf("operator on site: %v", a)
	}
	b.Status = StatusCompleted
	finalizedAt := time.Now()
	b.FinalizedAt = &finalizedAt
	if a := actionsFor(b, SideCustomer, nil, &MoneySummary{BalanceDuePaise: 10}); !slices.Contains(a, "review") || !slices.Contains(a, "pay_balance") {
		t.Fatalf("customer completed: %v", a)
	}
}

func TestPolicySummary(t *testing.T) {
	got := PolicySummary([]CancellationRule{{HoursBefore: 0, FeeBps: 2500}, {HoursBefore: 24, FeeBps: 0}})
	if got != "Free with 1 day or more notice; 25% fee at the last minute." {
		t.Fatalf("summary = %q", got)
	}
}
