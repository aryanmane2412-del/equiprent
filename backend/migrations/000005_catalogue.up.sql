-- Equipment catalogue: categories, models, equipment, specifications,
-- photographs, documents, pricing rules and availability.

CREATE TABLE equipment_categories (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  parent_id   uuid REFERENCES equipment_categories (id),
  slug        text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
  name        jsonb NOT NULL CHECK (jsonb_typeof(name) = 'object' AND name ? 'en'),  -- {"en": "...", "hi": "..."}
  spec_schema jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(spec_schema) = 'object'),
  icon_key    text,
  sort_order  integer NOT NULL DEFAULT 0,
  is_active   boolean NOT NULL DEFAULT true,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  CHECK (parent_id IS DISTINCT FROM id)
);
CREATE TRIGGER equipment_categories_updated_at BEFORE UPDATE ON equipment_categories FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE equipment_makes (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  name       citext NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE equipment_models (
  id             uuid PRIMARY KEY DEFAULT uuidv7(),
  category_id    uuid NOT NULL REFERENCES equipment_categories (id),
  make_id        uuid NOT NULL REFERENCES equipment_makes (id),
  name           text NOT NULL,
  default_specs  jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(default_specs) = 'object'),
  created_at     timestamptz NOT NULL DEFAULT now(),
  UNIQUE (make_id, name)
);
CREATE INDEX equipment_models_category_idx ON equipment_models (category_id);

-- Operator skills (which categories an operator may run).
CREATE TABLE operator_skills (
  operator_user_id uuid NOT NULL REFERENCES operator_profiles (user_id) ON DELETE CASCADE,
  category_id      uuid NOT NULL REFERENCES equipment_categories (id),
  PRIMARY KEY (operator_user_id, category_id)
);

CREATE TYPE listing_status AS ENUM ('draft', 'pending_review', 'active', 'paused', 'rejected', 'suspended', 'retired');
CREATE TYPE fuel_policy AS ENUM ('customer', 'owner', 'shared');

CREATE TABLE equipment (
  id                uuid PRIMARY KEY DEFAULT uuidv7(),
  owner_org_id      uuid NOT NULL REFERENCES organizations (id),
  model_id          uuid NOT NULL REFERENCES equipment_models (id),
  title             text NOT NULL CHECK (length(btrim(title)) BETWEEN 3 AND 140),
  description       text NOT NULL DEFAULT '',
  manufacture_year  smallint CHECK (manufacture_year BETWEEN 1980 AND 2100),
  registration_no   text CHECK (registration_no IS NULL OR registration_no ~ '^[A-Z]{2}[0-9]{1,2}[A-Z]{0,3}[0-9]{1,4}$'),
  serial_no         text,
  current_hmr       numeric(10, 1) NOT NULL DEFAULT 0 CHECK (current_hmr >= 0),
  base_location     geography(Point, 4326) NOT NULL,
  base_address_id   uuid REFERENCES addresses (id),
  city_id           uuid REFERENCES cities (id),
  service_radius_km integer NOT NULL DEFAULT 50 CHECK (service_radius_km BETWEEN 1 AND 500),
  wet_hire          boolean NOT NULL DEFAULT true,
  dry_hire          boolean NOT NULL DEFAULT false,
  fuel_policy       fuel_policy NOT NULL DEFAULT 'customer',
  instant_book      boolean NOT NULL DEFAULT false,
  status            listing_status NOT NULL DEFAULT 'draft',
  rating_avg        numeric(3, 2) CHECK (rating_avg BETWEEN 0 AND 5),
  rating_count      integer NOT NULL DEFAULT 0 CHECK (rating_count >= 0),
  version           integer NOT NULL DEFAULT 1,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  deleted_at        timestamptz,
  CONSTRAINT equipment_hire_mode_ck CHECK (wet_hire OR dry_hire),
  CONSTRAINT equipment_deleted_retired_ck CHECK (deleted_at IS NULL OR status = 'retired')
);
-- Registration numbers are stored without spaces, e.g. MH12KX4471.
CREATE UNIQUE INDEX equipment_registration_uq ON equipment (registration_no) WHERE registration_no IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX equipment_location_gix ON equipment USING gist (base_location) WHERE deleted_at IS NULL;
CREATE INDEX equipment_owner_idx ON equipment (owner_org_id) WHERE deleted_at IS NULL;
CREATE INDEX equipment_search_idx ON equipment (status, model_id) WHERE deleted_at IS NULL;
CREATE INDEX equipment_title_trgm ON equipment USING gin (title gin_trgm_ops);
CREATE TRIGGER equipment_updated_at BEFORE UPDATE ON equipment FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE equipment_specifications (
  equipment_id  uuid NOT NULL REFERENCES equipment (id) ON DELETE CASCADE,
  spec_key      text NOT NULL CHECK (spec_key ~ '^[a-z0-9_]+$'),   -- e.g. operating_weight_kg
  value_numeric numeric,
  value_text    text,
  unit          text,
  PRIMARY KEY (equipment_id, spec_key),
  CHECK (value_numeric IS NOT NULL OR value_text IS NOT NULL)
);
CREATE INDEX equipment_specs_numeric_idx ON equipment_specifications (spec_key, value_numeric) WHERE value_numeric IS NOT NULL;

