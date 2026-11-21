package bookings

// The parts of the booking engine around the core request → rental flow:
// drafts, cancellation requests that need the owner, extensions, what each
// party may do next, and the published policy.

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/events"
	"github.com/equiprent/backend/internal/httpx"
)

// ---- drafts ----------------------------------------------------------------------------------

func (s *Service) draft(ctx context.Context, p *httpx.Principal, id uuid.UUID, version int32) (Booking, error) {
	b, side, err := s.Access(ctx, p, id)
	if err != nil {
		return Booking{}, err
	}
	if !side.Has(SideCustomer) {
		return Booking{}, apperror.ErrForbidden.WithDetail("only the customer edits a draft")
	}
	if b.Status != StatusDraft {
		return Booking{}, apperror.ErrInvalidTransition.WithDetail("this booking was already submitted")
	}
	return b, checkVersion(b, version)
}

// UpdateDraft replaces a draft's choices (the equipment stays) and
// re-prices it. Body fields are those of POST /bookings.
func (s *Service) UpdateDraft(ctx context.Context, p *httpx.Principal, id uuid.UUID, req CreateRequest, version int32) (Booking, error) {
	b, err := s.draft(ctx, p, id, version)
	if err != nil {
		return Booking{}, err
	}
	req.EquipmentID = b.EquipmentID
	pr, err := s.prepare(ctx, p, b.CustomerOrgID, req)
	if err != nil {
		return Booking{}, err
	}
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: httpx.RoleCustomer, Reason: "draft updated"}, func(ctx context.Context) error {
		_, err := s.repo.UpdateDraft(ctx, draftParams(pr.params, b.ID, b.Version))
		return err
	})
	if err != nil {
		return Booking{}, err
	}
	return s.Get(ctx, p, id)
}

func draftParams(c db.CreateBookingParams, id uuid.UUID, version int32) db.UpdateDraftBookingParams {
	return db.UpdateDraftBookingParams{
		ID: id, Version: version, SiteAddressID: c.SiteAddressID, SiteLat: c.SiteLat, SiteLng: c.SiteLng,
		StartsAt: c.StartsAt, EndsAt: c.EndsAt, WithOperator: c.WithOperator, PricingUnit: c.PricingUnit,
		PriceSnapshot: c.PriceSnapshot, EstimatedTotalPaise: c.EstimatedTotalPaise, DepositPaise: c.DepositPaise,
		CancellationPolicyID: c.CancellationPolicyID, PaymentMode: c.PaymentMode, WorkDescription: c.WorkDescription,
		Delivery: c.Delivery, Pickup: c.Pickup,
	}
}

// requestOf rebuilds the request a draft was saved from.
func requestOf(b Booking) (CreateRequest, error) {
	var price Price
	if err := json.Unmarshal(b.Price, &price); err != nil {
		return CreateRequest{}, err
	}
	unit := price.Unit
	if unit == "" {
		unit = b.PricingUnit
	}
	start, end := b.Period.Start, b.Period.End
	return CreateRequest{
		EquipmentID: b.EquipmentID, SiteAddressID: b.SiteAddressID, Period: &Period{Start: start, End: end},
		PricingUnit: unit, WithOperator: b.WithOperator, Delivery: &b.Delivery, Pickup: &b.Pickup,
		ExpectedHours: price.ExpectedHours, WorkDescription: b.WorkDescription, PaymentMode: b.PaymentMode,
	}, nil
}

