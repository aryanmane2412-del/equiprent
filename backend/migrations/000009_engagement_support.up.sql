-- Notifications, conversations & messages, reviews, disputes, support
-- tickets and the transactional outbox.

CREATE TABLE notification_templates (
  code            text NOT NULL CHECK (code ~ '^[a-z_.]+$'),
  channel         text NOT NULL CHECK (channel IN ('push', 'sms', 'whatsapp', 'email', 'in_app')),
  language        text NOT NULL DEFAULT 'en',
  title           text,
  body            text NOT NULL,
  dlt_template_id text,   -- TRAI DLT registration for SMS
  updated_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (code, channel, language),
  CHECK (channel <> 'sms' OR dlt_template_id IS NOT NULL)
);

CREATE TABLE notifications (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  type       text NOT NULL CHECK (type ~ '^[a-z_.]+$'),
  title      text NOT NULL,
  body       text NOT NULL,
  data       jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(data) = 'object'),
  channels   text[] NOT NULL DEFAULT '{in_app}' CHECK (channels <@ ARRAY['push', 'sms', 'whatsapp', 'email', 'in_app']),
  read_at    timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notifications_user_idx ON notifications (user_id, created_at DESC);
CREATE INDEX notifications_unread_idx ON notifications (user_id) WHERE read_at IS NULL;

-- Messages ---------------------------------------------------------------------------
CREATE TYPE conversation_kind AS ENUM ('booking', 'support', 'dispute', 'direct');

CREATE TABLE conversations (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  kind       conversation_kind NOT NULL,
  booking_id uuid REFERENCES bookings (id),
  subject    text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (kind <> 'booking' OR booking_id IS NOT NULL)
);
CREATE UNIQUE INDEX conversations_booking_uq ON conversations (booking_id) WHERE kind = 'booking';

CREATE TABLE conversation_participants (
  conversation_id uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
  user_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  organization_id uuid REFERENCES organizations (id),
  joined_at       timestamptz NOT NULL DEFAULT now(),
  last_read_at    timestamptz,
  PRIMARY KEY (conversation_id, user_id)
);
CREATE INDEX conversation_participants_user_idx ON conversation_participants (user_id);

CREATE TABLE messages (
  id                    uuid PRIMARY KEY DEFAULT uuidv7(),
  conversation_id       uuid NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
  sender_user_id        uuid REFERENCES users (id),   -- NULL = system message
  body                  text CHECK (body IS NULL OR length(body) BETWEEN 1 AND 4000),
  attachment_object_key text CHECK (attachment_object_key IS NULL OR is_object_key(attachment_object_key)),
  client_id             uuid UNIQUE,
  created_at            timestamptz NOT NULL DEFAULT now(),
  edited_at             timestamptz,
  deleted_at            timestamptz,   -- soft delete: shown as "message removed"
  CHECK (body IS NOT NULL OR attachment_object_key IS NOT NULL OR deleted_at IS NOT NULL)
);
CREATE INDEX messages_conversation_idx ON messages (conversation_id, created_at DESC);

-- Only participants may post (system messages exempt).
CREATE OR REPLACE FUNCTION messages_check_participant() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.sender_user_id IS NOT NULL AND NOT EXISTS (
       SELECT 1 FROM conversation_participants WHERE conversation_id = NEW.conversation_id AND user_id = NEW.sender_user_id) THEN
    RAISE EXCEPTION 'user % is not a participant of conversation %', NEW.sender_user_id, NEW.conversation_id USING ERRCODE = 'EQ004';
  END IF;
  UPDATE conversations SET updated_at = now() WHERE id = NEW.conversation_id;
  RETURN NEW;
END;
$$;
CREATE TRIGGER messages_check_participant BEFORE INSERT ON messages FOR EACH ROW EXECUTE FUNCTION messages_check_participant();

-- Reviews ----------------------------------------------------------------------------
CREATE TYPE review_subject AS ENUM ('equipment', 'owner', 'operator', 'customer');

CREATE TABLE reviews (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  booking_id       uuid NOT NULL REFERENCES bookings (id),
  reviewer_org_id  uuid NOT NULL REFERENCES organizations (id),
  reviewer_user_id uuid NOT NULL REFERENCES users (id),
  subject_type     review_subject NOT NULL,
  subject_id       uuid NOT NULL,
  rating           smallint NOT NULL CHECK (rating BETWEEN 1 AND 5),
  comment          text CHECK (comment IS NULL OR length(comment) <= 2000),
  status           text NOT NULL DEFAULT 'published' CHECK (status IN ('published', 'hidden', 'flagged')),
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  UNIQUE (booking_id, reviewer_org_id, subject_type)
);
CREATE INDEX reviews_subject_idx ON reviews (subject_type, subject_id, created_at DESC) WHERE status = 'published';
CREATE TRIGGER reviews_updated_at BEFORE UPDATE ON reviews FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Only parties of a finished booking may review it.
CREATE OR REPLACE FUNCTION reviews_check_party() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE b bookings%ROWTYPE;
BEGIN
  SELECT * INTO b FROM bookings WHERE id = NEW.booking_id;
  IF b.status NOT IN ('completed', 'closed') THEN
    RAISE EXCEPTION 'booking % is % and cannot be reviewed yet', b.reference, b.status USING ERRCODE = 'EQ004';
  END IF;
  IF NEW.reviewer_org_id NOT IN (b.owner_org_id, b.customer_org_id) THEN
    RAISE EXCEPTION 'reviewer is not a party to booking %', b.reference USING ERRCODE = 'EQ004';
  END IF;
  IF (NEW.subject_type = 'customer') <> (NEW.reviewer_org_id = b.owner_org_id) THEN
    RAISE EXCEPTION 'owners review customers; customers review equipment, owners and operators' USING ERRCODE = 'EQ004';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER reviews_check_party BEFORE INSERT ON reviews FOR EACH ROW EXECUTE FUNCTION reviews_check_party();

