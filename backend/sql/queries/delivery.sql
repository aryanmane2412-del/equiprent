-- Notification delivery (migration 000025): in-app copies deduplicated per
-- outbox event, the push/email/SMS delivery queue, per-channel preferences,
-- reminders, realtime fan-out and current conversation membership.

-- ---- in-app ----------------------------------------------------------------------------------

-- name: CreateEventNotifications :many
-- One in-app notification per user and event; replays insert nothing.
INSERT INTO notifications (user_id, type, title, body, data, channels, event_id)
SELECT u, @type, @title, @body, @data, @channels, sqlc.narg(event_id) FROM unnest(@user_ids::uuid[]) AS u
ON CONFLICT (event_id, user_id) WHERE event_id IS NOT NULL DO NOTHING
RETURNING id, user_id;

-- name: NotificationIDsForEvent :many
SELECT id, user_id FROM notifications WHERE event_id = @event_id AND user_id = ANY(@user_ids::uuid[]);

-- ---- recipients and preferences -----------------------------------------------------------------

-- name: RecipientContacts :many
-- Channel settings for one category (defaults: push on, email on, SMS off)
-- and verified contact details. Inactive users are left out.
SELECT u.id AS user_id, u.email, u.email_verified_at, u.phone, u.phone_verified_at,
       COALESCE(np.push, true)::bool AS push, COALESCE(np.email, true)::bool AS email_on, COALESCE(np.sms, false)::bool AS sms_on,
       EXISTS (SELECT 1 FROM devices d WHERE d.user_id = u.id) AS has_device
FROM users u
LEFT JOIN notification_preferences np ON np.user_id = u.id AND np.category = @category
WHERE u.id = ANY(@user_ids::uuid[]) AND u.status = 'active';

-- name: ListChannelPreferences :many
SELECT category, push, email, sms FROM notification_preferences WHERE user_id = @user_id ORDER BY category;

-- name: UpsertChannelPreference :exec
INSERT INTO notification_preferences (user_id, category, push, email, sms)
VALUES (@user_id, @category, @push, @email, @sms)
ON CONFLICT (user_id, category) DO UPDATE SET push = EXCLUDED.push, email = EXCLUDED.email, sms = EXCLUDED.sms;

-- ---- delivery queue -----------------------------------------------------------------------------

-- name: EnqueueDelivery :execrows
INSERT INTO notification_deliveries (user_id, notification_id, channel, dedupe_key, type, title, body, data, status, last_error)
VALUES (@user_id, sqlc.narg(notification_id), @channel, @dedupe_key, @type, @title, @body, @data, @status, sqlc.narg(last_error))
ON CONFLICT (user_id, channel, dedupe_key) DO NOTHING;

-- name: ClaimDeliveries :many
-- Due deliveries, plus ones a crashed worker left in 'sending'. The lock
-- lease stops two workers sending the same delivery.
UPDATE notification_deliveries d
SET status = 'sending', attempts = d.attempts + 1, locked_until = now() + make_interval(secs => @lease_seconds::int)
WHERE d.id IN (
  SELECT x.id FROM notification_deliveries x
  WHERE (x.status = 'pending' AND x.next_attempt_at <= now()) OR (x.status = 'sending' AND x.locked_until < now())
  ORDER BY x.next_attempt_at
  LIMIT @batch_size
  FOR UPDATE SKIP LOCKED)
RETURNING d.id, d.user_id, d.channel, d.dedupe_key, d.type, d.title, d.body, d.data, d.attempts;

-- name: FinishDelivery :exec
UPDATE notification_deliveries
SET status = @status, provider = sqlc.narg(provider), provider_ref = sqlc.narg(provider_ref), last_error = sqlc.narg(last_error),
    sent_at = CASE WHEN @status::text IN ('sent', 'simulated') THEN now() ELSE sent_at END, locked_until = NULL
WHERE id = @id;

-- name: RetryDelivery :exec
UPDATE notification_deliveries
SET status = 'pending', next_attempt_at = @next_attempt_at, last_error = @last_error, provider = sqlc.narg(provider), locked_until = NULL
WHERE id = @id;

-- name: UserContact :one
SELECT email, email_verified_at, phone, phone_verified_at, status FROM users WHERE id = @id;

-- name: ListDeliveries :many
SELECT d.id, d.user_id, d.channel, d.type, d.title, d.body, d.status, d.attempts, d.provider, d.provider_ref, d.last_error,
       d.sent_at, d.created_at, u.full_name AS user_name
FROM notification_deliveries d
JOIN users u ON u.id = d.user_id
WHERE (sqlc.narg(user_id)::uuid IS NULL OR d.user_id = sqlc.narg(user_id))
  AND (sqlc.narg(channel)::text IS NULL OR d.channel = sqlc.narg(channel))
  AND (sqlc.narg(status)::text IS NULL OR d.status = sqlc.narg(status))
  AND (sqlc.narg(after)::uuid IS NULL OR d.id < sqlc.narg(after))
ORDER BY d.id DESC
LIMIT @result_limit;

