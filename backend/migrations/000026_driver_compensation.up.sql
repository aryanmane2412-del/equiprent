-- Owner agreed compensation and auditable payment record for an assigned
-- operator. Status changes are append-only events to retain the history.
CREATE TABLE driver_compensations (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  booking_id uuid NOT NULL REFERENCES bookings(id),
  owner_org_id uuid NOT NULL REFERENCES organizations(id),
  driver_user_id uuid NOT NULL REFERENCES users(id),
  agreed_amount_paise bigint NOT NULL CHECK (agreed_amount_paise > 0),
  currency char(3) NOT NULL DEFAULT 'INR' CHECK (currency = 'INR'),
  status text NOT NULL DEFAULT 'agreed' CHECK (status IN ('agreed', 'paid')),
  recorded_by uuid NOT NULL REFERENCES users(id),
  paid_at timestamptz,
  transaction_reference text CHECK (transaction_reference IS NULL OR length(transaction_reference) BETWEEN 1 AND 120),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (booking_id),
  CHECK ((status = 'agreed' AND paid_at IS NULL) OR (status = 'paid' AND paid_at IS NOT NULL))
);
CREATE INDEX driver_compensations_owner_idx ON driver_compensations(owner_org_id, created_at DESC);
CREATE INDEX driver_compensations_driver_idx ON driver_compensations(driver_user_id, created_at DESC);
ALTER TABLE operator_profiles
  ADD COLUMN home_address_line1 text,
  ADD COLUMN home_address_line2 text,
  ADD COLUMN home_address_locality text,
  ADD COLUMN home_address_city text,
  ADD COLUMN home_address_district text,
  ADD COLUMN home_address_state_code char(2),
  ADD COLUMN home_address_pincode text,
  ADD COLUMN identity_consent_at timestamptz,
  ADD CONSTRAINT operator_home_address_complete_ck CHECK (
    (home_address_line1 IS NULL AND home_address_city IS NULL AND home_address_state_code IS NULL AND home_address_pincode IS NULL)
    OR (home_address_line1 IS NOT NULL AND home_address_city IS NOT NULL AND home_address_state_code ~ '^[A-Z]{2}$' AND home_address_pincode ~ '^[0-9]{6}$')
  );
CREATE TRIGGER driver_compensations_updated_at BEFORE UPDATE ON driver_compensations FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER driver_compensations_audit AFTER INSERT OR UPDATE ON driver_compensations FOR EACH ROW EXECUTE FUNCTION audit_row_change();

CREATE TABLE driver_compensation_events (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  compensation_id uuid NOT NULL REFERENCES driver_compensations(id),
  status text NOT NULL CHECK (status IN ('agreed', 'paid')),
  amount_paise bigint NOT NULL CHECK (amount_paise > 0),
  transaction_reference text,
  actor_user_id uuid NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX driver_compensation_events_history_idx ON driver_compensation_events(compensation_id, created_at DESC);
CREATE TRIGGER driver_compensation_events_no_mutation BEFORE UPDATE OR DELETE ON driver_compensation_events
  FOR EACH ROW EXECUTE FUNCTION forbid_modification();

CREATE OR REPLACE FUNCTION record_driver_compensation_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO driver_compensation_events(compensation_id, status, amount_paise, transaction_reference, actor_user_id)
  VALUES (NEW.id, NEW.status, NEW.agreed_amount_paise, NEW.transaction_reference,
          COALESCE(NULLIF(current_setting('app.actor_user_id', true), '')::uuid, NEW.recorded_by));
  RETURN NEW;
END $$;
CREATE TRIGGER driver_compensations_event AFTER INSERT OR UPDATE OF status, agreed_amount_paise, transaction_reference
  ON driver_compensations FOR EACH ROW EXECUTE FUNCTION record_driver_compensation_event();
