ALTER TABLE payouts DROP COLUMN IF EXISTS linked_account_id;
-- payouts.bank_account_id stays nullable: rows written without one cannot be restored.
DROP TABLE IF EXISTS payout_transfers;
DROP FUNCTION IF EXISTS payout_transfers_guard();
DROP TABLE IF EXISTS payout_linked_accounts;

DROP TRIGGER IF EXISTS settlements_guard ON settlements;
DROP FUNCTION IF EXISTS settlements_guard();
ALTER TABLE settlements
  DROP COLUMN IF EXISTS customer_paid_paise, DROP COLUMN IF EXISTS rental_paise, DROP COLUMN IF EXISTS delivery_paise,
  DROP COLUMN IF EXISTS owner_tax_paise, DROP COLUMN IF EXISTS platform_fee_paise, DROP COLUMN IF EXISTS platform_fee_tax_paise,
  DROP COLUMN IF EXISTS deposit_deduction_paise, DROP COLUMN IF EXISTS commission_base_paise, DROP COLUMN IF EXISTS refunded_paise,
  DROP COLUMN IF EXISTS paid_at;

DROP TABLE IF EXISTS security_deposits;
DROP TYPE IF EXISTS deposit_status;

DROP INDEX IF EXISTS ledger_transactions_once_uq;

DROP INDEX IF EXISTS payments_payer_idx;
DROP INDEX IF EXISTS payments_open_order_uq;
DROP TRIGGER IF EXISTS payments_receipt ON payments;
DROP FUNCTION IF EXISTS payments_receipt();
ALTER TABLE payments DROP COLUMN IF EXISTS receipt_number, DROP COLUMN IF EXISTS gateway_mode, DROP COLUMN IF EXISTS authorized_at;
DROP SEQUENCE IF EXISTS payment_receipt_seq;
ALTER TABLE payments DROP CONSTRAINT payments_gateway_check;
ALTER TABLE payments ADD CONSTRAINT payments_gateway_check CHECK (gateway IN ('razorpay', 'cashfree', 'payu', 'offline'));
