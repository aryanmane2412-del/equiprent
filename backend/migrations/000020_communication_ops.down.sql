DROP TABLE IF EXISTS review_reports;
DROP INDEX IF EXISTS reviews_booking_idx;
DROP INDEX IF EXISTS reviews_moderation_idx;
UPDATE reviews SET status = 'hidden' WHERE status = 'pending';
ALTER TABLE reviews
  DROP CONSTRAINT IF EXISTS reviews_reply_ck,
  DROP COLUMN IF EXISTS replied_at,
  DROP COLUMN IF EXISTS reply_by,
  DROP COLUMN IF EXISTS reply,
  DROP COLUMN IF EXISTS moderation_note,
  DROP COLUMN IF EXISTS moderated_at,
  DROP COLUMN IF EXISTS moderated_by,
  DROP COLUMN IF EXISTS report_count,
  DROP COLUMN IF EXISTS screening_flags,
  DROP CONSTRAINT IF EXISTS reviews_status_check;
ALTER TABLE reviews ADD CONSTRAINT reviews_status_check CHECK (status IN ('published', 'hidden', 'flagged'));

ALTER TABLE maintenance_plans DROP COLUMN IF EXISTS last_reminded_at;
DROP INDEX IF EXISTS maintenance_records_reminder_idx;
DROP INDEX IF EXISTS maintenance_records_kind_idx;
ALTER TABLE maintenance_records
  DROP CONSTRAINT IF EXISTS maintenance_records_cost_split_ck,
  DROP COLUMN IF EXISTS overdue_notified_at,
  DROP COLUMN IF EXISTS reminded_at,
  DROP COLUMN IF EXISTS invoice_upload_id,
  DROP COLUMN IF EXISTS availability_block_id,
  DROP COLUMN IF EXISTS downtime,
  DROP COLUMN IF EXISTS labour_cost_paise,
  DROP COLUMN IF EXISTS parts_cost_paise,
  DROP COLUMN IF EXISTS kind;
DROP TYPE IF EXISTS maintenance_kind;

DROP INDEX IF EXISTS conversations_enquiry_uq;
DELETE FROM conversations WHERE equipment_id IS NOT NULL;
ALTER TABLE conversations
  DROP CONSTRAINT IF EXISTS conversations_enquiry_ck,
  DROP COLUMN IF EXISTS owner_org_id,
  DROP COLUMN IF EXISTS customer_org_id,
  DROP COLUMN IF EXISTS equipment_id;

DROP INDEX IF EXISTS messages_conversation_id_idx;
DROP INDEX IF EXISTS messages_sender_client_uq;
-- Restoring global uniqueness drops duplicate client ids first.
UPDATE messages m SET client_id = NULL
WHERE client_id IS NOT NULL AND EXISTS (
  SELECT 1 FROM messages o WHERE o.client_id = m.client_id AND o.id < m.id);
ALTER TABLE messages ADD CONSTRAINT messages_client_id_key UNIQUE (client_id);
ALTER TABLE messages DROP COLUMN IF EXISTS attachment_upload_id;

DROP TABLE IF EXISTS agreement_documents;
DROP TABLE IF EXISTS agreement_signing_sessions;
DROP TABLE IF EXISTS agreement_signatures;
DROP FUNCTION IF EXISTS agreement_signatures_guard();

CREATE OR REPLACE FUNCTION rental_agreements_freeze() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (OLD.customer_accepted_at IS NOT NULL AND OLD.owner_accepted_at IS NOT NULL) THEN
    RAISE EXCEPTION 'rental agreement % is signed and immutable', OLD.id USING ERRCODE = 'EQ001';
  END IF;
  IF NEW.terms_sha256 <> OLD.terms_sha256 OR NEW.booking_id <> OLD.booking_id THEN
    RAISE EXCEPTION 'agreement terms cannot change after creation' USING ERRCODE = 'EQ001';
  END IF;
  RETURN NEW;
END;
$$;
ALTER TABLE rental_agreements DROP COLUMN IF EXISTS snapshot;

ALTER TABLE devices DROP COLUMN IF EXISTS last_error, DROP COLUMN IF EXISTS last_push_at;
DROP TABLE IF EXISTS notification_preferences;

DELETE FROM uploads WHERE purpose IN ('chat_image', 'maintenance_document', 'agreement_document');
ALTER TABLE uploads DROP CONSTRAINT IF EXISTS uploads_purpose_check;
ALTER TABLE uploads ADD CONSTRAINT uploads_purpose_check
  CHECK (purpose IN ('identity_document', 'business_document', 'operator_document', 'avatar', 'equipment_photo', 'equipment_document'));
