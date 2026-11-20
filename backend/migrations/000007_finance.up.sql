-- Payments, refunds, gateway webhooks, idempotency, double-entry ledger,
-- commissions, owner settlements and payouts, GST invoices.
-- All amounts are bigint paise with an explicit currency.

CREATE TYPE payment_purpose AS ENUM ('advance', 'deposit', 'balance', 'extension', 'damage', 'cancellation_fee');
CREATE TYPE payment_status AS ENUM ('created', 'authorized', 'captured', 'failed', 'partially_refunded', 'refunded');
CREATE TYPE payment_method AS ENUM ('upi', 'card', 'netbanking', 'wallet', 'offline_cash', 'offline_bank_transfer');

CREATE TABLE payments (
  id                       uuid PRIMARY KEY DEFAULT uuidv7(),
  booking_id               uuid NOT NULL REFERENCES bookings (id),
  payer_org_id             uuid NOT NULL REFERENCES organizations (id),
  purpose                  payment_purpose NOT NULL,
  amount_paise             bigint NOT NULL CHECK (amount_paise > 0),
  currency                 char(3) NOT NULL DEFAULT 'INR' CHECK (currency = 'INR'),
  method                   payment_method,
  gateway                  text NOT NULL CHECK (gateway IN ('razorpay', 'cashfree', 'payu', 'offline')),
  gateway_order_id         text UNIQUE,
  gateway_payment_id       text UNIQUE,
  status                   payment_status NOT NULL DEFAULT 'created',
  idempotency_key          text NOT NULL UNIQUE,
  failure_code             text,
  failure_reason           text,
  offline_proof_object_key text CHECK (offline_proof_object_key IS NULL OR is_object_key(offline_proof_object_key)),
  offline_confirmed_by     uuid REFERENCES users (id),
  captured_at              timestamptz,
  created_at               timestamptz NOT NULL DEFAULT now(),
  updated_at               timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT payments_offline_ck CHECK ((gateway = 'offline') = (method IN ('offline_cash', 'offline_bank_transfer'))),
  CONSTRAINT payments_captured_ck CHECK (status IN ('created', 'authorized', 'failed') OR captured_at IS NOT NULL),
  CONSTRAINT payments_failed_ck CHECK (status <> 'failed' OR failure_reason IS NOT NULL)
);
CREATE INDEX payments_booking_idx ON payments (booking_id, created_at);
CREATE INDEX payments_status_idx ON payments (status, created_at DESC);

-- Payment rows are financial records: money fields never change, rows are
-- never deleted, status only moves forward.
CREATE OR REPLACE FUNCTION payments_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'payments are never deleted' USING ERRCODE = 'EQ001';
  END IF;
  IF NEW.amount_paise <> OLD.amount_paise OR NEW.currency <> OLD.currency OR NEW.booking_id <> OLD.booking_id
     OR NEW.payer_org_id <> OLD.payer_org_id OR NEW.purpose <> OLD.purpose OR NEW.idempotency_key <> OLD.idempotency_key
     OR NEW.gateway <> OLD.gateway OR NEW.created_at <> OLD.created_at THEN
    RAISE EXCEPTION 'payment % amount, parties and purpose are immutable', OLD.id USING ERRCODE = 'EQ001';
  END IF;
  IF NEW.status <> OLD.status AND NOT (
       (OLD.status = 'created' AND NEW.status IN ('authorized', 'captured', 'failed'))
    OR (OLD.status = 'authorized' AND NEW.status IN ('captured', 'failed'))
    OR (OLD.status = 'captured' AND NEW.status IN ('partially_refunded', 'refunded'))
    OR (OLD.status = 'partially_refunded' AND NEW.status = 'refunded')) THEN
    RAISE EXCEPTION 'invalid payment transition % -> %', OLD.status, NEW.status USING ERRCODE = 'EQ002';
  END IF;
  NEW.updated_at := now();
  RETURN NEW;
