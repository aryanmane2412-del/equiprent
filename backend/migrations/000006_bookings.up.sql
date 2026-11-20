-- Bookings, history, rental agreements, operator assignments and the job
-- evidence (handover/return checklists, daily hour-meter logs).

CREATE TYPE booking_status AS ENUM (
  'requested', 'quoted', 'accepted', 'payment_pending', 'confirmed',
  'operator_assigned', 'dispatched', 'on_site', 'active', 'return_initiated',
  'returned', 'completed', 'closed',
  'rejected', 'expired', 'cancelled', 'disputed'
);
CREATE TYPE payment_mode AS ENUM ('online', 'offline', 'credit');

-- Allowed lifecycle edges (docs/USER_FLOWS.md §6). Enforced by trigger.
CREATE TABLE booking_status_transitions (
  from_status booking_status NOT NULL,
  to_status   booking_status NOT NULL,
  PRIMARY KEY (from_status, to_status),
  CHECK (from_status <> to_status)
);
INSERT INTO booking_status_transitions (from_status, to_status) VALUES
  ('requested', 'accepted'), ('requested', 'rejected'), ('requested', 'expired'), ('requested', 'cancelled'),
  ('quoted', 'accepted'), ('quoted', 'expired'), ('quoted', 'cancelled'),
  ('accepted', 'payment_pending'), ('accepted', 'confirmed'), ('accepted', 'cancelled'),
  ('payment_pending', 'confirmed'), ('payment_pending', 'expired'), ('payment_pending', 'cancelled'),
  ('confirmed', 'operator_assigned'), ('confirmed', 'dispatched'), ('confirmed', 'cancelled'),
  ('operator_assigned', 'confirmed'), ('operator_assigned', 'dispatched'), ('operator_assigned', 'cancelled'),
  ('dispatched', 'on_site'), ('dispatched', 'cancelled'),
  ('on_site', 'active'), ('on_site', 'cancelled'),
  ('active', 'return_initiated'), ('active', 'disputed'),
  ('return_initiated', 'returned'),
  ('returned', 'completed'), ('returned', 'disputed'),
  ('completed', 'closed'), ('completed', 'disputed'),
  ('disputed', 'completed'), ('disputed', 'closed');

-- Statuses in which a booking holds the machine's calendar.
CREATE OR REPLACE FUNCTION booking_holds_slot(s booking_status) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
  SELECT s IN ('accepted', 'payment_pending', 'confirmed', 'operator_assigned', 'dispatched', 'on_site', 'active', 'return_initiated')
$$;

CREATE TABLE bookings (
  id                     uuid PRIMARY KEY DEFAULT uuidv7(),
  reference              text NOT NULL UNIQUE CHECK (reference ~ '^ER-[0-9]{4}-[A-Z0-9]{4}$'),
  equipment_id           uuid NOT NULL REFERENCES equipment (id),
  owner_org_id           uuid NOT NULL REFERENCES organizations (id),
  customer_org_id        uuid NOT NULL REFERENCES organizations (id),
  requested_by           uuid NOT NULL REFERENCES users (id),
  site_address_id        uuid NOT NULL REFERENCES addresses (id),
  site_location          geography(Point, 4326) NOT NULL,
  site_geofence_m        integer NOT NULL DEFAULT 300 CHECK (site_geofence_m BETWEEN 50 AND 5000),
  period                 tstzrange NOT NULL CHECK (NOT isempty(period) AND NOT lower_inf(period) AND NOT upper_inf(period)),
  actual_period          tstzrange CHECK (actual_period IS NULL OR NOT isempty(actual_period)),
  with_operator          boolean NOT NULL,
  pricing_unit           price_unit NOT NULL,
  price_snapshot         jsonb NOT NULL CHECK (jsonb_typeof(price_snapshot) = 'object'),
  estimated_total_paise  bigint NOT NULL CHECK (estimated_total_paise >= 0),
  final_total_paise      bigint CHECK (final_total_paise >= 0),
  deposit_paise          bigint NOT NULL DEFAULT 0 CHECK (deposit_paise >= 0),
  currency               char(3) NOT NULL DEFAULT 'INR' CHECK (currency = 'INR'),
  cancellation_policy_id uuid REFERENCES cancellation_policies (id),
  payment_mode           payment_mode NOT NULL DEFAULT 'online',
  status                 booking_status NOT NULL DEFAULT 'requested',
  status_changed_at      timestamptz NOT NULL DEFAULT now(),
  owner_response_due_at  timestamptz,
  work_description       text NOT NULL DEFAULT '',
  cancel_reason          text,
  version                integer NOT NULL DEFAULT 1,
  created_at             timestamptz NOT NULL DEFAULT now(),
  updated_at             timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT bookings_parties_ck CHECK (owner_org_id <> customer_org_id),
  CONSTRAINT bookings_cancel_reason_ck CHECK (status <> 'cancelled' OR cancel_reason IS NOT NULL),
  -- The double-booking guard: no two slot-holding bookings may overlap.
  CONSTRAINT bookings_no_overlap EXCLUDE USING gist (equipment_id WITH =, period WITH &&)
    WHERE (status IN ('accepted', 'payment_pending', 'confirmed', 'operator_assigned', 'dispatched', 'on_site', 'active', 'return_initiated'))
);
CREATE INDEX bookings_customer_idx ON bookings (customer_org_id, created_at DESC);
CREATE INDEX bookings_owner_idx ON bookings (owner_org_id, status, created_at DESC);
CREATE INDEX bookings_equipment_period_idx ON bookings USING gist (equipment_id, period);
CREATE INDEX bookings_response_due_idx ON bookings (owner_response_due_at) WHERE status = 'requested';

