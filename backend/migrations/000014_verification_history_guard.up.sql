-- Verification history is evidence: neither updates nor deletes are allowed.
-- (000013 only blocked updates.)
DROP TRIGGER verification_events_append_only ON verification_events;
CREATE TRIGGER verification_events_append_only BEFORE UPDATE OR DELETE ON verification_events
  FOR EACH ROW EXECUTE FUNCTION forbid_modification();
