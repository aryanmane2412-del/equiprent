-- Inspection records and acknowledgements are append-only. Corrections require
-- a separately audited administrative procedure; ordinary edits and deletes
-- must never rewrite the evidence trail.
CREATE TRIGGER booking_inspection_ack_append_only
  BEFORE UPDATE OR DELETE ON booking_inspection_acknowledgements
  FOR EACH ROW EXECUTE FUNCTION forbid_modification();

CREATE TRIGGER customer_delivery_photos_append_only
  BEFORE UPDATE OR DELETE ON customer_delivery_photos
  FOR EACH ROW EXECUTE FUNCTION forbid_modification();

CREATE TRIGGER inspection_decision_history_append_only
  BEFORE UPDATE OR DELETE ON inspection_decision_history
  FOR EACH ROW EXECUTE FUNCTION forbid_modification();
