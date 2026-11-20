-- Bookings in the new statuses are mapped back to the closest older status
-- before the graph shrinks (enum values themselves stay; see 000018).
ALTER TABLE bookings DISABLE TRIGGER USER;
UPDATE bookings SET status = 'cancelled', cancel_reason = COALESCE(cancel_reason, 'draft discarded') WHERE status = 'draft';
UPDATE bookings SET status = 'payment_pending' WHERE status = 'payment_processing';
UPDATE bookings SET status = 'active' WHERE status = 'extension_requested';
UPDATE bookings SET status = resume_status WHERE status = 'cancellation_requested';
UPDATE bookings SET status = 'cancelled', cancel_reason = COALESCE(cancel_reason, 'refunded') WHERE status IN ('refund_pending', 'refunded');
ALTER TABLE bookings ENABLE TRIGGER USER;

DROP TRIGGER IF EXISTS refunds_after_write ON refunds;
DROP FUNCTION IF EXISTS refunds_after_write();
DROP INDEX IF EXISTS refunds_booking_idx;
DROP INDEX IF EXISTS refunds_pending_idx;
ALTER TABLE refunds DROP COLUMN IF EXISTS attempts, DROP COLUMN IF EXISTS failure_reason, DROP COLUMN IF EXISTS booking_id;

DROP TABLE IF EXISTS booking_extensions;
DROP FUNCTION IF EXISTS booking_extensions_guard();
DROP TYPE IF EXISTS extension_status;

DROP INDEX IF EXISTS bookings_status_changed_idx;
DROP INDEX IF EXISTS bookings_payment_due_idx;
ALTER TABLE bookings
  DROP CONSTRAINT IF EXISTS bookings_resume_status_ck,
  DROP COLUMN IF EXISTS payment_due_at,
  DROP COLUMN IF EXISTS resume_status;

DELETE FROM booking_status_transitions
 WHERE from_status IN ('draft', 'payment_processing', 'extension_requested', 'cancellation_requested', 'refund_pending', 'refunded')
    OR to_status IN ('draft', 'payment_processing', 'extension_requested', 'cancellation_requested', 'refund_pending', 'refunded');

ALTER TABLE bookings DROP CONSTRAINT bookings_no_overlap;
ALTER TABLE bookings ADD CONSTRAINT bookings_no_overlap EXCLUDE USING gist (equipment_id WITH =, period WITH &&)
  WHERE (status IN ('accepted', 'payment_pending', 'confirmed', 'operator_assigned', 'dispatched', 'on_site', 'active', 'return_initiated'));

CREATE OR REPLACE FUNCTION booking_holds_slot(s booking_status) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
  SELECT s IN ('accepted', 'payment_pending', 'confirmed', 'operator_assigned', 'dispatched', 'on_site', 'active', 'return_initiated')
$$;
COMMENT ON FUNCTION booking_holds_slot(booking_status) IS NULL;

CREATE OR REPLACE FUNCTION bookings_before_insert() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  v_owner  uuid;
  v_status listing_status;
  v_deleted timestamptz;
BEGIN
  SELECT owner_org_id, status, deleted_at INTO v_owner, v_status, v_deleted
    FROM equipment WHERE id = NEW.equipment_id FOR UPDATE;
  IF NOT FOUND OR v_deleted IS NOT NULL OR v_status <> 'active' THEN
    RAISE EXCEPTION 'equipment % is not available for booking', NEW.equipment_id USING ERRCODE = 'EQ004';
  END IF;
  NEW.owner_org_id := v_owner;
  IF NEW.status NOT IN ('requested', 'quoted', 'payment_pending', 'confirmed') THEN
    RAISE EXCEPTION 'a booking cannot start in status %', NEW.status USING ERRCODE = 'EQ002';
  END IF;
  PERFORM assert_no_block_overlap(NEW.equipment_id, NEW.period);
  NEW.status_changed_at := now();
  RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION bookings_before_update() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.reference <> OLD.reference OR NEW.equipment_id <> OLD.equipment_id
     OR NEW.customer_org_id <> OLD.customer_org_id OR NEW.owner_org_id <> OLD.owner_org_id THEN
    RAISE EXCEPTION 'booking identity columns are immutable' USING ERRCODE = 'EQ001';
  END IF;
  IF NEW.status <> OLD.status THEN
    IF NOT EXISTS (SELECT 1 FROM booking_status_transitions WHERE from_status = OLD.status AND to_status = NEW.status) THEN
      RAISE EXCEPTION 'invalid booking transition % -> %', OLD.status, NEW.status USING ERRCODE = 'EQ002';
    END IF;
    IF booking_holds_slot(NEW.status) AND NOT booking_holds_slot(OLD.status) THEN
      PERFORM 1 FROM equipment WHERE id = NEW.equipment_id FOR UPDATE;
      PERFORM assert_no_block_overlap(NEW.equipment_id, NEW.period);
    END IF;
    NEW.status_changed_at := now();
  ELSIF NEW.period <> OLD.period AND booking_holds_slot(NEW.status) THEN
    PERFORM 1 FROM equipment WHERE id = NEW.equipment_id FOR UPDATE;
    PERFORM assert_no_block_overlap(NEW.equipment_id, NEW.period);
  END IF;
  NEW.version := OLD.version + 1;
  NEW.updated_at := now();
  RETURN NEW;
END;
$$;
