-- Marketplace payments: simulated gateway, receipts, ledger idempotency,
-- security deposit records, settlement breakdown, Razorpay Route linked
-- accounts and transfers.

-- ---- payments -------------------------------------------------------------------------------
ALTER TABLE payments DROP CONSTRAINT payments_gateway_check;
ALTER TABLE payments ADD CONSTRAINT payments_gateway_check
  CHECK (gateway IN ('razorpay', 'cashfree', 'payu', 'offline', 'simulated'));

-- Receipts are numbered when the money is captured (not GST invoices; those
-- stay in invoices). The number never changes once set.
CREATE SEQUENCE payment_receipt_seq;
ALTER TABLE payments
  ADD COLUMN receipt_number text UNIQUE,
  ADD COLUMN gateway_mode text NOT NULL DEFAULT 'simulated' CHECK (gateway_mode IN ('simulated', 'test', 'live', 'offline')),
  ADD COLUMN authorized_at timestamptz;

UPDATE payments SET gateway_mode = 'offline' WHERE gateway = 'offline';
UPDATE payments SET gateway_mode = 'test'
 WHERE gateway = 'razorpay' AND gateway_order_id IS NOT NULL AND gateway_order_id NOT LIKE 'order_sbx_%';
UPDATE payments p SET receipt_number = x.num
  FROM (SELECT id, 'RCPT-' || to_char(captured_at AT TIME ZONE 'Asia/Kolkata', 'YYYY') || '-'
                   || lpad(nextval('payment_receipt_seq')::text, 7, '0') AS num
          FROM (SELECT id, captured_at FROM payments
                 WHERE status IN ('captured', 'partially_refunded', 'refunded') ORDER BY captured_at, id) o) x
 WHERE p.id = x.id;

CREATE OR REPLACE FUNCTION payments_receipt() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'UPDATE' AND OLD.receipt_number IS NOT NULL AND NEW.receipt_number IS DISTINCT FROM OLD.receipt_number THEN
    RAISE EXCEPTION 'receipt numbers are immutable' USING ERRCODE = 'EQ001';
  END IF;
  IF NEW.status IN ('captured', 'partially_refunded', 'refunded') AND NEW.receipt_number IS NULL THEN
    NEW.receipt_number := 'RCPT-' || to_char(COALESCE(NEW.captured_at, now()) AT TIME ZONE 'Asia/Kolkata', 'YYYY') || '-'
                          || lpad(nextval('payment_receipt_seq')::text, 7, '0');
  END IF;
  IF TG_OP = 'UPDATE' AND NEW.gateway_mode <> OLD.gateway_mode THEN
    RAISE EXCEPTION 'payment gateway mode is immutable' USING ERRCODE = 'EQ001';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER payments_receipt BEFORE INSERT OR UPDATE ON payments FOR EACH ROW EXECUTE FUNCTION payments_receipt();

-- No "one open order" constraint: an abandoned checkout can still be paid
-- at the gateway later, and that money must be recorded (and refunded if
-- it is surplus), never rejected.
CREATE INDEX payments_payer_idx ON payments (payer_org_id, created_at DESC);

-- ---- ledger ----------------------------------------------------------------------------------
-- A business event is posted once: (kind, reference) is unique, so a replayed
-- webhook or job can never double-post.
CREATE UNIQUE INDEX ledger_transactions_once_uq ON ledger_transactions (kind, reference_type, reference_id)
  WHERE reference_id IS NOT NULL;
INSERT INTO ledger_accounts (code, type) VALUES ('customer_funds_held', 'liability') ON CONFLICT (code) DO NOTHING;

-- ---- security deposits ----------------------------------------------------------------------
CREATE TYPE deposit_status AS ENUM ('held', 'claimed', 'released', 'partially_forfeited', 'forfeited', 'refunded_on_cancel');

