-- name: InsertIdempotencyKey :one
-- Claims a key. A conflict (existing key) returns no row.
INSERT INTO idempotency_keys (key, user_id, method, path, request_hash)
VALUES (@key, @user_id, @method, @path, @request_hash)
ON CONFLICT (user_id, key) DO NOTHING
RETURNING key;

-- name: GetIdempotencyKey :one
SELECT key, method, path, request_hash, response_status, response_body, expires_at
FROM idempotency_keys WHERE user_id = @user_id AND key = @key;

-- name: CompleteIdempotencyKey :exec
UPDATE idempotency_keys SET response_status = @response_status, response_body = @response_body
WHERE user_id = @user_id AND key = @key;

-- name: DeleteIdempotencyKey :exec
DELETE FROM idempotency_keys WHERE user_id = @user_id AND key = @key;

-- name: DeleteExpiredIdempotencyKeys :execrows
DELETE FROM idempotency_keys WHERE expires_at < now();

-- name: DeleteOldOTPChallenges :execrows
DELETE FROM otp_challenges WHERE created_at < now() - interval '7 days';

-- name: TryAdvisoryXactLock :one
-- Ensures a background job runs on one worker at a time.
SELECT pg_try_advisory_xact_lock(@lock_key::bigint) AS locked;

-- name: DeleteExpiredAuthTokens :execrows
-- Verification and reset tokens a day after they expired or were used.
DELETE FROM auth_tokens WHERE expires_at < now() - interval '1 day' OR consumed_at < now() - interval '1 day';
