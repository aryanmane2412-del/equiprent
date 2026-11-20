-- Users, roles, permissions, role assignments, sessions, organisations.
-- Organisation-scoped user_roles replace the separate org_members table of
-- the draft design: one assignment table for platform and org roles.

CREATE TABLE roles (
  code        text PRIMARY KEY CHECK (code ~ '^[a-z_]+$'),
  name        text NOT NULL,
  description text NOT NULL DEFAULT '',
  scope       text NOT NULL CHECK (scope IN ('platform', 'organization')),
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE permissions (
  code        text PRIMARY KEY CHECK (code ~ '^[a-z_]+\.[a-z_]+$'),  -- resource.action
  description text NOT NULL
);

CREATE TABLE role_permissions (
  role_code       text NOT NULL REFERENCES roles (code) ON DELETE CASCADE,
  permission_code text NOT NULL REFERENCES permissions (code) ON DELETE CASCADE,
  PRIMARY KEY (role_code, permission_code)
);
CREATE INDEX role_permissions_permission_idx ON role_permissions (permission_code);

CREATE TYPE account_status AS ENUM ('active', 'suspended', 'deleted');

CREATE TABLE users (
  id                 uuid PRIMARY KEY DEFAULT uuidv7(),
  phone              text NOT NULL CHECK (phone ~ '^\+[1-9][0-9]{7,14}$'),  -- E.164
  phone_verified_at  timestamptz,
  email              citext CHECK (email IS NULL OR email ~ '^[^@\s]+@[^@\s]+\.[^@\s]+$'),
  full_name          text NOT NULL DEFAULT '',
  preferred_language text NOT NULL DEFAULT 'en' CHECK (preferred_language IN ('en', 'hi', 'mr', 'ta', 'te', 'kn', 'gu', 'bn')),
  avatar_object_key  text CHECK (avatar_object_key IS NULL OR is_object_key(avatar_object_key)),
  password_hash      text,   -- admins only, argon2id
  totp_secret_enc    bytea,  -- admins only, envelope-encrypted
  status             account_status NOT NULL DEFAULT 'active',
  last_login_at      timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  deleted_at         timestamptz,
  CONSTRAINT users_deleted_status_ck CHECK ((status = 'deleted') = (deleted_at IS NOT NULL))
);
-- Soft-deleted accounts release their phone/email for re-registration.
CREATE UNIQUE INDEX users_phone_uq ON users (phone) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX users_email_uq ON users (email) WHERE deleted_at IS NULL AND email IS NOT NULL;
CREATE TRIGGER users_updated_at BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE otp_challenges (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  phone       text NOT NULL CHECK (phone ~ '^\+[1-9][0-9]{7,14}$'),
  code_hash   bytea NOT NULL,      -- HMAC-SHA256, never the code
  purpose     text NOT NULL CHECK (purpose IN ('login', 'change_phone', 'step_up', 'handover')),
  attempts    smallint NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
  expires_at  timestamptz NOT NULL,
  consumed_at timestamptz,
  ip          inet,
  device_id   text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  CHECK (expires_at > created_at)
);
CREATE INDEX otp_challenges_phone_idx ON otp_challenges (phone, created_at DESC);

CREATE TABLE sessions (
  id                 uuid PRIMARY KEY DEFAULT uuidv7(),
  user_id            uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  family_id          uuid NOT NULL,            -- rotation chain; reuse revokes the family
  refresh_token_hash bytea NOT NULL UNIQUE,
  device_id          text,
  platform           text CHECK (platform IN ('android', 'ios', 'web', 'admin')),
  ip                 inet,
  user_agent         text,
  expires_at         timestamptz NOT NULL,
  last_used_at       timestamptz,
  revoked_at         timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_active_idx ON sessions (user_id) WHERE revoked_at IS NULL;
CREATE INDEX sessions_family_idx ON sessions (family_id);

CREATE TABLE devices (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  platform     text NOT NULL CHECK (platform IN ('android', 'ios', 'web')),
  push_token   text NOT NULL UNIQUE,
  app_version  text,
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX devices_user_idx ON devices (user_id);

-- DPDP Act consent records.
CREATE TABLE consents (
  id             uuid PRIMARY KEY DEFAULT uuidv7(),
  user_id        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  purpose        text NOT NULL CHECK (purpose IN ('service', 'location_tracking', 'marketing', 'whatsapp')),
  notice_version text NOT NULL,
  granted_at     timestamptz NOT NULL DEFAULT now(),
  withdrawn_at   timestamptz,
  CHECK (withdrawn_at IS NULL OR withdrawn_at >= granted_at)
);
CREATE UNIQUE INDEX consents_active_uq ON consents (user_id, purpose) WHERE withdrawn_at IS NULL;

-- Business information --------------------------------------------------------
CREATE TYPE org_type AS ENUM ('individual', 'proprietorship', 'partnership', 'llp', 'private_limited', 'public_limited', 'government', 'other');
CREATE TYPE verification_status AS ENUM ('pending', 'submitted', 'verified', 'rejected', 'expired');

CREATE TABLE organizations (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  legal_name      text NOT NULL CHECK (length(btrim(legal_name)) > 0),
  display_name    text NOT NULL CHECK (length(btrim(display_name)) > 0),
  org_type        org_type NOT NULL,
  is_buyer        boolean NOT NULL DEFAULT true,
  is_supplier     boolean NOT NULL DEFAULT false,
  gstin           text CHECK (gstin IS NULL OR gstin ~ '^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$'),
  gst_state_code  char(2) CHECK (gst_state_code IS NULL OR gst_state_code ~ '^[0-9]{2}$'),
  pan_enc         bytea,          -- AES-256-GCM, application-encrypted
  pan_hash        bytea,          -- HMAC for lookups / duplicate detection
  pan_last4       text CHECK (pan_last4 IS NULL OR pan_last4 ~ '^[A-Z0-9]{4}$'),
  udyam_number    text CHECK (udyam_number IS NULL OR udyam_number ~ '^UDYAM-[A-Z]{2}-[0-9]{2}-[0-9]{7}$'),
  cin             text,
  kyc_status      verification_status NOT NULL DEFAULT 'pending',
  kyc_verified_at timestamptz,
  rating_avg      numeric(3, 2) CHECK (rating_avg BETWEEN 0 AND 5),
  rating_count    integer NOT NULL DEFAULT 0 CHECK (rating_count >= 0),
  created_by      uuid REFERENCES users (id),
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  deleted_at      timestamptz,
  CONSTRAINT organizations_gst_state_ck CHECK (gstin IS NULL OR gst_state_code = left(gstin, 2)),
  CONSTRAINT organizations_kyc_ck CHECK ((kyc_status = 'verified') = (kyc_verified_at IS NOT NULL))
);
CREATE UNIQUE INDEX organizations_gstin_uq ON organizations (gstin) WHERE gstin IS NOT NULL AND deleted_at IS NULL;
CREATE UNIQUE INDEX organizations_pan_uq ON organizations (pan_hash) WHERE pan_hash IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX organizations_name_trgm ON organizations USING gin (display_name gin_trgm_ops);
CREATE TRIGGER organizations_updated_at BEFORE UPDATE ON organizations FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Role assignments --------------------------------------------------------------
CREATE TABLE user_roles (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  user_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  role_code       text NOT NULL REFERENCES roles (code),
  organization_id uuid REFERENCES organizations (id) ON DELETE CASCADE,  -- NULL for platform roles
  granted_by      uuid REFERENCES users (id),
  granted_at      timestamptz NOT NULL DEFAULT now(),
  revoked_at      timestamptz,
  CHECK (revoked_at IS NULL OR revoked_at >= granted_at)
);
CREATE UNIQUE INDEX user_roles_active_uq
  ON user_roles (user_id, role_code, COALESCE(organization_id, '00000000-0000-0000-0000-000000000000'::uuid))
  WHERE revoked_at IS NULL;
CREATE INDEX user_roles_org_idx ON user_roles (organization_id) WHERE revoked_at IS NULL;

-- Platform roles must not carry an organisation; organisation roles must.
CREATE OR REPLACE FUNCTION user_roles_check_scope() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE v_scope text;
BEGIN
  SELECT scope INTO v_scope FROM roles WHERE code = NEW.role_code;
  IF v_scope = 'platform' AND NEW.organization_id IS NOT NULL THEN
    RAISE EXCEPTION 'platform role % cannot be scoped to an organization', NEW.role_code USING ERRCODE = 'EQ004';
  ELSIF v_scope = 'organization' AND NEW.organization_id IS NULL THEN
    RAISE EXCEPTION 'organization role % requires organization_id', NEW.role_code USING ERRCODE = 'EQ004';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER user_roles_scope BEFORE INSERT OR UPDATE ON user_roles FOR EACH ROW EXECUTE FUNCTION user_roles_check_scope();
CREATE TRIGGER user_roles_audit AFTER INSERT OR UPDATE OR DELETE ON user_roles FOR EACH ROW EXECUTE FUNCTION audit_row_change();
