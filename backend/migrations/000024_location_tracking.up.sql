-- Location and tracking: consented tracking sessions (trips), hardware
-- tracker devices, simulated sources, and an audit of who looked at
-- locations.
--
-- A phone's GPS fix is where the PHONE is (normally the operator's,
-- travelling with the machine); only a telematics device fitted to the
-- machine reports the machine itself. Every ping records its source.

-- ---- sources ---------------------------------------------------------------------------------------
ALTER TABLE location_pings DROP CONSTRAINT IF EXISTS location_pings_source_check;
ALTER TABLE location_pings ADD CONSTRAINT location_pings_source_check CHECK (source IN ('app', 'telematics', 'simulated'));

ALTER TABLE equipment_last_locations
  ADD COLUMN source     text NOT NULL DEFAULT 'app' CHECK (source IN ('app', 'telematics', 'simulated')),
  ADD COLUMN accuracy_m real CHECK (accuracy_m IS NULL OR accuracy_m >= 0),
  ADD COLUMN booking_id uuid REFERENCES bookings (id);

-- ---- consent -----------------------------------------------------------------------------------------
-- A person agrees to share their device's location for one booking, after
-- reading the tracking notice (policy_version). Withdrawing ends the session.
CREATE TABLE tracking_consents (
  id             uuid PRIMARY KEY DEFAULT uuidv7(),
  booking_id     uuid NOT NULL REFERENCES bookings (id),
  user_id        uuid NOT NULL REFERENCES users (id),
  policy_version text NOT NULL CHECK (length(policy_version) BETWEEN 1 AND 40),
  background     boolean NOT NULL DEFAULT false,
  platform       text CHECK (platform IS NULL OR platform IN ('android', 'ios', 'web', 'other')),
  granted_at     timestamptz NOT NULL DEFAULT now(),
  withdrawn_at   timestamptz,
  CHECK (withdrawn_at IS NULL OR withdrawn_at >= granted_at)
);
CREATE UNIQUE INDEX tracking_consents_active_uq ON tracking_consents (booking_id, user_id) WHERE withdrawn_at IS NULL;
CREATE TRIGGER tracking_consents_audit AFTER INSERT OR UPDATE ON tracking_consents FOR EACH ROW EXECUTE FUNCTION audit_row_change();

-- Trips are the tracking sessions: who started them, under which consent,
-- from which source, and why they ended.
ALTER TABLE trips
  ADD COLUMN consent_id uuid REFERENCES tracking_consents (id),
  ADD COLUMN source     text NOT NULL DEFAULT 'app' CHECK (source IN ('app', 'telematics', 'simulated')),
  ADD COLUMN end_reason text CHECK (end_reason IS NULL OR end_reason IN
    ('arrived', 'stopped_by_user', 'session_ended', 'consent_withdrawn', 'idle_timeout', 'returned', 'cancelled'));

-- ---- hardware trackers ---------------------------------------------------------------------------------
-- A GPS tracker or telematics unit fitted to a machine. It authenticates
-- with a device token (only its SHA-256 is stored, shown once at
-- registration). Providers adapt vendor payloads to one position format.
CREATE TABLE telematics_devices (
  id            uuid PRIMARY KEY DEFAULT uuidv7(),
  equipment_id  uuid NOT NULL REFERENCES equipment (id),
  owner_org_id  uuid NOT NULL REFERENCES organizations (id),
  provider      text NOT NULL CHECK (provider ~ '^[a-z0-9_]{2,30}$'),
  external_id   text NOT NULL CHECK (length(external_id) BETWEEN 1 AND 120),
  token_sha256  bytea NOT NULL UNIQUE CHECK (length(token_sha256) = 32),
  label         text CHECK (label IS NULL OR length(label) <= 80),
  status        text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
  last_seen_at  timestamptz,
  created_by    uuid REFERENCES users (id),
  created_at    timestamptz NOT NULL DEFAULT now(),
  revoked_at    timestamptz,
  UNIQUE (provider, external_id)
);
CREATE UNIQUE INDEX telematics_devices_one_active_uq ON telematics_devices (equipment_id) WHERE status = 'active';
CREATE TRIGGER telematics_devices_audit AFTER INSERT OR UPDATE ON telematics_devices FOR EACH ROW EXECUTE FUNCTION audit_row_change();

-- ---- location access audit ------------------------------------------------------------------------------
-- Who looked at a location, aggregated per hour (live views poll often):
-- first and last access and how many times.
CREATE TABLE location_access_log (
  user_id      uuid NOT NULL,
  subject_type text NOT NULL CHECK (subject_type IN ('booking', 'equipment', 'fleet')),
  subject_id   uuid NOT NULL,
  access_kind  text NOT NULL CHECK (access_kind IN ('live', 'history', 'stream', 'fleet_live')),
  hour         timestamptz NOT NULL,
  first_at     timestamptz NOT NULL DEFAULT now(),
  last_at      timestamptz NOT NULL DEFAULT now(),
  count        integer NOT NULL DEFAULT 1 CHECK (count >= 1),
  PRIMARY KEY (user_id, subject_type, subject_id, access_kind, hour)
);
CREATE INDEX location_access_log_subject_idx ON location_access_log (subject_type, subject_id, hour DESC);

-- ---- sessions stop with the booking ----------------------------------------------------------------
-- Location is shared from dispatch until the machine is handed back. When
-- a booking leaves those stages by any path (return, cancellation, admin
-- action), its open sessions end and consents lapse in the same transaction.
CREATE OR REPLACE FUNCTION bookings_stop_tracking() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.status IS DISTINCT FROM OLD.status
     AND NEW.status::text NOT IN ('dispatched', 'on_site', 'active', 'extension_requested', 'cancellation_requested', 'return_initiated') THEN
    UPDATE trips
       SET ended_at = greatest(now(), started_at),
           end_reason = CASE WHEN NEW.status::text = 'returned' THEN 'returned'
                             WHEN NEW.status::text IN ('cancelled', 'refund_pending', 'refunded') THEN 'cancelled'
                             ELSE 'session_ended' END,
           distance_m = (SELECT COALESCE(ST_Length(ST_MakeLine(lp.location::geometry ORDER BY lp.recorded_at)::geography), 0)::int
                           FROM location_pings lp WHERE lp.trip_id = trips.id)
     WHERE booking_id = NEW.id AND ended_at IS NULL;
    UPDATE tracking_consents SET withdrawn_at = now() WHERE booking_id = NEW.id AND withdrawn_at IS NULL;
  END IF;
  RETURN NULL;
END;
$$;
CREATE TRIGGER bookings_stop_tracking AFTER UPDATE OF status ON bookings FOR EACH ROW EXECUTE FUNCTION bookings_stop_tracking();
