DELETE FROM uploads WHERE purpose IN ('equipment_photo', 'equipment_document');
ALTER TABLE uploads DROP CONSTRAINT IF EXISTS uploads_purpose_check;
ALTER TABLE uploads ADD CONSTRAINT uploads_purpose_check
  CHECK (purpose IN ('identity_document', 'business_document', 'operator_document', 'avatar'));

DELETE FROM equipment_documents WHERE content_type = 'image/webp';
ALTER TABLE equipment_documents DROP CONSTRAINT IF EXISTS equipment_documents_content_type_check;
ALTER TABLE equipment_documents ADD CONSTRAINT equipment_documents_content_type_check
  CHECK (content_type IN ('application/pdf', 'image/jpeg', 'image/png'));

-- Enum values cannot be dropped; removed listings become suspended.
UPDATE equipment SET status = 'suspended' WHERE status = 'removed';

ALTER TABLE equipment
  DROP COLUMN IF EXISTS moderated_at,
  DROP COLUMN IF EXISTS moderated_by,
  DROP COLUMN IF EXISTS moderation_note,
  DROP COLUMN IF EXISTS location_label,
  DROP COLUMN IF EXISTS delivery_available,
  DROP COLUMN IF EXISTS condition;
DROP TYPE IF EXISTS equipment_condition;

-- Models and makes added by the up migration (kept if listings use them).
DELETE FROM equipment_models m USING equipment_makes mk
WHERE m.make_id = mk.id AND (mk.name, m.name) IN (
  ('Liugong', '835H'), ('SDLG', 'LG936L'), ('JCB', '432ZX'), ('Godrej', 'GX 300 Diesel'), ('Godrej', 'UNO 25 Electric'),
  ('Mahindra', 'Arjun 605 DI'), ('Sonalika', 'DI 750 III'), ('Swaraj', '855 FE'), ('CAT', 'D6R'), ('Dynapac', 'CA2500D'),
  ('BharatBenz', '2828C'), ('ACE', 'NX 360'))
  AND NOT EXISTS (SELECT 1 FROM equipment e WHERE e.model_id = m.id);
DELETE FROM equipment_makes mk WHERE mk.name IN ('Liugong', 'SDLG', 'Godrej', 'Mahindra', 'Sonalika', 'Swaraj', 'Dynapac', 'BharatBenz')
  AND NOT EXISTS (SELECT 1 FROM equipment_models m WHERE m.make_id = mk.id);

UPDATE equipment_categories SET is_active = true WHERE slug = 'telehandler';
UPDATE equipment_models SET category_id = (SELECT id FROM equipment_categories WHERE slug = 'telehandler')
WHERE category_id = (SELECT id FROM equipment_categories WHERE slug = 'forklift')
  AND NOT EXISTS (SELECT 1 FROM equipment e WHERE e.model_id = equipment_models.id);
DELETE FROM equipment_models m USING equipment_categories c
WHERE m.category_id = c.id AND c.slug IN ('wheel-loader', 'forklift', 'tractor', 'other')
  AND NOT EXISTS (SELECT 1 FROM equipment e WHERE e.model_id = m.id);
DELETE FROM equipment_categories c WHERE c.slug IN ('wheel-loader', 'forklift', 'tractor', 'other')
  AND NOT EXISTS (SELECT 1 FROM equipment_models m WHERE m.category_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM operator_skills s WHERE s.category_id = c.id);

UPDATE equipment_categories SET slug = 'dozer-grader', name = '{"en": "Dozers & Graders", "hi": "डोज़र"}' WHERE slug = 'bulldozer';
UPDATE equipment_categories SET slug = 'tipper', name = '{"en": "Tippers & Dumpers", "hi": "टिपर"}' WHERE slug = 'dump-truck';
UPDATE equipment_categories SET slug = 'roller', name = '{"en": "Rollers & Compactors", "hi": "रोलर"}' WHERE slug = 'road-roller';
UPDATE equipment_categories SET slug = 'transit-mixer', name = '{"en": "Transit Mixers", "hi": "ट्रांजिट मिक्सर"}' WHERE slug = 'concrete-mixer';
UPDATE equipment_categories SET name = '{"en": "Backhoe Loaders", "hi": "बैकहो लोडर"}' WHERE slug = 'backhoe-loader';
UPDATE equipment_categories SET name = '{"en": "Excavators", "hi": "एक्सकेवेटर"}' WHERE slug = 'excavator';
UPDATE equipment_categories SET name = '{"en": "Cranes", "hi": "क्रेन"}' WHERE slug = 'crane';
