-- Cities, addresses, KYC, bank accounts and the role-specific profiles.

CREATE TABLE cities (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  name       text NOT NULL,
  state_code char(2) NOT NULL CHECK (state_code ~ '^[0-9]{2}$'),  -- GST state code
  center     geography(Point, 4326) NOT NULL,
  is_live    boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (name, state_code)
);

CREATE TABLE addresses (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  label           text NOT NULL,
  line1           text NOT NULL,
  line2           text,
  locality        text,
  city            text NOT NULL,
  district        text,
  state_code      char(2) NOT NULL CHECK (state_code ~ '^[0-9]{2}$'),
  pincode         char(6) NOT NULL CHECK (pincode ~ '^[1-9][0-9]{5}$'),
  location        geography(Point, 4326),
  is_site         boolean NOT NULL DEFAULT false,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  deleted_at      timestamptz,
  CONSTRAINT addresses_site_location_ck CHECK (NOT is_site OR location IS NOT NULL)
);
CREATE INDEX addresses_org_idx ON addresses (organization_id) WHERE deleted_at IS NULL;
CREATE INDEX addresses_location_gix ON addresses USING gist (location);
CREATE TRIGGER addresses_updated_at BEFORE UPDATE ON addresses FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE organizations ADD COLUMN billing_address_id uuid REFERENCES addresses (id);

CREATE TYPE kyc_document_type AS ENUM ('pan', 'gstin', 'aadhaar_reference', 'driving_licence', 'bank_account', 'udyam', 'cin', 'other');

CREATE TABLE kyc_records (
  id                  uuid PRIMARY KEY DEFAULT uuidv7(),
  organization_id     uuid REFERENCES organizations (id) ON DELETE CASCADE,
  user_id             uuid REFERENCES users (id) ON DELETE CASCADE,
  doc_type            kyc_document_type NOT NULL,
  number_enc          bytea,      -- encrypted; NEVER set for aadhaar_reference
  number_hash         bytea,
  number_last4        text CHECK (number_last4 IS NULL OR number_last4 ~ '^[A-Z0-9]{4}$'),
  document_object_key text CHECK (document_object_key IS NULL OR is_object_key(document_object_key)),
  document_sha256     bytea CHECK (document_sha256 IS NULL OR length(document_sha256) = 32),
  provider            text,
  provider_ref        text,
  status              verification_status NOT NULL DEFAULT 'submitted',
  reviewed_by         uuid REFERENCES users (id),
  reviewed_at         timestamptz,
  review_note         text,
  expires_on          date,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT kyc_subject_ck CHECK (organization_id IS NOT NULL OR user_id IS NOT NULL),
  -- Aadhaar Act: only a provider reference and last 4 digits may be stored.
  CONSTRAINT kyc_no_aadhaar_number_ck CHECK (doc_type <> 'aadhaar_reference' OR (number_enc IS NULL AND number_hash IS NULL)),
  CONSTRAINT kyc_review_ck CHECK ((status IN ('verified', 'rejected')) <= (reviewed_at IS NOT NULL))
);
CREATE INDEX kyc_records_org_idx ON kyc_records (organization_id, doc_type);
CREATE INDEX kyc_records_user_idx ON kyc_records (user_id, doc_type);
CREATE INDEX kyc_records_queue_idx ON kyc_records (created_at) WHERE status = 'submitted';
CREATE TRIGGER kyc_records_updated_at BEFORE UPDATE ON kyc_records FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE bank_accounts (
  id                        uuid PRIMARY KEY DEFAULT uuidv7(),
  organization_id           uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  account_holder_name       text NOT NULL,
  account_number_enc        bytea NOT NULL,
  account_number_hash       bytea NOT NULL,
  account_last4             text NOT NULL CHECK (account_last4 ~ '^[0-9]{4}$'),
  ifsc                      char(11) NOT NULL CHECK (ifsc ~ '^[A-Z]{4}0[A-Z0-9]{6}$'),
  vpa                       text CHECK (vpa IS NULL OR vpa ~ '^[A-Za-z0-9._-]+@[A-Za-z]+$'),
  gateway_linked_account_id text UNIQUE,
  verification_status       verification_status NOT NULL DEFAULT 'pending',
  verified_at               timestamptz,
  is_primary                boolean NOT NULL DEFAULT false,
  created_at                timestamptz NOT NULL DEFAULT now(),
  updated_at                timestamptz NOT NULL DEFAULT now(),
  deleted_at                timestamptz
);
CREATE UNIQUE INDEX bank_accounts_primary_uq ON bank_accounts (organization_id) WHERE is_primary AND deleted_at IS NULL;
CREATE UNIQUE INDEX bank_accounts_number_uq ON bank_accounts (organization_id, account_number_hash) WHERE deleted_at IS NULL;
CREATE TRIGGER bank_accounts_updated_at BEFORE UPDATE ON bank_accounts FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER bank_accounts_audit AFTER INSERT OR UPDATE OR DELETE ON bank_accounts FOR EACH ROW EXECUTE FUNCTION audit_row_change();