CREATE TABLE security_deposits (
  id                 uuid PRIMARY KEY DEFAULT uuidv7(),
  booking_id         uuid NOT NULL UNIQUE REFERENCES bookings (id),
  payment_id         uuid NOT NULL UNIQUE REFERENCES payments (id),
  owner_org_id       uuid NOT NULL REFERENCES organizations (id),
  customer_org_id    uuid NOT NULL REFERENCES organizations (id),
  amount_paise       bigint NOT NULL CHECK (amount_paise > 0),
  status             deposit_status NOT NULL DEFAULT 'held',
  claim_paise        bigint CHECK (claim_paise IS NULL OR claim_paise > 0),
  claim_reason       text,
  claimed_by         uuid REFERENCES users (id),
  claimed_at         timestamptz,
  deducted_paise     bigint NOT NULL DEFAULT 0 CHECK (deducted_paise >= 0),
  refunded_paise     bigint NOT NULL DEFAULT 0 CHECK (refunded_paise >= 0),
  resolution_note    text,
  resolved_by        uuid REFERENCES users (id),
  resolved_at        timestamptz,
  refund_id          uuid REFERENCES refunds (id),
  claim_deadline     timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  CHECK (deducted_paise + refunded_paise <= amount_paise),
  CHECK (claim_paise IS NULL OR claim_paise <= amount_paise),
  CHECK ((status = 'claimed') <= (claim_paise IS NOT NULL)),
  CHECK (status IN ('held', 'claimed') OR resolved_at IS NOT NULL)
);
CREATE INDEX security_deposits_owner_idx ON security_deposits (owner_org_id, status);
CREATE INDEX security_deposits_open_idx ON security_deposits (claim_deadline) WHERE status = 'held';
CREATE TRIGGER security_deposits_updated_at BEFORE UPDATE ON security_deposits FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER security_deposits_audit AFTER INSERT OR UPDATE ON security_deposits FOR EACH ROW EXECUTE FUNCTION audit_row_change();

-- ---- settlements ----------------------------------------------------------------------------
-- The customer-side breakdown behind gross_paise, so an owner can see how
-- their payable was reached.
ALTER TABLE settlements
  ADD COLUMN customer_paid_paise      bigint NOT NULL DEFAULT 0 CHECK (customer_paid_paise >= 0),
  ADD COLUMN rental_paise             bigint NOT NULL DEFAULT 0 CHECK (rental_paise >= 0),
  ADD COLUMN delivery_paise           bigint NOT NULL DEFAULT 0 CHECK (delivery_paise >= 0),
  ADD COLUMN owner_tax_paise          bigint NOT NULL DEFAULT 0 CHECK (owner_tax_paise >= 0),
  ADD COLUMN platform_fee_paise       bigint NOT NULL DEFAULT 0 CHECK (platform_fee_paise >= 0),
  ADD COLUMN platform_fee_tax_paise   bigint NOT NULL DEFAULT 0 CHECK (platform_fee_tax_paise >= 0),
  ADD COLUMN deposit_deduction_paise  bigint NOT NULL DEFAULT 0 CHECK (deposit_deduction_paise >= 0),
  ADD COLUMN commission_base_paise    bigint NOT NULL DEFAULT 0 CHECK (commission_base_paise >= 0),
  ADD COLUMN refunded_paise           bigint NOT NULL DEFAULT 0 CHECK (refunded_paise >= 0),
  ADD COLUMN paid_at                  timestamptz;

-- Money columns are frozen once written (the ledger was posted from them).
CREATE OR REPLACE FUNCTION settlements_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'settlements are never deleted' USING ERRCODE = 'EQ001';
  END IF;
  IF (to_jsonb(NEW) - ARRAY['status', 'hold_reason', 'eligible_at', 'paid_at', 'updated_at'])
     <> (to_jsonb(OLD) - ARRAY['status', 'hold_reason', 'eligible_at', 'paid_at', 'updated_at']) THEN
    RAISE EXCEPTION 'settlement % amounts are immutable', OLD.id USING ERRCODE = 'EQ001';
  END IF;
  IF OLD.status = 'paid' AND NEW.status <> 'paid' THEN
    RAISE EXCEPTION 'a paid settlement cannot change status' USING ERRCODE = 'EQ002';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER settlements_guard BEFORE UPDATE OR DELETE ON settlements FOR EACH ROW EXECUTE FUNCTION settlements_guard();

