-- Equipment discovery: privacy-safe public locations, full-text search,
-- PIN code reference data, saved equipment and search history.

-- ---- approximate public location ---------------------------------------------------------
-- Customers never see where a machine is parked. Public search, maps and
-- distances use the centre of the ~1.1 km grid cell holding the machine
-- (0.01 degree), so neither the owner's yard nor home can be located.
CREATE FUNCTION equipment_public_point(p geography) RETURNS geography
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
  SELECT ST_SetSRID(ST_MakePoint(
           floor(ST_X(p::geometry) / 0.01) * 0.01 + 0.005,
           floor(ST_Y(p::geometry) / 0.01) * 0.01 + 0.005), 4326)::geography
$$;

ALTER TABLE equipment
  ADD COLUMN public_location geography(Point, 4326) GENERATED ALWAYS AS (equipment_public_point(base_location)) STORED;

CREATE INDEX equipment_public_location_gix ON equipment USING gist (public_location)
  WHERE status = 'active' AND deleted_at IS NULL;
CREATE INDEX equipment_active_created_idx ON equipment (created_at DESC, id DESC)
  WHERE status = 'active' AND deleted_at IS NULL;
CREATE INDEX equipment_active_rating_idx ON equipment (rating_avg DESC NULLS LAST)
  WHERE status = 'active' AND deleted_at IS NULL;

-- ---- full-text search ---------------------------------------------------------------------
-- 'simple' configuration: machine names, model codes (3DX, PC210) and Indian
-- place names must not be stemmed as English words.
CREATE FUNCTION equipment_search_document(p_title text, p_description text, p_model_id uuid, p_city_id uuid, p_label text)
RETURNS tsvector LANGUAGE sql STABLE AS $$
  SELECT setweight(to_tsvector('simple', coalesce(p_title, '')), 'A')
      || setweight(to_tsvector('simple', coalesce((SELECT mk.name::text || ' ' || m.name
                                                    FROM equipment_models m JOIN equipment_makes mk ON mk.id = m.make_id
                                                    WHERE m.id = p_model_id), '')), 'A')
      || setweight(to_tsvector('simple', coalesce((SELECT concat_ws(' ', c.slug, c.name ->> 'en', c.name ->> 'hi', replace(c.slug, '-', ' '))
                                                    FROM equipment_models m JOIN equipment_categories c ON c.id = m.category_id
                                                    WHERE m.id = p_model_id), '')), 'B')
      || setweight(to_tsvector('simple', concat_ws(' ', p_label, (SELECT ci.name FROM cities ci WHERE ci.id = p_city_id))), 'B')
      || setweight(to_tsvector('simple', coalesce(p_description, '')), 'C')
$$;

ALTER TABLE equipment ADD COLUMN search_document tsvector NOT NULL DEFAULT ''::tsvector;

CREATE FUNCTION equipment_search_document_trg() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.search_document := equipment_search_document(NEW.title, NEW.description, NEW.model_id, NEW.city_id, NEW.location_label);
  RETURN NEW;
END $$;

CREATE TRIGGER equipment_search_document BEFORE INSERT OR UPDATE OF title, description, model_id, city_id, location_label
  ON equipment FOR EACH ROW EXECUTE FUNCTION equipment_search_document_trg();

UPDATE equipment SET search_document = equipment_search_document(title, description, model_id, city_id, location_label);

CREATE INDEX equipment_search_document_gin ON equipment USING gin (search_document);
-- Typo-tolerant fallback (e.g. "excavtor") through trigram similarity.
CREATE INDEX equipment_models_name_trgm ON equipment_models USING gin (name gin_trgm_ops);
CREATE INDEX equipment_makes_name_trgm ON equipment_makes USING gin ((name::text) gin_trgm_ops);

-- Renaming a make, model, category or city refreshes the affected documents.
CREATE FUNCTION equipment_search_refresh_trg() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_TABLE_NAME = 'equipment_makes' THEN
    UPDATE equipment e SET search_document = equipment_search_document(e.title, e.description, e.model_id, e.city_id, e.location_label)
    WHERE e.model_id IN (SELECT id FROM equipment_models WHERE make_id = NEW.id);
  ELSIF TG_TABLE_NAME = 'equipment_models' THEN
    UPDATE equipment e SET search_document = equipment_search_document(e.title, e.description, e.model_id, e.city_id, e.location_label)
    WHERE e.model_id = NEW.id;
  ELSIF TG_TABLE_NAME = 'equipment_categories' THEN
    UPDATE equipment e SET search_document = equipment_search_document(e.title, e.description, e.model_id, e.city_id, e.location_label)
    WHERE e.model_id IN (SELECT id FROM equipment_models WHERE category_id = NEW.id);
  ELSIF TG_TABLE_NAME = 'cities' THEN
    UPDATE equipment e SET search_document = equipment_search_document(e.title, e.description, e.model_id, e.city_id, e.location_label)
    WHERE e.city_id = NEW.id;
  END IF;
  RETURN NULL;
