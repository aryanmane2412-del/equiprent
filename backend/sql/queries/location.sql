-- Location and tracking: consented sessions, history, hardware trackers,
-- access audit.

-- ---- consent / sessions --------------------------------------------------------------------------

-- name: GetActiveConsent :one
SELECT * FROM tracking_consents WHERE booking_id = @booking_id AND user_id = @user_id AND withdrawn_at IS NULL;

-- name: InsertConsent :one
INSERT INTO tracking_consents (booking_id, user_id, policy_version, background, platform)
VALUES (@booking_id, @user_id, @policy_version, @background, sqlc.narg(platform))
RETURNING *;

-- name: WithdrawConsent :execrows
UPDATE tracking_consents SET withdrawn_at = now() WHERE booking_id = @booking_id AND user_id = @user_id AND withdrawn_at IS NULL;

-- name: StartSessionTrip :one
INSERT INTO trips (booking_id, equipment_id, operator_user_id, kind, started_at, consent_id, source)
VALUES (@booking_id, @equipment_id, sqlc.narg(operator_user_id), @kind, now(), sqlc.narg(consent_id), @source)
RETURNING id, kind, started_at, consent_id, source;

-- name: AttachConsentToTrip :exec
UPDATE trips SET consent_id = @consent_id WHERE id = @id AND ended_at IS NULL AND consent_id IS NULL;

-- name: GetOpenSession :one
SELECT t.id, t.kind, t.started_at, t.consent_id, t.source, t.equipment_id, t.operator_user_id,
       COALESCE((SELECT max(lp.recorded_at) FROM location_pings lp WHERE lp.trip_id = t.id), t.started_at)::timestamptz AS last_ping_at
FROM trips t WHERE t.booking_id = @booking_id AND t.ended_at IS NULL;

-- name: EndSession :one
UPDATE trips
SET ended_at = greatest(now(), started_at), end_reason = @end_reason,
    distance_m = (SELECT COALESCE(ST_Length(ST_MakeLine(lp.location::geometry ORDER BY lp.recorded_at)::geography), 0)::int
                  FROM location_pings lp WHERE lp.trip_id = trips.id)
WHERE trips.booking_id = @booking_id AND trips.ended_at IS NULL
RETURNING id, kind, started_at, ended_at, distance_m, end_reason;

-- name: CloseIdleSessions :many
-- Sessions with no position for the idle window are closed (a forgotten
-- phone must not keep a session open).
UPDATE trips
SET ended_at = greatest(now(), started_at), end_reason = 'idle_timeout',
    distance_m = (SELECT COALESCE(ST_Length(ST_MakeLine(lp.location::geometry ORDER BY lp.recorded_at)::geography), 0)::int
                  FROM location_pings lp WHERE lp.trip_id = trips.id)
WHERE trips.ended_at IS NULL
  AND greatest(trips.started_at, COALESCE((SELECT max(lp.received_at) FROM location_pings lp WHERE lp.trip_id = trips.id), trips.started_at))
      < now() - make_interval(secs => @idle_seconds::float8)
RETURNING id, booking_id;

-- name: InsertSourcedPing :execrows
INSERT INTO location_pings (trip_id, equipment_id, recorded_at, location, accuracy_m, speed_mps, heading, battery_pct, source)
VALUES (@trip_id, @equipment_id, @recorded_at, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography,
        sqlc.narg(accuracy_m), sqlc.narg(speed_mps), sqlc.narg(heading), NULL, @source)
ON CONFLICT DO NOTHING;

-- name: UpsertSourcedLastLocation :exec
INSERT INTO equipment_last_locations (equipment_id, location, recorded_at, source, accuracy_m, booking_id)
VALUES (@equipment_id, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography, @recorded_at, @source,
        sqlc.narg(accuracy_m), sqlc.narg(booking_id))
ON CONFLICT (equipment_id) DO UPDATE
SET location = EXCLUDED.location, recorded_at = EXCLUDED.recorded_at, source = EXCLUDED.source,
    accuracy_m = EXCLUDED.accuracy_m, booking_id = EXCLUDED.booking_id, updated_at = now()
WHERE equipment_last_locations.recorded_at < EXCLUDED.recorded_at;

-- name: GetLastLocation :one
SELECT ST_Y(l.location::geometry)::float8 AS lat, ST_X(l.location::geometry)::float8 AS lng, l.recorded_at, l.source, l.accuracy_m, l.booking_id
FROM equipment_last_locations l WHERE l.equipment_id = @equipment_id;