-- ---- Razorpay Route ---------------------------------------------------------------------------
-- An owner's linked account (acc_...) with its Route product configuration.
-- Money only moves to an account whose product is 'activated'.
CREATE TABLE payout_linked_accounts (
  id                 uuid PRIMARY KEY DEFAULT uuidv7(),
  owner_org_id       uuid NOT NULL UNIQUE REFERENCES organizations (id),
  provider           text NOT NULL CHECK (provider IN ('razorpay', 'simulated')),
  mode               text NOT NULL CHECK (mode IN ('simulated', 'test', 'live')),
  account_id         text NOT NULL UNIQUE,
  product_id         text,
  activation_status  text NOT NULL CHECK (activation_status IN ('created', 'requested', 'needs_clarification', 'under_review', 'activated', 'suspended')),
  requirements       jsonb NOT NULL DEFAULT '[]',
  bank_account_id    uuid NOT NULL REFERENCES bank_accounts (id),
  created_by         uuid REFERENCES users (id),
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER payout_linked_accounts_updated_at BEFORE UPDATE ON payout_linked_accounts FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER payout_linked_accounts_audit AFTER INSERT OR UPDATE ON payout_linked_accounts FOR EACH ROW EXECUTE FUNCTION audit_row_change();

-- A payout is paid as Route transfers out of the booking's captured payments
-- (POST /payments/:id/transfers); one row per transfer.
CREATE TABLE payout_transfers (
  id                  uuid PRIMARY KEY DEFAULT uuidv7(),
  payout_id           uuid NOT NULL REFERENCES payouts (id),
  payment_id          uuid NOT NULL REFERENCES payments (id),
  amount_paise        bigint NOT NULL CHECK (amount_paise > 0),
  gateway_transfer_id text UNIQUE,
  status              text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'created', 'processed', 'failed', 'reversed')),
  failure_reason      text,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  UNIQUE (payout_id, payment_id)
);
CREATE TRIGGER payout_transfers_updated_at BEFORE UPDATE ON payout_transfers FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER payout_transfers_audit AFTER INSERT OR UPDATE ON payout_transfers FOR EACH ROW EXECUTE FUNCTION audit_row_change();

-- Transfers are capped by what is left of each payment after refunds.
CREATE OR REPLACE FUNCTION payout_transfers_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  v_amount bigint;
  v_used   bigint;
BEGIN
  SELECT p.amount_paise - COALESCE((SELECT sum(r.amount_paise) FROM refunds r WHERE r.payment_id = p.id AND r.status <> 'failed'), 0)
    INTO v_amount FROM payments p WHERE p.id = NEW.payment_id FOR UPDATE;
  SELECT COALESCE(sum(amount_paise), 0) INTO v_used FROM payout_transfers
   WHERE payment_id = NEW.payment_id AND status NOT IN ('failed', 'reversed') AND id <> NEW.id;
  IF NEW.status NOT IN ('failed', 'reversed') AND v_used + NEW.amount_paise > v_amount THEN
    RAISE EXCEPTION 'transfers (% + %) exceed what is left of payment %', v_used, NEW.amount_paise, NEW.payment_id USING ERRCODE = 'EQ004';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER payout_transfers_guard BEFORE INSERT OR UPDATE ON payout_transfers FOR EACH ROW EXECUTE FUNCTION payout_transfers_guard();

ALTER TABLE payouts ALTER COLUMN bank_account_id DROP NOT NULL;
ALTER TABLE payouts ADD COLUMN linked_account_id uuid REFERENCES payout_linked_accounts (id);
