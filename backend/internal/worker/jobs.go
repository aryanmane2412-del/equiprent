package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/events"
	"github.com/equiprent/backend/internal/modules/maintenance"
)

// PGLocker implements Locker with a transaction-scoped advisory lock: the
// job runs inside that transaction, so the lock is released at commit or
// rollback, even if the process dies.
type PGLocker struct {
	pool *pgxpool.Pool
	tx   database.Transactor
}

func NewPGLocker(pool *pgxpool.Pool, tx database.Transactor) *PGLocker {
	return &PGLocker{pool: pool, tx: tx}
}

var errSkip = errors.New("lock held elsewhere")

func (l *PGLocker) WithLock(ctx context.Context, key int64, fn func(ctx context.Context) error) (bool, error) {
	err := l.tx.InTxAs(ctx, database.Actor{Role: "system", Reason: "background job"}, func(ctx context.Context) error {
		locked, err := database.Queries(ctx, l.pool).TryAdvisoryXactLock(ctx, key)
		if err != nil {
			return err
		}
		if !locked {
			return errSkip
		}
		return fn(ctx)
	})
	if errors.Is(err, errSkip) {
		return false, nil
	}
	return err == nil, err
}

// Dispatcher turns an outbox event into notifications.
type Dispatcher interface {
	Dispatch(ctx context.Context, eventType string, aggregateID uuid.UUID, payload json.RawMessage) (int, error)
}

// Jobs builds the standard job set.
type Jobs struct {
	Pool       *pgxpool.Pool
	Dispatcher Dispatcher
	// Refunds sends queued refunds to the payment gateway (optional).
	Refunds RefundProcessor
	// Tracking closes idle location sessions (optional).
	Tracking IdleSessionCloser
	// ProcessingTimeout returns abandoned checkouts to payment_pending
	// (default 30 minutes).
	ProcessingTimeout time.Duration
	Log               *slog.Logger
	Now               func() time.Time
}

// IdleSessionCloser is implemented by the tracking service.
type IdleSessionCloser interface {
	CloseIdle(ctx context.Context) (int, error)
}

// RefundProcessor is implemented by the payments service.
type RefundProcessor interface {
	ProcessRefunds(ctx context.Context) error
	ReconcileWebhooks(ctx context.Context) error
	ReleaseDeposits(ctx context.Context) error
}

func (j Jobs) q(ctx context.Context) *db.Queries { return database.Queries(ctx, j.Pool) }

// All returns every background job with its schedule.
func (j Jobs) All() []Job {
	if j.Now == nil {
		j.Now = time.Now
	}
	if j.ProcessingTimeout <= 0 {
		j.ProcessingTimeout = 30 * time.Minute
	}
	return []Job{
		{Name: "outbox.dispatch", Interval: 5 * time.Second, Timeout: 30 * time.Second, Run: j.DispatchOutbox},
		{Name: "bookings.expire_requests", Interval: time.Minute, Run: j.ExpireRequests},
		{Name: "bookings.expire_unpaid", Interval: time.Minute, Run: j.ExpireUnpaid},
		{Name: "bookings.release_processing", Interval: time.Minute, Run: j.ReleaseProcessing},
		{Name: "bookings.lapse_extensions", Interval: 5 * time.Minute, Run: j.LapseExtensions},
		{Name: "refunds.process", Interval: time.Minute, Timeout: 2 * time.Minute, Run: j.ProcessRefunds},
		{Name: "deposits.release", Interval: time.Minute, Timeout: 2 * time.Minute, Run: j.ReleaseDeposits},
		{Name: "payments.reconcile_webhooks", Interval: time.Minute, Timeout: 2 * time.Minute, Run: j.ReconcileWebhooks},
		{Name: "tracking.close_idle_sessions", Interval: 10 * time.Minute, Run: j.CloseIdleSessions},
		{Name: "worklogs.auto_approve", Interval: 15 * time.Minute, Run: j.AutoApproveWorkLogs},
		{Name: "cleanup.expired_keys", Interval: time.Hour, Timeout: 5 * time.Minute, Run: j.Cleanup},
		{Name: "tracking.ensure_partitions", Interval: 24 * time.Hour, Timeout: 5 * time.Minute, Run: j.EnsurePartitions},
		{Name: "documents.expiry_scan", Interval: 24 * time.Hour, Timeout: 5 * time.Minute, Run: j.DocumentExpiryScan},
		{Name: "maintenance.due_scan", Interval: 6 * time.Hour, Timeout: 5 * time.Minute, Run: j.MaintenanceDueScan},
		{Name: "maintenance.reminders", Interval: time.Hour, Timeout: 5 * time.Minute, Run: j.MaintenanceReminders},
	}
}

