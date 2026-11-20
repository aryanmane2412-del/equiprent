DROP TRIGGER verification_events_append_only ON verification_events;
CREATE TRIGGER verification_events_append_only BEFORE UPDATE ON verification_events
  FOR EACH ROW EXECUTE FUNCTION forbid_modification();
