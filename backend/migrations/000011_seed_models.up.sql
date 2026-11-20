-- Common makes and models in the Indian construction-equipment market, so
-- owners can list machines out of the box. Admins add more through
-- POST /api/v1/admin/catalogue/models.

INSERT INTO equipment_makes (name) VALUES
  ('JCB'), ('Tata Hitachi'), ('CAT'), ('Komatsu'), ('ACE'), ('Escorts'),
  ('L&T'), ('Volvo'), ('Schwing Stetter'), ('Ashok Leyland'), ('Hyundai')
ON CONFLICT (name) DO NOTHING;

INSERT INTO equipment_models (category_id, make_id, name, default_specs)
SELECT c.id, m.id, v.model, v.specs::jsonb
FROM (VALUES
  ('backhoe-loader', 'JCB',             '3DX Super',   '{"operating_weight_kg": 8070, "engine_hp": 76}'),
  ('backhoe-loader', 'CAT',             '424',         '{"operating_weight_kg": 7360, "engine_hp": 74}'),
  ('excavator',      'Tata Hitachi',    'EX 200LC',    '{"operating_weight_kg": 20000, "bucket_m3": 0.9}'),
  ('excavator',      'Komatsu',         'PC210',       '{"operating_weight_kg": 21000, "bucket_m3": 1.0}'),
  ('excavator',      'Hyundai',         'R215',        '{"operating_weight_kg": 21500, "bucket_m3": 0.92}'),
  ('crane',          'ACE',             'NX 360',      '{"capacity_t": 14}'),
  ('crane',          'Escorts',         'Hydra 14',    '{"capacity_t": 14}'),
  ('roller',         'L&T',             '1190 Vibratory Roller', '{"operating_weight_kg": 11000}'),
  ('transit-mixer',  'Schwing Stetter', 'AM 7',        '{"drum_m3": 7}'),
  ('tipper',         'Ashok Leyland',   '2820 Tipper', '{"payload_t": 18}'),
  ('dozer-grader',   'Volvo',           'G930',        '{"blade_m": 3.7}')
) AS v(category, make, model, specs)
JOIN equipment_categories c ON c.slug = v.category
JOIN equipment_makes m ON m.name = v.make
ON CONFLICT (make_id, name) DO NOTHING;
