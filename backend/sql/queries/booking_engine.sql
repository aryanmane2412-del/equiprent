-- Booking engine: drafts, extensions, refunds, payment deadlines and the
-- worker sweeps (migration 000019).

-- name: UpdateDraftBooking :one
-- Replaces a draft's choices and price (version-checked).
UPDATE bookings SET
  site_address_id = @site_address_id,
  site_location = ST_SetSRID(ST_MakePoint(@site_lng::float8, @site_lat::float8), 4326)::geography,
  period = tstzrange(@starts_at::timestamptz, @ends_at::timestamptz, '[)'),
  with_operator = @with_operator, pricing_unit = @pricing_unit, price_snapshot = @price_snapshot,
  estimated_total_paise = @estimated_total_paise, deposit_paise = @deposit_paise,
  cancellation_policy_id = sqlc.narg(cancellation_policy_id), payment_mode = @payment_mode,
  work_description = @work_description, delivery = @delivery, pickup = @pickup
WHERE id = @id AND status = 'draft' AND version = @version
RETURNING id, status, version;

-- name: RepriceBooking :one
-- Stores the price recomputed when a draft is submitted (version-checked).
UPDATE bookings SET price_snapshot = @price_snapshot, estimated_total_paise = @estimated_total_paise,
  deposit_paise = @deposit_paise, cancellation_policy_id = sqlc.narg(cancellation_policy_id)
WHERE id = @id AND version = @version
RETURNING id, status, version;

-- name: SetBookingResponseDue :one
UPDATE bookings SET owner_response_due_at = sqlc.narg(owner_response_due_at), payment_due_at = sqlc.narg(payment_due_at)
WHERE id = @id AND version = @version
RETURNING id, status, version;

-- name: OverlappingRequests :many
-- Requests for the same machine and overlapping dates that cannot be
-- accepted once another booking holds the slot.
SELECT id, reference, customer_org_id, owner_org_id, version, status::text AS status
FROM bookings
WHERE equipment_id = @equipment_id AND id <> @id AND status IN ('requested', 'quoted')
  AND period && tstzrange(@starts_at::timestamptz, @ends_at::timestamptz, '[)');

-- name: SlotConflictsExcept :many
-- Held bookings (other than @booking_id) and owner blocks overlapping [from, to).
SELECT lower(b.period)::timestamptz AS starts_at, upper(b.period)::timestamptz AS ends_at, 'booked'::text AS kind
FROM bookings b
WHERE b.equipment_id = @equipment_id AND b.id <> @booking_id AND booking_holds_slot(b.status, b.resume_status)
  AND b.period && tstzrange(@from_at::timestamptz, @to_at::timestamptz, '[)')
UNION ALL
SELECT lower(a.period)::timestamptz, upper(a.period)::timestamptz, 'blocked'::text
FROM availability_blocks a
WHERE a.equipment_id = @equipment_id AND a.period && tstzrange(@from_at::timestamptz, @to_at::timestamptz, '[)')
ORDER BY 1;

-- ---- extensions ----------------------------------------------------------------------------

-- name: CreateExtension :one
INSERT INTO booking_extensions (booking_id, requested_by, requested_side, previous_end, new_end, price_snapshot, amount_paise, note)
VALUES (@booking_id, @requested_by, @requested_side, @previous_end, @new_end, @price_snapshot, @amount_paise, @note)
RETURNING *;

-- name: GetExtension :one
SELECT * FROM booking_extensions WHERE id = @id AND booking_id = @booking_id;

-- name: ListExtensions :many
SELECT * FROM booking_extensions WHERE booking_id = @booking_id ORDER BY created_at;

-- name: RespondExtension :one
UPDATE booking_extensions
SET status = @status, responded_by = sqlc.narg(responded_by), responded_at = now(), response_reason = sqlc.narg(response_reason)
WHERE id = @id AND status = 'pending'
RETURNING *;

-- name: ExtendBookingPeriod :one
-- Moves the end of a running booking; the exclusion constraint and block
-- trigger refuse an overlap.
UPDATE bookings
SET period = tstzrange(lower(period), @new_end::timestamptz, '[)'),
    estimated_total_paise = estimated_total_paise + @amount_paise::bigint
WHERE id = @id AND version = @version
RETURNING id, status, version;

-- name: ExtendAssignmentPeriod :exec
UPDATE operator_assignments SET period = tstzrange(lower(period), @new_end::timestamptz, '[)')
WHERE booking_id = @booking_id AND status IN ('assigned', 'accepted');

