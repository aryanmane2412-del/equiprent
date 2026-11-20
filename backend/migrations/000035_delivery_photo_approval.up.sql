-- The equipment owner approves a customer's delivery photo. Evidence rows stay append-only
-- (migration 31), so each approval is its own append-only row.
CREATE TABLE customer_delivery_photo_approvals (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  photo_id    uuid NOT NULL UNIQUE REFERENCES customer_delivery_photos (id),
  booking_id  uuid NOT NULL REFERENCES bookings (id),
  approved_by uuid NOT NULL REFERENCES users (id),
  approved_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX customer_delivery_photo_approvals_booking_idx ON customer_delivery_photo_approvals (booking_id);
CREATE TRIGGER customer_delivery_photo_approvals_audit AFTER INSERT ON customer_delivery_photo_approvals
  FOR EACH ROW EXECUTE FUNCTION audit_row_change();
CREATE TRIGGER customer_delivery_photo_approvals_append_only BEFORE UPDATE OR DELETE ON customer_delivery_photo_approvals
  FOR EACH ROW EXECUTE FUNCTION forbid_modification();
