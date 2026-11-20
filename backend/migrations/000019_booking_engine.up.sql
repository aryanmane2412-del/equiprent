-- Complete booking engine: drafts, payment processing and payment deadlines,
-- extension requests, cancellation requests, refunds (refund_pending →
-- refunded). The database stays the final authority for the lifecycle graph
-- and for double booking.

-- ---- statuses that hold the machine's calendar ---------------------------------------------
-- Drafts and requests awaiting the owner do not hold the slot (several
-- customers may ask for the same dates; the owner accepts one). From the
-- owner's acceptance until the machine is back, the slot is held.
CREATE OR REPLACE FUNCTION booking_holds_slot(s booking_status) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
  SELECT s IN ('accepted', 'payment_pending', 'payment_processing', 'confirmed', 'operator_assigned', 'dispatched',
               'on_site', 'active', 'extension_requested', 'return_initiated', 'cancellation_requested')
$$;

ALTER TABLE bookings DROP CONSTRAINT bookings_no_overlap;
ALTER TABLE bookings ADD CONSTRAINT bookings_no_overlap EXCLUDE USING gist (equipment_id WITH =, period WITH &&)
  WHERE (status IN ('accepted', 'payment_pending', 'payment_processing', 'confirmed', 'operator_assigned', 'dispatched',
                    'on_site', 'active', 'extension_requested', 'return_initiated', 'cancellation_requested'));

-- ---- lifecycle graph ------------------------------------------------------------------------
INSERT INTO booking_status_transitions (from_status, to_status) VALUES
  -- drafts: submitted (request, instant book awaiting payment, or confirmed offline) or discarded
  ('draft', 'requested'), ('draft', 'payment_pending'), ('draft', 'confirmed'), ('draft', 'cancelled'),
  -- payment at the gateway
  ('payment_pending', 'payment_processing'),
  ('payment_processing', 'payment_pending'), ('payment_processing', 'confirmed'),
  ('payment_processing', 'cancelled'), ('payment_processing', 'expired'), ('payment_processing', 'refund_pending'),
  -- cancellations that leave money to refund
  ('accepted', 'refund_pending'), ('payment_pending', 'refund_pending'), ('confirmed', 'refund_pending'),
  ('operator_assigned', 'refund_pending'), ('dispatched', 'refund_pending'), ('on_site', 'refund_pending'),
  -- a payment captured after the booking ended (late webhook) is refunded
  ('cancelled', 'refund_pending'), ('expired', 'refund_pending'), ('rejected', 'refund_pending'),
  ('disputed', 'refund_pending'),
  ('refund_pending', 'refunded'), ('refunded', 'refund_pending'),
  -- cancellation once the machine has left the yard needs the owner's approval
  ('dispatched', 'cancellation_requested'), ('on_site', 'cancellation_requested'),
  ('cancellation_requested', 'dispatched'), ('cancellation_requested', 'on_site'),
  ('cancellation_requested', 'cancelled'), ('cancellation_requested', 'refund_pending'),
  -- extensions while the machine is working
  ('active', 'extension_requested'), ('extension_requested', 'active')
ON CONFLICT DO NOTHING;

-- ---- columns ---------------------------------------------------------------------------------
ALTER TABLE bookings
  -- The status a pending extension/cancellation request returns to when it
  -- is declined or withdrawn. Maintained by the trigger below.
  ADD COLUMN resume_status  booking_status,
  -- Online bookings awaiting payment must pay by then or the slot is freed.
  ADD COLUMN payment_due_at timestamptz,
  ADD CONSTRAINT bookings_resume_status_ck CHECK (
    (status IN ('extension_requested', 'cancellation_requested')) = (resume_status IS NOT NULL));
CREATE INDEX bookings_payment_due_idx ON bookings (payment_due_at) WHERE status IN ('payment_pending', 'payment_processing');
CREATE INDEX bookings_status_changed_idx ON bookings (status, status_changed_at)
  WHERE status IN ('payment_processing', 'extension_requested', 'refund_pending');

-- ---- triggers -------------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION bookings_before_insert() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  v_owner  uuid;
  v_status listing_status;
  v_deleted timestamptz;
BEGIN
  -- Row lock serialises concurrent bookings/blocks for the same machine.
  SELECT owner_org_id, status, deleted_at INTO v_owner, v_status, v_deleted
    FROM equipment WHERE id = NEW.equipment_id FOR UPDATE;
  IF NOT FOUND OR v_deleted IS NOT NULL OR v_status <> 'active' THEN
    RAISE EXCEPTION 'equipment % is not available for booking', NEW.equipment_id USING ERRCODE = 'EQ004';
  END IF;
  NEW.owner_org_id := v_owner;  -- always derived, never trusted from the client
  IF NEW.status NOT IN ('draft', 'requested', 'quoted', 'payment_pending', 'confirmed') THEN
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

