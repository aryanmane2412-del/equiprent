package payments

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/events"
	"github.com/equiprent/backend/internal/ledger"
	"github.com/equiprent/backend/internal/modules/bookings"
)

// DepositClaimWindowSeconds is how long an open damage claim may delay the release
// of a deposit after the booking completed (14 days). After it the deposit is
// returned minus only the damage claims already approved.
const DepositClaimWindowSeconds = 14 * 24 * 3600

// recordCapture posts a captured payment to the ledger and, when it carries the
// security deposit, starts tracking the deposit. It runs inside the capture
// transaction, so it commits or rolls back with the payment.
func (s *Service) recordCapture(ctx context.Context, pay Payment) error {
	if s.refunds == nil {
		return nil
	}
	b, err := s.bookings.Load(ctx, pay.BookingID)
	if err != nil {
		return err
	}
	var deposit int64
	if pay.Purpose == "advance" || pay.Purpose == "deposit" {
		var price bookings.Price
		if err := json.Unmarshal(b.Price, &price); err != nil {
			return err
		}
		deposit = min(price.DepositPaise, pay.AmountPaise)
	}
	q := s.refunds(ctx)
	if deposit > 0 {
		if err := q.InsertSecurityDeposit(ctx, db.InsertSecurityDepositParams{
			BookingID: b.ID, PaymentID: pay.ID, OwnerOrgID: b.OwnerOrgID, CustomerOrgID: b.CustomerOrgID, AmountPaise: deposit,
		}); err != nil {
			return database.MapError(err)
		}
	}
	_, err = ledger.Post(ctx, q, ledger.Posting{Kind: ledger.KindPaymentCaptured, BookingID: b.ID, ReferenceType: "payment", ReferenceID: pay.ID,
		Memo: "Payment captured (" + pay.Purpose + ")",
		Legs: []ledger.Leg{ledger.Dr(ledger.GatewayClearing, pay.AmountPaise), ledger.Cr(ledger.CustomerDeposits, deposit), ledger.Cr(ledger.CustomerFunds, pay.AmountPaise-deposit)}})
	return err
}

// ReleaseDeposits refunds the security deposit of every completed booking (minus
// approved damage claims) through the normal refund pipeline, and closes the
// deposit records of cancelled bookings. Each deposit is its own transaction.
func (s *Service) ReleaseDeposits(ctx context.Context) error {
	if s.refunds == nil {
		return nil
	}
	dctx := database.WithoutTx(ctx)
	rows, err := s.refunds(dctx).DepositsToRelease(dctx, db.DepositsToReleaseParams{WindowSeconds: DepositClaimWindowSeconds, Lim: 25})
	if err != nil {
		return err
	}
	for _, d := range rows {
		err := s.tx.InTxAs(dctx, database.Actor{Role: "system", Reason: "security deposit release"}, func(ctx context.Context) error {
			return s.releaseDeposit(ctx, d.ID)
		})
		if err != nil {
			s.log.ErrorContext(ctx, "ALERT: security deposit could not be released", "deposit_id", d.ID, "error", err)
		}
	}
	cancelled, err := s.refunds(dctx).HeldDepositsOfCancelled(dctx, 25)
	if err != nil {
		return err
	}
	for _, d := range cancelled {
		err := s.tx.InTxAs(dctx, database.Actor{Role: "system", Reason: "deposit of a cancelled booking"}, func(ctx context.Context) error {
			q := s.refunds(ctx)
			refunded, err := q.ProcessedRefundTotal(ctx, d.PaymentID)
			if err != nil {
				return err
			}
			return q.DepositRefundedOnCancel(ctx, db.DepositRefundedOnCancelParams{PaymentID: d.PaymentID, AmountPaise: min(d.AmountPaise, refunded)})
		})
		if err != nil {
			s.log.ErrorContext(ctx, "cancelled booking deposit could not be closed", "deposit_id", d.ID, "error", err)
		}
	}
	return nil
}

// releaseDeposit queues the refund and resolves the deposit atomically. The
// deposit row (held -> released/partially_forfeited/forfeited), the unique refund
// idempotency key and the refunds guard make a duplicate release impossible.
func (s *Service) releaseDeposit(ctx context.Context, id uuid.UUID) error {
	q := s.refunds(ctx)
	d, err := q.GetDepositForUpdate(ctx, id)
	if err != nil {
		return database.MapError(err)
	}
	if d.Status != db.DepositStatusHeld {
		return nil
	}
	deducted, err := q.ApprovedDamageDeductions(ctx, db.ApprovedDamageDeductionsParams{BookingID: d.BookingID, OwnerOrgID: d.OwnerOrgID, CustomerOrgID: d.CustomerOrgID})
	if err != nil {
		return err
	}
	deducted = min(deducted, d.AmountPaise)
	release := d.AmountPaise - deducted
	status, note := db.DepositStatusReleased, "Deposit released in full after completion."
	switch {
	case release == 0:
		status, note = db.DepositStatusForfeited, "Deposit fully applied to approved damage claims."
	case deducted > 0:
		status, note = db.DepositStatusPartiallyForfeited, "Deposit released minus approved damage claims."
	}
	var refundID uuid.NullUUID
	if release > 0 {
		rp, err := q.RefundablePayment(ctx, d.PaymentID)
		if err != nil {
			return database.MapError(err)
		}
		if rp.RefundablePaise < release { // never mark money returned that cannot be refunded
			return errors.New("deposit " + id.String() + " exceeds what is refundable on its payment; needs manual review")
		}
		rid, err := q.CreateRefund(ctx, db.CreateRefundParams{PaymentID: d.PaymentID, BookingID: d.BookingID, AmountPaise: release,
			Reason: "Security deposit release", IdempotencyKey: "deposit:" + id.String()})
		if err != nil {
			return database.MapError(err)
		}
		refundID = uuid.NullUUID{UUID: rid, Valid: true}
	}
	if _, err := q.ResolveDeposit(ctx, db.ResolveDepositParams{ID: id, Status: status, DeductedPaise: deducted, RefundedPaise: release,
		ResolutionNote: &note, RefundID: refundID}); err != nil {
		return database.MapError(err)
	}
	if deducted > 0 { // retained for the owner: customer deposit liability -> owner payable
		if _, err := ledger.Post(ctx, q, ledger.Posting{Kind: ledger.KindDepositRelease, BookingID: d.BookingID, ReferenceType: "deposit_deduction", ReferenceID: id,
			Memo: "Approved damage claim deducted from the deposit",
			Legs: []ledger.Leg{ledger.Dr(ledger.CustomerDeposits, deducted), ledger.CrOwner(d.OwnerOrgID, deducted)}}); err != nil {
			return err
		}
	}
	b, err := s.bookings.Load(ctx, d.BookingID)
	if err != nil {
		return err
	}
	pl := bookings.EventPayload(b, uuid.Nil)
	pl.Extra = map[string]any{"booking_id": b.ID, "amount_paise": release, "deducted_paise": deducted}
	return events.Emit(ctx, q, "deposit", id, events.DepositReleased, pl)
}