-- Disputes ---------------------------------------------------------------------------
CREATE TYPE dispute_category AS ENUM ('hour_meter', 'damage', 'payment', 'no_show', 'late_arrival', 'quality', 'other');
CREATE TYPE dispute_status AS ENUM ('open', 'under_review', 'resolved', 'rejected');

CREATE TABLE disputes (
  id                    uuid PRIMARY KEY DEFAULT uuidv7(),
  booking_id            uuid NOT NULL REFERENCES bookings (id),
  raised_by_org_id      uuid NOT NULL REFERENCES organizations (id),
  raised_by_user_id     uuid NOT NULL REFERENCES users (id),
  against_org_id        uuid NOT NULL REFERENCES organizations (id),
  category              dispute_category NOT NULL,
  summary               text NOT NULL,
  amount_claimed_paise  bigint NOT NULL DEFAULT 0 CHECK (amount_claimed_paise >= 0),
  resolved_amount_paise bigint CHECK (resolved_amount_paise >= 0),
  currency              char(3) NOT NULL DEFAULT 'INR' CHECK (currency = 'INR'),
  status                dispute_status NOT NULL DEFAULT 'open',
  resolution            text,
  assigned_admin_id     uuid REFERENCES users (id),
  conversation_id       uuid REFERENCES conversations (id),
  opened_at             timestamptz NOT NULL DEFAULT now(),
  resolved_at           timestamptz,
  updated_at            timestamptz NOT NULL DEFAULT now(),
  CHECK (raised_by_org_id <> against_org_id),
  CHECK ((status IN ('resolved', 'rejected')) = (resolved_at IS NOT NULL)),
  CHECK (status NOT IN ('resolved', 'rejected') OR resolution IS NOT NULL)
);
-- One open dispute per booking per category.
CREATE UNIQUE INDEX disputes_open_uq ON disputes (booking_id, category) WHERE status IN ('open', 'under_review');
CREATE INDEX disputes_queue_idx ON disputes (status, opened_at);
CREATE TRIGGER disputes_updated_at BEFORE UPDATE ON disputes FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER disputes_audit AFTER INSERT OR UPDATE ON disputes FOR EACH ROW EXECUTE FUNCTION audit_row_change();

-- Support tickets ----------------------------------------------------------------------
CREATE TYPE ticket_status AS ENUM ('open', 'in_progress', 'pending_customer', 'resolved', 'closed');
CREATE TYPE ticket_priority AS ENUM ('low', 'normal', 'high', 'urgent');

CREATE TABLE support_tickets (
  id                uuid PRIMARY KEY DEFAULT uuidv7(),
  reference         text NOT NULL UNIQUE CHECK (reference ~ '^ST-[0-9]{6}$'),
  requester_user_id uuid NOT NULL REFERENCES users (id),
  organization_id   uuid REFERENCES organizations (id),
  booking_id        uuid REFERENCES bookings (id),
  category          text NOT NULL CHECK (category IN ('booking', 'payment', 'account', 'kyc', 'listing', 'technical', 'other')),
  priority          ticket_priority NOT NULL DEFAULT 'normal',
  subject           text NOT NULL CHECK (length(subject) BETWEEN 3 AND 200),
  description       text NOT NULL,
  status            ticket_status NOT NULL DEFAULT 'open',
  assigned_to       uuid REFERENCES users (id),
  conversation_id   uuid REFERENCES conversations (id),
  first_response_at timestamptz,
  resolved_at       timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  CHECK (status NOT IN ('resolved', 'closed') OR resolved_at IS NOT NULL)
);
CREATE SEQUENCE support_ticket_number_seq START 100001;
ALTER TABLE support_tickets ALTER COLUMN reference SET DEFAULT 'ST-' || nextval('support_ticket_number_seq')::text;
CREATE INDEX support_tickets_queue_idx ON support_tickets (status, priority, created_at) WHERE status NOT IN ('resolved', 'closed');
CREATE INDEX support_tickets_requester_idx ON support_tickets (requester_user_id, created_at DESC);
CREATE TRIGGER support_tickets_updated_at BEFORE UPDATE ON support_tickets FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Transactional outbox: written in the same transaction as the change, then
-- published to the job queue by a worker (at-least-once delivery).
CREATE TABLE outbox_events (
  id             uuid PRIMARY KEY DEFAULT uuidv7(),
  aggregate_type text NOT NULL,
  aggregate_id   uuid NOT NULL,
  event_type     text NOT NULL CHECK (event_type ~ '^[a-z_]+\.[a-z_]+$'),
  payload        jsonb NOT NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  published_at   timestamptz
);
CREATE INDEX outbox_events_unpublished_idx ON outbox_events (created_at) WHERE published_at IS NULL;
