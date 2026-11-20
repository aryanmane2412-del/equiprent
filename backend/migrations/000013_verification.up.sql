-- User verification: private uploads, verification applications with an
-- append-only review history, owner contact details, operator availability
-- and qualifications, and fleet-manager equipment grants.

-- More document kinds. (Aadhaar is never stored as a document: the Aadhaar
-- Act allows only a provider reference and the last four digits.)
ALTER TYPE kyc_document_type ADD VALUE IF NOT EXISTS 'passport';
ALTER TYPE kyc_document_type ADD VALUE IF NOT EXISTS 'voter_id';
ALTER TYPE kyc_document_type ADD VALUE IF NOT EXISTS 'address_proof';
ALTER TYPE kyc_document_type ADD VALUE IF NOT EXISTS 'gst_certificate';
ALTER TYPE kyc_document_type ADD VALUE IF NOT EXISTS 'business_registration';
ALTER TYPE kyc_document_type ADD VALUE IF NOT EXISTS 'operator_certificate';

-- Files uploaded to the PRIVATE store. Clients only ever hold an upload id;
-- reads go through short-lived signed URLs issued after an access check.
CREATE TABLE uploads (
  id            uuid PRIMARY KEY DEFAULT uuidv7(),
  user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  purpose       text NOT NULL CHECK (purpose IN ('identity_document', 'business_document', 'operator_document', 'avatar')),
  object_key    text NOT NULL UNIQUE CHECK (is_object_key(object_key)),
  content_type  text NOT NULL CHECK (content_type IN ('application/pdf', 'image/jpeg', 'image/png', 'image/webp')),
  size_bytes    bigint NOT NULL CHECK (size_bytes > 0 AND size_bytes <= 10485760),
  sha256        bytea NOT NULL CHECK (length(sha256) = 32),
  original_name text CHECK (original_name IS NULL OR length(original_name) <= 255),
  attached_at   timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX uploads_user_idx ON uploads (user_id, created_at DESC);
CREATE INDEX uploads_unattached_idx ON uploads (created_at) WHERE attached_at IS NULL;

ALTER TABLE kyc_records
  ADD COLUMN upload_id    uuid REFERENCES uploads (id),
  ADD COLUMN content_type text,
  ADD COLUMN context      text CHECK (context IS NULL OR context IN ('owner', 'operator', 'customer'));
CREATE TRIGGER kyc_records_audit AFTER INSERT OR UPDATE OR DELETE ON kyc_records FOR EACH ROW EXECUTE FUNCTION audit_row_change();

-- Verification applications: one per subject and role, re-submitted after a
-- rejection (the event history keeps every round).
CREATE TYPE application_status AS ENUM ('draft', 'submitted', 'under_review', 'approved', 'rejected');

CREATE TABLE verification_applications (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  role             text NOT NULL CHECK (role IN ('owner', 'operator')),
  user_id          uuid REFERENCES users (id) ON DELETE CASCADE,
  organization_id  uuid REFERENCES organizations (id) ON DELETE CASCADE,
  status           application_status NOT NULL DEFAULT 'draft',
  submitted_by     uuid REFERENCES users (id),
  submitted_at     timestamptz,
  reviewer_id      uuid REFERENCES users (id),
  reviewed_at      timestamptz,
  rejection_reason text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  CHECK ((role = 'owner') = (organization_id IS NOT NULL AND user_id IS NULL)),
  CHECK ((role = 'operator') = (user_id IS NOT NULL AND organization_id IS NULL)),
  CHECK (status <> 'rejected' OR length(btrim(rejection_reason)) > 0),
  CHECK (status NOT IN ('approved', 'rejected') OR (reviewer_id IS NOT NULL AND reviewed_at IS NOT NULL))
);
CREATE UNIQUE INDEX verification_applications_subject_uq
  ON verification_applications (role, COALESCE(organization_id, user_id));
CREATE INDEX verification_applications_queue_idx ON verification_applications (status, submitted_at);
CREATE TRIGGER verification_applications_updated_at BEFORE UPDATE ON verification_applications FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER verification_applications_audit AFTER INSERT OR UPDATE OR DELETE ON verification_applications FOR EACH ROW EXECUTE FUNCTION audit_row_change();

CREATE TABLE verification_events (
  id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  application_id uuid NOT NULL REFERENCES verification_applications (id) ON DELETE CASCADE,
  actor_user_id  uuid REFERENCES users (id),
  action         text NOT NULL CHECK (action ~ '^[a-z_]+$'),
  document_id    uuid REFERENCES kyc_records (id) ON DELETE SET NULL,
  reason         text,
  created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX verification_events_app_idx ON verification_events (application_id, id);
CREATE TRIGGER verification_events_append_only BEFORE UPDATE ON verification_events
  FOR EACH ROW EXECUTE FUNCTION forbid_modification();

-- Owner contact details.
ALTER TABLE organizations
  ADD COLUMN contact_name  text CHECK (contact_name IS NULL OR length(btrim(contact_name)) > 0),
  ADD COLUMN contact_phone text CHECK (contact_phone IS NULL OR contact_phone ~ '^\+[1-9][0-9]{7,14}$'),
  ADD COLUMN contact_email citext CHECK (contact_email IS NULL OR contact_email ~ '^[^@\s]+@[^@\s]+\.[^@\s]+$');

-- Operator availability: {"days": ["mon", ...], "start": "08:00", "end": "18:00"}.
ALTER TABLE operator_profiles ADD COLUMN availability jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(availability) = 'object');

CREATE TABLE operator_qualifications (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  operator_user_id uuid NOT NULL REFERENCES operator_profiles (user_id) ON DELETE CASCADE,
  title            text NOT NULL CHECK (length(btrim(title)) BETWEEN 2 AND 120),
  issuer           text CHECK (issuer IS NULL OR length(issuer) <= 120),
  issued_on        date,
  valid_until      date CHECK (valid_until IS NULL OR issued_on IS NULL OR valid_until >= issued_on),
  document_id      uuid REFERENCES kyc_records (id) ON DELETE SET NULL,
  created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX operator_qualifications_user_idx ON operator_qualifications (operator_user_id);

-- Fleet managers manage only the machines they are granted, or all of the
-- organisation's machines when an owner / org admin sets all_equipment.
ALTER TABLE fleet_manager_profiles ADD COLUMN all_equipment boolean NOT NULL DEFAULT false;

CREATE TABLE fleet_manager_equipment (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  user_id         uuid NOT NULL REFERENCES fleet_manager_profiles (user_id) ON DELETE CASCADE,
  organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  equipment_id    uuid NOT NULL REFERENCES equipment (id) ON DELETE CASCADE,
  granted_by      uuid REFERENCES users (id),
  granted_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, equipment_id)
);
CREATE INDEX fleet_manager_equipment_org_idx ON fleet_manager_equipment (organization_id);
CREATE TRIGGER fleet_manager_equipment_audit AFTER INSERT OR DELETE ON fleet_manager_equipment
  FOR EACH ROW EXECUTE FUNCTION audit_row_change();
