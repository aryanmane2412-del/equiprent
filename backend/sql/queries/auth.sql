-- name: CreateOTPChallenge :one
INSERT INTO otp_challenges (phone, code_hash, purpose, expires_at, device_id)
VALUES (@phone, @code_hash, @purpose, @expires_at, sqlc.narg(device_id))
RETURNING id, expires_at;

-- name: GetLatestOTPChallenge :one
SELECT id, code_hash, attempts, expires_at
FROM otp_challenges
WHERE phone = @phone AND purpose = @purpose AND consumed_at IS NULL AND expires_at > now()
ORDER BY created_at DESC
LIMIT 1;

-- name: IncrementOTPAttempts :one
UPDATE otp_challenges SET attempts = attempts + 1
WHERE id = @id AND attempts < 5
RETURNING attempts;

-- name: ConsumeOTPChallenge :execrows
UPDATE otp_challenges SET consumed_at = now()
WHERE id = @id AND consumed_at IS NULL;

-- name: MarkUserLoggedIn :exec
UPDATE users SET last_login_at = now() WHERE id = @id;

-- name: CreateSession :one
INSERT INTO sessions (user_id, family_id, refresh_token_hash, device_id, platform, user_agent, expires_at)
VALUES (@user_id, @family_id, @refresh_token_hash, sqlc.narg(device_id), sqlc.narg(platform), sqlc.narg(user_agent), @expires_at)
RETURNING id, expires_at;

-- name: GetSessionByRefreshHash :one
SELECT * FROM sessions WHERE refresh_token_hash = @refresh_token_hash;

-- name: RevokeSession :execrows
UPDATE sessions SET revoked_at = now() WHERE id = @id AND revoked_at IS NULL;

-- name: RevokeUserSession :execrows
UPDATE sessions SET revoked_at = now()
WHERE id = @id AND user_id = @user_id AND revoked_at IS NULL;

-- name: RevokeSessionFamily :execrows
UPDATE sessions SET revoked_at = now() WHERE family_id = @family_id AND revoked_at IS NULL;

-- name: RevokeAllUserSessions :execrows
UPDATE sessions SET revoked_at = now() WHERE user_id = @user_id AND revoked_at IS NULL;

-- name: TouchSession :exec
UPDATE sessions SET last_used_at = now() WHERE id = @id;

-- name: ListUserSessions :many
SELECT id, device_id, platform, user_agent, created_at, last_used_at, expires_at
FROM sessions
WHERE user_id = @user_id AND revoked_at IS NULL AND expires_at > now()
ORDER BY created_at DESC;

-- name: GetSessionPrincipal :one
-- Validates an access token's session on every authenticated request.
SELECT s.user_id, u.status AS user_status, s.revoked_at, s.expires_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.id = @session_id;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = @email::citext AND deleted_at IS NULL;

-- name: CreatePasswordUser :one
INSERT INTO users (phone, email, full_name, password_hash, password_changed_at, preferred_language)
VALUES (@phone, @email::citext, @full_name, @password_hash, now(), 'en')
RETURNING *;

-- name: SetPasswordHash :exec
UPDATE users SET password_hash = @password_hash, password_changed_at = now() WHERE id = @id;

-- name: MarkPhoneVerified :exec
UPDATE users SET phone_verified_at = COALESCE(phone_verified_at, now()) WHERE id = @id;

-- name: MarkEmailVerified :execrows
-- Only if the address is still the one the token was sent to.
UPDATE users SET email_verified_at = now()
WHERE id = @id AND email = @email::citext AND deleted_at IS NULL;

-- name: CreateAuthToken :exec
INSERT INTO auth_tokens (user_id, purpose, token_hash, email, expires_at)
VALUES (@user_id, @purpose, @token_hash, sqlc.narg(email)::citext, @expires_at);

-- name: ConsumeAuthToken :one
UPDATE auth_tokens SET consumed_at = now()
WHERE token_hash = @token_hash AND purpose = @purpose AND consumed_at IS NULL AND expires_at > now()
RETURNING user_id, COALESCE(email::text, '')::text AS email;

-- name: InvalidateAuthTokens :exec
UPDATE auth_tokens SET consumed_at = now() WHERE user_id = @user_id AND purpose = @purpose AND consumed_at IS NULL;

-- name: RevokeOtherUserSessions :execrows
UPDATE sessions SET revoked_at = now() WHERE user_id = @user_id AND id <> @keep_session_id AND revoked_at IS NULL;

-- name: InsertAuthAudit :exec
INSERT INTO audit_logs (actor_user_id, actor_role, action, entity_type, entity_id, reason, request_id, ip, after)
VALUES (sqlc.narg(actor_user_id), 'user', @action, 'users', sqlc.narg(entity_id), sqlc.narg(reason),
        sqlc.narg(request_id), sqlc.narg(ip)::inet, sqlc.narg(after));
-- name: InsertEquipmentInspectionPolicyConsent :one
INSERT INTO equipment_inspection_policy_consents (user_id, policy_version)
VALUES (@user_id, @policy_version)
RETURNING id, user_id, policy_version, accepted_at;


-- name: VerifyAuthToken :one
-- One atomic step for the newest live token of this address: a wrong code costs an attempt
-- (the token is burned at 5), the right code consumes it. Expired, consumed or exhausted
-- tokens never match, so a code cannot be replayed or guessed without limit.
WITH cand AS (
  SELECT auth_tokens.id, (auth_tokens.token_hash = @token_hash::bytea) AS ok FROM auth_tokens
  WHERE auth_tokens.purpose = @purpose AND auth_tokens.email = @email::citext AND auth_tokens.consumed_at IS NULL
    AND auth_tokens.expires_at > now() AND auth_tokens.attempts < 5
  ORDER BY auth_tokens.created_at DESC LIMIT 1
  FOR UPDATE)
UPDATE auth_tokens t SET attempts = t.attempts + (CASE WHEN c.ok THEN 0 ELSE 1 END),
       consumed_at = CASE WHEN c.ok OR t.attempts + 1 >= 5 THEN now() END
FROM cand c WHERE t.id = c.id
RETURNING t.user_id, c.ok;