END $$;

CREATE TRIGGER equipment_makes_search AFTER UPDATE OF name ON equipment_makes
  FOR EACH ROW WHEN (OLD.name IS DISTINCT FROM NEW.name) EXECUTE FUNCTION equipment_search_refresh_trg();
CREATE TRIGGER equipment_models_search AFTER UPDATE OF name, make_id, category_id ON equipment_models
  FOR EACH ROW EXECUTE FUNCTION equipment_search_refresh_trg();
CREATE TRIGGER equipment_categories_search AFTER UPDATE OF name, slug ON equipment_categories
  FOR EACH ROW EXECUTE FUNCTION equipment_search_refresh_trg();
CREATE TRIGGER cities_search AFTER UPDATE OF name ON cities
  FOR EACH ROW WHEN (OLD.name IS DISTINCT FROM NEW.name) EXECUTE FUNCTION equipment_search_refresh_trg();

-- ---- cities and PIN codes ------------------------------------------------------------------
-- GST state codes: 07 Delhi, 06 Haryana, 33 Tamil Nadu.
INSERT INTO cities (name, state_code, center, is_live) VALUES
  ('Nashik',   '27', ST_SetSRID(ST_MakePoint(73.7898, 19.9975), 4326)::geography, false),
  ('Nagpur',   '27', ST_SetSRID(ST_MakePoint(79.0882, 21.1458), 4326)::geography, false),
  ('Delhi',    '07', ST_SetSRID(ST_MakePoint(77.2090, 28.6139), 4326)::geography, false),
  ('Gurugram', '06', ST_SetSRID(ST_MakePoint(77.0266, 28.4595), 4326)::geography, false),
  ('Chennai',  '33', ST_SetSRID(ST_MakePoint(80.2707, 13.0827), 4326)::geography, false)
ON CONFLICT (name, state_code) DO NOTHING;

-- Reference centroids of postal areas (public data, approximate).
CREATE TABLE pincodes (
  pincode    char(6) PRIMARY KEY CHECK (pincode ~ '^[1-9][0-9]{5}$'),
  area       text NOT NULL,
  city_id    uuid REFERENCES cities (id),
  state_code char(2) NOT NULL CHECK (state_code ~ '^[0-9]{2}$'),
  center     geography(Point, 4326) NOT NULL
);
CREATE INDEX pincodes_area_trgm ON pincodes USING gin (area gin_trgm_ops);

