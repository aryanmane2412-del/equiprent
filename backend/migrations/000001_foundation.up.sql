-- Foundation: extensions and shared helpers used by every later migration.
-- No business tables yet (see docs/DATABASE_DESIGN.md).
--
-- PostGIS is intentionally NOT enabled here. It is enabled by the first
-- migration that adds geo columns (catalogue / bookings), which requires the
-- postgis/postgis image from docker-compose.yml or a PostGIS-enabled server.

CREATE EXTENSION IF NOT EXISTS citext;     -- case-insensitive email
CREATE EXTENSION IF NOT EXISTS pg_trgm;    -- fuzzy search on titles
CREATE EXTENSION IF NOT EXISTS btree_gist; -- exclusion constraints (no double booking)

-- Maintains updated_at on any table that attaches this trigger:
--   CREATE TRIGGER set_updated_at BEFORE UPDATE ON <table>
--     FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at := now();
  RETURN NEW;
END;
$$;
