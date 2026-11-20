DROP FUNCTION IF EXISTS conversation_member(uuid, uuid);
DROP TABLE IF EXISTS booking_reminders;
DROP TABLE IF EXISTS notification_deliveries;
DROP INDEX IF EXISTS notifications_event_user_uq;
ALTER TABLE notifications DROP COLUMN IF EXISTS event_id;
ALTER TABLE notification_preferences DROP COLUMN IF EXISTS sms, DROP COLUMN IF EXISTS email;