INSERT INTO pincodes (pincode, area, city_id, state_code, center)
SELECT v.pin, v.area, (SELECT id FROM cities c WHERE c.name = v.city), v.st, ST_SetSRID(ST_MakePoint(v.lng, v.lat), 4326)::geography
FROM (VALUES
  ('411001', 'Pune Camp',            'Pune', '27', 18.5167, 73.8790),
  ('411004', 'Deccan Gymkhana',      'Pune', '27', 18.5167, 73.8406),
  ('411005', 'Shivajinagar',         'Pune', '27', 18.5308, 73.8475),
  ('411014', 'Viman Nagar',          'Pune', '27', 18.5679, 73.9143),
  ('411018', 'Pimpri',               'Pune', '27', 18.6279, 73.8009),
  ('411019', 'Chinchwad',            'Pune', '27', 18.6298, 73.7997),
  ('411028', 'Hadapsar',             'Pune', '27', 18.5089, 73.9260),
  ('411038', 'Kothrud',              'Pune', '27', 18.5074, 73.8077),
  ('411041', 'Dhayari',              'Pune', '27', 18.4500, 73.8200),
  ('411047', 'Lohegaon',             'Pune', '27', 18.5975, 73.9200),
  ('411057', 'Hinjewadi',            'Pune', '27', 18.5912, 73.7389),
  ('412207', 'Wagholi',              'Pune', '27', 18.5808, 73.9787),
  ('410501', 'Chakan',               'Pune', '27', 18.7606, 73.8636),
  ('410507', 'Talegaon Dabhade',     'Pune', '27', 18.7350, 73.6757),
  ('412308', 'Uruli Kanchan',        'Pune', '27', 18.4833, 74.1333),
  ('400001', 'Fort',                 'Mumbai', '27', 18.9338, 72.8356),
  ('400050', 'Bandra West',          'Mumbai', '27', 19.0596, 72.8295),
  ('400053', 'Andheri West',         'Mumbai', '27', 19.1364, 72.8296),
  ('400069', 'Andheri East',         'Mumbai', '27', 19.1136, 72.8697),
  ('400072', 'Saki Naka',            'Mumbai', '27', 19.1030, 72.8880),
  ('400078', 'Bhandup',              'Mumbai', '27', 19.1440, 72.9370),
  ('400080', 'Mulund',               'Mumbai', '27', 19.1726, 72.9425),
  ('400097', 'Malad East',           'Mumbai', '27', 19.1860, 72.8580),
  ('400601', 'Thane',                'Mumbai', '27', 19.1972, 72.9722),
  ('400614', 'CBD Belapur',          'Mumbai', '27', 19.0222, 73.0389),
  ('400705', 'Turbhe',               'Mumbai', '27', 19.0790, 73.0120),
  ('410206', 'Panvel',               'Mumbai', '27', 18.9894, 73.1175),
  ('410208', 'Taloja',               'Mumbai', '27', 19.0607, 73.1214),
  ('421302', 'Bhiwandi',             'Mumbai', '27', 19.2967, 73.0631),
  ('422001', 'Nashik Road',          'Nashik', '27', 19.9975, 73.7898),
  ('422007', 'Satpur MIDC',          'Nashik', '27', 20.0100, 73.7300),
  ('422010', 'Ambad MIDC',           'Nashik', '27', 19.9600, 73.7400),
  ('440001', 'Sitabuldi',            'Nagpur', '27', 21.1458, 79.0882),
  ('440016', 'Hingna MIDC',          'Nagpur', '27', 21.0900, 78.9800),
  ('441108', 'Butibori MIDC',        'Nagpur', '27', 20.9500, 79.0000),
  ('560001', 'MG Road',              'Bengaluru', '29', 12.9716, 77.5946),
  ('560058', 'Peenya',               'Bengaluru', '29', 13.0285, 77.5197),
  ('560100', 'Electronic City',      'Bengaluru', '29', 12.8452, 77.6602),
  ('562123', 'Nelamangala',          'Bengaluru', '29', 13.0985, 77.3934),
  ('500001', 'Abids',                'Hyderabad', '36', 17.3850, 78.4867),
  ('501401', 'Medchal',              'Hyderabad', '36', 17.6297, 78.4814),
  ('501218', 'Shamshabad',           'Hyderabad', '36', 17.2500, 78.4300),
  ('502319', 'Patancheru',           'Hyderabad', '36', 17.5300, 78.2600),
  ('380001', 'Ahmedabad GPO',        'Ahmedabad', '24', 23.0225, 72.5714),
  ('382213', 'Changodar',            'Ahmedabad', '24', 22.9264, 72.4393),
  ('382445', 'Vatva GIDC',           'Ahmedabad', '24', 22.9600, 72.6400),
  ('382110', 'Sanand',               'Ahmedabad', '24', 22.9900, 72.3800),
  ('382330', 'Naroda',               'Ahmedabad', '24', 23.0700, 72.6600),
  ('110001', 'Connaught Place',      'Delhi', '07', 28.6315, 77.2167),
  ('110020', 'Okhla Industrial Area','Delhi', '07', 28.5355, 77.2732),
  ('122001', 'Gurugram',             'Gurugram', '06', 28.4595, 77.0266),
  ('122050', 'Manesar',              'Gurugram', '06', 28.3540, 76.9400),
  ('600001', 'Chennai GPO',          'Chennai', '33', 13.0827, 80.2707),
  ('600058', 'Ambattur',             'Chennai', '33', 13.1143, 80.1548),
  ('602105', 'Sriperumbudur',        'Chennai', '33', 12.9675, 79.9419)
) AS v (pin, area, city, st, lat, lng);

-- ---- saved equipment and search history ----------------------------------------------------
CREATE TABLE saved_equipment (
  user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  equipment_id uuid NOT NULL REFERENCES equipment (id) ON DELETE CASCADE,
  created_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, equipment_id)
);
CREATE INDEX saved_equipment_user_idx ON saved_equipment (user_id, created_at DESC);

-- A signed-in user's recent searches (newest 30 kept by the API). The same
-- search repeated only moves to the top.
CREATE TABLE search_history (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  signature   text NOT NULL,
  label       text NOT NULL CHECK (length(label) BETWEEN 1 AND 200),
  params      jsonb NOT NULL CHECK (jsonb_typeof(params) = 'object'),
  result_count integer NOT NULL DEFAULT 0,
  searched_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, signature)
);
CREATE INDEX search_history_user_idx ON search_history (user_id, searched_at DESC);
