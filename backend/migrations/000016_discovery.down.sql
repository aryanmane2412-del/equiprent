DROP TABLE IF EXISTS search_history;
DROP TABLE IF EXISTS saved_equipment;
DROP TABLE IF EXISTS pincodes;
DELETE FROM cities c WHERE c.name IN ('Nashik', 'Nagpur', 'Delhi', 'Gurugram', 'Chennai')
  AND NOT EXISTS (SELECT 1 FROM equipment e WHERE e.city_id = c.id);

DROP TRIGGER IF EXISTS cities_search ON cities;
DROP TRIGGER IF EXISTS equipment_categories_search ON equipment_categories;
DROP TRIGGER IF EXISTS equipment_models_search ON equipment_models;
DROP TRIGGER IF EXISTS equipment_makes_search ON equipment_makes;
DROP FUNCTION IF EXISTS equipment_search_refresh_trg();
DROP INDEX IF EXISTS equipment_makes_name_trgm;
DROP INDEX IF EXISTS equipment_models_name_trgm;
DROP TRIGGER IF EXISTS equipment_search_document ON equipment;
DROP FUNCTION IF EXISTS equipment_search_document_trg();
ALTER TABLE equipment DROP COLUMN IF EXISTS search_document;
DROP FUNCTION IF EXISTS equipment_search_document(text, text, uuid, uuid, text);

DROP INDEX IF EXISTS equipment_active_rating_idx;
DROP INDEX IF EXISTS equipment_active_created_idx;
ALTER TABLE equipment DROP COLUMN IF EXISTS public_location;
DROP FUNCTION IF EXISTS equipment_public_point(geography);
