DROP TABLE IF EXISTS driver_compensation_events;
DROP TABLE IF EXISTS driver_compensations;
DROP FUNCTION IF EXISTS record_driver_compensation_event();
ALTER TABLE operator_profiles DROP CONSTRAINT IF EXISTS operator_home_address_complete_ck,
  DROP COLUMN IF EXISTS home_address_line1, DROP COLUMN IF EXISTS home_address_line2, DROP COLUMN IF EXISTS home_address_locality,
  DROP COLUMN IF EXISTS home_address_city, DROP COLUMN IF EXISTS home_address_district, DROP COLUMN IF EXISTS home_address_state_code,
  DROP COLUMN IF EXISTS home_address_pincode, DROP COLUMN IF EXISTS identity_consent_at;
