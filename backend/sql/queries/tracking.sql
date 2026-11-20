-- name: StartTrip :one
INSERT INTO trips (booking_id, equipment_id, operator_user_id, kind, started_at)
VALUES (@booking_id, @equipment_id, sqlc.narg(operator_user_id), @kind, @started_at)
RETURNING id, booking_id, equipment_id, kind, started_at, ended_at, distance_m;

-- name: GetOpenTripForBooking :one
SELECT id, booking_id, equipment_id, kind, started_at, ended_at, distance_m
FROM trips WHERE booking_id = @booking_id AND ended_at IS NULL;

-- name: EndTrip :one
UPDATE trips
SET ended_at = @ended_at,
    distance_m = (SELECT COALESCE(ST_Length(ST_MakeLine(lp.location::geometry ORDER BY lp.recorded_at)::geography), 0)::int
                  FROM location_pings lp WHERE lp.trip_id = trips.id)
WHERE trips.id = @id AND trips.ended_at IS NULL
RETURNING id, booking_id, equipment_id, kind, started_at, ended_at, distance_m;

-- name: InsertLocationPing :execrows
INSERT INTO location_pings (trip_id, equipment_id, recorded_at, location, accuracy_m, speed_mps, heading, battery_pct, source)
VALUES (@trip_id, @equipment_id, @recorded_at, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography,
        sqlc.narg(accuracy_m), sqlc.narg(speed_mps), sqlc.narg(heading), sqlc.narg(battery_pct), 'app')
ON CONFLICT DO NOTHING;

-- name: UpsertLastLocation :exec
INSERT INTO equipment_last_locations (equipment_id, location, recorded_at)
VALUES (@equipment_id, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography, @recorded_at)
ON CONFLICT (equipment_id) DO UPDATE
SET location = EXCLUDED.location, recorded_at = EXCLUDED.recorded_at, updated_at = now()
WHERE equipment_last_locations.recorded_at < EXCLUDED.recorded_at;

-- name: DistanceToSite :one
SELECT ST_Distance(b.site_location, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography)::float8 AS meters,
       b.site_geofence_m
FROM bookings b WHERE b.id = @booking_id;

-- name: InsertGeofenceEvent :exec
INSERT INTO geofence_events (booking_id, event, occurred_at, location)
VALUES (@booking_id, @event, @occurred_at, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography);

-- name: HasGeofenceEvent :one
SELECT EXISTS (SELECT 1 FROM geofence_events WHERE booking_id = @booking_id AND event = @event) AS ok;

-- name: GetTrackingSnapshot :one
SELECT b.id AS booking_id, b.status, b.equipment_id,
       ST_Y(b.site_location::geometry)::float8 AS site_lat, ST_X(b.site_location::geometry)::float8 AS site_lng,
       COALESCE(ST_Y(l.location::geometry), 0)::float8 AS lat, COALESCE(ST_X(l.location::geometry), 0)::float8 AS lng,
       l.recorded_at,
       COALESCE(ST_Distance(l.location, b.site_location), 0)::float8 AS meters_to_site,
       (l.equipment_id IS NOT NULL)::bool AS has_position
FROM bookings b
LEFT JOIN equipment_last_locations l ON l.equipment_id = b.equipment_id
WHERE b.id = @booking_id;

-- name: ListBookingTrips :many
SELECT id, booking_id, equipment_id, kind, started_at, ended_at, distance_m
FROM trips WHERE booking_id = @booking_id ORDER BY started_at;

-- name: EnsureLocationPingPartitions :exec
SELECT ensure_location_ping_partitions(@from_date::date, @months::int);
