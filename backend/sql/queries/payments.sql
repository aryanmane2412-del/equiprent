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
SET status = 'captured', gateway_payment_id = @gateway_payment_id, method = sqlc.narg(method), captured_at = now()
WHERE id = @id AND status IN ('created', 'authorized')
RETURNING *;

-- name: FailPayment :one
UPDATE payments
SET status = 'failed', failure_code = sqlc.narg(failure_code), failure_reason = @failure_reason
WHERE id = @id AND status IN ('created', 'authorized')
RETURNING *;

-- name: SumCapturedPayments :one
SELECT COALESCE(sum(amount_paise), 0)::bigint AS total
FROM payments WHERE booking_id = @booking_id AND status IN ('captured', 'partially_refunded');

-- name: InsertWebhookEvent :one
-- A duplicate delivery (same provider + event id) returns no row.
INSERT INTO webhook_events (provider, event_id, event_type, payload, signature_valid)
VALUES (@provider, @event_id, @event_type, @payload, @signature_valid)
ON CONFLICT (provider, event_id) DO NOTHING
RETURNING id;

-- name: MarkWebhookProcessed :exec
UPDATE webhook_events SET processed_at = now(), error = sqlc.narg(error) WHERE id = @id;
