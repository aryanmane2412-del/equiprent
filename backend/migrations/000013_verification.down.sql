DROP TABLE IF EXISTS fleet_manager_equipment;
ALTER TABLE fleet_manager_profiles DROP COLUMN IF EXISTS all_equipment;
DROP TABLE IF EXISTS operator_qualifications;
ALTER TABLE operator_profiles DROP COLUMN IF EXISTS availability;
ALTER TABLE organizations DROP COLUMN IF EXISTS contact_name, DROP COLUMN IF EXISTS contact_phone, DROP COLUMN IF EXISTS contact_email;
DROP TABLE IF EXISTS verification_events;
DROP TABLE IF EXISTS verification_applications;
DROP TYPE IF EXISTS application_status;
DROP TRIGGER IF EXISTS kyc_records_audit ON kyc_records;
ALTER TABLE kyc_records DROP COLUMN IF EXISTS context, DROP COLUMN IF EXISTS content_type, DROP COLUMN IF EXISTS upload_id;
DROP TABLE IF EXISTS uploads;
-- Values added to kyc_document_type cannot be removed in PostgreSQL; unused
-- values are harmless.
