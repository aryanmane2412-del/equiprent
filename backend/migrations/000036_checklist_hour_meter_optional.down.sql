UPDATE checklists SET hmr_reading = 0 WHERE hmr_reading IS NULL;
UPDATE checklists SET hmr_photo_object_key = 'none/removed' WHERE hmr_photo_object_key IS NULL;
ALTER TABLE checklists ALTER COLUMN hmr_reading SET NOT NULL, ALTER COLUMN hmr_photo_object_key SET NOT NULL;
