-- Webhook inbox lifecycle (received -> processing -> processed | failed), retry
-- bookkeeping, and payments that may recover from failed to captured.
ALTER TABLE webhook_events
  ADD COLUMN status    text NOT NULL DEFAULT 'received' CHECK (status IN ('received', 'processing', 'processed', 'failed')),
  ADD COLUMN attempts  int NOT NULL DEFAULT 0,
  ADD COLUMN locked_at timestamptz;
UPDATE webhook_events SET status = CASE WHEN error IS NOT NULL THEN 'failed' WHEN processed_at IS NOT NULL THEN 'processed' ELSE 'received' END,
                          processed_at = CASE WHEN error IS NOT NULL THEN NULL ELSE processed_at END;
DROP INDEX IF EXISTS webhook_events_unprocessed_idx;
CREATE INDEX webhook_events_retry_idx ON webhook_events (received_at) WHERE status <> 'processed';

-- A genuine capture can arrive after an earlier payment.failed for the same order.
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
    OR (OLD.status = 'failed' AND NEW.status = 'captured')
    OR (OLD.status = 'captured' AND NEW.status IN ('partially_refunded', 'refunded'))
    OR (OLD.status = 'partially_refunded' AND NEW.status = 'refunded')) THEN
    RAISE EXCEPTION 'invalid payment transition % -> %', OLD.status, NEW.status USING ERRCODE = 'EQ002';
  END IF;
  NEW.updated_at := now();
  RETURN NEW;
END;
$$;

-- One open (unpaid) order per booking and purpose for advance/deposit.
CREATE UNIQUE INDEX payments_one_open_order_idx ON payments (booking_id, purpose)
  WHERE purpose IN ('advance', 'deposit') AND status IN ('created', 'authorized');