-- name: SumApprovedExtensions :one
SELECT COALESCE(sum(amount_paise), 0)::bigint AS total
FROM booking_extensions WHERE booking_id = @booking_id AND status = 'approved';

-- name: LapsedExtensions :many
-- Pending requests whose booking reached its current end unanswered.
SELECT x.id, x.booking_id, b.version, b.reference, b.customer_org_id, b.owner_org_id
FROM booking_extensions x
JOIN bookings b ON b.id = x.booking_id
WHERE x.status = 'pending' AND x.previous_end <= now() AND b.status = 'extension_requested'
ORDER BY x.previous_end
LIMIT @batch_size
FOR UPDATE OF x, b SKIP LOCKED;

-- ---- payments seen from bookings ---------------------------------------------------------------

-- name: BookingPaymentTotals :many
-- Captured money per purpose, net of refunds that were not failed.
SELECT p.purpose::text AS purpose,
       COALESCE(sum(p.amount_paise), 0)::bigint AS captured_paise,
       COALESCE(sum((SELECT COALESCE(sum(r.amount_paise), 0) FROM refunds r WHERE r.payment_id = p.id AND r.status <> 'failed')), 0)::bigint AS refunded_paise
FROM payments p
WHERE p.booking_id = @booking_id AND p.status IN ('captured', 'partially_refunded', 'refunded')
GROUP BY p.purpose;

-- name: OpenPaymentAttempts :one
SELECT count(*)::int FROM payments WHERE booking_id = @booking_id AND status IN ('created', 'authorized');

-- name: RefundablePayments :many
-- Captured payments with what is left to refund (deposits first, newest first).
SELECT p.id, p.purpose::text AS purpose, p.amount_paise,
       (p.amount_paise - COALESCE((SELECT sum(r.amount_paise) FROM refunds r WHERE r.payment_id = p.id AND r.status <> 'failed'), 0))::bigint AS refundable_paise
FROM payments p
WHERE p.booking_id = @booking_id AND p.status IN ('captured', 'partially_refunded')
ORDER BY (p.purpose = 'deposit') DESC, p.created_at DESC
FOR UPDATE OF p;

-- name: CreateRefund :one
INSERT INTO refunds (payment_id, booking_id, amount_paise, reason, idempotency_key, initiated_by)
VALUES (@payment_id, @booking_id, @amount_paise, @reason, @idempotency_key, sqlc.narg(initiated_by))
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING id;

-- name: ListBookingRefunds :many
SELECT r.id, r.payment_id, r.amount_paise, r.reason, r.status::text AS status, r.gateway_refund_id, r.failure_reason,
       r.processed_at, r.created_at
FROM refunds r WHERE r.booking_id = @booking_id ORDER BY r.created_at;

-- name: CountOpenRefunds :one
SELECT count(*)::int FROM refunds WHERE booking_id = @booking_id AND status = 'pending';

-- name: ClaimRefundsForSubmit :many
-- Short transaction: marks due refunds as submitting before the gateway is called. A refund
-- stuck in submitting (worker crashed mid-call) is claimed again after 10 minutes and the
-- worker looks it up at the gateway before sending anything.
WITH due AS (
  SELECT r.id FROM refunds r
  WHERE r.status = 'pending' AND r.gateway_refund_id IS NULL AND (r.attempts < @max_attempts::int OR r.submit_state <> 'queued')
    AND ((r.submit_state IN ('queued', 'uncertain') AND r.next_attempt_at <= now())
      OR (r.submit_state = 'submitting' AND r.submit_started_at < now() - interval '10 minutes'))
  ORDER BY r.created_at
  LIMIT @batch_size
  FOR UPDATE SKIP LOCKED)
UPDATE refunds r SET submit_state = 'submitting', submit_started_at = now(), attempts = r.attempts + 1
FROM due, payments p
WHERE r.id = due.id AND p.id = r.payment_id
RETURNING r.id, r.payment_id, r.booking_id, r.amount_paise, r.idempotency_key, r.attempts, p.gateway, p.gateway_payment_id;

-- name: MarkRefundSubmitted :exec
UPDATE refunds SET gateway_refund_id = @gateway_refund_id, submit_state = 'submitted', failure_reason = NULL, last_checked_at = now()
WHERE id = @id AND status = 'pending' AND gateway_refund_id IS NULL;

