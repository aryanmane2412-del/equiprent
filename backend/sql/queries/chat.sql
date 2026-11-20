-- name: GetBookingConversation :one
SELECT * FROM conversations WHERE booking_id = @booking_id AND kind = 'booking';

-- name: GetSupportConversationForUser :one
SELECT c.*
FROM conversations c
JOIN conversation_participants p ON p.conversation_id = c.id
WHERE c.kind = 'support' AND p.user_id = @user_id
ORDER BY c.updated_at DESC
LIMIT 1;

-- name: CreateSupportConversation :one
INSERT INTO conversations (kind, subject)
VALUES ('support', 'EQUIPRENT Customer Care')
RETURNING *;

-- name: ListSupportAgentIDs :many
SELECT DISTINCT u.id
FROM user_roles ur
JOIN users u ON u.id = ur.user_id
WHERE ur.role_code IN ('admin_support', 'admin_ops', 'admin_super')
  AND ur.organization_id IS NULL
  AND ur.revoked_at IS NULL
  AND u.deleted_at IS NULL
ORDER BY u.id;

-- name: CreateConversation :one
INSERT INTO conversations (kind, booking_id, subject)
VALUES (@kind, sqlc.narg(booking_id), sqlc.narg(subject))
RETURNING *;

-- name: AddConversationParticipant :exec
INSERT INTO conversation_participants (conversation_id, user_id, organization_id)
VALUES (@conversation_id, @user_id, sqlc.narg(organization_id))
ON CONFLICT (conversation_id, user_id) DO NOTHING;

-- name: IsConversationParticipant :one
SELECT EXISTS (
  SELECT 1 FROM conversation_participants WHERE conversation_id = @conversation_id AND user_id = @user_id
) AS ok;

-- name: GetConversation :one
SELECT * FROM conversations WHERE id = @id;

-- name: ListUserConversations :many
SELECT c.id, c.kind, c.booking_id, c.subject, c.updated_at,
       COALESCE((SELECT m.body FROM messages m WHERE m.conversation_id = c.id AND m.deleted_at IS NULL
         ORDER BY m.created_at DESC LIMIT 1), '')::text AS last_message,
       (SELECT count(*) FROM messages m WHERE m.conversation_id = c.id
         AND m.created_at > COALESCE(p.last_read_at, '-infinity'::timestamptz)
         AND m.sender_user_id IS DISTINCT FROM @user_id)::int AS unread
FROM conversation_participants p
JOIN conversations c ON c.id = p.conversation_id
WHERE p.user_id = @user_id
ORDER BY c.updated_at DESC
LIMIT @result_limit;

-- name: ListMessages :many
SELECT m.id, m.conversation_id, m.sender_user_id, m.body, m.attachment_object_key, m.created_at, m.edited_at, m.deleted_at,
       u.full_name AS sender_name
FROM messages m
LEFT JOIN users u ON u.id = m.sender_user_id
WHERE m.conversation_id = @conversation_id
  AND (sqlc.narg(before)::uuid IS NULL OR m.id < sqlc.narg(before))
ORDER BY m.id DESC
LIMIT @result_limit;

-- name: CreateMessage :one
-- client_id makes offline resends idempotent.
INSERT INTO messages (conversation_id, sender_user_id, body, attachment_object_key, client_id)
VALUES (@conversation_id, sqlc.narg(sender_user_id), sqlc.narg(body), sqlc.narg(attachment_object_key), sqlc.narg(client_id))
ON CONFLICT (client_id) DO UPDATE SET client_id = EXCLUDED.client_id
RETURNING id, conversation_id, sender_user_id, body, attachment_object_key, created_at, edited_at, deleted_at;

-- name: MarkConversationRead :exec
UPDATE conversation_participants SET last_read_at = now()
WHERE conversation_id = @conversation_id AND user_id = @user_id;