// Submit sends a draft: it is re-priced with today's terms, the dates are
// checked, and it becomes a request for the owner (or, for instant-book
// listings, awaits payment / is confirmed).
func (s *Service) Submit(ctx context.Context, p *httpx.Principal, id uuid.UUID, version int32) (Booking, error) {
	b, err := s.draft(ctx, p, id, version)
	if err != nil {
		return Booking{}, err
	}
	req, err := requestOf(b)
	if err != nil {
		return Booking{}, err
	}
	pr, err := s.prepare(ctx, p, b.CustomerOrgID, req)
	if err != nil {
		return Booking{}, err
	}
	if err := s.ensureFree(ctx, b.EquipmentID, b.ID, pr.period); err != nil {
		return Booking{}, err
	}
	status, event, responseDue, paymentDue := s.entry(pr.facts.InstantBook, b.PaymentMode, pr.period.Start)
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: httpx.RoleCustomer, Reason: "submitted"}, func(ctx context.Context) error {
		t, err := s.repo.UpdateDraft(ctx, draftParams(pr.params, b.ID, b.Version))
		if err != nil {
			return err
		}
		if t, err = s.repo.SetDue(ctx, b.ID, t.Version, responseDue, nil); err != nil {
			return err
		}
		if _, err = s.repo.TransitionDue(ctx, b.ID, StatusDraft, t.Version, status, paymentDue); err != nil {
			return err
		}
		return s.repo.Emit(ctx, b.ID, event, payload(b, p.UserID))
	})
	if err != nil {
		return Booking{}, err
	}
	return s.Get(ctx, p, id)
}

// ---- cancellation requests ---------------------------------------------------------------------

// RespondToCancellation lets the owner approve or decline a customer's
// cancellation after dispatch, and the customer withdraw it.
func (s *Service) RespondToCancellation(ctx context.Context, p *httpx.Principal, id uuid.UUID, decision string, req ActionRequest) (Transition, error) {
	b, side, err := s.Access(ctx, p, id)
	if err != nil {
		return Transition{}, err
	}
	if b.Status != StatusCancellationRequested || b.ResumeStatus == nil {
		return Transition{}, apperror.ErrInvalidTransition.WithDetail("there is no cancellation request on this booking")
	}
	if err := checkVersion(b, req.Version); err != nil {
		return Transition{}, err
	}
	need := SideOwner | SideAdmin
	if decision == "withdraw" {
		need = SideCustomer
	}
	acting := side & need
	if acting == 0 {
		return Transition{}, apperror.ErrForbidden.WithDetail("you cannot " + decision + " this cancellation request")
	}
	reason := strings.TrimSpace(req.Reason)
	if decision == "decline" && reason == "" {
		return Transition{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "reason", Code: "required", Message: "tell the customer why"})
	}
	var out Transition
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: acting.Role(), Reason: reason}, func(ctx context.Context) error {
		if decision == "approve" {
			// The fee follows the policy as at approval (the customer asked).
			fee := CancellationFee(b.cancellationRules, b.EstimatedTotal, s.now(), b.Period.Start)
			if reason == "" {
				reason = "cancellation approved by the owner"
			}
			t, _, err := s.cancelNow(ctx, p.UserID, b, b.Status, b.Version, fee, reason, "customer")
			out = t
			return err
		}
		t, err := s.repo.Transition(ctx, b.ID, b.Status, b.Version, *b.ResumeStatus, nil)
		if err != nil {
			return err
		}
		out = t
		if decision == "withdraw" {
			return nil
		}
		pl := payload(b, p.UserID)
		pl.Extra = map[string]any{"reason": reason}
		return s.repo.Emit(ctx, b.ID, events.BookingCancellationDeclined, pl)
	})
	return out, err
}

// ---- extensions ----------------------------------------------------------------------------------

// partySide picks the side a caller acts for (customer or owner).
func partySide(side Side) (Side, string) {
	switch {
	case side.Has(SideCustomer):
		return SideCustomer, "customer"
	case side.Has(SideOwner):
		return SideOwner, "owner"
	}
	return 0, ""
}

