DROP TABLE IF EXISTS maintenance_records;
DROP TYPE IF EXISTS maintenance_status;
DROP TABLE IF EXISTS maintenance_plans;
DROP TABLE IF EXISTS equipment_last_locations;
DROP TABLE IF EXISTS geofence_events;
DROP TYPE IF EXISTS geofence_event_type;
DROP TABLE IF EXISTS location_pings CASCADE;  -- drops all partitions
DROP FUNCTION IF EXISTS ensure_location_ping_partitions(date, integer);
DROP TABLE IF EXISTS trips;
DROP TYPE IF EXISTS trip_kind;
