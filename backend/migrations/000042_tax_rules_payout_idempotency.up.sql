-- Configurable, effective-dated statutory rates. Seeded from docs/PROJECT_PLAN.md §Compliance;
-- the owner chose to treat the seeds as active, but they stay editable and are dated for audit.
CREATE TABLE tax_rules (
  id             uuid PRIMARY KEY DEFAULT uuidv7(),
  code           text NOT NULL CHECK (code IN ('gst_platform', 'tcs_gst_52', 'tds_194o', 'tds_194o_no_pan')),
  rate_bps       integer NOT NULL CHECK (rate_bps BETWEEN 0 AND 10000),
  effective_from timestamptz NOT NULL DEFAULT now(),
  note           text,
  created_by     uuid REFERENCES users (id),
  created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX tax_rules_lookup_idx ON tax_rules (code, effective_from DESC);
INSERT INTO tax_rules (code, rate_bps, effective_from, note) VALUES
  ('gst_platform', 1800, '2000-01-01', 'GST on platform commission/fee (docs example 18%)'),
  ('tcs_gst_52', 50, '2000-01-01', 'GST TCS s.52, 0.5% (PROJECT_PLAN)'),
  ('tds_194o', 10, '2000-01-01', 'Income-tax TDS 194-O, 0.1% (PROJECT_PLAN)'),
  ('tds_194o_no_pan', 500, '2000-01-01', 'PLACEHOLDER 5% when PAN missing: verify with CA before use');

-- A failed payout must free its settlements for a retry, so uniqueness applies to active items only.
ALTER TABLE payout_items ADD COLUMN active boolean NOT NULL DEFAULT true;
ALTER TABLE payout_items DROP CONSTRAINT payout_items_settlement_id_key;
CREATE UNIQUE INDEX payout_items_active_settlement_uq ON payout_items (settlement_id) WHERE active;

-- Idempotency for payout creation: one payout per (owner, key).
ALTER TABLE payouts ADD COLUMN idempotency_key text;
CREATE UNIQUE INDEX payouts_idem_uq ON payouts (owner_org_id, idempotency_key) WHERE idempotency_key IS NOT NULL;

-- Reconciliation of our payouts against gateway reports.
CREATE TABLE payout_reconciliations (
  id                uuid PRIMARY KEY DEFAULT uuidv7(),
  payout_id         uuid NOT NULL REFERENCES payouts (id),
  gateway_status    text NOT NULL,
  gateway_paise     bigint NOT NULL,
  matches           boolean NOT NULL,
  note              text,
  checked_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX payout_reconciliations_payout_idx ON payout_reconciliations (payout_id, checked_at DESC);