-- name: DeliveryStats :many
SELECT channel, status, count(*)::int AS n FROM notification_deliveries
WHERE created_at > now() - interval '7 days'
GROUP BY channel, status ORDER BY channel, status;

-- ---- reminders ----------------------------------------------------------------------------------

-- name: ClaimEndingSoonReminders :many
-- Running rentals ending within the window that were not reminded about
-- this end time yet (an extension moves the end and earns a new reminder).
WITH due AS (
  INSERT INTO booking_reminders (booking_id, kind, due_for)
  SELECT b.id, 'ending_soon', upper(b.period) FROM bookings b
  WHERE b.status IN ('active', 'extension_requested')
    AND upper(b.period) > now() AND upper(b.period) <= now() + make_interval(secs => @within_seconds::int)
  ON CONFLICT DO NOTHING
  RETURNING booking_id, due_for
)
SELECT b.id, b.reference, b.customer_org_id, b.owner_org_id, due.due_for
FROM due JOIN bookings b ON b.id = due.booking_id;

-- ---- realtime -----------------------------------------------------------------------------------

-- name: PublishRealtime :exec
-- Delivered to every API instance's listener when the transaction commits.
SELECT pg_notify('equiprent_realtime', @payload::text);

-- ---- conversations ------------------------------------------------------------------------------

-- name: CurrentConversationMembers :many
-- Participants who may still use the conversation.
SELECT p.user_id FROM conversation_participants p
WHERE p.conversation_id = @conversation_id AND conversation_member(@conversation_id, p.user_id);

-- name: ConversationReaders :many
-- Read positions of the current members (read receipts).
SELECT p.user_id, u.full_name, p.last_read_at FROM conversation_participants p
JOIN users u ON u.id = p.user_id
WHERE p.conversation_id = @conversation_id AND conversation_member(@conversation_id, p.user_id)
ORDER BY u.full_name;

-- name: MarkConversationReadAt :one
UPDATE conversation_participants SET last_read_at = GREATEST(COALESCE(last_read_at, '-infinity'::timestamptz), now())
WHERE conversation_id = @conversation_id AND user_id = @user_id
RETURNING last_read_at;

-- name: ListMemberConversations :many
-- The caller's conversations they may still use.
SELECT c.id, c.kind, c.booking_id, c.equipment_id, c.subject, c.updated_at,
       COALESCE((SELECT CASE WHEN m.attachment_upload_id IS NOT NULL AND m.body IS NULL THEN '[photo]' ELSE m.body END
                 FROM messages m WHERE m.conversation_id = c.id AND m.deleted_at IS NULL
                 ORDER BY m.id DESC LIMIT 1), '')::text AS last_message,
       (SELECT count(*) FROM messages m WHERE m.conversation_id = c.id AND m.deleted_at IS NULL
         AND m.created_at > COALESCE(p.last_read_at, '-infinity'::timestamptz)
         AND m.sender_user_id IS DISTINCT FROM @user_id)::int AS unread,
       COALESCE(b.reference, '')::text AS booking_reference, COALESCE(b.status::text, '')::text AS booking_status
FROM conversation_participants p
JOIN conversations c ON c.id = p.conversation_id
LEFT JOIN bookings b ON b.id = c.booking_id
WHERE p.user_id = @user_id AND conversation_member(c.id, @user_id)
ORDER BY c.updated_at DESC
LIMIT @result_limit;

-- name: CountMemberUnread :one
SELECT COALESCE(sum((SELECT count(*) FROM messages m WHERE m.conversation_id = p.conversation_id AND m.deleted_at IS NULL
         AND m.created_at > COALESCE(p.last_read_at, '-infinity'::timestamptz)
         AND m.sender_user_id IS DISTINCT FROM @user_id)), 0)::int AS unread
FROM conversation_participants p
WHERE p.user_id = @user_id AND conversation_member(p.conversation_id, @user_id);

-- name: NotificationBookingContext :one
-- Wording facts for booking notifications (never copied into the stored data payload).
SELECT e.title AS equipment_title,
       COALESCE(NULLIF(b.customer_details->>'company_name',''), NULLIF(b.customer_details->>'name',''), cu.display_name)::text AS customer_name,
       concat_ws(', ', sa.line1, sa.city, sa.pincode)::text AS site_address,
       lower(b.period)::timestamptz AS starts_at
FROM bookings b JOIN equipment e ON e.id = b.equipment_id
JOIN organizations cu ON cu.id = b.customer_org_id
LEFT JOIN addresses sa ON sa.id = b.site_address_id
WHERE b.id = @id;

-- name: BookingActiveOperators :many
SELECT operator_user_id FROM operator_assignments
WHERE booking_id = @booking_id AND status IN ('assigned', 'accepted');

-- name: PlatformUsersWithRoles :many
SELECT DISTINCT ur.user_id FROM user_roles ur
JOIN users u ON u.id = ur.user_id AND u.deleted_at IS NULL AND u.status = 'active'
WHERE ur.role_code = ANY(@roles::text[]) AND ur.revoked_at IS NULL AND ur.organization_id IS NULL;
