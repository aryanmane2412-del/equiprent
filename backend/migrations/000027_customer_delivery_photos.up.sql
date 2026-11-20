-- Customer evidence captured at delivery is private to the booking parties.
CREATE TABLE customer_delivery_photos (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  booking_id uuid NOT NULL REFERENCES bookings(id),
  upload_id uuid NOT NULL UNIQUE REFERENCES uploads(id),
  client_id uuid NOT NULL UNIQUE,
  captured_at timestamptz NOT NULL,
  submitted_at timestamptz NOT NULL DEFAULT now(),
  submitted_by uuid NOT NULL REFERENCES users(id),
  UNIQUE (booking_id, upload_id),
  CHECK (captured_at <= submitted_at + interval '10 minutes')
);
CREATE INDEX customer_delivery_photos_booking_idx ON customer_delivery_photos(booking_id, captured_at);
CREATE TRIGGER customer_delivery_photos_audit
  AFTER INSERT ON customer_delivery_photos
  FOR EACH ROW EXECUTE FUNCTION audit_row_change();
