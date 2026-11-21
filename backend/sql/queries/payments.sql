-- name: CreatePayment :one
INSERT INTO payments (booking_id, payer_org_id, purpose, amount_paise, gateway, gateway_order_id, idempotency_key)
VALUES (@booking_id, @payer_org_id, @purpose, @amount_paise, @gateway, sqlc.narg(gateway_order_id), @idempotency_key)
RETURNING *;

-- name: GetPayment :one
SELECT * FROM payments WHERE id = @id;

-- name: GetPaymentByIdempotencyKey :one
SELECT * FROM payments WHERE idempotency_key = @idempotency_key;

-- name: GetPaymentByOrderID :one
SELECT * FROM payments WHERE gateway_order_id = @gateway_order_id;

-- name: ListBookingPayments :many
SELECT * FROM payments WHERE booking_id = @booking_id ORDER BY created_at;

-- name: CapturePayment :one
UPDATE payments
SET status = 'captured', gateway_payment_id = @gateway_payment_id, method = sqlc.narg(method), captured_at = now(),
    failure_code = NULL, failure_reason = NULL
WHERE id = @id AND status IN ('created', 'authorized', 'failed')
RETURNING *;

-- name: FailPayment :one
UPDATE payments
SET status = 'failed', failure_code = sqlc.narg(failure_code), failure_reason = @failure_reason
WHERE id = @id AND status IN ('created', 'authorized')
RETURNING *;

-- name: SumCapturedPayments :one
SELECT COALESCE(sum(amount_paise), 0)::bigint AS total
FROM payments WHERE booking_id = @booking_id AND status IN ('captured', 'partially_refunded');

-- name: GetOpenBookingPayment :one
SELECT * FROM payments
WHERE booking_id = @booking_id AND purpose = @purpose AND status IN ('created', 'authorized')
ORDER BY created_at DESC LIMIT 1;

-- name: CountOtherCapturedPayments :one
SELECT count(*)::int FROM payments
WHERE booking_id = @booking_id AND purpose = @purpose AND id <> @exclude_id AND status IN ('captured', 'partially_refunded');

-- name: LockPaymentKey :exec
SELECT pg_advisory_xact_lock(hashtextextended(@key::text, 0));

-- name: ClaimWebhookEvent :one
-- New deliveries start processing; a redelivery re-claims only a failed, received or
-- stale-processing event. A processed or in-flight duplicate returns no row.
INSERT INTO webhook_events (provider, event_id, event_type, payload, signature_valid, status, attempts, locked_at)
VALUES (@provider, @event_id, @event_type, @payload, @signature_valid, 'processing', 1, now())
ON CONFLICT (provider, event_id) DO UPDATE
SET status = 'processing', attempts = webhook_events.attempts + 1, locked_at = now(), error = NULL
WHERE webhook_events.status IN ('received', 'failed')
   OR (webhook_events.status = 'processing' AND webhook_events.locked_at < now() - interval '5 minutes')
RETURNING id;

-- name: MarkWebhookProcessed :exec
UPDATE webhook_events SET status = 'processed', processed_at = now(), error = NULL, locked_at = NULL WHERE id = @id;

-- name: MarkWebhookFailed :exec
UPDATE webhook_events SET status = 'failed', error = @error, locked_at = NULL WHERE id = @id;

-- name: ListRetryableWebhooks :many
SELECT id, provider, payload, attempts FROM webhook_events
WHERE signature_valid AND attempts < @max_attempts
  AND (status IN ('received', 'failed') OR (status = 'processing' AND locked_at < now() - interval '5 minutes'))
  AND received_at < now() - interval '1 minute'
ORDER BY received_at LIMIT @batch;

-- name: ClaimWebhookByID :one
UPDATE webhook_events SET status = 'processing', attempts = attempts + 1, locked_at = now(), error = NULL
WHERE id = @id AND (status IN ('received', 'failed') OR (status = 'processing' AND locked_at < now() - interval '5 minutes'))
RETURNING payload;