-- Role-specific profiles (one row per user per role) -----------------------------
CREATE TABLE customer_profiles (
  user_id                 uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
  default_organization_id uuid NOT NULL REFERENCES organizations (id),
  default_site_address_id uuid REFERENCES addresses (id),
  verification_level      smallint NOT NULL DEFAULT 0 CHECK (verification_level BETWEEN 0 AND 2),
  deducts_tds_194i        boolean NOT NULL DEFAULT false,
  created_at              timestamptz NOT NULL DEFAULT now(),
  updated_at              timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER customer_profiles_updated_at BEFORE UPDATE ON customer_profiles FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE owner_profiles (
  user_id               uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
  organization_id       uuid NOT NULL REFERENCES organizations (id),
  response_time_minutes integer CHECK (response_time_minutes >= 0),
  acceptance_rate_bps   integer CHECK (acceptance_rate_bps BETWEEN 0 AND 10000),
  payout_hold_days      smallint NOT NULL DEFAULT 2 CHECK (payout_hold_days BETWEEN 0 AND 30),
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX owner_profiles_org_idx ON owner_profiles (organization_id);
CREATE TRIGGER owner_profiles_updated_at BEFORE UPDATE ON owner_profiles FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE operator_profiles (
  user_id                  uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
  employer_organization_id uuid REFERENCES organizations (id),
  licence_number_enc       bytea,
  licence_number_hash      bytea,
  licence_last4            text CHECK (licence_last4 IS NULL OR licence_last4 ~ '^[A-Z0-9]{4}$'),
  licence_class            text,
  licence_expires_on       date,
  licence_object_key       text CHECK (licence_object_key IS NULL OR is_object_key(licence_object_key)),
  experience_years         smallint CHECK (experience_years BETWEEN 0 AND 60),
  on_duty                  boolean NOT NULL DEFAULT false,
  verified_at              timestamptz,
  rating_avg               numeric(3, 2) CHECK (rating_avg BETWEEN 0 AND 5),
  rating_count             integer NOT NULL DEFAULT 0 CHECK (rating_count >= 0),
  created_at               timestamptz NOT NULL DEFAULT now(),
  updated_at               timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX operator_profiles_licence_uq ON operator_profiles (licence_number_hash) WHERE licence_number_hash IS NOT NULL;
CREATE INDEX operator_profiles_employer_idx ON operator_profiles (employer_organization_id);
CREATE INDEX operator_profiles_licence_expiry_idx ON operator_profiles (licence_expires_on) WHERE verified_at IS NOT NULL;
CREATE TRIGGER operator_profiles_updated_at BEFORE UPDATE ON operator_profiles FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE fleet_manager_profiles (
  user_id         uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
  organization_id uuid NOT NULL REFERENCES organizations (id),
  can_set_pricing boolean NOT NULL DEFAULT false,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX fleet_manager_profiles_org_idx ON fleet_manager_profiles (organization_id);
CREATE TRIGGER fleet_manager_profiles_updated_at BEFORE UPDATE ON fleet_manager_profiles FOR EACH ROW EXECUTE FUNCTION set_updated_at();
