-- Removes the seeded models (and makes) unless equipment references them.
DELETE FROM equipment_models em
USING equipment_makes mk
WHERE em.make_id = mk.id
  AND (mk.name, em.name) IN (
    ('JCB', '3DX Super'), ('CAT', '424'), ('Tata Hitachi', 'EX 200LC'), ('Komatsu', 'PC210'), ('Hyundai', 'R215'),
    ('ACE', 'NX 360'), ('Escorts', 'Hydra 14'), ('L&T', '1190 Vibratory Roller'), ('Schwing Stetter', 'AM 7'),
    ('Ashok Leyland', '2820 Tipper'), ('Volvo', 'G930'))
  AND NOT EXISTS (SELECT 1 FROM equipment e WHERE e.model_id = em.id);

DELETE FROM equipment_makes mk
WHERE mk.name IN ('JCB', 'Tata Hitachi', 'CAT', 'Komatsu', 'ACE', 'Escorts', 'L&T', 'Volvo', 'Schwing Stetter', 'Ashok Leyland', 'Hyundai')
  AND NOT EXISTS (SELECT 1 FROM equipment_models em WHERE em.make_id = mk.id);