-- ---- extensions -------------------------------------------------------------------------------
CREATE TYPE extension_status AS ENUM ('pending', 'approved', 'declined', 'withdrawn', 'expired');

-- An extension moves a running booking's end. Either party may propose it;
-- the other approves. The price is an estimate of the extra period at the
-- listing's rates (no new delivery or pickup), frozen at request time.
CREATE TABLE booking_extensions (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  booking_id      uuid NOT NULL REFERENCES bookings (id),
  requested_by    uuid NOT NULL REFERENCES users (id),
  requested_side  text NOT NULL CHECK (requested_side IN ('customer', 'owner')),
  previous_end    timestamptz NOT NULL,
  new_end         timestamptz NOT NULL,
  price_snapshot  jsonb NOT NULL CHECK (jsonb_typeof(price_snapshot) = 'object'),
  amount_paise    bigint NOT NULL CHECK (amount_paise >= 0),
  note            text NOT NULL DEFAULT '' CHECK (length(note) <= 500),
  status          extension_status NOT NULL DEFAULT 'pending',
  response_reason text CHECK (length(response_reason) <= 500),
  responded_by    uuid REFERENCES users (id),
  responded_at    timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT booking_extensions_end_ck CHECK (new_end > previous_end),
  CONSTRAINT booking_extensions_response_ck CHECK ((status = 'pending') = (responded_at IS NULL))
);
-- At most one open request per booking.
CREATE UNIQUE INDEX booking_extensions_pending_uq ON booking_extensions (booking_id) WHERE status = 'pending';
CREATE INDEX booking_extensions_booking_idx ON booking_extensions (booking_id, created_at);

-- Answered requests are history.
CREATE OR REPLACE FUNCTION booking_extensions_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' OR OLD.status <> 'pending' THEN
    RAISE EXCEPTION 'extension % is % and cannot change', OLD.id, OLD.status USING ERRCODE = 'EQ001';
  END IF;
  IF NEW.booking_id <> OLD.booking_id OR NEW.new_end <> OLD.new_end OR NEW.amount_paise <> OLD.amount_paise
     OR NEW.previous_end <> OLD.previous_end OR NEW.price_snapshot <> OLD.price_snapshot THEN
    RAISE EXCEPTION 'extension terms are immutable' USING ERRCODE = 'EQ001';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER booking_extensions_guard BEFORE UPDATE OR DELETE ON booking_extensions
  FOR EACH ROW EXECUTE FUNCTION booking_extensions_guard();
CREATE TRIGGER booking_extensions_audit AFTER INSERT OR UPDATE ON booking_extensions
  FOR EACH ROW EXECUTE FUNCTION audit_row_change();

-- ---- refunds ----------------------------------------------------------------------------------
-- Which booking a refund belongs to (denormalised for the worker) and why.
ALTER TABLE refunds
  ADD COLUMN booking_id     uuid REFERENCES bookings (id),
  ADD COLUMN failure_reason text,
  ADD COLUMN attempts       integer NOT NULL DEFAULT 0;
UPDATE refunds r SET booking_id = p.booking_id FROM payments p WHERE p.id = r.payment_id;
ALTER TABLE refunds ALTER COLUMN booking_id SET NOT NULL;
CREATE INDEX refunds_pending_idx ON refunds (created_at) WHERE status = 'pending';
CREATE INDEX refunds_booking_idx ON refunds (booking_id);

-- A processed refund moves its payment to partially_refunded / refunded.
CREATE OR REPLACE FUNCTION refunds_after_write() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  v_amount bigint;
  v_done   bigint;
BEGIN
  IF NEW.status = 'processed' AND (TG_OP = 'INSERT' OR OLD.status <> 'processed') THEN
    SELECT amount_paise INTO v_amount FROM payments WHERE id = NEW.payment_id;
    SELECT COALESCE(sum(amount_paise), 0) INTO v_done FROM refunds WHERE payment_id = NEW.payment_id AND status = 'processed';
    UPDATE payments SET status = CASE WHEN v_done >= v_amount THEN 'refunded'::payment_status ELSE 'partially_refunded'::payment_status END
     WHERE id = NEW.payment_id AND status <> CASE WHEN v_done >= v_amount THEN 'refunded'::payment_status ELSE 'partially_refunded'::payment_status END;
  END IF;
  RETURN NULL;
END;
$$;
CREATE TRIGGER refunds_after_write AFTER INSERT OR UPDATE ON refunds FOR EACH ROW EXECUTE FUNCTION refunds_after_write();

-- ---- reporting queries that list in-progress statuses ------------------------------------------
COMMENT ON FUNCTION booking_holds_slot(booking_status) IS
  'Statuses that reserve the machine: accepted through return_initiated, including payment_processing, extension_requested and cancellation_requested.';
