-- H1/H2: a disputed booking remembers where it came from (resume_status), keeps holding the
-- machine while the rental is still running, and returns to its previous state when resolved.

-- When the booking completed (the 7-day dispute window runs from here and survives a dispute).
ALTER TABLE bookings ADD COLUMN completed_at timestamptz;
UPDATE bookings SET completed_at = status_changed_at WHERE status IN ('completed', 'closed', 'disputed');
ALTER TABLE bookings DROP CONSTRAINT bookings_resume_status_ck;
-- Bookings disputed before this change were always sent to completed.
UPDATE bookings SET resume_status = 'completed' WHERE status = 'disputed' AND resume_status IS NULL;
ALTER TABLE bookings ADD CONSTRAINT bookings_resume_status_ck CHECK (
  (status IN ('extension_requested', 'cancellation_requested', 'disputed')) = (resume_status IS NOT NULL));

INSERT INTO booking_status_transitions (from_status, to_status) VALUES ('disputed', 'active'), ('disputed', 'returned')
ON CONFLICT DO NOTHING;

-- Does a booking reserve the machine? A dispute on a rental that is still running does;
-- one raised after the machine came back does not.
CREATE OR REPLACE FUNCTION booking_holds_slot(s booking_status, resume booking_status) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
  SELECT booking_holds_slot(s) OR (s = 'disputed' AND resume IN ('dispatched', 'on_site', 'active', 'return_initiated'))
$$;

ALTER TABLE bookings DROP CONSTRAINT bookings_no_overlap;
ALTER TABLE bookings ADD CONSTRAINT bookings_no_overlap EXCLUDE USING gist (equipment_id WITH =, period WITH &&)
  WHERE (status IN ('accepted', 'payment_pending', 'payment_processing', 'confirmed', 'operator_assigned', 'dispatched',
                    'on_site', 'active', 'extension_requested', 'return_initiated', 'cancellation_requested')
         OR (status = 'disputed' AND resume_status IN ('dispatched', 'on_site', 'active', 'return_initiated')));

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
    -- A declined request or a resolved dispute goes back exactly where it came from.
    IF OLD.status IN ('extension_requested', 'cancellation_requested', 'disputed') THEN
      IF NEW.status NOT IN ('cancelled', 'refund_pending') AND NEW.status IS DISTINCT FROM OLD.resume_status
         AND NOT (OLD.status = 'disputed' AND NEW.status = 'closed') THEN
        RAISE EXCEPTION 'a % booking returns to %, not %', OLD.status, OLD.resume_status, NEW.status USING ERRCODE = 'EQ002';
      END IF;
      NEW.resume_status := NULL;
    END IF;
    IF NEW.status IN ('extension_requested', 'cancellation_requested', 'disputed') THEN
      NEW.resume_status := OLD.status;
    END IF;
    -- A booking cannot complete while a dispute is unresolved.
    IF NEW.status = 'completed' AND EXISTS (SELECT 1 FROM disputes WHERE booking_id = NEW.id AND status IN ('open', 'under_review')) THEN
      RAISE EXCEPTION 'booking % has an unresolved dispute and cannot be completed', NEW.id USING ERRCODE = 'EQ004';
    END IF;
    IF NEW.status = 'completed' AND NEW.completed_at IS NULL THEN
      NEW.completed_at := now();
    END IF;
    IF booking_holds_slot(NEW.status, NEW.resume_status) AND NOT booking_holds_slot(OLD.status, OLD.resume_status) THEN
      PERFORM 1 FROM equipment WHERE id = NEW.equipment_id FOR UPDATE;
      PERFORM assert_no_block_overlap(NEW.equipment_id, NEW.period);
    END IF;
    NEW.status_changed_at := now();
  ELSIF NEW.period <> OLD.period AND booking_holds_slot(NEW.status, NEW.resume_status) THEN
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
              WHERE equipment_id = NEW.equipment_id AND period && NEW.period AND booking_holds_slot(status, resume_status)) THEN
    RAISE EXCEPTION 'cannot block % : an active booking holds this period', NEW.period USING ERRCODE = 'EQ004';
  END IF;
  RETURN NEW;
END;
$$;

-- What a resolved dispute turned into money.
ALTER TABLE disputes
  ADD COLUMN refunded_paise    bigint NOT NULL DEFAULT 0 CHECK (refunded_paise >= 0),
  ADD COLUMN unrecovered_paise bigint NOT NULL DEFAULT 0 CHECK (unrecovered_paise >= 0);
