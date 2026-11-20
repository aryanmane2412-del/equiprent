DROP TABLE IF EXISTS issue_reports;
DROP TYPE IF EXISTS issue_status;

ALTER TABLE checklists DROP COLUMN IF EXISTS photo_upload_ids, DROP COLUMN IF EXISTS notes;
ALTER TABLE uploads DROP CONSTRAINT IF EXISTS uploads_purpose_check;
ALTER TABLE uploads ADD CONSTRAINT uploads_purpose_check
  CHECK (purpose IN ('identity_document', 'business_document', 'operator_document', 'avatar', 'equipment_photo',
                     'equipment_document', 'chat_image', 'maintenance_document', 'agreement_document')) NOT VALID;

DROP VIEW IF EXISTS fleet_manager_scope;
DROP FUNCTION IF EXISTS is_restricted(uuid, text);
DROP TABLE IF EXISTS account_restrictions;
DROP TABLE IF EXISTS fleet_managers;
DROP TABLE IF EXISTS fleet_equipment;
DROP FUNCTION IF EXISTS fleet_membership_guard();
DROP TABLE IF EXISTS fleets;
