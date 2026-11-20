DROP TRIGGER IF EXISTS bookings_stop_tracking ON bookings;
DROP FUNCTION IF EXISTS bookings_stop_tracking();
DROP TABLE IF EXISTS location_access_log;
DROP TABLE IF EXISTS telematics_devices;
ALTER TABLE trips DROP COLUMN IF EXISTS end_reason, DROP COLUMN IF EXISTS source, DROP COLUMN IF EXISTS consent_id;
DROP TABLE IF EXISTS tracking_consents;
ALTER TABLE equipment_last_locations DROP COLUMN IF EXISTS booking_id, DROP COLUMN IF EXISTS accuracy_m, DROP COLUMN IF EXISTS source;
ALTER TABLE location_pings DROP CONSTRAINT IF EXISTS location_pings_source_check;
ALTER TABLE location_pings ADD CONSTRAINT location_pings_source_check CHECK (source IN ('app', 'telematics')) NOT VALID;