// quoteExtension prices [current end, newEnd) at the booking's own unit and
// hire mode (no delivery, pickup or minimum) and checks the machine is free.
func (s *Service) quoteExtension(ctx context.Context, b Booking, newEnd time.Time) (ExtensionQuote, error) {
	newEnd = newEnd.UTC()
	if !newEnd.After(b.Period.End) {
		return ExtensionQuote{}, field("new_end", "gt", "must be after the current end")
	}
	if newEnd.Sub(b.Period.Start) > maxRentalDays*24*time.Hour {
		return ExtensionQuote{}, field("new_end", "max", "rentals are limited to 180 days")
	}
	var est Price
	if err := json.Unmarshal(b.Price, &est); err != nil {
		return ExtensionQuote{}, err
	}
	unit := est.Unit
	if unit == "" {
		unit = b.PricingUnit
	}
	facts, err := s.repo.QuoteFacts(ctx, b.EquipmentID, b.WithOperator, nil, localDate(b.Period.End, b.Timezone))
	if err != nil {
		return ExtensionQuote{}, err
	}
	facts.Terms.MinRentalHours = 0 // an extension adds to a rental that met the minimum
	facts.Terms.MaxRentalDays = 0
	extra := Period{Start: b.Period.End, End: newEnd}
	price, err := s.quote(ctx, facts, extra, Choice{Unit: unit, WithOperator: b.WithOperator})
	if err != nil {
		return ExtensionQuote{}, err
	}
	price.DepositPaise, price.AdvanceDuePaise, price.DueOnConfirmationPaise = 0, 0, 0
	busy, err := s.repo.ConflictsExcept(ctx, b.EquipmentID, b.ID, extra.Start, extra.End)
	if err != nil {
		return ExtensionQuote{}, err
	}
	avail := AvailabilityResult{Available: len(busy) == 0, Period: view(extra, b.Timezone), Conflicts: busy}
	return ExtensionQuote{PreviousEnd: b.Period.End, NewEnd: newEnd, AmountPaise: price.EstimatedTotalPaise, Price: price, Availability: avail}, nil
}

// RequestExtension proposes a later end for a running booking (either
// party; the other approves). dry_run only prices it.
func (s *Service) RequestExtension(ctx context.Context, p *httpx.Principal, id uuid.UUID, req ExtensionRequest) (ExtensionQuote, error) {
	b, side, err := s.Access(ctx, p, id)
	if err != nil {
		return ExtensionQuote{}, err
	}
	acting, sideName := partySide(side)
	if acting == 0 {
		return ExtensionQuote{}, apperror.ErrForbidden.WithDetail("only the customer or the owner can extend a booking")
	}
	if b.Status != StatusActive {
		if b.Status == StatusExtensionRequested {
			return ExtensionQuote{}, apperror.ErrConflict.WithDetail("an extension request is already open")
		}
		return ExtensionQuote{}, apperror.ErrInvalidTransition.WithDetail("only an active rental can be extended; this one is " + b.Status)
	}
	q, err := s.quoteExtension(ctx, b, req.NewEnd)
	if err != nil || req.DryRun {
		return q, err
	}
	if !q.Availability.Available {
		return q, apperror.ErrBookingOverlap.WithDetail("the machine is booked or blocked after the current end")
	}
	note := strings.TrimSpace(req.Note)
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: acting.Role(), Reason: "extension requested"}, func(ctx context.Context) error {
		x, err := s.repo.CreateExtension(ctx, db.CreateExtensionParams{
			BookingID: b.ID, RequestedBy: p.UserID, RequestedSide: sideName, PreviousEnd: q.PreviousEnd, NewEnd: q.NewEnd,
			PriceSnapshot: snapshot(q.Price), AmountPaise: q.AmountPaise, Note: note,
		})
		if err != nil {
			return err
		}
		q.Extension = &x
		if _, err := s.repo.Transition(ctx, b.ID, b.Status, b.Version, StatusExtensionRequested, nil); err != nil {
			return err
		}
		pl := payload(b, p.UserID)
		pl.Extra = map[string]any{"extension_id": x.ID, "new_end": x.NewEnd, "amount_paise": x.AmountPaise, "requested_side": sideName}
		return s.repo.Emit(ctx, b.ID, events.BookingExtensionRequested, pl)
	})
	return q, err
}

// Extensions lists a booking's extension requests.
func (s *Service) Extensions(ctx context.Context, p *httpx.Principal, id uuid.UUID) (ExtensionList, error) {
	if _, _, err := s.Access(ctx, p, id); err != nil {
		return ExtensionList{}, err
	}
	list, err := s.repo.Extensions(ctx, id)
	return ExtensionList{Data: list}, err
}