-- Fails if the period collides with an availability block (owner block or
-- maintenance). Callers must hold the equipment row lock.
CREATE OR REPLACE FUNCTION assert_no_block_overlap(p_equipment uuid, p_period tstzrange) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM availability_blocks WHERE equipment_id = p_equipment AND period && p_period) THEN
    RAISE EXCEPTION 'equipment % is blocked during %', p_equipment, p_period USING ERRCODE = 'EQ004';
  END IF;
END;
$$;

CREATE OR REPLACE FUNCTION bookings_before_insert() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  v_owner  uuid;
  v_status listing_status;
  v_deleted timestamptz;
BEGIN
  -- Row lock serialises concurrent bookings/blocks for the same machine.
  SELECT owner_org_id, status, deleted_at INTO v_owner, v_status, v_deleted
    FROM equipment WHERE id = NEW.equipment_id FOR UPDATE;
  IF NOT FOUND OR v_deleted IS NOT NULL OR v_status <> 'active' THEN
    RAISE EXCEPTION 'equipment % is not available for booking', NEW.equipment_id USING ERRCODE = 'EQ004';
  END IF;
  NEW.owner_org_id := v_owner;  -- always derived, never trusted from the client
  IF NEW.status NOT IN ('requested', 'quoted', 'payment_pending', 'confirmed') THEN
    RAISE EXCEPTION 'a booking cannot start in status %', NEW.status USING ERRCODE = 'EQ002';
  END IF;
  PERFORM assert_no_block_overlap(NEW.equipment_id, NEW.period);
  NEW.status_changed_at := now();
  RETURN NEW;
END;
$$;
CREATE TRIGGER bookings_before_insert BEFORE INSERT ON bookings FOR EACH ROW EXECUTE FUNCTION bookings_before_insert();

CREATE OR REPLACE FUNCTION bookings_before_update() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.reference <> OLD.reference OR NEW.equipment_id <> OLD.equipment_id
     OR NEW.customer_org_id <> OLD.customer_org_id OR NEW.owner_org_id <> OLD.owner_org_id THEN
    RAISE EXCEPTION 'booking identity columns are immutable' USING ERRCODE = 'EQ001';
  END IF;
  IF NEW.status <> OLD.status THEN
    IF NOT EXISTS (SELECT 1 FROM booking_status_transitions WHERE from_status = OLD.status AND to_status = NEW.status) THEN
      RAISE EXCEPTION 'invalid booking transition % -> %', OLD.status, NEW.status USING ERRCODE = 'EQ002';
    END IF;
    IF booking_holds_slot(NEW.status) AND NOT booking_holds_slot(OLD.status) THEN
      PERFORM 1 FROM equipment WHERE id = NEW.equipment_id FOR UPDATE;
      PERFORM assert_no_block_overlap(NEW.equipment_id, NEW.period);
    END IF;
    NEW.status_changed_at := now();
  ELSIF NEW.period <> OLD.period AND booking_holds_slot(NEW.status) THEN
    PERFORM 1 FROM equipment WHERE id = NEW.equipment_id FOR UPDATE;
    PERFORM assert_no_block_overlap(NEW.equipment_id, NEW.period);
  END IF;
  NEW.version := OLD.version + 1;
  NEW.updated_at := now();
  RETURN NEW;