END;
$$;
CREATE TRIGGER payments_guard BEFORE UPDATE OR DELETE ON payments FOR EACH ROW EXECUTE FUNCTION payments_guard();
CREATE TRIGGER payments_audit AFTER INSERT OR UPDATE ON payments FOR EACH ROW EXECUTE FUNCTION audit_row_change();

CREATE TYPE refund_status AS ENUM ('pending', 'processed', 'failed');

CREATE TABLE refunds (
  id                uuid PRIMARY KEY DEFAULT uuidv7(),
  payment_id        uuid NOT NULL REFERENCES payments (id),
  amount_paise      bigint NOT NULL CHECK (amount_paise > 0),
  currency          char(3) NOT NULL DEFAULT 'INR' CHECK (currency = 'INR'),
  reason            text NOT NULL,
  status            refund_status NOT NULL DEFAULT 'pending',
  gateway_refund_id text UNIQUE,
  idempotency_key   text NOT NULL UNIQUE,
  initiated_by      uuid REFERENCES users (id),
  processed_at      timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  CHECK ((status = 'processed') = (processed_at IS NOT NULL))
);
CREATE INDEX refunds_payment_idx ON refunds (payment_id);

-- Refunds may never exceed the captured amount. Locks the payment row so
-- concurrent refunds are serialised.
CREATE OR REPLACE FUNCTION refunds_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  v_amount   bigint;
  v_status   payment_status;
  v_refunded bigint;
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'refunds are never deleted' USING ERRCODE = 'EQ001';
  END IF;
  IF TG_OP = 'UPDATE' AND (NEW.amount_paise <> OLD.amount_paise OR NEW.payment_id <> OLD.payment_id) THEN
    RAISE EXCEPTION 'refund amount is immutable' USING ERRCODE = 'EQ001';
  END IF;
  SELECT amount_paise, status INTO v_amount, v_status FROM payments WHERE id = NEW.payment_id FOR UPDATE;
  IF v_status NOT IN ('captured', 'partially_refunded', 'refunded') THEN
    RAISE EXCEPTION 'payment % is % and cannot be refunded', NEW.payment_id, v_status USING ERRCODE = 'EQ004';
  END IF;
  SELECT COALESCE(sum(amount_paise), 0) INTO v_refunded
    FROM refunds WHERE payment_id = NEW.payment_id AND status <> 'failed' AND id <> NEW.id;
  IF NEW.status <> 'failed' AND v_refunded + NEW.amount_paise > v_amount THEN
    RAISE EXCEPTION 'refunds (% + %) exceed payment amount %', v_refunded, NEW.amount_paise, v_amount USING ERRCODE = 'EQ004';
  END IF;
  NEW.updated_at := now();
  RETURN NEW;
END;
$$;
CREATE TRIGGER refunds_guard BEFORE INSERT OR UPDATE OR DELETE ON refunds FOR EACH ROW EXECUTE FUNCTION refunds_guard();
CREATE TRIGGER refunds_audit AFTER INSERT OR UPDATE ON refunds FOR EACH ROW EXECUTE FUNCTION audit_row_change();

-- Gateway webhook inbox (dedupe + replay) and API idempotency.
CREATE TABLE webhook_events (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  provider        text NOT NULL,
  event_id        text NOT NULL,
  event_type      text NOT NULL,
  payload         jsonb NOT NULL,
  signature_valid boolean NOT NULL,
  received_at     timestamptz NOT NULL DEFAULT now(),
  processed_at    timestamptz,
  error           text,
  UNIQUE (provider, event_id)
);
CREATE INDEX webhook_events_unprocessed_idx ON webhook_events (received_at) WHERE processed_at IS NULL;

CREATE TABLE idempotency_keys (
  key             text NOT NULL,
  user_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  method          text NOT NULL,
  path            text NOT NULL,
  request_hash    bytea NOT NULL,
  response_status smallint,
  response_body   jsonb,
  created_at      timestamptz NOT NULL DEFAULT now(),
  expires_at      timestamptz NOT NULL DEFAULT now() + interval '72 hours',
  PRIMARY KEY (user_id, key)
);
CREATE INDEX idempotency_keys_expiry_idx ON idempotency_keys (expires_at);

