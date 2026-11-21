DROP TABLE payout_reconciliations;
DROP INDEX payouts_idem_uq; ALTER TABLE payouts DROP COLUMN idempotency_key;
DROP INDEX payout_items_active_settlement_uq; ALTER TABLE payout_items DROP COLUMN active;
ALTER TABLE payout_items ADD CONSTRAINT payout_items_settlement_id_key UNIQUE (settlement_id);
DROP TABLE tax_rules;
