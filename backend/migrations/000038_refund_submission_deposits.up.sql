-- Refund submission lifecycle (queued -> submitting -> submitted | uncertain) so the
-- worker never holds a transaction open across the gateway call and can tell an
-- uncertain gateway answer from a definite one.
ALTER TABLE refunds
  ADD COLUMN submit_state      text NOT NULL DEFAULT 'queued' CHECK (submit_state IN ('queued', 'submitting', 'submitted', 'uncertain')),
  ADD COLUMN submit_started_at timestamptz,
  ADD COLUMN next_attempt_at   timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN last_checked_at   timestamptz;
UPDATE refunds SET submit_state = 'submitted' WHERE gateway_refund_id IS NOT NULL;
CREATE INDEX refunds_submit_idx ON refunds (next_attempt_at) WHERE status = 'pending';

-- Deposit lifecycle events are posted to the ledger once each.
CREATE UNIQUE INDEX IF NOT EXISTS security_deposits_refund_idx ON security_deposits (refund_id) WHERE refund_id IS NOT NULL;

-- Track deposits already collected inside captured advance/deposit payments (they were never recorded).
INSERT INTO security_deposits (booking_id, payment_id, owner_org_id, customer_org_id, amount_paise)
SELECT DISTINCT ON (b.id) b.id, p.id, b.owner_org_id, b.customer_org_id, LEAST(b.deposit_paise, p.amount_paise)
FROM payments p JOIN bookings b ON b.id = p.booking_id
WHERE p.purpose IN ('advance', 'deposit') AND p.status IN ('captured', 'partially_refunded') AND b.deposit_paise > 0
ORDER BY b.id, p.created_at
ON CONFLICT DO NOTHING;
