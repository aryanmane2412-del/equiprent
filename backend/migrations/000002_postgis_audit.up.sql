-- PostGIS, platform-wide integrity helpers and the audit log.
--
-- Custom SQLSTATEs raised by EQUIPRENT triggers (mapped to API errors in Go):
--   EQ001  modification of an append-only / immutable row
--   EQ002  invalid booking status transition
--   EQ003  unbalanced ledger transaction
--   EQ004  business rule violation (see message)

CREATE EXTENSION IF NOT EXISTS postgis;

-- Raises for UPDATE/DELETE on append-only tables (ledger, events, audit).
CREATE OR REPLACE FUNCTION forbid_modification() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION '% on % is not allowed (append-only)', TG_OP, TG_TABLE_NAME
    USING ERRCODE = 'EQ001';
END;
$$;

-- Generic audit log. Rows are written by audit_row_change() triggers on
-- sensitive tables and by the application for admin actions. Never updated.
CREATE TABLE audit_logs (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  occurred_at   timestamptz NOT NULL DEFAULT now(),
  actor_user_id uuid,           -- no FK: audit rows must survive user erasure
  actor_role    text,
  action        text NOT NULL CHECK (action ~ '^[a-z_.]+$'),
  entity_type   text NOT NULL,
  entity_id     uuid,
  before        jsonb,
  after         jsonb,
  reason        text,
  request_id    text,
  ip            inet
);
CREATE INDEX audit_logs_entity_idx ON audit_logs (entity_type, entity_id, occurred_at DESC);
CREATE INDEX audit_logs_actor_idx ON audit_logs (actor_user_id, occurred_at DESC) WHERE actor_user_id IS NOT NULL;
CREATE INDEX audit_logs_time_brin ON audit_logs USING brin (occurred_at);

CREATE TRIGGER audit_logs_append_only
  BEFORE UPDATE OR DELETE ON audit_logs
  FOR EACH ROW EXECUTE FUNCTION forbid_modification();

-- Row-change auditing. The application identifies the actor per transaction:
--   SET LOCAL app.actor_user_id = '<uuid>'; SET LOCAL app.request_id = '...';
--   SET LOCAL app.reason = '...';
CREATE OR REPLACE FUNCTION audit_row_change() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  v_actor uuid := nullif(current_setting('app.actor_user_id', true), '')::uuid;
BEGIN
  INSERT INTO audit_logs (actor_user_id, action, entity_type, entity_id, before, after, reason, request_id)
  VALUES (
    v_actor,
    lower(TG_OP),
    TG_TABLE_NAME,
    CASE WHEN TG_OP = 'DELETE' THEN OLD.id ELSE NEW.id END,
    CASE WHEN TG_OP IN ('UPDATE', 'DELETE') THEN to_jsonb(OLD) END,
    CASE WHEN TG_OP IN ('INSERT', 'UPDATE') THEN to_jsonb(NEW) END,
    nullif(current_setting('app.reason', true), ''),
    nullif(current_setting('app.request_id', true), '')
  );
  RETURN NULL;
END;
$$;

-- Secure document references: a bucket-relative object key, never a URL,
-- never a path traversal. Presigned URLs are generated on demand.
CREATE OR REPLACE FUNCTION is_object_key(k text) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
  SELECT k ~ '^[a-z][a-z0-9_-]*/[A-Za-z0-9._/-]+$' AND k !~ '\.\.' AND length(k) <= 512
$$;