// DispatchOutbox delivers up to 100 pending events. Database errors roll the
// batch back for retry; malformed events are logged and skipped so they
// cannot block the queue.
func (j Jobs) DispatchOutbox(ctx context.Context) error {
	q := j.q(ctx)
	evs, err := q.ClaimOutboxEvents(ctx, 100)
	if err != nil {
		return err
	}
	for _, e := range evs {
		n, err := j.Dispatcher.Dispatch(events.WithEventID(ctx, e.ID), e.EventType, e.AggregateID, e.Payload)
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		switch {
		case errors.As(err, &syntaxErr), errors.As(err, &typeErr):
			j.Log.Error("skipping malformed outbox event", "event_id", e.ID, "type", e.EventType, "error", err)
		case err != nil:
			return fmt.Errorf("dispatch %s %s: %w", e.EventType, e.ID, err)
		default:
			j.Log.Debug("outbox event delivered", "type", e.EventType, "recipients", n)
		}
		if err := q.MarkOutboxPublished(ctx, e.ID); err != nil {
			return err
		}
	}
	return nil
}

// ExpireRequests expires booking requests whose owner missed the response
// SLA and announces it.
func (j Jobs) ExpireRequests(ctx context.Context) error {
	q := j.q(ctx)
	rows, err := q.ExpireStaleBookingRequests(ctx, 200)
	if err != nil {
		return err
	}
	for _, b := range rows {
		if err := events.Emit(ctx, q, "booking", b.ID, events.BookingExpired, events.Payload{
			Reference: b.Reference, CustomerOrgID: b.CustomerOrgID, OwnerOrgID: b.OwnerOrgID,
		}); err != nil {
			return err
		}
	}
	if len(rows) > 0 {
		j.Log.Info("expired stale booking requests", "count", len(rows))
	}
	return nil
}

// ExpireUnpaid releases the dates of online bookings whose advance was not
// paid by the payment deadline.
func (j Jobs) ExpireUnpaid(ctx context.Context) error {
	q := j.q(ctx)
	rows, err := q.ExpireUnpaidBookings(ctx, 200)
	if err != nil {
		return err
	}
	for _, b := range rows {
		if err := events.Emit(ctx, q, "booking", b.ID, events.BookingPaymentExpired, events.Payload{
			Reference: b.Reference, CustomerOrgID: b.CustomerOrgID, OwnerOrgID: b.OwnerOrgID,
		}); err != nil {
			return err
		}
	}
	if len(rows) > 0 {
		j.Log.Info("expired unpaid bookings", "count", len(rows))
	}
	return nil
}

// ReleaseProcessing returns checkouts abandoned without a gateway answer to
// awaiting payment, so the customer can pay again.
func (j Jobs) ReleaseProcessing(ctx context.Context) error {
	ids, err := j.q(ctx).ReleaseStaleProcessing(ctx, db.ReleaseStaleProcessingParams{
		AfterMinutes: int32(j.ProcessingTimeout.Minutes()), BatchSize: 200,
	})
	if len(ids) > 0 {
		j.Log.Info("released stale payment checkouts", "count", len(ids))
	}
	return err
}

