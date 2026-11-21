-- Restore the status-only slot rules.
UPDATE bookings SET resume_status = NULL WHERE status = 'disputed';
ALTER TABLE bookings DROP CONSTRAINT bookings_no_overlap;
ALTER TABLE bookings ADD CONSTRAINT bookings_no_overlap EXCLUDE USING gist (equipment_id WITH =, period WITH &&)
  WHERE (status IN ('accepted', 'payment_pending', 'payment_processing', 'confirmed', 'operator_assigned', 'dispatched',
                    'on_site', 'active', 'extension_requested', 'return_initiated', 'cancellation_requested'));

ALTER TABLE bookings DROP CONSTRAINT bookings_resume_status_ck;
ALTER TABLE bookings ADD CONSTRAINT bookings_resume_status_ck CHECK (
    (status IN ('extension_requested', 'cancellation_requested')) = (resume_status IS NOT NULL));
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
    -- A declined or withdrawn request goes back exactly where it came from.
    IF OLD.status IN ('extension_requested', 'cancellation_requested') THEN
      IF NEW.status NOT IN ('cancelled', 'refund_pending') AND NEW.status IS DISTINCT FROM OLD.resume_status THEN
        RAISE EXCEPTION 'a % booking returns to %, not %', OLD.status, OLD.resume_status, NEW.status USING ERRCODE = 'EQ002';
      END IF;
      NEW.resume_status := NULL;
    END IF;
    IF NEW.status IN ('extension_requested', 'cancellation_requested') THEN
      NEW.resume_status := OLD.status;
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
CREATE OR REPLACE FUNCTION availability_blocks_before_write() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  PERFORM 1 FROM equipment WHERE id = NEW.equipment_id FOR UPDATE;
  IF EXISTS (SELECT 1 FROM bookings
              WHERE equipment_id = NEW.equipment_id AND period && NEW.period AND booking_holds_slot(status)) THEN
    RAISE EXCEPTION 'cannot block % : an active booking holds this period', NEW.period USING ERRCODE = 'EQ004';
  END IF;
  RETURN NEW;
END;
$$;
DROP FUNCTION booking_holds_slot(booking_status, booking_status);
DELETE FROM booking_status_transitions WHERE from_status = 'disputed' AND to_status IN ('active', 'returned');
ALTER TABLE disputes DROP COLUMN unrecovered_paise, DROP COLUMN refunded_paise;
ALTER TABLE bookings DROP COLUMN completed_at;
