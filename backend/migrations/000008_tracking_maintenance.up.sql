-- GPS tracking (trips, partitioned location pings, geofence events, last
-- known position) and maintenance (plans + records).

CREATE TYPE trip_kind AS ENUM ('mobilisation', 'on_site', 'demobilisation');

CREATE TABLE trips (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  booking_id       uuid NOT NULL REFERENCES bookings (id),
  equipment_id     uuid NOT NULL REFERENCES equipment (id),
  operator_user_id uuid REFERENCES users (id),
  kind             trip_kind NOT NULL,
  started_at       timestamptz NOT NULL,
  ended_at         timestamptz CHECK (ended_at IS NULL OR ended_at >= started_at),
  distance_m       integer CHECK (distance_m >= 0),
  route            geography(LineString, 4326),   -- simplified polyline written when the trip closes
  created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX trips_booking_idx ON trips (booking_id, started_at);
CREATE UNIQUE INDEX trips_one_open_uq ON trips (equipment_id) WHERE ended_at IS NULL;

-- High-volume: range-partitioned by month (90-day default retention, older
-- partitions are dropped; dispute evidence is copied out first).
CREATE TABLE location_pings (
  trip_id      uuid NOT NULL REFERENCES trips (id),
  equipment_id uuid NOT NULL,
  recorded_at  timestamptz NOT NULL,                -- device time
  received_at  timestamptz NOT NULL DEFAULT now(),  -- server time (skew detection)
  location     geography(Point, 4326) NOT NULL,
  accuracy_m   real CHECK (accuracy_m >= 0),
  speed_mps    real CHECK (speed_mps >= 0),
  heading      smallint CHECK (heading BETWEEN 0 AND 359),
  battery_pct  smallint CHECK (battery_pct BETWEEN 0 AND 100),
  source       text NOT NULL DEFAULT 'app' CHECK (source IN ('app', 'telematics')),
  PRIMARY KEY (trip_id, recorded_at)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX location_pings_equipment_idx ON location_pings (equipment_id, recorded_at DESC);
CREATE INDEX location_pings_time_brin ON location_pings USING brin (recorded_at);

-- Creates monthly partitions [p_from, p_from + p_months) plus a default
-- partition. Idempotent; run by migrations and a monthly job.
CREATE OR REPLACE FUNCTION ensure_location_ping_partitions(p_from date, p_months integer) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE
  v_start date := date_trunc('month', p_from)::date;
  v_end   date;
  v_name  text;
BEGIN
  FOR i IN 0 .. p_months - 1 LOOP
    v_end  := (v_start + interval '1 month')::date;
    v_name := format('location_pings_%s', to_char(v_start, 'YYYY_MM'));
    IF to_regclass(v_name) IS NULL THEN
      EXECUTE format('CREATE TABLE %I PARTITION OF location_pings FOR VALUES FROM (%L) TO (%L)', v_name, v_start, v_end);
    END IF;
    v_start := v_end;
  END LOOP;
  IF to_regclass('location_pings_default') IS NULL THEN
    CREATE TABLE location_pings_default PARTITION OF location_pings DEFAULT;
  END IF;
END;
$$;
SELECT ensure_location_ping_partitions((now() - interval '1 month')::date, 6);

CREATE TYPE geofence_event_type AS ENUM ('enter_site', 'exit_site');

CREATE TABLE geofence_events (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  booking_id  uuid NOT NULL REFERENCES bookings (id),
  event       geofence_event_type NOT NULL,
  occurred_at timestamptz NOT NULL,
  location    geography(Point, 4326) NOT NULL
);
CREATE INDEX geofence_events_booking_idx ON geofence_events (booking_id, occurred_at);

-- Snapshot of the latest position (live position itself lives in Redis).
CREATE TABLE equipment_last_locations (
  equipment_id uuid PRIMARY KEY REFERENCES equipment (id) ON DELETE CASCADE,
  location     geography(Point, 4326) NOT NULL,
  recorded_at  timestamptz NOT NULL,
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX equipment_last_locations_gix ON equipment_last_locations USING gist (location);

-- Maintenance ----------------------------------------------------------------------
CREATE TABLE maintenance_plans (
  id            uuid PRIMARY KEY DEFAULT uuidv7(),
  equipment_id  uuid NOT NULL REFERENCES equipment (id) ON DELETE CASCADE,
  task          text NOT NULL,
  every_hours   integer CHECK (every_hours > 0),
  every_days    integer CHECK (every_days > 0),
  last_done_hmr numeric(10, 1),
  last_done_at  timestamptz,
  is_active     boolean NOT NULL DEFAULT true,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  CHECK (every_hours IS NOT NULL OR every_days IS NOT NULL)
);
CREATE INDEX maintenance_plans_equipment_idx ON maintenance_plans (equipment_id) WHERE is_active;
CREATE TRIGGER maintenance_plans_updated_at BEFORE UPDATE ON maintenance_plans FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TYPE maintenance_status AS ENUM ('scheduled', 'in_progress', 'done', 'cancelled');

CREATE TABLE maintenance_records (
  id                 uuid PRIMARY KEY DEFAULT uuidv7(),
  equipment_id       uuid NOT NULL REFERENCES equipment (id),
  plan_id            uuid REFERENCES maintenance_plans (id),
  task               text NOT NULL,
  status             maintenance_status NOT NULL DEFAULT 'scheduled',
  scheduled_for      date,
  performed_at       timestamptz,
  hmr_at             numeric(10, 1) CHECK (hmr_at >= 0),
  cost_paise         bigint CHECK (cost_paise >= 0),
  currency           char(3) NOT NULL DEFAULT 'INR' CHECK (currency = 'INR'),
  vendor             text,
  notes              text,
  invoice_object_key text CHECK (invoice_object_key IS NULL OR is_object_key(invoice_object_key)),
  recorded_by        uuid REFERENCES users (id),
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  CHECK ((status = 'done') = (performed_at IS NOT NULL)),
  CHECK (status <> 'scheduled' OR scheduled_for IS NOT NULL)
);
CREATE INDEX maintenance_records_equipment_idx ON maintenance_records (equipment_id, created_at DESC);
CREATE INDEX maintenance_records_due_idx ON maintenance_records (scheduled_for) WHERE status = 'scheduled';
CREATE TRIGGER maintenance_records_updated_at BEFORE UPDATE ON maintenance_records FOR EACH ROW EXECUTE FUNCTION set_updated_at();