-- ---- history ---------------------------------------------------------------------------------------

-- name: TripPoints :many
-- A trip's points in time order, thinned to at most @max_points (every
-- n-th point, always keeping the last).
WITH pts AS (
  SELECT lp.recorded_at, ST_Y(lp.location::geometry)::float8 AS lat, ST_X(lp.location::geometry)::float8 AS lng,
         lp.accuracy_m, lp.speed_mps, lp.source,
         row_number() OVER (ORDER BY lp.recorded_at) AS rn, count(*) OVER () AS total
  FROM location_pings lp WHERE lp.trip_id = @trip_id
)
SELECT recorded_at, lat, lng, accuracy_m, speed_mps, source FROM pts
WHERE total <= @max_points::bigint OR (rn - 1) % ceil(total::numeric / @max_points::bigint)::bigint = 0 OR rn = total
ORDER BY recorded_at;

-- name: BookingTripsDetailed :many
SELECT t.id, t.kind, t.started_at, t.ended_at, t.distance_m, t.source, t.end_reason, (t.consent_id IS NOT NULL)::bool AS consented,
       (SELECT count(*) FROM location_pings lp WHERE lp.trip_id = t.id)::int AS points
FROM trips t WHERE t.booking_id = @booking_id ORDER BY t.started_at;

-- ---- hardware trackers -------------------------------------------------------------------------------

-- name: InsertTelematicsDevice :one
INSERT INTO telematics_devices (equipment_id, owner_org_id, provider, external_id, token_sha256, label, created_by)
VALUES (@equipment_id, @owner_org_id, @provider, @external_id, @token_sha256, sqlc.narg(label), @created_by)
RETURNING id, equipment_id, provider, external_id, label, status, last_seen_at, created_at;

-- name: ListTelematicsDevices :many
SELECT id, equipment_id, provider, external_id, label, status, last_seen_at, created_at
FROM telematics_devices WHERE equipment_id = @equipment_id ORDER BY created_at DESC;

-- name: RevokeTelematicsDevice :execrows
UPDATE telematics_devices SET status = 'revoked', revoked_at = now() WHERE id = @id AND equipment_id = @equipment_id AND status = 'active';

-- name: DeviceByToken :one
SELECT d.id, d.equipment_id, d.owner_org_id, d.provider, d.external_id
FROM telematics_devices d WHERE d.token_sha256 = @token_sha256 AND d.status = 'active';

-- name: TouchDevice :exec
UPDATE telematics_devices SET last_seen_at = now() WHERE id = @id;

-- name: OpenTripForEquipment :one
SELECT t.id, t.booking_id FROM trips t WHERE t.equipment_id = @equipment_id AND t.ended_at IS NULL;

-- ---- access audit ---------------------------------------------------------------------------------------

-- name: LogLocationAccess :exec
INSERT INTO location_access_log (user_id, subject_type, subject_id, access_kind, hour)
VALUES (@user_id, @subject_type, @subject_id, @access_kind, date_trunc('hour', now()))
ON CONFLICT (user_id, subject_type, subject_id, access_kind, hour)
DO UPDATE SET count = location_access_log.count + 1, last_at = now();

-- name: ListLocationAccess :many
SELECT l.user_id, COALESCE(u.full_name, '')::text AS user_name, l.subject_type, l.subject_id, l.access_kind, l.hour,
       l.first_at, l.last_at, l.count
FROM location_access_log l LEFT JOIN users u ON u.id = l.user_id
WHERE (sqlc.narg(subject_id)::uuid IS NULL OR l.subject_id = sqlc.narg(subject_id)::uuid)
  AND (sqlc.narg(user_id)::uuid IS NULL OR l.user_id = sqlc.narg(user_id)::uuid)
ORDER BY l.hour DESC, l.last_at DESC
LIMIT @lim;

-- name: LatestPhoneFix :one
-- The newest phone (app/simulated) fix of a trip: where the operator's
-- device is.
SELECT ST_Y(lp.location::geometry)::float8 AS lat, ST_X(lp.location::geometry)::float8 AS lng, lp.recorded_at, lp.accuracy_m, lp.source
FROM location_pings lp WHERE lp.trip_id = @trip_id AND lp.source IN ('app', 'simulated')
ORDER BY lp.recorded_at DESC LIMIT 1;