END;
$$;
CREATE TRIGGER bookings_before_update BEFORE UPDATE ON bookings FOR EACH ROW EXECUTE FUNCTION bookings_before_update();

-- Availability blocks may not cover days already held by a booking.
CREATE OR REPLACE FUNCTION availability_blocks_before_write() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  PERFORM 1 FROM equipment WHERE id = NEW.equipment_id FOR UPDATE;
  IF EXISTS (SELECT 1 FROM bookings
              WHERE equipment_id = NEW.equipment_id AND period && NEW.period AND booking_holds_slot(status)) THEN
    RAISE EXCEPTION 'cannot block % : an active booking holds this period', NEW.period USING ERRCODE = 'EQ004';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER availability_blocks_before_write BEFORE INSERT OR UPDATE ON availability_blocks
  FOR EACH ROW EXECUTE FUNCTION availability_blocks_before_write();

-- Booking history -------------------------------------------------------------------
CREATE TABLE booking_events (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  booking_id    uuid NOT NULL REFERENCES bookings (id),
  from_status   booking_status,
  to_status     booking_status NOT NULL,
  actor_user_id uuid REFERENCES users (id),
  actor_role    text,
  reason        text,
  payload       jsonb,
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX booking_events_booking_idx ON booking_events (booking_id, id);
CREATE TRIGGER booking_events_append_only BEFORE UPDATE OR DELETE ON booking_events
  FOR EACH ROW EXECUTE FUNCTION forbid_modification();

-- History is written by the database, so no code path can skip it. The actor
-- comes from SET LOCAL app.actor_user_id / app.actor_role / app.reason.
CREATE OR REPLACE FUNCTION bookings_record_event() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' OR NEW.status <> OLD.status THEN
    INSERT INTO booking_events (booking_id, from_status, to_status, actor_user_id, actor_role, reason)
    VALUES (
      NEW.id,
      CASE WHEN TG_OP = 'UPDATE' THEN OLD.status END,
      NEW.status,
      nullif(current_setting('app.actor_user_id', true), '')::uuid,
      nullif(current_setting('app.actor_role', true), ''),
      COALESCE(nullif(current_setting('app.reason', true), ''), NEW.cancel_reason)
    );
  END IF;
  RETURN NULL;
END;
$$;
CREATE TRIGGER bookings_record_event AFTER INSERT OR UPDATE ON bookings FOR EACH ROW EXECUTE FUNCTION bookings_record_event();

-- Rental agreements -------------------------------------------------------------------
CREATE TABLE rental_agreements (
  id                   uuid PRIMARY KEY DEFAULT uuidv7(),
  booking_id           uuid NOT NULL UNIQUE REFERENCES bookings (id),
  template_version     text NOT NULL,
  terms_sha256         bytea NOT NULL CHECK (length(terms_sha256) = 32),  -- hash of the exact rendered terms
  document_object_key  text CHECK (document_object_key IS NULL OR is_object_key(document_object_key)),
  customer_accepted_by uuid REFERENCES users (id),
  customer_accepted_at timestamptz,
  customer_accept_ip   inet,
  owner_accepted_by    uuid REFERENCES users (id),
  owner_accepted_at    timestamptz,
  created_at           timestamptz NOT NULL DEFAULT now(),
  CHECK ((customer_accepted_by IS NULL) = (customer_accepted_at IS NULL)),
  CHECK ((owner_accepted_by IS NULL) = (owner_accepted_at IS NULL))
);

-- Once both parties accepted, the agreement is frozen.
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
CREATE TRIGGER rental_agreements_freeze BEFORE UPDATE OR DELETE ON rental_agreements FOR EACH ROW EXECUTE FUNCTION rental_agreements_freeze();
CREATE TRIGGER rental_agreements_audit AFTER INSERT OR UPDATE ON rental_agreements FOR EACH ROW EXECUTE FUNCTION audit_row_change();

-- Operator assignments ------------------------------------------------------------------
CREATE TYPE assignment_status AS ENUM ('assigned', 'accepted', 'declined', 'replaced', 'completed');

CREATE TABLE operator_assignments (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  booking_id       uuid NOT NULL REFERENCES bookings (id),
  operator_user_id uuid NOT NULL REFERENCES operator_profiles (user_id),
  assigned_by      uuid NOT NULL REFERENCES users (id),
  period           tstzrange NOT NULL CHECK (NOT isempty(period)),
  status           assignment_status NOT NULL DEFAULT 'assigned',
  responded_at     timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  -- An operator cannot be on two overlapping jobs.
  CONSTRAINT operator_assignments_no_overlap EXCLUDE USING gist (operator_user_id WITH =, period WITH &&)
    WHERE (status IN ('assigned', 'accepted'))
);
CREATE UNIQUE INDEX operator_assignments_one_active_uq ON operator_assignments (booking_id) WHERE status IN ('assigned', 'accepted');
CREATE INDEX operator_assignments_operator_idx ON operator_assignments (operator_user_id, status);
CREATE TRIGGER operator_assignments_updated_at BEFORE UPDATE ON operator_assignments FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Job evidence ---------------------------------------------------------------------------
CREATE TABLE checklists (
  id                      uuid PRIMARY KEY DEFAULT uuidv7(),
  booking_id              uuid NOT NULL REFERENCES bookings (id),
  kind                    text NOT NULL CHECK (kind IN ('handover', 'return')),
  hmr_reading             numeric(10, 1) NOT NULL CHECK (hmr_reading >= 0),
  hmr_photo_object_key    text NOT NULL CHECK (is_object_key(hmr_photo_object_key)),
  fuel_level_pct          smallint CHECK (fuel_level_pct BETWEEN 0 AND 100),
  items                   jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(items) = 'object'),
  photo_object_keys       text[] NOT NULL DEFAULT '{}',
  damage_notes            text,
  location                geography(Point, 4326),
  captured_at             timestamptz NOT NULL,        -- device clock
  received_at             timestamptz NOT NULL DEFAULT now(),
  submitted_by            uuid NOT NULL REFERENCES users (id),
  customer_otp_verified_at timestamptz,
  client_id               uuid NOT NULL UNIQUE,        -- offline idempotency
  UNIQUE (booking_id, kind)
);
CREATE TRIGGER checklists_append_only BEFORE UPDATE OR DELETE ON checklists FOR EACH ROW EXECUTE FUNCTION forbid_modification();

