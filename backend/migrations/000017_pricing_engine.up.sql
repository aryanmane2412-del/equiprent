-- Rental pricing and availability engine: owner booking rules, pickup and
-- fuel charges, configurable taxes, per-listing cancellation policies and
-- final (post-rental) charges. All money is integer paise.

-- ---- owner booking rules -----------------------------------------------------------------
ALTER TABLE equipment
  ADD COLUMN min_rental_hours         integer CHECK (min_rental_hours BETWEEN 1 AND 4320),
  ADD COLUMN max_rental_days          integer CHECK (max_rental_days BETWEEN 1 AND 180),
  ADD COLUMN security_deposit_paise   bigint CHECK (security_deposit_paise >= 0),
  ADD COLUMN fuel_rate_per_hour_paise bigint CHECK (fuel_rate_per_hour_paise >= 0),
  ADD COLUMN late_fee_per_hour_paise  bigint CHECK (late_fee_per_hour_paise >= 0),
  ADD COLUMN late_grace_minutes       integer NOT NULL DEFAULT 60 CHECK (late_grace_minutes BETWEEN 0 AND 720),
  -- IANA zone the owner works in: dates customers pick are local to it.
  ADD COLUMN timezone                 text NOT NULL DEFAULT 'Asia/Kolkata' CHECK (timezone ~ '^[A-Za-z_]+(/[A-Za-z0-9_+-]+)*$'),
  ADD COLUMN cancellation_policy_id   uuid REFERENCES cancellation_policies (id),
  ADD CONSTRAINT equipment_rental_window_ck
    CHECK (min_rental_hours IS NULL OR max_rental_days IS NULL OR min_rental_hours <= max_rental_days * 24);

-- Pickup (return trip) priced separately from delivery. Zero = included.
ALTER TABLE mobilisation_rates
  ADD COLUMN pickup_fixed_paise  bigint NOT NULL DEFAULT 0 CHECK (pickup_fixed_paise >= 0),
  ADD COLUMN pickup_per_km_paise bigint NOT NULL DEFAULT 0 CHECK (pickup_per_km_paise >= 0);

-- ---- cancellation policies -----------------------------------------------------------------
-- Presets are platform-wide; owners may also define their own (owner_org_id set).
ALTER TABLE cancellation_policies
  ADD COLUMN owner_org_id uuid REFERENCES organizations (id),
  DROP CONSTRAINT cancellation_policies_code_check,
  ADD CONSTRAINT cancellation_policies_code_check CHECK (code ~ '^[a-z0-9_]+$');

INSERT INTO cancellation_policies (code, name, rules, is_default) VALUES
  ('flexible', 'Flexible — free until 6 hours before start',
   '[{"hours_before": 6, "fee_bps": 0}, {"hours_before": 0, "fee_bps": 1000}]', false)
ON CONFLICT (code) DO NOTHING;

-- ---- configurable taxes -------------------------------------------------------------------
-- A tax applies to the listed charge components while active and within its
-- validity window (by the rental start date). Rates are basis points.
CREATE TABLE tax_rates (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  code        text NOT NULL CHECK (code ~ '^[a-z0-9_]+$'),
  name        text NOT NULL CHECK (length(name) BETWEEN 2 AND 60),
  rate_bps    integer NOT NULL CHECK (rate_bps BETWEEN 0 AND 10000),
  applies_to  text[] NOT NULL CHECK (
                cardinality(applies_to) > 0 AND
                applies_to <@ ARRAY['rental', 'extra_hours', 'delivery', 'pickup', 'operator', 'fuel', 'platform_fee', 'late_return', 'damage']::text[]),
  valid_from  date NOT NULL DEFAULT DATE '2017-07-01',
  valid_until date CHECK (valid_until IS NULL OR valid_until >= valid_from),
  is_active   boolean NOT NULL DEFAULT true,
  updated_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (code, valid_from)
);
CREATE TRIGGER tax_rates_updated_at BEFORE UPDATE ON tax_rates FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- GST on machinery hire with or without operator (SAC 997313): 18%.
INSERT INTO tax_rates (code, name, rate_bps, applies_to) VALUES
  ('gst', 'GST', 1800, ARRAY['rental', 'extra_hours', 'delivery', 'pickup', 'operator', 'fuel', 'platform_fee', 'late_return']);

-- ---- bookings: options and final charges ----------------------------------------------------
ALTER TABLE bookings
  ADD COLUMN delivery          boolean NOT NULL DEFAULT true,
  ADD COLUMN pickup            boolean NOT NULL DEFAULT true,
  -- Settled after the rental (actual hours, late return, fuel, damage).
  ADD COLUMN final_charges     jsonb CHECK (final_charges IS NULL OR jsonb_typeof(final_charges) = 'object'),
  ADD COLUMN finalized_at      timestamptz,
  ADD CONSTRAINT bookings_final_ck CHECK ((final_charges IS NULL) = (finalized_at IS NULL)),
  ADD CONSTRAINT bookings_final_total_ck CHECK (final_charges IS NULL OR final_total_paise IS NOT NULL);
