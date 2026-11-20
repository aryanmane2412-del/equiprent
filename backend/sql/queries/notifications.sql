-- name: CreateNotification :one
INSERT INTO notifications (user_id, type, title, body, data, channels)
VALUES (@user_id, @type, @title, @body, @data, @channels)
RETURNING *;

-- name: ListNotifications :many
SELECT * FROM notifications
WHERE user_id = @user_id
  AND (NOT @unread_only::bool OR read_at IS NULL)
  AND (sqlc.narg(after)::uuid IS NULL OR id < sqlc.narg(after))
ORDER BY id DESC
LIMIT @result_limit;

-- name: CountUnreadNotifications :one
SELECT count(*)::int FROM notifications WHERE user_id = @user_id AND read_at IS NULL;

-- name: MarkNotificationRead :execrows
UPDATE notifications SET read_at = now() WHERE id = @id AND user_id = @user_id AND read_at IS NULL;

-- name: MarkAllNotificationsRead :execrows
UPDATE notifications SET read_at = now() WHERE user_id = @user_id AND read_at IS NULL;

-- name: UpsertDevice :one
INSERT INTO devices (user_id, platform, push_token, app_version)
VALUES (@user_id, @platform, @push_token, sqlc.narg(app_version))
ON CONFLICT (push_token) DO UPDATE
SET user_id = EXCLUDED.user_id, platform = EXCLUDED.platform, app_version = EXCLUDED.app_version, last_seen_at = now()
RETURNING id;

-- name: DeleteDevice :execrows
DELETE FROM devices WHERE push_token = @push_token AND user_id = @user_id;

-- name: ListUserPushTokens :many
SELECT platform, push_token FROM devices WHERE user_id = @user_id;