// RespondToExtension approves or declines (the other party) or withdraws
// (the requester) an open extension request. Approval moves the booking's
// end; the exclusion constraint refuses it if the dates were taken meanwhile.
func (s *Service) RespondToExtension(ctx context.Context, p *httpx.Principal, id, extID uuid.UUID, decision string, req ActionRequest) (Extension, error) {
	b, side, err := s.Access(ctx, p, id)
	if err != nil {
		return Extension{}, err
	}
	x, err := s.repo.Extension(ctx, b.ID, extID)
	if err != nil {
		return Extension{}, err
	}
	if x.Status != "pending" || b.Status != StatusExtensionRequested {
		return Extension{}, apperror.ErrInvalidTransition.WithDetail("this extension request was already answered")
	}
	acting, sideName := partySide(side)
	if acting == 0 {
		return Extension{}, apperror.ErrForbidden
	}
	if (decision == "withdraw") != (sideName == x.RequestedSide) {
		if decision == "withdraw" {
			return Extension{}, apperror.ErrForbidden.WithDetail("only the side that asked can withdraw the request")
		}
		return Extension{}, apperror.ErrForbidden.WithDetail("the other party answers this request")
	}
	if err := checkVersion(b, req.Version); err != nil {
		return Extension{}, err
	}
	reason := strings.TrimSpace(req.Reason)
	if decision == "decline" && reason == "" {
		return Extension{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "reason", Code: "required", Message: "say why you decline"})
	}
	status := map[string]string{"approve": "approved", "decline": "declined", "withdraw": "withdrawn"}[decision]
	var out Extension
	why := "extension " + status
	if reason != "" {
		why = reason
	}
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: acting.Role(), Reason: why}, func(ctx context.Context) error {
		var why *string
		if reason != "" {
			why = &reason
		}
		out, err = s.repo.RespondExtension(ctx, x.ID, status, uuid.NullUUID{UUID: p.UserID, Valid: true}, why)
		if err != nil {
			return err
		}
		t, err := s.repo.Transition(ctx, b.ID, b.Status, b.Version, StatusActive, nil)
		if err != nil {
			return err
		}
		pl := payload(b, p.UserID)
		pl.Extra = map[string]any{"extension_id": x.ID, "new_end": x.NewEnd, "amount_paise": x.AmountPaise, "reason": reason}
		switch decision {
		case "approve":
			if _, err := s.repo.ExtendPeriod(ctx, b.ID, t.Version, x.NewEnd, x.AmountPaise); err != nil {
				return err
			}
			return s.repo.Emit(ctx, b.ID, events.BookingExtensionApproved, pl)
		case "decline":
			return s.repo.Emit(ctx, b.ID, events.BookingExtensionDeclined, pl)
		}
		return nil
	})
	return out, err
}

// ---- what the caller may do ----------------------------------------------------------------------