// LapseExtensions expires extension requests nobody answered before the
// rental reached its current end; the rental carries on unchanged.
func (j Jobs) LapseExtensions(ctx context.Context) error {
	q := j.q(ctx)
	rows, err := q.LapsedExtensions(ctx, 100)
	if err != nil {
		return err
	}
	for _, x := range rows {
		if _, err := q.RespondExtension(ctx, db.RespondExtensionParams{ID: x.ID, Status: db.ExtensionStatusExpired}); err != nil {
			return err
		}
		if _, err := q.TransitionBooking(ctx, db.TransitionBookingParams{
			ID: x.BookingID, FromStatus: db.BookingStatusExtensionRequested, Version: x.Version, ToStatus: db.BookingStatusActive,
		}); err != nil {
			return err
		}
		if err := events.Emit(ctx, q, "booking", x.BookingID, events.BookingExtensionExpired, events.Payload{
			Reference: x.Reference, CustomerOrgID: x.CustomerOrgID, OwnerOrgID: x.OwnerOrgID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// ProcessRefunds hands queued refunds to the gateway.
func (j Jobs) ProcessRefunds(ctx context.Context) error {
	if j.Refunds == nil {
		return nil
	}
	return j.Refunds.ProcessRefunds(ctx)
}

// ReleaseDeposits refunds security deposits of completed bookings.
func (j Jobs) ReleaseDeposits(ctx context.Context) error {
	if j.Refunds == nil {
		return nil
	}
	return j.Refunds.ReleaseDeposits(ctx)
}

// ReconcileWebhooks retries failed payment webhook events.
func (j Jobs) ReconcileWebhooks(ctx context.Context) error {
	if j.Refunds == nil {
		return nil
	}
	return j.Refunds.ReconcileWebhooks(ctx)
}

// CloseIdleSessions ends location sessions that stopped reporting.
func (j Jobs) CloseIdleSessions(ctx context.Context) error {
	if j.Tracking == nil {
		return nil
	}
	n, err := j.Tracking.CloseIdle(ctx)
	if n > 0 && j.Log != nil {
		j.Log.InfoContext(ctx, "closed idle tracking sessions", "count", n)
	}
	return err
}

// AutoApproveWorkLogs approves daily logs the customer did not act on.
func (j Jobs) AutoApproveWorkLogs(ctx context.Context) error {
	n, err := j.q(ctx).AutoApproveWorkLogs(ctx)
	if n > 0 {
		j.Log.Info("auto-approved work logs", "count", n)
	}
	return err
}

// Cleanup purges expired idempotency keys and old OTP challenges.
func (j Jobs) Cleanup(ctx context.Context) error {
	q := j.q(ctx)
	keys, err := q.DeleteExpiredIdempotencyKeys(ctx)
	if err != nil {
		return err
	}
	otps, err := q.DeleteOldOTPChallenges(ctx)
	if err != nil {
		return err
	}
	tokens, err := q.DeleteExpiredAuthTokens(ctx)
	if err != nil {
		return err
	}
	j.Log.Debug("cleanup", "idempotency_keys", keys, "otp_challenges", otps, "auth_tokens", tokens)
	return nil
}

// EnsurePartitions keeps six months of location_pings partitions ahead.
func (j Jobs) EnsurePartitions(ctx context.Context) error {
	return j.q(ctx).EnsureLocationPingPartitions(ctx, db.EnsureLocationPingPartitionsParams{
		FromDate: j.Now().AddDate(0, -1, 0), Months: 6,
	})
}

// DocumentExpiryScan warns owners about documents expiring within 30 days.
func (j Jobs) DocumentExpiryScan(ctx context.Context) error {
	q := j.q(ctx)
	docs, err := q.ListExpiringDocuments(ctx, j.Now().AddDate(0, 0, 30))
	if err != nil {
		return err
	}
	for _, d := range docs {
		until := "soon"
		if d.ValidUntil != nil {
			until = d.ValidUntil.Format(time.DateOnly)
		}
		if err := events.Emit(ctx, q, "equipment", d.EquipmentID, events.DocumentExpiring, events.Payload{
			OwnerOrgID: d.OwnerOrgID,
			Title:      fmt.Sprintf("%s: %s expires on %s", d.Title, d.DocType, until),
		}); err != nil {
			return err
		}
	}
	return nil
}

// MaintenanceDueScan notifies owners of plans that have fallen due (by
// calendar or hour meter), at most once a week per plan.
func (j Jobs) MaintenanceDueScan(ctx context.Context) error {
	q := j.q(ctx)
	due, err := q.DuePlansToRemind(ctx, j.Now().Add(-7*24*time.Hour))
	if err != nil {
		return err
	}
	ids := make([]uuid.UUID, 0, len(due))
	for _, d := range due {
		if err := events.Emit(ctx, q, "equipment", d.EquipmentID, events.MaintenanceDue, events.Payload{
			OwnerOrgID: d.OwnerOrgID, Title: d.Title + ": " + d.Task + " is due",
			Extra: map[string]any{"equipment_id": d.EquipmentID, "plan_id": d.PlanID},
		}); err != nil {
			return err
		}
		ids = append(ids, d.PlanID)
	}
	if len(ids) == 0 {
		return nil
	}
	return q.MarkPlansReminded(ctx, ids)
}

// ReminderDays is how far ahead scheduled maintenance is announced.
const ReminderDays = 3

var ist = time.FixedZone("IST", 5*3600+1800)

// MaintenanceReminders announces scheduled jobs coming up within
// ReminderDays (once a day per job) and scheduled jobs whose date has passed
// without being started (once).
func (j Jobs) MaintenanceReminders(ctx context.Context) error {
	q := j.q(ctx)
	today, _ := time.Parse(time.DateOnly, j.Now().In(ist).Format(time.DateOnly))
	upcoming, err := q.UpcomingMaintenanceReminders(ctx, db.UpcomingMaintenanceRemindersParams{Today: today, Until: today.AddDate(0, 0, ReminderDays)})
	if err != nil {
		return err
	}
	var reminded []uuid.UUID
	for _, r := range upcoming {
		if r.ScheduledFor == nil {
			continue
		}
		if err := events.Emit(ctx, q, "equipment", r.EquipmentID, events.MaintenanceReminder, events.Payload{
			OwnerOrgID: r.OwnerOrgID, Title: maintenance.ReminderText(r.Title, r.Task, *r.ScheduledFor, today),
			Extra: map[string]any{"equipment_id": r.EquipmentID, "record_id": r.ID},
		}); err != nil {
			return err
		}
		reminded = append(reminded, r.ID)
	}
	if len(reminded) > 0 {
		if err := q.MarkMaintenanceReminded(ctx, reminded); err != nil {
			return err
		}
	}
	overdue, err := q.OverdueMaintenanceRecords(ctx, today)
	if err != nil {
		return err
	}
	var late []uuid.UUID
	for _, r := range overdue {
		if err := events.Emit(ctx, q, "equipment", r.EquipmentID, events.MaintenanceOverdue, events.Payload{
			OwnerOrgID: r.OwnerOrgID, Title: fmt.Sprintf("%s: %s was scheduled for %s and has not started.", r.Title, r.Task, r.ScheduledFor.Format("02 Jan")),
			Extra: map[string]any{"equipment_id": r.EquipmentID, "record_id": r.ID},
		}); err != nil {
			return err
		}
		late = append(late, r.ID)
	}
	if len(late) == 0 {
		return nil
	}
	return q.MarkMaintenanceOverdueNotified(ctx, late)
}