-- Photographs (and short videos). Object keys point at the public media bucket.
CREATE TABLE equipment_media (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  equipment_id uuid NOT NULL REFERENCES equipment (id) ON DELETE CASCADE,
  kind         text NOT NULL DEFAULT 'photo' CHECK (kind IN ('photo', 'video')),
  object_key   text NOT NULL UNIQUE CHECK (is_object_key(object_key)),
  content_type text NOT NULL CHECK (content_type IN ('image/jpeg', 'image/png', 'image/webp', 'video/mp4')),
  size_bytes   bigint NOT NULL CHECK (size_bytes > 0 AND size_bytes <= 15728640),
  sha256       bytea NOT NULL CHECK (length(sha256) = 32),
  width        integer CHECK (width > 0),
  height       integer CHECK (height > 0),
  angle        text CHECK (angle IN ('front', 'rear', 'left', 'right', 'cabin', 'hour_meter', 'attachment', 'other')),
  sort_order   integer NOT NULL DEFAULT 0,
  is_cover     boolean NOT NULL DEFAULT false,
  uploaded_by  uuid REFERENCES users (id),
  created_at   timestamptz NOT NULL DEFAULT now(),
  deleted_at   timestamptz,
  CHECK (kind = 'video' OR content_type LIKE 'image/%')
);
CREATE INDEX equipment_media_equipment_idx ON equipment_media (equipment_id, sort_order) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX equipment_media_cover_uq ON equipment_media (equipment_id) WHERE is_cover AND deleted_at IS NULL;

CREATE TYPE equipment_document_type AS ENUM ('rc', 'insurance', 'fitness', 'puc', 'invoice', 'permit', 'load_test', 'other');

-- Documents live in the PRIVATE bucket; served only via short presigned URLs.
CREATE TABLE equipment_documents (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  equipment_id    uuid NOT NULL REFERENCES equipment (id) ON DELETE CASCADE,
  doc_type        equipment_document_type NOT NULL,
  document_number text,
  object_key      text NOT NULL UNIQUE CHECK (is_object_key(object_key)),
  content_type    text NOT NULL CHECK (content_type IN ('application/pdf', 'image/jpeg', 'image/png')),
  size_bytes      bigint NOT NULL CHECK (size_bytes > 0 AND size_bytes <= 15728640),
  sha256          bytea NOT NULL CHECK (length(sha256) = 32),
  valid_from      date,
  valid_until     date,
  status          verification_status NOT NULL DEFAULT 'submitted',
  reviewed_by     uuid REFERENCES users (id),
  reviewed_at     timestamptz,
  uploaded_by     uuid REFERENCES users (id),
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  deleted_at      timestamptz,
  CHECK (valid_until IS NULL OR valid_from IS NULL OR valid_until >= valid_from)
);
CREATE INDEX equipment_documents_equipment_idx ON equipment_documents (equipment_id, doc_type) WHERE deleted_at IS NULL;
CREATE INDEX equipment_documents_expiry_idx ON equipment_documents (valid_until) WHERE status = 'verified' AND deleted_at IS NULL;
CREATE TRIGGER equipment_documents_updated_at BEFORE UPDATE ON equipment_documents FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Pricing rules -------------------------------------------------------------------
CREATE TYPE price_unit AS ENUM ('hour', 'shift', 'day', 'week', 'month');

