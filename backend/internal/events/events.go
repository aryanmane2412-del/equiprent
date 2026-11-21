// Package events writes domain events to the transactional outbox
// (outbox_events). Events are inserted in the same transaction as the state
// change, so they are never lost or published for rolled-back work; the
// background worker delivers them (notifications today, a message bus later).
package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/db"
)

// Event types (resource.action, enforced by a CHECK constraint).
const (
	BookingRequested        = "booking.requested"
	BookingAccepted         = "booking.accepted"
	BookingRejected         = "booking.rejected"
	BookingConfirmed        = "booking.confirmed"
	BookingCancelled        = "booking.cancelled"
	BookingExpired          = "booking.expired"
	BookingOperatorAssigned = "booking.operator_assigned"
	BookingJobUnassigned    = "booking.job_unassigned" // tells a replaced driver the job moved
	OwnerRegistered         = "owner.registered"       // admins: a new equipment-owner account
	VerificationSubmitted   = "verification.submitted" // admins: a business/driver verification awaits review
	DeliveryPhotoSubmitted  = "booking.delivery_photo_submitted"
	DeliveryPhotoApproved   = "booking.delivery_photo_approved"
	BookingJobAssigned      = "booking.job_assigned"      // tells the driver (only) about a new assignment
	BookingOperatorAccepted = "booking.operator_accepted" // the driver took the job
	BookingOperatorDeclined = "booking.operator_declined" // the driver refused; the owner must pick another
	BookingDispatched       = "booking.dispatched"
	BookingOnSite           = "booking.on_site"
	BookingActive           = "booking.active"
	BookingReturnInitiated  = "booking.return_initiated"
	BookingReturned         = "booking.returned"
	BookingCompleted        = "booking.completed"
	BookingDisputed         = "booking.disputed"
	PaymentCaptured         = "payment.captured"
	PaymentFailed           = "payment.failed"
	DisputeOpened           = "dispute.opened"
	DisputeResolved         = "dispute.resolved"
	MaintenanceDue          = "maintenance.due"
	DocumentExpiring        = "document.expiring"
	VerificationApproved    = "verification.approved"
	VerificationRejected    = "verification.rejected"

	// Communication and operations (migration 000020).
	BookingFinalCharges = "booking.final_charges" // owner settled the final bill
	PayoutPaid          = "payout.paid"           // routed; emitted once payouts are built
	RefundProcessed     = "refund.processed"      // routed; emitted once refunds are built
	DepositReleased     = "deposit.released"      // security deposit refund started after completion
	AgreementSigned     = "agreement.signed"
	AgreementExecuted   = "agreement.executed"
	ChatMessage         = "chat.message"
	MaintenanceReminder = "maintenance.reminder"
	MaintenanceOverdue  = "maintenance.overdue"
	ReviewReceived      = "review.received"

	// Booking engine: cancellation requests, extensions, refunds.
	BookingCancellationRequested = "booking.cancellation_requested"
	BookingCancellationDeclined  = "booking.cancellation_declined"
	BookingExtensionRequested    = "booking.extension_requested"
	BookingExtensionApproved     = "booking.extension_approved"
	BookingExtensionDeclined     = "booking.extension_declined"
	BookingExtensionExpired      = "booking.extension_expired"
	BookingRefundPending         = "booking.refund_pending"
	BookingRefunded              = "booking.refunded"
	BookingPaymentExpired        = "booking.payment_expired"

	// Fleet operations.
	IssueReported = "issue.reported" // damage / breakdown / safety report on a job
	IssueReviewed = "issue.reviewed"
)

// Payload is the common event body. Recipients are derived from the
// organisations / users named here.
type Payload struct {
	Reference     string         `json:"reference,omitempty"`
	CustomerOrgID uuid.UUID      `json:"customer_org_id,omitzero"`
	OwnerOrgID    uuid.UUID      `json:"owner_org_id,omitzero"`
	ActorUserID   uuid.UUID      `json:"actor_user_id,omitzero"`
	UserIDs       []uuid.UUID    `json:"user_ids,omitempty"`
	Title         string         `json:"title,omitempty"`
	Extra         map[string]any `json:"extra,omitempty"`
}

// Emitter inserts events using the caller's transaction.
type Emitter interface {
	InsertOutboxEvent(ctx context.Context, arg db.InsertOutboxEventParams) error
}

// Emit writes one event to the outbox.
func Emit(ctx context.Context, q Emitter, aggregate string, id uuid.UUID, eventType string, p Payload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", eventType, err)
	}
	return q.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{
		AggregateType: aggregate, AggregateID: id, EventType: eventType, Payload: body,
	})
}

type eventIDKey struct{}

// WithEventID marks ctx with the outbox event being dispatched, so consumers
// can deduplicate replays.
func WithEventID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, eventIDKey{}, id)
}

// EventIDFrom returns the outbox event id set by WithEventID.
func EventIDFrom(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(eventIDKey{}).(uuid.UUID)
	return id, ok && id != uuid.Nil
}
