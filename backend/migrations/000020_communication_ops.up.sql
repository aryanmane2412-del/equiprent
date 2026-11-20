-- Communication and operations:
--   * push delivery bookkeeping and per-category notification preferences
--   * rental agreements: rendering snapshot, signatures (built-in typed-name
--     e-signature or an external e-signature provider), stored PDF documents
--   * chat: image attachments by upload, per-sender client ids, listing enquiries
--   * maintenance: kinds (service / repair / inspection / breakdown), cost split,
--     downtime that blocks the calendar, invoices, reminder bookkeeping
--   * reviews: pre-publication screening, reports, moderation, owner replies

-- ---- uploads: new purposes ------------------------------------------------------------
ALTER TABLE uploads DROP CONSTRAINT IF EXISTS uploads_purpose_check;
ALTER TABLE uploads ADD CONSTRAINT uploads_purpose_check
  CHECK (purpose IN ('identity_document', 'business_document', 'operator_document', 'avatar', 'equipment_photo',
                     'equipment_document', 'chat_image', 'maintenance_document', 'agreement_document'));

-- ---- notifications ----------------------------------------------------------------------
-- Push can be switched off per category; in-app notifications are always kept.
CREATE TABLE notification_preferences (
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  category   text NOT NULL CHECK (category IN ('bookings', 'payments', 'agreements', 'chat', 'maintenance', 'reviews', 'account')),
  push       boolean NOT NULL DEFAULT true,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, category)
);
CREATE TRIGGER notification_preferences_updated_at BEFORE UPDATE ON notification_preferences
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE devices
  ADD COLUMN last_push_at timestamptz,
  ADD COLUMN last_error   text CHECK (last_error IS NULL OR length(last_error) <= 300);

-- ---- rental agreements -----------------------------------------------------------------------
-- The data the agreement was rendered from (template rental-v2 onwards), so
-- the wording never shifts when an organisation later renames itself.
ALTER TABLE rental_agreements
  ADD COLUMN snapshot jsonb CHECK (snapshot IS NULL OR jsonb_typeof(snapshot) = 'object');

CREATE OR REPLACE FUNCTION rental_agreements_freeze() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (OLD.customer_accepted_at IS NOT NULL AND OLD.owner_accepted_at IS NOT NULL) THEN
    RAISE EXCEPTION 'rental agreement % is signed and immutable', OLD.id USING ERRCODE = 'EQ001';
  END IF;
  IF NEW.terms_sha256 <> OLD.terms_sha256 OR NEW.booking_id <> OLD.booking_id
     OR NEW.template_version <> OLD.template_version OR NEW.snapshot IS DISTINCT FROM OLD.snapshot THEN
    RAISE EXCEPTION 'agreement terms cannot change after creation' USING ERRCODE = 'EQ001';
  END IF;
  RETURN NEW;
END;
$$;

-- One signature per party, bound to the exact text (hash) it was given for.
CREATE TABLE agreement_signatures (
  id                 uuid PRIMARY KEY DEFAULT uuidv7(),
  agreement_id       uuid NOT NULL REFERENCES rental_agreements (id),
  party              text NOT NULL CHECK (party IN ('customer', 'owner')),
  signer_user_id     uuid NOT NULL REFERENCES users (id),
  signer_name        text NOT NULL CHECK (length(btrim(signer_name)) BETWEEN 2 AND 120),
  signer_designation text CHECK (signer_designation IS NULL OR length(signer_designation) <= 80),
  method             text NOT NULL CHECK (method IN ('typed_name', 'provider')),
  provider           text NOT NULL DEFAULT 'equiprent' CHECK (provider ~ '^[a-z0-9_]{2,30}$'),
  provider_reference text CHECK (provider_reference IS NULL OR length(provider_reference) <= 200),
  terms_sha256       bytea NOT NULL CHECK (length(terms_sha256) = 32),
  ip                 inet,
  user_agent         text CHECK (user_agent IS NULL OR length(user_agent) <= 512),
  signed_at          timestamptz NOT NULL DEFAULT now(),
  UNIQUE (agreement_id, party),
  CHECK ((method = 'provider') = (provider <> 'equiprent'))
);

CREATE OR REPLACE FUNCTION agreement_signatures_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE v_hash bytea;
BEGIN
  IF TG_OP <> 'INSERT' THEN
    RAISE EXCEPTION 'agreement signatures are append-only' USING ERRCODE = 'EQ001';
  END IF;
  SELECT terms_sha256 INTO v_hash FROM rental_agreements WHERE id = NEW.agreement_id;
  IF v_hash IS DISTINCT FROM NEW.terms_sha256 THEN
    RAISE EXCEPTION 'the signature does not match the agreement text' USING ERRCODE = 'EQ004';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER agreement_signatures_guard BEFORE INSERT OR UPDATE OR DELETE ON agreement_signatures
  FOR EACH ROW EXECUTE FUNCTION agreement_signatures_guard();
CREATE TRIGGER agreement_signatures_audit AFTER INSERT ON agreement_signatures
  FOR EACH ROW EXECUTE FUNCTION audit_row_change();

-- Signing sessions at an external e-signature provider (redirect + webhook).
CREATE TABLE agreement_signing_sessions (
  id                 uuid PRIMARY KEY DEFAULT uuidv7(),
  agreement_id       uuid NOT NULL REFERENCES rental_agreements (id),
  party              text NOT NULL CHECK (party IN ('customer', 'owner')),
  signer_user_id     uuid NOT NULL REFERENCES users (id),
  signer_name        text NOT NULL,
  provider           text NOT NULL CHECK (provider ~ '^[a-z0-9_]{2,30}$'),
  provider_reference text NOT NULL,
  status             text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'completed', 'declined', 'expired')),
  terms_sha256       bytea NOT NULL CHECK (length(terms_sha256) = 32),
  created_at         timestamptz NOT NULL DEFAULT now(),
  completed_at       timestamptz,
  UNIQUE (provider, provider_reference)
);
CREATE INDEX agreement_signing_sessions_agreement_idx ON agreement_signing_sessions (agreement_id, party);

