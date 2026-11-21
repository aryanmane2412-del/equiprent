DROP INDEX IF EXISTS payments_one_open_order_idx;
DROP INDEX IF EXISTS webhook_events_retry_idx;
CREATE INDEX webhook_events_unprocessed_idx ON webhook_events (received_at) WHERE processed_at IS NULL;
ALTER TABLE webhook_events DROP COLUMN locked_at, DROP COLUMN attempts, DROP COLUMN status;
-- payments_guard keeps the more permissive transition rules (failed -> captured).