// actionsFor lists the lifecycle steps open to the caller's side(s). The
// names match the endpoints; the server re-checks every one of them.
func actionsFor(b Booking, side Side, pending *Extension, money *MoneySummary) []string {
	var out []string
	add := func(when bool, names ...string) {
		if when {
			out = append(out, names...)
		}
	}
	st := b.Status
	in := func(list ...string) bool { return slices.Contains(list, st) }
	online := b.PaymentMode == "online"

	if side.Has(SideCustomer) {
		add(st == StatusDraft, "update_draft", "submit")
		add(online && in(awaitingPayment...), "pay")
		add(in(StatusDraft, StatusRequested, StatusQuoted, StatusAccepted, StatusPaymentPending, StatusPaymentProcessing,
			StatusConfirmed, StatusOperatorAssigned), "cancel")
		add(in(approvalCancel...), "request_cancellation")
		add(st == StatusCancellationRequested, "withdraw_cancellation")
		add(st == StatusOnSite, "confirm_handover")
		add(st == StatusActive, "request_return")
	}
	if side.Has(SideOwner) {
		add(in(StatusRequested, StatusQuoted), "accept", "reject")
		add(st == StatusConfirmed && b.WithOperator, "assign_operator")
		add(in(StatusConfirmed, StatusOperatorAssigned), "dispatch")
		add(in(StatusConfirmed, StatusOperatorAssigned, StatusDispatched, StatusOnSite), "cancel")
		add(st == StatusCancellationRequested, "approve_cancellation", "decline_cancellation")
		add(st == StatusReturnInitiated, "mark_returned")
		add(st == StatusReturned && (!online || money == nil || money.BalanceDuePaise == 0), "complete")
		add(st == StatusReturned && b.FinalizedAt != nil, "issue_invoice")
		add(b.FinalizedAt == nil && in(finalizable...), "settle_final")
	}
	if side.Has(SideOperator) && !side.Has(SideOwner) {
		add(in(StatusConfirmed, StatusOperatorAssigned), "dispatch")
		add(st == StatusDispatched, "arrive")
		add(st == StatusOnSite, "start")
		add(st == StatusReturnInitiated, "mark_returned")
	}
	if side.Has(SideOwner) && st == StatusDispatched {
		out = append(out, "arrive")
	}
	if side.Has(SideCustomer | SideOwner) {
		add(st == StatusActive, "request_extension")
		if pending != nil {
			mine := (pending.RequestedSide == "customer" && side.Has(SideCustomer)) || (pending.RequestedSide == "owner" && side.Has(SideOwner))
			add(mine, "withdraw_extension")
			add(!mine, "approve_extension", "decline_extension")
		}
		add(in(StatusDispatched, StatusOnSite, StatusActive, StatusExtensionRequested, StatusReturnInitiated), "track")
		add(in(StatusActive, StatusReturned, StatusCompleted), "dispute")
		add(in(StatusReturned, StatusCompleted, StatusClosed, StatusDisputed), "invoices")
		add(in(StatusCompleted, StatusClosed), "review")
	}
	if side.Has(SideCustomer) && online && b.FinalizedAt != nil && money != nil && money.BalanceDuePaise > 0 &&
		in(StatusReturned, StatusCompleted, StatusClosed) {
		out = append(out, "pay_balance")
	}
	if out == nil {
		out = []string{}
	}
	return out
}

// decorate fills the caller-specific parts of the detail view.
func (s *Service) decorate(ctx context.Context, b *Booking, side Side) error {
	var est Price
	if err := decodeStored(b.Price, &est, "booking price"); err != nil {
		return err
	}

	paid, refunded, err := s.repo.PaymentTotals(ctx, b.ID)
	if err != nil {
		return err
	}
	refunds, err := s.repo.Refunds(ctx, b.ID)
	if err != nil {
		return err
	}
	total := b.EstimatedTotal
	if b.FinalTotal != nil {
		total = *b.FinalTotal
	}
	money := &MoneySummary{PaymentMode: b.PaymentMode, AdvanceDuePaise: est.AdvanceDuePaise, DepositPaise: b.DepositPaise,
		PaidPaise: paid, RefundedPaise: refunded, PaymentDueAt: b.PaymentDueAt, Refunds: refunds}
	if !slices.Contains(deadStatuses, b.Status) && b.Status != StatusDraft {
		money.BalanceDuePaise = max(total+b.DepositPaise-(paid-refunded), 0)
	}
	b.Money = money

	if b.Status == StatusExtensionRequested {
		list, err := s.repo.Extensions(ctx, b.ID)
		if err != nil {
			return err
		}
		for i := range list {
			if list[i].Status == "pending" {
				b.PendingExtension = &list[i]
			}
		}
	}

	if side.Has(SideOwner | SideAdmin) {
		d := b.customerDetails
		c := &CustomerInfo{CustomerType: b.customerType, OrgName: b.CustomerName, ContactName: b.requesterName, KYCStatus: b.customerKYC, CompletedRentals: b.customerRentals}
		if b.customerType == CustomerCompany {
			c.CompanyName, c.ContactName, c.City, c.State, c.GSTIN = d.CompanyName, d.ContactName, d.City, d.State, d.GSTIN
		} else if d.Name != "" {
			c.ContactName = d.Name
		}
		if !slices.Contains([]string{StatusDraft, StatusRequested, StatusQuoted, StatusRejected, StatusExpired}, b.Status) {
			// Contact details are released to the owner only once the booking is accepted.
			c.Phone, c.Email, c.PinCode, c.Address = b.requesterPhone, d.Email, d.PinCode, d.Address
			if d.Phone != "" {
				c.Phone = d.Phone
			}
		}
		b.Customer = c
		if b.Status == StatusRequested || b.Status == StatusQuoted {
			busy, err := s.repo.ConflictsExcept(ctx, b.EquipmentID, b.ID, b.Period.Start, b.Period.End)
			if err != nil {
				return err
			}
			b.Availability = &AvailabilityResult{Available: len(busy) == 0, Period: view(b.Period, b.Timezone), Conflicts: busy}
		}
	}

	var rules []CancellationRule
	if err := decodeStored(b.cancellationRules, &rules, "cancellation rules"); err != nil {
		return err
	}
	cv := &CancellationView{Rules: rules, Summary: PolicySummary(rules)}
	if rules == nil {
		cv.Rules = []CancellationRule{}
	}
	if !slices.Contains(freeCancel, b.Status) {
		cv.FeeNow = CancellationFee(b.cancellationRules, b.EstimatedTotal, s.now(), b.Period.Start)
	}
	b.Cancellation = cv

	b.Actions = actionsFor(*b, side, b.PendingExtension, money)
	return nil
}