-- Generated PDFs, stored privately and served only through signed links.
CREATE TABLE agreement_documents (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  agreement_id uuid NOT NULL REFERENCES rental_agreements (id),
  kind         text NOT NULL CHECK (kind IN ('draft', 'executed', 'provider_signed')),
  upload_id    uuid NOT NULL REFERENCES uploads (id),
  sha256       bytea NOT NULL CHECK (length(sha256) = 32),
  created_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (agreement_id, kind)
);

-- ---- chat -----------------------------------------------------------------------------
ALTER TABLE messages ADD COLUMN attachment_upload_id uuid REFERENCES uploads (id);
-- client_id was globally unique, so a resend could return someone else's
-- message; it is now unique per sender.
ALTER TABLE messages DROP CONSTRAINT messages_client_id_key;
CREATE UNIQUE INDEX messages_sender_client_uq ON messages (sender_user_id, client_id) WHERE client_id IS NOT NULL;
CREATE INDEX messages_conversation_id_idx ON messages (conversation_id, id);

-- Listing enquiries: a direct conversation between a customer organisation
-- and the machine's owner before any booking exists.
ALTER TABLE conversations
  ADD COLUMN equipment_id    uuid REFERENCES equipment (id),
  ADD COLUMN customer_org_id uuid REFERENCES organizations (id),
  ADD COLUMN owner_org_id    uuid REFERENCES organizations (id),
  ADD CONSTRAINT conversations_enquiry_ck CHECK (
    equipment_id IS NULL OR (kind = 'direct' AND customer_org_id IS NOT NULL AND owner_org_id IS NOT NULL
                             AND customer_org_id <> owner_org_id));
CREATE UNIQUE INDEX conversations_enquiry_uq ON conversations (equipment_id, customer_org_id) WHERE equipment_id IS NOT NULL;

-- ---- maintenance ----------------------------------------------------------------------------
CREATE TYPE maintenance_kind AS ENUM ('service', 'repair', 'inspection', 'breakdown');

ALTER TABLE maintenance_records
  ADD COLUMN kind                  maintenance_kind NOT NULL DEFAULT 'service',
  ADD COLUMN parts_cost_paise      bigint CHECK (parts_cost_paise >= 0),
  ADD COLUMN labour_cost_paise     bigint CHECK (labour_cost_paise >= 0),
  -- While the machine is out of service; mirrored by an availability block.
  ADD COLUMN downtime              tstzrange CHECK (downtime IS NULL OR (NOT isempty(downtime) AND NOT lower_inf(downtime) AND NOT upper_inf(downtime))),
  ADD COLUMN availability_block_id uuid REFERENCES availability_blocks (id) ON DELETE SET NULL,
  ADD COLUMN invoice_upload_id     uuid REFERENCES uploads (id),
  ADD COLUMN reminded_at           timestamptz,
  ADD COLUMN overdue_notified_at   timestamptz,
  ADD CONSTRAINT maintenance_records_cost_split_ck CHECK (
    cost_paise IS NULL OR parts_cost_paise IS NULL OR labour_cost_paise IS NULL
    OR cost_paise >= parts_cost_paise + labour_cost_paise);
CREATE INDEX maintenance_records_kind_idx ON maintenance_records (equipment_id, kind, performed_at DESC);
CREATE INDEX maintenance_records_reminder_idx ON maintenance_records (scheduled_for)
  WHERE status IN ('scheduled', 'in_progress');

ALTER TABLE maintenance_plans ADD COLUMN last_reminded_at timestamptz;

-- ---- reviews ----------------------------------------------------------------------------------
-- pending: held by automatic screening; flagged: reported by enough users and
-- hidden until a moderator decides; hidden: removed by a moderator.
ALTER TABLE reviews DROP CONSTRAINT reviews_status_check;
ALTER TABLE reviews
  ADD CONSTRAINT reviews_status_check CHECK (status IN ('pending', 'published', 'hidden', 'flagged')),
  ADD COLUMN screening_flags text[] NOT NULL DEFAULT '{}',
  ADD COLUMN report_count    integer NOT NULL DEFAULT 0 CHECK (report_count >= 0),
  ADD COLUMN moderated_by    uuid REFERENCES users (id),
  ADD COLUMN moderated_at    timestamptz,
  ADD COLUMN moderation_note text CHECK (moderation_note IS NULL OR length(moderation_note) <= 500),
  ADD COLUMN reply           text CHECK (reply IS NULL OR length(btrim(reply)) BETWEEN 1 AND 1000),
  ADD COLUMN reply_by        uuid REFERENCES users (id),
  ADD COLUMN replied_at      timestamptz,
  ADD CONSTRAINT reviews_reply_ck CHECK ((reply IS NULL) = (replied_at IS NULL));
CREATE INDEX reviews_moderation_idx ON reviews (status, created_at) WHERE status IN ('pending', 'flagged');
CREATE INDEX reviews_booking_idx ON reviews (booking_id);

CREATE TABLE review_reports (
  review_id        uuid NOT NULL REFERENCES reviews (id) ON DELETE CASCADE,
  reporter_user_id uuid NOT NULL REFERENCES users (id),
  reason           text NOT NULL CHECK (reason IN ('spam', 'abusive', 'false_information', 'personal_information', 'irrelevant', 'other')),
  note             text CHECK (note IS NULL OR length(note) <= 500),
  created_at       timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (review_id, reporter_user_id)
);
