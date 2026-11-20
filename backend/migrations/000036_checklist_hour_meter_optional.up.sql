-- The driver's checklist no longer requires an hour-meter reading or photo.
ALTER TABLE checklists ALTER COLUMN hmr_reading DROP NOT NULL, ALTER COLUMN hmr_photo_object_key DROP NOT NULL;