// PolicySummary describes cancellation rules in one sentence.
func PolicySummary(rules []CancellationRule) string {
	if len(rules) == 0 {
		return "Free cancellation until the owner dispatches the machine."
	}
	r := slices.Clone(rules)
	slices.SortFunc(r, func(a, b CancellationRule) int {
		switch {
		case a.HoursBefore > b.HoursBefore:
			return -1
		case a.HoursBefore < b.HoursBefore:
			return 1
		}
		return 0
	})
	var parts []string
	for i, x := range r {
		fee := "free"
		if x.FeeBps > 0 {
			fee = pct(x.FeeBps) + "% fee"
		}
		switch {
		case i == 0 && x.HoursBefore > 0:
			parts = append(parts, fmt.Sprintf("%s with %s or more notice", fee, hoursText(x.HoursBefore)))
		case x.HoursBefore > 0:
			parts = append(parts, fmt.Sprintf("%s from %s before the start", fee, hoursText(x.HoursBefore)))
		default:
			parts = append(parts, fee+" at the last minute")
		}
	}
	s := strings.Join(parts, "; ") + "."
	return strings.ToUpper(s[:1]) + s[1:]
}

func hoursText(h float64) string {
	if h >= 24 && int(h)%24 == 0 {
		return hoursLabel(int32(h))
	}
	return fmt.Sprintf("%g hours", h)
}

// Policy publishes the booking, payment and cancellation rules.
func (s *Service) Policy(ctx context.Context) (Policy, error) {
	raw, err := s.repo.DefaultCancellationRules(ctx)
	if err != nil {
		return Policy{}, err
	}
	var rules []CancellationRule
	if err := decodeStored(raw, &rules, "default cancellation rules"); err != nil {
		return Policy{}, err
	}
	if rules == nil {
		rules = []CancellationRule{}
	}
	return Policy{
		AdvanceBps: s.cfg.AdvanceBps, PaymentWindowMinutes: int(s.cfg.PaymentWindow.Minutes()),
		OwnerResponseMinutes: int(s.cfg.OwnerResponseWindow.Minutes()), ProcessingTimeoutMinutes: int(s.cfg.ProcessingTimeout.Minutes()),
		DefaultCancellation: rules, FreeCancellation: freeCancel, ApprovalNeeded: approvalCancel,
		Extendable: []string{StatusActive},
	}, nil
}

// RefundList returns a booking's refunds (system use).
func (s *Service) RefundList(ctx context.Context, bookingID uuid.UUID) ([]Refund, error) {
	return s.repo.Refunds(ctx, bookingID)
}