-- Double-entry ledger -------------------------------------------------------------
CREATE TABLE ledger_accounts (
  id         uuid PRIMARY KEY DEFAULT uuidv7(),
  code       text NOT NULL UNIQUE,   -- gateway_clearing, owner_payable:<org>, gst_output, ...
  type       text NOT NULL CHECK (type IN ('asset', 'liability', 'revenue', 'expense', 'equity')),
  org_id     uuid REFERENCES organizations (id),
  currency   char(3) NOT NULL DEFAULT 'INR' CHECK (currency = 'INR'),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE ledger_transactions (
  id             uuid PRIMARY KEY DEFAULT uuidv7(),
  kind           text NOT NULL CHECK (kind IN ('payment_captured', 'commission', 'tax', 'refund', 'payout', 'deposit_release', 'adjustment')),
  booking_id     uuid REFERENCES bookings (id),
  reference_type text,
  reference_id   uuid,
  memo           text,
  created_by     uuid REFERENCES users (id),
  created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ledger_transactions_booking_idx ON ledger_transactions (booking_id);
CREATE INDEX ledger_transactions_reference_idx ON ledger_transactions (reference_type, reference_id);

CREATE TABLE ledger_entries (
  id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  transaction_id uuid NOT NULL REFERENCES ledger_transactions (id),
  account_id     uuid NOT NULL REFERENCES ledger_accounts (id),
  direction      char(1) NOT NULL CHECK (direction IN ('D', 'C')),
  amount_paise   bigint NOT NULL CHECK (amount_paise > 0),
  currency       char(3) NOT NULL DEFAULT 'INR' CHECK (currency = 'INR')
);
CREATE INDEX ledger_entries_transaction_idx ON ledger_entries (transaction_id);
CREATE INDEX ledger_entries_account_idx ON ledger_entries (account_id, id);

CREATE TRIGGER ledger_transactions_append_only BEFORE UPDATE OR DELETE ON ledger_transactions FOR EACH ROW EXECUTE FUNCTION forbid_modification();
CREATE TRIGGER ledger_entries_append_only BEFORE UPDATE OR DELETE ON ledger_entries FOR EACH ROW EXECUTE FUNCTION forbid_modification();

-- Checked at COMMIT: debits equal credits and at least two legs.
CREATE OR REPLACE FUNCTION ledger_assert_balanced() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  v_debit  bigint;
  v_credit bigint;
  v_legs   integer;
BEGIN
  SELECT COALESCE(sum(amount_paise) FILTER (WHERE direction = 'D'), 0),
         COALESCE(sum(amount_paise) FILTER (WHERE direction = 'C'), 0),
         count(*)
    INTO v_debit, v_credit, v_legs
    FROM ledger_entries WHERE transaction_id = NEW.transaction_id;
  IF v_debit <> v_credit OR v_legs < 2 THEN
    RAISE EXCEPTION 'ledger transaction % is unbalanced (debit %, credit %, legs %)', NEW.transaction_id, v_debit, v_credit, v_legs
      USING ERRCODE = 'EQ003';
  END IF;
  RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER ledger_entries_balanced AFTER INSERT ON ledger_entries
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION ledger_assert_balanced();

CREATE VIEW ledger_balances AS
SELECT a.id AS account_id, a.code, a.type,
       COALESCE(sum(CASE WHEN e.direction = 'D' THEN e.amount_paise ELSE -e.amount_paise END), 0)::bigint AS debit_balance_paise
FROM ledger_accounts a
LEFT JOIN ledger_entries e ON e.account_id = a.id
GROUP BY a.id, a.code, a.type;

-- Commissions ---------------------------------------------------------------------
-- Rules are configuration; the applied rate is copied onto each settlement.
CREATE TABLE commission_rules (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  category_id      uuid REFERENCES equipment_categories (id),
  city_id          uuid REFERENCES cities (id),
  owner_org_id     uuid REFERENCES organizations (id),
  commission_bps   integer NOT NULL CHECK (commission_bps BETWEEN 0 AND 5000),
  customer_fee_bps integer NOT NULL DEFAULT 0 CHECK (customer_fee_bps BETWEEN 0 AND 2000),
  priority         integer NOT NULL DEFAULT 0,
  valid_from       timestamptz NOT NULL DEFAULT now(),
  valid_until      timestamptz CHECK (valid_until IS NULL OR valid_until > valid_from),
  created_by       uuid REFERENCES users (id),
  created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX commission_rules_lookup_idx ON commission_rules (priority DESC, valid_from DESC);
CREATE TRIGGER commission_rules_audit AFTER INSERT OR UPDATE OR DELETE ON commission_rules FOR EACH ROW EXECUTE FUNCTION audit_row_change();

-- Owner settlements & payouts ------------------------------------------------------
CREATE TYPE settlement_status AS ENUM ('pending', 'on_hold', 'eligible', 'paid');

CREATE TABLE settlements (
  id                      uuid PRIMARY KEY DEFAULT uuidv7(),
  booking_id              uuid NOT NULL UNIQUE REFERENCES bookings (id),
  owner_org_id            uuid NOT NULL REFERENCES organizations (id),
  commission_rule_id      uuid REFERENCES commission_rules (id),
  commission_bps          integer NOT NULL CHECK (commission_bps BETWEEN 0 AND 5000),
  gross_paise             bigint NOT NULL CHECK (gross_paise >= 0),
  commission_paise        bigint NOT NULL CHECK (commission_paise >= 0),
  gst_on_commission_paise bigint NOT NULL DEFAULT 0 CHECK (gst_on_commission_paise >= 0),
  tcs_paise               bigint NOT NULL DEFAULT 0 CHECK (tcs_paise >= 0),
  tds_paise               bigint NOT NULL DEFAULT 0 CHECK (tds_paise >= 0),
  deductions_paise        bigint NOT NULL DEFAULT 0 CHECK (deductions_paise >= 0),
  net_payable_paise       bigint NOT NULL CHECK (net_payable_paise >= 0),
  currency                char(3) NOT NULL DEFAULT 'INR' CHECK (currency = 'INR'),
  eligible_at             timestamptz NOT NULL,
  status                  settlement_status NOT NULL DEFAULT 'pending',
  hold_reason             text,
  created_at              timestamptz NOT NULL DEFAULT now(),
  updated_at              timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT settlements_net_ck CHECK (net_payable_paise = gross_paise - commission_paise - gst_on_commission_paise - tcs_paise - tds_paise - deductions_paise),
  CONSTRAINT settlements_hold_ck CHECK (status <> 'on_hold' OR hold_reason IS NOT NULL)
);
CREATE INDEX settlements_owner_idx ON settlements (owner_org_id, status);
CREATE INDEX settlements_eligible_idx ON settlements (eligible_at) WHERE status = 'eligible';
CREATE TRIGGER settlements_updated_at BEFORE UPDATE ON settlements FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER settlements_audit AFTER INSERT OR UPDATE OR DELETE ON settlements FOR EACH ROW EXECUTE FUNCTION audit_row_change();

CREATE TYPE payout_status AS ENUM ('queued', 'processing', 'paid', 'failed', 'reversed');

CREATE TABLE payouts (
  id                  uuid PRIMARY KEY DEFAULT uuidv7(),
  owner_org_id        uuid NOT NULL REFERENCES organizations (id),
  bank_account_id     uuid NOT NULL REFERENCES bank_accounts (id),
  amount_paise        bigint NOT NULL CHECK (amount_paise > 0),
  currency            char(3) NOT NULL DEFAULT 'INR' CHECK (currency = 'INR'),
  gateway_transfer_id text UNIQUE,
  status              payout_status NOT NULL DEFAULT 'queued',
  batch_date          date NOT NULL,
  initiated_by        uuid REFERENCES users (id),
  paid_at             timestamptz,
  failure_reason      text,
  version             integer NOT NULL DEFAULT 1,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  CHECK ((status = 'paid') = (paid_at IS NOT NULL))
);
CREATE INDEX payouts_owner_idx ON payouts (owner_org_id, batch_date DESC);
CREATE TRIGGER payouts_updated_at BEFORE UPDATE ON payouts FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER payouts_audit AFTER INSERT OR UPDATE OR DELETE ON payouts FOR EACH ROW EXECUTE FUNCTION audit_row_change();

CREATE TABLE payout_items (
  payout_id     uuid NOT NULL REFERENCES payouts (id),
  settlement_id uuid NOT NULL UNIQUE REFERENCES settlements (id),  -- a settlement is paid once
  amount_paise  bigint NOT NULL CHECK (amount_paise > 0),
  PRIMARY KEY (payout_id, settlement_id)
);

-- GST invoices ----------------------------------------------------------------------
CREATE TABLE invoice_series (
  id             uuid PRIMARY KEY DEFAULT uuidv7(),
  issuer_org_id  uuid REFERENCES organizations (id),   -- NULL = EQUIPRENT itself
  financial_year text NOT NULL CHECK (financial_year ~ '^[0-9]{4}$'),  -- 2627 = FY 2026-27
  prefix         text NOT NULL CHECK (prefix ~ '^[A-Z0-9]{1,6}$'),
  next_number    integer NOT NULL DEFAULT 1 CHECK (next_number >= 1),
  created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX invoice_series_uq ON invoice_series (COALESCE(issuer_org_id, '00000000-0000-0000-0000-000000000000'::uuid), financial_year, prefix);

-- Gap-free invoice numbers (GST rule): the series row is locked until the
-- surrounding transaction commits; a rollback returns the number. Format
-- PREFIX/FY/NNNNNN stays within the 16-character GST limit.
CREATE OR REPLACE FUNCTION next_invoice_number(p_series uuid) RETURNS text
LANGUAGE plpgsql AS $$
DECLARE v_number text;
BEGIN
  UPDATE invoice_series
     SET next_number = next_number + 1
   WHERE id = p_series
  RETURNING prefix || '/' || financial_year || '/' || lpad((next_number - 1)::text, 5, '0') INTO v_number;
  IF v_number IS NULL THEN
    RAISE EXCEPTION 'invoice series % not found', p_series USING ERRCODE = 'EQ004';
  END IF;
  RETURN v_number;
END;
$$;

CREATE TYPE invoice_type AS ENUM ('rental', 'commission', 'convenience_fee', 'credit_note');
CREATE TYPE invoice_status AS ENUM ('draft', 'issued', 'cancelled');

CREATE TABLE invoices (
  id                  uuid PRIMARY KEY DEFAULT uuidv7(),
  series_id           uuid NOT NULL REFERENCES invoice_series (id),
  invoice_number      text NOT NULL CHECK (length(invoice_number) <= 16),
  issuer_org_id       uuid REFERENCES organizations (id),   -- NULL = EQUIPRENT
  recipient_org_id    uuid NOT NULL REFERENCES organizations (id),
  booking_id          uuid REFERENCES bookings (id),
  invoice_type        invoice_type NOT NULL,
  original_invoice_id uuid REFERENCES invoices (id),
  place_of_supply     char(2) NOT NULL CHECK (place_of_supply ~ '^[0-9]{2}$'),
  supply_type         text NOT NULL CHECK (supply_type IN ('intra', 'inter')),
  sac_code            text NOT NULL CHECK (sac_code ~ '^99[0-9]{4}$'),
  subtotal_paise      bigint NOT NULL CHECK (subtotal_paise >= 0),
  cgst_paise          bigint NOT NULL DEFAULT 0 CHECK (cgst_paise >= 0),
  sgst_paise          bigint NOT NULL DEFAULT 0 CHECK (sgst_paise >= 0),
  igst_paise          bigint NOT NULL DEFAULT 0 CHECK (igst_paise >= 0),
  total_paise         bigint NOT NULL CHECK (total_paise >= 0),
  currency            char(3) NOT NULL DEFAULT 'INR' CHECK (currency = 'INR'),
  pdf_object_key      text CHECK (pdf_object_key IS NULL OR is_object_key(pdf_object_key)),
  irn                 text UNIQUE,   -- e-invoice reference (when applicable)
  status              invoice_status NOT NULL DEFAULT 'draft',
  issued_at           timestamptz,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  UNIQUE (series_id, invoice_number),
  CONSTRAINT invoices_total_ck CHECK (total_paise = subtotal_paise + cgst_paise + sgst_paise + igst_paise),
  CONSTRAINT invoices_gst_split_ck CHECK (
    (supply_type = 'intra' AND igst_paise = 0 AND cgst_paise = sgst_paise)
    OR (supply_type = 'inter' AND cgst_paise = 0 AND sgst_paise = 0)),
  CONSTRAINT invoices_credit_note_ck CHECK ((invoice_type = 'credit_note') = (original_invoice_id IS NOT NULL)),
  CONSTRAINT invoices_issued_ck CHECK ((status = 'draft') = (issued_at IS NULL))
);
CREATE INDEX invoices_booking_idx ON invoices (booking_id);
CREATE INDEX invoices_recipient_idx ON invoices (recipient_org_id, issued_at DESC);

-- Issued invoices are legal documents: only issued -> cancelled is allowed
-- (corrections are credit notes), and invoices are never deleted.
CREATE OR REPLACE FUNCTION invoices_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'invoices are never deleted' USING ERRCODE = 'EQ001';
  END IF;
  IF OLD.status <> 'draft' THEN
    IF NOT (OLD.status = 'issued' AND NEW.status = 'cancelled'
            AND (to_jsonb(NEW) - ARRAY['status', 'updated_at']) = (to_jsonb(OLD) - ARRAY['status', 'updated_at'])) THEN
      RAISE EXCEPTION 'invoice % is % and immutable', OLD.invoice_number, OLD.status USING ERRCODE = 'EQ001';
    END IF;
  END IF;
  NEW.updated_at := now();
  RETURN NEW;
END;
$$;
CREATE TRIGGER invoices_guard BEFORE UPDATE OR DELETE ON invoices FOR EACH ROW EXECUTE FUNCTION invoices_guard();
CREATE TRIGGER invoices_audit AFTER INSERT OR UPDATE ON invoices FOR EACH ROW EXECUTE FUNCTION audit_row_change();

CREATE TABLE invoice_lines (
  id            uuid PRIMARY KEY DEFAULT uuidv7(),
  invoice_id    uuid NOT NULL REFERENCES invoices (id),
  line_no       smallint NOT NULL CHECK (line_no >= 1),
  description   text NOT NULL,
  sac_code      text NOT NULL CHECK (sac_code ~ '^99[0-9]{4}$'),
  quantity      numeric(10, 2) NOT NULL CHECK (quantity > 0),
  unit          text NOT NULL,
  rate_paise    bigint NOT NULL CHECK (rate_paise >= 0),
  taxable_paise bigint NOT NULL CHECK (taxable_paise >= 0),
  gst_rate_bps  integer NOT NULL CHECK (gst_rate_bps IN (0, 500, 1200, 1800, 2800)),
  UNIQUE (invoice_id, line_no)
);

-- Lines of an issued invoice cannot change either.
CREATE OR REPLACE FUNCTION invoice_lines_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE v_status invoice_status;
BEGIN
  SELECT status INTO v_status FROM invoices WHERE id = COALESCE(NEW.invoice_id, OLD.invoice_id);
  IF v_status <> 'draft' THEN
    RAISE EXCEPTION 'lines of a % invoice are immutable', v_status USING ERRCODE = 'EQ001';
  END IF;
  RETURN COALESCE(NEW, OLD);
END;
$$;
CREATE TRIGGER invoice_lines_guard BEFORE INSERT OR UPDATE OR DELETE ON invoice_lines FOR EACH ROW EXECUTE FUNCTION invoice_lines_guard();
