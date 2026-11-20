-- Equipment listing system: the marketplace category set, listing fields
-- owners fill in (condition, delivery, location label), moderation notes and
-- removal of prohibited listings, and upload purposes for listing media.

-- ---- categories -------------------------------------------------------------------
-- Existing slugs are renamed in place so models, listings and operator
-- skills keep pointing at the same rows.
UPDATE equipment_categories SET name = '{"en": "JCB / Backhoe Loader", "hi": "जेसीबी / बैकहो लोडर"}', sort_order = 10 WHERE slug = 'backhoe-loader';
UPDATE equipment_categories SET name = '{"en": "Excavator", "hi": "एक्सकेवेटर"}', sort_order = 20 WHERE slug = 'excavator';
UPDATE equipment_categories SET slug = 'bulldozer', name = '{"en": "Bulldozer", "hi": "बुलडोज़र"}', sort_order = 30,
  spec_schema = '{"operating_weight_kg": "number", "blade_width_m": "number", "engine_hp": "number"}' WHERE slug = 'dozer-grader';
UPDATE equipment_categories SET name = '{"en": "Crane", "hi": "क्रेन"}', sort_order = 40 WHERE slug = 'crane';
UPDATE equipment_categories SET slug = 'dump-truck', name = '{"en": "Dump Truck", "hi": "डंप ट्रक"}', sort_order = 50 WHERE slug = 'tipper';
UPDATE equipment_categories SET slug = 'road-roller', name = '{"en": "Road Roller", "hi": "रोड रोलर"}', sort_order = 70 WHERE slug = 'roller';
UPDATE equipment_categories SET slug = 'concrete-mixer', name = '{"en": "Concrete Mixer", "hi": "कंक्रीट मिक्सर"}', sort_order = 90 WHERE slug = 'transit-mixer';

INSERT INTO equipment_categories (slug, name, sort_order, spec_schema) VALUES
  ('wheel-loader', '{"en": "Wheel Loader", "hi": "व्हील लोडर"}', 60, '{"operating_weight_kg": "number", "bucket_m3": "number", "engine_hp": "number"}'),
  ('forklift',     '{"en": "Forklift", "hi": "फोर्कलिफ्ट"}', 80, '{"max_capacity_kg": "number", "lift_height_m": "number", "fuel": "text"}'),
  ('tractor',      '{"en": "Tractor", "hi": "ट्रैक्टर"}', 100, '{"engine_hp": "number", "drive": "text"}'),
  ('other',        '{"en": "Other machinery", "hi": "अन्य मशीनरी"}', 1000, '{}')
ON CONFLICT (slug) DO NOTHING;

-- Telehandlers are telescopic forklifts: their models move to Forklift and
-- the old category is hidden (admins can re-enable it).
UPDATE equipment_models SET category_id = (SELECT id FROM equipment_categories WHERE slug = 'forklift')
WHERE category_id = (SELECT id FROM equipment_categories WHERE slug = 'telehandler');
UPDATE operator_skills SET category_id = (SELECT id FROM equipment_categories WHERE slug = 'forklift')
WHERE category_id = (SELECT id FROM equipment_categories WHERE slug = 'telehandler')
  AND NOT EXISTS (SELECT 1 FROM operator_skills s2 WHERE s2.operator_user_id = operator_skills.operator_user_id
                  AND s2.category_id = (SELECT id FROM equipment_categories WHERE slug = 'forklift'));
UPDATE equipment_categories SET is_active = false WHERE slug = 'telehandler';

-- Common makes/models for the new categories.
INSERT INTO equipment_makes (name) VALUES ('Liugong'), ('SDLG'), ('Godrej'), ('Mahindra'), ('Sonalika'), ('Swaraj'), ('Dynapac'), ('BharatBenz')
ON CONFLICT (name) DO NOTHING;
INSERT INTO equipment_models (category_id, make_id, name, default_specs)
SELECT c.id, m.id, v.model, v.specs::jsonb
FROM (VALUES
  ('wheel-loader', 'Liugong',    '835H',          '{"operating_weight_kg": 10800, "bucket_m3": 1.8, "engine_hp": 125}'),
  ('wheel-loader', 'SDLG',       'LG936L',        '{"operating_weight_kg": 10600, "bucket_m3": 1.8, "engine_hp": 125}'),
  ('wheel-loader', 'JCB',        '432ZX',         '{"operating_weight_kg": 11400, "bucket_m3": 2.0, "engine_hp": 130}'),
  ('forklift',     'Godrej',     'GX 300 Diesel', '{"max_capacity_kg": 3000, "lift_height_m": 3.0, "fuel": "diesel"}'),
  ('forklift',     'Godrej',     'UNO 25 Electric', '{"max_capacity_kg": 2500, "lift_height_m": 4.5, "fuel": "electric"}'),
  ('tractor',      'Mahindra',   'Arjun 605 DI',  '{"engine_hp": 57, "drive": "2WD"}'),
  ('tractor',      'Sonalika',   'DI 750 III',    '{"engine_hp": 55, "drive": "2WD"}'),
  ('tractor',      'Swaraj',     '855 FE',        '{"engine_hp": 52, "drive": "2WD"}'),
  ('bulldozer',    'CAT',        'D6R',           '{"operating_weight_kg": 18500, "blade_width_m": 3.3, "engine_hp": 200}'),
  ('road-roller',  'Dynapac',    'CA2500D',       '{"operating_weight_kg": 11000, "drum_width_mm": 2130}'),
  ('dump-truck',   'BharatBenz', '2828C',         '{"payload_t": 25, "body_volume_m3": 16}'),
  ('crane',        'ACE',        'NX 360',        '{"max_capacity_t": 14, "boom_length_m": 17.5, "lift_height_m": 18}')
) AS v (category, make, model, specs)
JOIN equipment_categories c ON c.slug = v.category
JOIN equipment_makes m ON m.name = v.make
ON CONFLICT (make_id, name) DO NOTHING;

-- ---- listing fields ---------------------------------------------------------------
CREATE TYPE equipment_condition AS ENUM ('excellent', 'good', 'fair', 'needs_repair');

ALTER TABLE equipment
  ADD COLUMN condition          equipment_condition NOT NULL DEFAULT 'good',
  ADD COLUMN delivery_available boolean NOT NULL DEFAULT true,
  ADD COLUMN location_label     text CHECK (location_label IS NULL OR length(btrim(location_label)) BETWEEN 2 AND 120),
  ADD COLUMN moderation_note    text CHECK (moderation_note IS NULL OR length(moderation_note) <= 1000),
  ADD COLUMN moderated_by       uuid REFERENCES users (id),
  ADD COLUMN moderated_at       timestamptz;

-- Listings removed by an admin (prohibited or fraudulent) cannot come back.
ALTER TYPE listing_status ADD VALUE IF NOT EXISTS 'removed';

-- Photos: who and in which order; documents may be WebP images too.
ALTER TABLE equipment_documents DROP CONSTRAINT IF EXISTS equipment_documents_content_type_check;
ALTER TABLE equipment_documents ADD CONSTRAINT equipment_documents_content_type_check
  CHECK (content_type IN ('application/pdf', 'image/jpeg', 'image/png', 'image/webp'));

-- ---- uploads -------------------------------------------------------------------------
ALTER TABLE uploads DROP CONSTRAINT IF EXISTS uploads_purpose_check;
ALTER TABLE uploads ADD CONSTRAINT uploads_purpose_check
  CHECK (purpose IN ('identity_document', 'business_document', 'operator_document', 'avatar', 'equipment_photo', 'equipment_document'));