CREATE TABLE pricing_plans (
  id                          uuid PRIMARY KEY DEFAULT uuidv7(),
  equipment_id                uuid NOT NULL REFERENCES equipment (id) ON DELETE CASCADE,
  unit                        price_unit NOT NULL,
  with_operator               boolean NOT NULL DEFAULT true,
  rate_paise                  bigint NOT NULL CHECK (rate_paise > 0),
  currency                    char(3) NOT NULL DEFAULT 'INR' CHECK (currency = 'INR'),
  min_units                   integer NOT NULL DEFAULT 1 CHECK (min_units >= 1),
  included_hours              numeric(6, 1) CHECK (included_hours > 0),   -- shift = 8, month = 200
  extra_hour_rate_paise       bigint CHECK (extra_hour_rate_paise >= 0),
  operator_bata_per_day_paise bigint NOT NULL DEFAULT 0 CHECK (operator_bata_per_day_paise >= 0),
  valid_from                  date NOT NULL DEFAULT CURRENT_DATE,
  valid_until                 date CHECK (valid_until IS NULL OR valid_until >= valid_from),
  is_active                   boolean NOT NULL DEFAULT true,
  created_at                  timestamptz NOT NULL DEFAULT now(),
  updated_at                  timestamptz NOT NULL DEFAULT now(),
  CHECK (with_operator OR operator_bata_per_day_paise = 0)
);
CREATE UNIQUE INDEX pricing_plans_active_uq ON pricing_plans (equipment_id, unit, with_operator) WHERE is_active;
CREATE TRIGGER pricing_plans_updated_at BEFORE UPDATE ON pricing_plans FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE mobilisation_rates (
  equipment_id  uuid PRIMARY KEY REFERENCES equipment (id) ON DELETE CASCADE,
  fixed_paise   bigint NOT NULL DEFAULT 0 CHECK (fixed_paise >= 0),
  per_km_paise  bigint NOT NULL DEFAULT 0 CHECK (per_km_paise >= 0),
  free_km       integer NOT NULL DEFAULT 0 CHECK (free_km >= 0),
  needs_trailer boolean NOT NULL DEFAULT true,
  currency      char(3) NOT NULL DEFAULT 'INR' CHECK (currency = 'INR'),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER mobilisation_rates_updated_at BEFORE UPDATE ON mobilisation_rates FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE cancellation_policies (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  code       text NOT NULL UNIQUE CHECK (code ~ '^[a-z_]+$'),
  name       text NOT NULL,
  -- [{"hours_before": 24, "fee_bps": 0}, {"hours_before": 0, "fee_bps": 2500}]
  rules      jsonb NOT NULL CHECK (jsonb_typeof(rules) = 'array'),
  is_default boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX cancellation_policies_default_uq ON cancellation_policies (is_default) WHERE is_default;

-- Availability ------------------------------------------------------------------
CREATE TYPE availability_block_reason AS ENUM ('owner_block', 'maintenance', 'offline_booking');

CREATE TABLE availability_blocks (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  equipment_id uuid NOT NULL REFERENCES equipment (id) ON DELETE CASCADE,
  period       tstzrange NOT NULL CHECK (NOT isempty(period) AND NOT lower_inf(period) AND NOT upper_inf(period)),
  reason       availability_block_reason NOT NULL,
  note         text,
  created_by   uuid REFERENCES users (id),
  created_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT availability_blocks_no_overlap EXCLUDE USING gist (equipment_id WITH =, period WITH &&)
);
