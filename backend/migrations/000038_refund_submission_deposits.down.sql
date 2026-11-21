DROP INDEX IF EXISTS security_deposits_refund_idx;
DROP INDEX IF EXISTS refunds_submit_idx;
ALTER TABLE refunds DROP COLUMN last_checked_at, DROP COLUMN next_attempt_at, DROP COLUMN submit_started_at, DROP COLUMN submit_state;