CREATE TYPE work_log_status AS ENUM ('submitted', 'approved', 'disputed', 'auto_approved');

CREATE TABLE work_logs (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  booking_id      uuid NOT NULL REFERENCES bookings (id),
  work_date       date NOT NULL,
  start_hmr       numeric(10, 1) NOT NULL CHECK (start_hmr >= 0),
  end_hmr         numeric(10, 1) NOT NULL,
  idle_hours      numeric(4, 1) NOT NULL DEFAULT 0 CHECK (idle_hours >= 0),
  breakdown_hours numeric(4, 1) NOT NULL DEFAULT 0 CHECK (breakdown_hours >= 0),
  billable_hours  numeric(4, 1) GENERATED ALWAYS AS (end_hmr - start_hmr - breakdown_hours) STORED,
  fuel_litres     numeric(6, 1) CHECK (fuel_litres >= 0),
  submitted_by    uuid NOT NULL REFERENCES users (id),
  status          work_log_status NOT NULL DEFAULT 'submitted',
  approved_by     uuid REFERENCES users (id),
  approved_at     timestamptz,
  client_id       uuid NOT NULL UNIQUE,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (booking_id, work_date),
  CONSTRAINT work_logs_hmr_ck CHECK (end_hmr >= start_hmr AND end_hmr - start_hmr <= 24),
  CONSTRAINT work_logs_breakdown_ck CHECK (breakdown_hours <= end_hmr - start_hmr),
  CONSTRAINT work_logs_approval_ck CHECK ((status IN ('approved', 'auto_approved')) = (approved_at IS NOT NULL))
);
CREATE TRIGGER work_logs_updated_at BEFORE UPDATE ON work_logs FOR EACH ROW EXECUTE FUNCTION set_updated_at();