-- name: MarkRefundRetry :exec
-- A rejected (definite) or unknown (uncertain) gateway answer: try again later with backoff.
UPDATE refunds SET submit_state = CASE WHEN @uncertain::bool THEN 'uncertain' ELSE 'queued' END,
       failure_reason = @failure_reason, next_attempt_at = now() + make_interval(secs => @delay_seconds::float8)
WHERE id = @id AND status = 'pending' AND gateway_refund_id IS NULL;

-- name: SubmittedRefundsToCheck :many
-- Refunds the gateway accepted but has not confirmed (no webhook yet).
SELECT id, booking_id, gateway_refund_id FROM refunds
WHERE status = 'pending' AND gateway_refund_id IS NOT NULL
  AND COALESCE(last_checked_at, submit_started_at, created_at) < now() - interval '10 minutes'
ORDER BY created_at LIMIT @batch_size;

-- name: TouchRefundCheck :exec
UPDATE refunds SET last_checked_at = now() WHERE id = @id;

-- name: GetRefundLedgerInfo :one
SELECT r.amount_paise, r.booking_id, r.payment_id,
       EXISTS (SELECT 1 FROM security_deposits d WHERE d.refund_id = r.id) AS is_deposit
FROM refunds r WHERE r.id = @id;

-- name: MarkRefundProcessed :one
UPDATE refunds SET status = 'processed', processed_at = now(), gateway_refund_id = COALESCE(sqlc.narg(gateway_refund_id), gateway_refund_id)
WHERE id = @id AND status = 'pending'
RETURNING booking_id;

-- name: MarkRefundFailed :one
UPDATE refunds SET status = 'failed', failure_reason = @failure_reason
WHERE id = @id AND status = 'pending'
RETURNING booking_id;

-- name: GetRefundByGatewayID :one
SELECT id, booking_id, status::text AS status FROM refunds WHERE gateway_refund_id = @gateway_refund_id;

-- name: GetRefund :one
SELECT r.id, r.booking_id, r.status::text AS status, p.gateway FROM refunds r JOIN payments p ON p.id = r.payment_id WHERE r.id = @id;

-- name: FinishRefundedBookings :many
-- refund_pending bookings whose refunds all went through become refunded
-- (a failed refund keeps the booking open for finance).
UPDATE bookings b SET status = 'refunded'
WHERE b.id IN (
  SELECT x.id FROM bookings x
  WHERE x.status = 'refund_pending'
    AND NOT EXISTS (SELECT 1 FROM refunds r WHERE r.booking_id = x.id AND r.status IN ('pending', 'failed'))
  ORDER BY x.status_changed_at
  LIMIT @batch_size
  FOR UPDATE SKIP LOCKED)
RETURNING b.id, b.reference, b.customer_org_id, b.owner_org_id;

-- ---- worker sweeps -------------------------------------------------------------------------------

-- name: ExpireUnpaidBookings :many
-- Online bookings not paid by their deadline free the machine.
UPDATE bookings SET status = 'expired'
WHERE id IN (
  SELECT id FROM bookings
  WHERE status IN ('payment_pending', 'payment_processing') AND payment_due_at < now()
  ORDER BY payment_due_at
  LIMIT @batch_size
  FOR UPDATE SKIP LOCKED)
RETURNING id, reference, customer_org_id, owner_org_id;

-- name: ReleaseStaleProcessing :many
-- A checkout abandoned without a gateway answer returns to awaiting payment.
UPDATE bookings SET status = 'payment_pending'
WHERE id IN (
  SELECT x.id FROM bookings x
  WHERE x.status = 'payment_processing' AND x.status_changed_at < now() - make_interval(mins => @after_minutes::int)
  ORDER BY x.status_changed_at
  LIMIT @batch_size
  FOR UPDATE SKIP LOCKED)
RETURNING id;

-- name: LockEquipmentForBooking :one
-- Taken first by transactions that change which bookings hold a machine's
-- calendar, so they queue on the machine instead of deadlocking on each
-- other's booking rows.
SELECT id FROM equipment WHERE id = @id FOR UPDATE;

-- name: EndOpenTripsForBooking :exec
-- A cancelled booking's journey is over: close its open trip so the machine
-- can be dispatched again.
UPDATE trips
SET ended_at = greatest(now(), started_at), end_reason = COALESCE(end_reason, 'arrived'),
    distance_m = (SELECT COALESCE(ST_Length(ST_MakeLine(lp.location::geometry ORDER BY lp.recorded_at)::geography), 0)::int
                  FROM location_pings lp WHERE lp.trip_id = trips.id)
WHERE trips.booking_id = @booking_id AND trips.ended_at IS NULL;
