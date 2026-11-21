-- Marketplace finance: ledger, receipts/history, security deposits,
-- commission configuration, settlements, Route linked accounts and payouts.

-- ---- ledger -----------------------------------------------------------------------------------

-- name: EnsureLedgerAccount :one
INSERT INTO ledger_accounts (code, type, org_id) VALUES (@code, @type, sqlc.narg(org_id))
ON CONFLICT (code) DO UPDATE SET code = EXCLUDED.code
RETURNING id;

-- name: InsertLedgerTransaction :one
-- No row when this (kind, reference) was already posted.
INSERT INTO ledger_transactions (kind, booking_id, reference_type, reference_id, memo, created_by)
VALUES (@kind, sqlc.narg(booking_id), @reference_type, @reference_id, @memo, sqlc.narg(created_by))
ON CONFLICT (kind, reference_type, reference_id) WHERE reference_id IS NOT NULL DO NOTHING
RETURNING id;

-- name: InsertLedgerEntry :exec
INSERT INTO ledger_entries (transaction_id, account_id, direction, amount_paise) VALUES (@transaction_id, @account_id, @direction, @amount_paise);

-- name: LedgerBalances :many
SELECT b.code, b.type, b.debit_balance_paise, a.org_id
FROM ledger_balances b JOIN ledger_accounts a ON a.id = b.account_id
WHERE (sqlc.narg(prefix)::text IS NULL OR b.code LIKE sqlc.narg(prefix)::text || '%')
ORDER BY b.code;

-- name: LedgerTransactions :many
SELECT t.id, t.kind, t.booking_id, t.reference_type, t.reference_id, t.memo, t.created_at,
       COALESCE(jsonb_agg(jsonb_build_object('account', a.code, 'direction', e.direction, 'amount_paise', e.amount_paise) ORDER BY e.id), '[]')::jsonb AS entries
FROM ledger_transactions t
JOIN ledger_entries e ON e.transaction_id = t.id
JOIN ledger_accounts a ON a.id = e.account_id
WHERE (sqlc.narg(booking_id)::uuid IS NULL OR t.booking_id = sqlc.narg(booking_id)::uuid)
  AND (sqlc.narg(before)::timestamptz IS NULL OR t.created_at < sqlc.narg(before)::timestamptz)
GROUP BY t.id
ORDER BY t.created_at DESC
LIMIT @lim;

-- name: LedgerTotals :one
-- Debits and credits over the whole ledger (always equal).
SELECT COALESCE(sum(amount_paise) FILTER (WHERE direction = 'D'), 0)::bigint AS debit_paise,
       COALESCE(sum(amount_paise) FILTER (WHERE direction = 'C'), 0)::bigint AS credit_paise
FROM ledger_entries;

-- ---- payments: checkout helpers, history, receipts --------------------------------------------

-- name: CreatePaymentV2 :one
INSERT INTO payments (booking_id, payer_org_id, purpose, amount_paise, gateway, gateway_mode, gateway_order_id, idempotency_key)
VALUES (@booking_id, @payer_org_id, @purpose, @amount_paise, @gateway, @gateway_mode, sqlc.narg(gateway_order_id), @idempotency_key)
RETURNING *;

-- name: GetOpenPayment :one
SELECT * FROM payments WHERE booking_id = @booking_id AND purpose = @purpose AND status IN ('created', 'authorized');

-- name: AuthorizePayment :one
UPDATE payments SET status = 'authorized', gateway_payment_id = @gateway_payment_id, method = sqlc.narg(method), authorized_at = now()
WHERE id = @id AND status = 'created'
RETURNING *;

-- name: CapturedByPurpose :one
SELECT COALESCE(sum(amount_paise), 0)::bigint AS total
FROM payments WHERE booking_id = @booking_id AND purpose = @purpose AND status IN ('captured', 'partially_refunded', 'refunded');

-- name: ListPaymentHistory :many
-- Payments seen by an organisation as payer (customer) or as the booking's owner.
SELECT p.id, p.booking_id, p.purpose::text AS purpose, p.amount_paise, p.currency, p.status::text AS status, p.gateway,
       p.gateway_mode, COALESCE(p.method::text, '')::text AS method, p.receipt_number, p.failure_reason, p.captured_at, p.created_at,
       b.reference AS booking_reference, e.title AS equipment_title, b.owner_org_id, b.customer_org_id,
       COALESCE((SELECT sum(r.amount_paise) FROM refunds r WHERE r.payment_id = p.id AND r.status = 'processed'), 0)::bigint AS refunded_paise,
       COALESCE((SELECT sum(r.amount_paise) FROM refunds r WHERE r.payment_id = p.id AND r.status = 'pending'), 0)::bigint AS refund_pending_paise
FROM payments p
JOIN bookings b ON b.id = p.booking_id
JOIN equipment e ON e.id = b.equipment_id
WHERE CASE WHEN @side::text = 'owner' THEN b.owner_org_id = @org_id ELSE p.payer_org_id = @org_id END
  AND (sqlc.narg(status)::text IS NULL OR p.status::text = sqlc.narg(status)::text)
  AND (sqlc.narg(before)::timestamptz IS NULL OR p.created_at < sqlc.narg(before)::timestamptz)
ORDER BY p.created_at DESC
LIMIT @lim;

-- name: GetReceipt :one
SELECT p.id, p.booking_id, p.purpose::text AS purpose, p.amount_paise, p.currency, p.status::text AS status, p.gateway,
       p.gateway_mode, COALESCE(p.method::text, '')::text AS method, p.receipt_number, p.gateway_payment_id, p.gateway_order_id, p.captured_at,
       b.reference AS booking_reference, e.title AS equipment_title, b.owner_org_id, b.customer_org_id,
       cu.legal_name AS payer_name, ow.legal_name AS owner_name,
       lower(b.period)::timestamptz AS starts_at, upper(b.period)::timestamptz AS ends_at,
       COALESCE((SELECT sum(r.amount_paise) FROM refunds r WHERE r.payment_id = p.id AND r.status = 'processed'), 0)::bigint AS refunded_paise
FROM payments p
JOIN bookings b ON b.id = p.booking_id
JOIN equipment e ON e.id = b.equipment_id
JOIN organizations cu ON cu.id = p.payer_org_id
JOIN organizations ow ON ow.id = b.owner_org_id
WHERE p.id = @id;

-- name: ListPaymentRefunds :many
SELECT id, amount_paise, reason, status::text AS status, processed_at, created_at FROM refunds WHERE payment_id = @payment_id ORDER BY created_at;

-- name: GetRefundDetail :one
SELECT r.id, r.payment_id, r.booking_id, r.amount_paise, p.purpose::text AS purpose
FROM refunds r JOIN payments p ON p.id = r.payment_id WHERE r.id = @id;

-- name: RefundablePayment :one
SELECT p.id, p.booking_id, p.purpose::text AS purpose, p.status::text AS status,
       (p.amount_paise - COALESCE((SELECT sum(r.amount_paise) FROM refunds r WHERE r.payment_id = p.id AND r.status <> 'failed'), 0))::bigint AS refundable_paise
FROM payments p WHERE p.id = @id
FOR UPDATE OF p;

-- ---- security deposits --------------------------------------------------------------------------

-- name: InsertSecurityDeposit :exec
INSERT INTO security_deposits (booking_id, payment_id, owner_org_id, customer_org_id, amount_paise)
VALUES (@booking_id, @payment_id, @owner_org_id, @customer_org_id, @amount_paise)
ON CONFLICT DO NOTHING;

-- name: GetDepositByBooking :one
SELECT * FROM security_deposits WHERE booking_id = @booking_id;

-- name: GetDepositForUpdate :one
SELECT * FROM security_deposits WHERE id = @id FOR UPDATE;

-- name: GetDepositByPayment :one
SELECT * FROM security_deposits WHERE payment_id = @payment_id FOR UPDATE;

-- name: ClaimDeposit :one
UPDATE security_deposits SET status = 'claimed', claim_paise = @claim_paise, claim_reason = @claim_reason,
       claimed_by = @claimed_by, claimed_at = now()
WHERE id = @id AND status = 'held'
RETURNING *;

-- name: ResolveDeposit :one
UPDATE security_deposits SET status = @status, deducted_paise = @deducted_paise, refunded_paise = @refunded_paise,
       resolution_note = sqlc.narg(resolution_note), resolved_by = sqlc.narg(resolved_by), resolved_at = now(),
       refund_id = sqlc.narg(refund_id)
WHERE id = @id AND status IN ('held', 'claimed')
RETURNING *;

-- name: DepositRefundedOnCancel :exec
UPDATE security_deposits SET status = 'refunded_on_cancel', refunded_paise = LEAST(amount_paise, refunded_paise + @amount_paise),
       resolved_at = COALESCE(resolved_at, now())
WHERE payment_id = @payment_id AND status IN ('held', 'claimed', 'refunded_on_cancel');

-- name: StartDepositClocks :many
-- Held deposits of finished rentals get their claim deadline.
UPDATE security_deposits d SET claim_deadline = now() + make_interval(secs => @window_seconds::float8)
FROM bookings b
WHERE b.id = d.booking_id AND d.status = 'held' AND d.claim_deadline IS NULL AND b.status IN ('completed', 'closed')
RETURNING d.id;

-- name: DueDeposits :many
SELECT * FROM security_deposits WHERE status = 'held' AND claim_deadline <= now() ORDER BY claim_deadline LIMIT @lim FOR UPDATE SKIP LOCKED;

-- name: DepositsToRelease :many
-- Held deposits of completed bookings. Released at once when no damage claim is open, else
-- after the claim window so an unresolved claim can never keep a customer's money forever.
SELECT d.* FROM security_deposits d JOIN bookings b ON b.id = d.booking_id
WHERE d.status = 'held' AND b.status IN ('completed', 'closed')
  AND ((NOT EXISTS (SELECT 1 FROM disputes x WHERE x.booking_id = b.id AND x.status IN ('open', 'under_review'))
        AND NOT EXISTS (SELECT 1 FROM issue_reports i WHERE i.booking_id = b.id AND i.kind = 'damage' AND i.status IN ('open', 'under_review')))
       OR b.status_changed_at < now() - make_interval(secs => @window_seconds::float8))
ORDER BY b.status_changed_at LIMIT @lim
FOR UPDATE OF d SKIP LOCKED;

-- name: ApprovedDamageDeductions :one
-- Resolved disputes the owner won against this customer (damage and any other approved charge),
-- each with a recorded resolution, less what could not be recovered from the deposit.
SELECT COALESCE(sum(resolved_amount_paise - unrecovered_paise), 0)::bigint AS total FROM disputes
WHERE booking_id = @booking_id AND status = 'resolved'
  AND raised_by_org_id = @owner_org_id AND against_org_id = @customer_org_id
  AND resolved_amount_paise > 0 AND btrim(resolution) <> '';

-- name: HeldDepositsOfCancelled :many
SELECT d.* FROM security_deposits d JOIN bookings b ON b.id = d.booking_id
WHERE d.status = 'held' AND b.status IN ('cancelled', 'refund_pending', 'refunded', 'expired', 'rejected')
  AND NOT EXISTS (SELECT 1 FROM refunds r WHERE r.booking_id = b.id AND r.status = 'pending')
ORDER BY d.created_at LIMIT @lim
FOR UPDATE OF d SKIP LOCKED;

-- name: ProcessedRefundTotal :one
SELECT COALESCE(sum(amount_paise), 0)::bigint AS total FROM refunds WHERE payment_id = @payment_id AND status = 'processed';

-- name: ListDeposits :many
SELECT d.*, b.reference AS booking_reference, e.title AS equipment_title
FROM security_deposits d JOIN bookings b ON b.id = d.booking_id JOIN equipment e ON e.id = b.equipment_id
WHERE (sqlc.narg(owner_org_id)::uuid IS NULL OR d.owner_org_id = sqlc.narg(owner_org_id)::uuid)
  AND (sqlc.narg(status)::text IS NULL OR d.status::text = sqlc.narg(status)::text)
ORDER BY d.created_at DESC
LIMIT @lim;

-- ---- commission configuration -------------------------------------------------------------------

-- name: CurrentDefaultCommission :one
SELECT id, commission_bps, customer_fee_bps, valid_from, created_by
FROM commission_rules
WHERE category_id IS NULL AND city_id IS NULL AND owner_org_id IS NULL
  AND valid_from <= now() AND (valid_until IS NULL OR valid_until > now())
ORDER BY priority DESC, valid_from DESC
LIMIT 1;

-- name: EndDefaultCommission :exec
UPDATE commission_rules SET valid_until = now()
WHERE category_id IS NULL AND city_id IS NULL AND owner_org_id IS NULL AND valid_from <= now() AND (valid_until IS NULL OR valid_until > now());

-- name: InsertCommissionRule :one
INSERT INTO commission_rules (category_id, city_id, owner_org_id, commission_bps, customer_fee_bps, priority, created_by)
VALUES (sqlc.narg(category_id), sqlc.narg(city_id), sqlc.narg(owner_org_id), @commission_bps, @customer_fee_bps, @priority, @created_by)
RETURNING id, valid_from;

-- name: ListActiveCommissionRules :many
SELECT r.id, r.category_id, r.city_id, r.owner_org_id, r.commission_bps, r.customer_fee_bps, r.priority, r.valid_from,
       c.name AS category_name, ci.name AS city_name, o.display_name AS owner_name
FROM commission_rules r
LEFT JOIN equipment_categories c ON c.id = r.category_id
LEFT JOIN cities ci ON ci.id = r.city_id
LEFT JOIN organizations o ON o.id = r.owner_org_id
WHERE r.valid_from <= now() AND (r.valid_until IS NULL OR r.valid_until > now())
ORDER BY r.priority DESC, r.valid_from DESC;

-- name: CommissionForBooking :one
-- The owner commission in force when the booking was made.
SELECT r.id, r.commission_bps
FROM bookings b
JOIN equipment e ON e.id = b.equipment_id
JOIN equipment_models m ON m.id = e.model_id
JOIN commission_rules r ON (r.category_id IS NULL OR r.category_id = m.category_id)
  AND (r.city_id IS NULL OR r.city_id = e.city_id)
  AND (r.owner_org_id IS NULL OR r.owner_org_id = b.owner_org_id)
  AND r.valid_from <= b.created_at AND (r.valid_until IS NULL OR r.valid_until > b.created_at)
WHERE b.id = @booking_id
ORDER BY r.priority DESC, r.valid_from DESC
LIMIT 1;

-- ---- settlements -------------------------------------------------------------------------------

-- name: SettlementCandidates :many
-- Finished bookings with no settlement whose deposit (if any) is resolved
-- and with no refund still in flight.
SELECT b.id
FROM bookings b
WHERE b.status IN ('completed', 'closed', 'cancelled', 'refunded')
  AND NOT EXISTS (SELECT 1 FROM settlements s WHERE s.booking_id = b.id)
  AND NOT EXISTS (SELECT 1 FROM security_deposits d WHERE d.booking_id = b.id AND d.status IN ('held', 'claimed'))
  AND NOT EXISTS (SELECT 1 FROM refunds r WHERE r.booking_id = b.id AND r.status = 'pending')
  AND EXISTS (SELECT 1 FROM payments p WHERE p.booking_id = b.id AND p.status IN ('captured', 'partially_refunded'))
ORDER BY b.status_changed_at
LIMIT @lim
FOR UPDATE OF b SKIP LOCKED;

-- name: NetCapturedByPurpose :many
SELECT p.purpose::text AS purpose,
       COALESCE(sum(p.amount_paise), 0)::bigint AS captured_paise,
       COALESCE(sum((SELECT COALESCE(sum(r.amount_paise), 0) FROM refunds r WHERE r.payment_id = p.id AND r.status = 'processed')), 0)::bigint AS refunded_paise
FROM payments p
WHERE p.booking_id = @booking_id AND p.status IN ('captured', 'partially_refunded', 'refunded')
GROUP BY p.purpose;

-- name: InsertSettlement :one
INSERT INTO settlements (booking_id, owner_org_id, commission_rule_id, commission_bps, gross_paise, commission_paise,
  gst_on_commission_paise, tcs_paise, tds_paise, deductions_paise, net_payable_paise, eligible_at, status, hold_reason,
  customer_paid_paise, rental_paise, delivery_paise, owner_tax_paise, platform_fee_paise, platform_fee_tax_paise,
  deposit_deduction_paise, commission_base_paise, refunded_paise)
VALUES (@booking_id, @owner_org_id, sqlc.narg(commission_rule_id), @commission_bps, @gross_paise, @commission_paise,
  @gst_on_commission_paise, @tcs_paise, @tds_paise, 0, @net_payable_paise, @eligible_at, @status, sqlc.narg(hold_reason),
  @customer_paid_paise, @rental_paise, @delivery_paise, @owner_tax_paise, @platform_fee_paise, @platform_fee_tax_paise,
  @deposit_deduction_paise, @commission_base_paise, @refunded_paise)
ON CONFLICT (booking_id) DO NOTHING
RETURNING *;

-- name: GetSettlement :one
SELECT s.*, b.reference AS booking_reference, e.title AS equipment_title, b.status::text AS booking_status
FROM settlements s JOIN bookings b ON b.id = s.booking_id JOIN equipment e ON e.id = b.equipment_id
WHERE s.id = @id;

-- name: GetSettlementByBooking :one
SELECT * FROM settlements WHERE booking_id = @booking_id;

-- name: ListSettlements :many
SELECT s.*, b.reference AS booking_reference, e.title AS equipment_title, o.display_name AS owner_name
FROM settlements s JOIN bookings b ON b.id = s.booking_id JOIN equipment e ON e.id = b.equipment_id
JOIN organizations o ON o.id = s.owner_org_id
WHERE (sqlc.narg(owner_org_id)::uuid IS NULL OR s.owner_org_id = sqlc.narg(owner_org_id)::uuid)
  AND (sqlc.narg(status)::text IS NULL OR s.status::text = sqlc.narg(status)::text)
  AND (sqlc.narg(before)::timestamptz IS NULL OR s.created_at < sqlc.narg(before)::timestamptz)
ORDER BY s.created_at DESC
LIMIT @lim;

-- name: SetSettlementStatus :one
UPDATE settlements SET status = @status, hold_reason = sqlc.narg(hold_reason),
       paid_at = CASE WHEN @status = 'paid' THEN now() ELSE paid_at END
WHERE id = @id AND status = ANY(@from_statuses::settlement_status[])
RETURNING *;

-- name: ReleaseDueSettlements :many
-- pending -> eligible once the hold window has passed and nothing is disputed.
UPDATE settlements s SET status = 'eligible'
WHERE s.status = 'pending' AND s.eligible_at <= now()
  AND NOT EXISTS (SELECT 1 FROM disputes d WHERE d.booking_id = s.booking_id AND d.status IN ('open', 'under_review'))
RETURNING s.id;

-- name: HoldDisputedSettlements :many
UPDATE settlements s SET status = 'on_hold', hold_reason = 'A dispute is open on this booking'
WHERE s.status IN ('pending', 'eligible')
  AND EXISTS (SELECT 1 FROM disputes d WHERE d.booking_id = s.booking_id AND d.status IN ('open', 'under_review'))
RETURNING s.id;

-- name: PayableSettlements :many
SELECT s.*, l.id AS linked_id, l.account_id, l.bank_account_id
FROM settlements s
JOIN payout_linked_accounts l ON l.owner_org_id = s.owner_org_id AND l.activation_status = 'activated'
WHERE s.status = 'eligible' AND s.net_payable_paise > 0
  AND NOT EXISTS (SELECT 1 FROM payout_items pi JOIN payouts po ON po.id = pi.payout_id
                   WHERE pi.settlement_id = s.id AND pi.active AND po.status IN ('queued', 'processing', 'paid'))
ORDER BY s.eligible_at
LIMIT @lim
FOR UPDATE OF s SKIP LOCKED;

-- name: ZeroSettlementsPaid :many
UPDATE settlements SET status = 'paid', paid_at = now() WHERE status = 'eligible' AND net_payable_paise = 0 RETURNING id;

-- name: OwnerEarnings :one
SELECT
  COALESCE(sum(net_payable_paise) FILTER (WHERE status = 'paid'), 0)::bigint AS paid_out_paise,
  COALESCE(sum(net_payable_paise) FILTER (WHERE status IN ('pending', 'eligible', 'on_hold')), 0)::bigint AS pending_paise,
  COALESCE(sum(net_payable_paise) FILTER (WHERE status = 'on_hold'), 0)::bigint AS on_hold_paise,
  COALESCE(sum(gross_paise), 0)::bigint AS gross_paise,
  COALESCE(sum(commission_paise + gst_on_commission_paise), 0)::bigint AS commission_paise,
  COALESCE(sum(tcs_paise + tds_paise), 0)::bigint AS withheld_paise,
  count(*) FILTER (WHERE status IN ('pending', 'eligible', 'on_hold'))::int AS pending_count
FROM settlements WHERE owner_org_id = @owner_org_id;

-- name: OwnerRentalCounts :one
SELECT count(*) FILTER (WHERE bk.status IN ('completed', 'closed'))::int AS completed,
       count(*) FILTER (WHERE bk.status IN ('confirmed', 'operator_assigned', 'dispatched', 'on_site', 'active', 'extension_requested', 'return_initiated', 'returned'))::int AS in_progress,
       count(*) FILTER (WHERE bk.status IN ('completed', 'closed') AND NOT EXISTS (SELECT 1 FROM settlements st WHERE st.booking_id = bk.id))::int AS awaiting_settlement
FROM bookings bk WHERE bk.owner_org_id = @owner_org_id;

-- name: OwnerMonthlyEarnings :many
SELECT to_char(date_trunc('month', created_at AT TIME ZONE 'Asia/Kolkata'), 'YYYY-MM') AS month,
       COALESCE(sum(net_payable_paise), 0)::bigint AS net_paise, count(*)::int AS settlements
FROM settlements WHERE owner_org_id = @owner_org_id AND created_at > now() - interval '12 months'
GROUP BY 1 ORDER BY 1;

-- ---- Route linked accounts --------------------------------------------------------------------

-- name: GetLinkedAccount :one
SELECT * FROM payout_linked_accounts WHERE owner_org_id = @owner_org_id;

-- name: InsertLinkedAccount :one
INSERT INTO payout_linked_accounts (owner_org_id, provider, mode, account_id, product_id, activation_status, requirements, bank_account_id, created_by)
VALUES (@owner_org_id, @provider, @mode, @account_id, sqlc.narg(product_id), @activation_status, @requirements, @bank_account_id, @created_by)
RETURNING *;

-- name: UpdateLinkedAccountStatus :one
UPDATE payout_linked_accounts SET activation_status = @activation_status, requirements = @requirements,
       product_id = COALESCE(sqlc.narg(product_id), product_id)
WHERE account_id = @account_id
RETURNING *;

-- name: OwnerPayoutProfile :one
SELECT o.id, o.legal_name, o.display_name, o.org_type::text AS org_type, o.kyc_status::text AS kyc_status, o.pan_enc,
       o.contact_name, o.contact_phone, COALESCE(o.contact_email::text, '')::text AS contact_email,
       ba.id AS bank_account_id, ba.account_holder_name, ba.account_number_enc, ba.ifsc, ba.account_last4
FROM organizations o
JOIN bank_accounts ba ON ba.organization_id = o.id AND ba.is_primary AND ba.deleted_at IS NULL AND ba.verification_status = 'verified'
WHERE o.id = @id;

-- ---- payouts ------------------------------------------------------------------------------------

-- name: InsertPayout :one
INSERT INTO payouts (owner_org_id, bank_account_id, linked_account_id, amount_paise, status, batch_date)
VALUES (@owner_org_id, sqlc.narg(bank_account_id), @linked_account_id, @amount_paise, 'processing', (now() AT TIME ZONE 'Asia/Kolkata')::date)
RETURNING id;

-- name: InsertPayoutItem :exec
INSERT INTO payout_items (payout_id, settlement_id, amount_paise) VALUES (@payout_id, @settlement_id, @amount_paise);

-- name: TransferSources :many
-- Captured payments of a booking with what can still be transferred.
SELECT p.id, p.gateway_payment_id, p.purpose::text AS purpose,
       (p.amount_paise
        - COALESCE((SELECT sum(r.amount_paise) FROM refunds r WHERE r.payment_id = p.id AND r.status <> 'failed'), 0)
        - COALESCE((SELECT sum(t.amount_paise) FROM payout_transfers t WHERE t.payment_id = p.id AND t.status NOT IN ('failed', 'reversed')), 0))::bigint AS available_paise,
       EXISTS (SELECT 1 FROM refunds r WHERE r.payment_id = p.id AND r.status <> 'failed') AS has_refunds
FROM payments p
WHERE p.booking_id = @booking_id AND p.status IN ('captured', 'partially_refunded')
ORDER BY (p.purpose = 'deposit'), p.created_at;

-- name: InsertPayoutTransfer :one
INSERT INTO payout_transfers (payout_id, payment_id, amount_paise, gateway_transfer_id, status)
VALUES (@payout_id, @payment_id, @amount_paise, sqlc.narg(gateway_transfer_id), @status)
RETURNING id;

-- name: SetTransferStatus :one
UPDATE payout_transfers SET status = @status, failure_reason = sqlc.narg(failure_reason)
WHERE gateway_transfer_id = @gateway_transfer_id AND status IN ('pending', 'created')
RETURNING payout_id;

-- name: PayoutTransferState :one
SELECT count(*) FILTER (WHERE status IN ('pending', 'created'))::int AS open,
       count(*) FILTER (WHERE status IN ('failed', 'reversed'))::int AS failed
FROM payout_transfers WHERE payout_id = @payout_id;

-- name: FinishPayout :one
UPDATE payouts SET status = @status, paid_at = CASE WHEN @status = 'paid' THEN now() END, failure_reason = sqlc.narg(failure_reason)
WHERE id = @id AND status = 'processing'
RETURNING id, owner_org_id, amount_paise;

-- name: PayoutSettlements :many
SELECT s.id, s.booking_id, s.net_payable_paise FROM payout_items pi JOIN settlements s ON s.id = pi.settlement_id WHERE pi.payout_id = @payout_id;

-- name: ListPayouts :many
SELECT po.id, po.amount_paise, po.status::text AS status, po.batch_date, po.paid_at, po.failure_reason, po.created_at,
       COALESCE((SELECT array_agg(b.reference ORDER BY b.reference) FROM payout_items pi JOIN settlements s ON s.id = pi.settlement_id
                  JOIN bookings b ON b.id = s.booking_id WHERE pi.payout_id = po.id), '{}')::text[] AS booking_references
FROM payouts po
WHERE (sqlc.narg(owner_org_id)::uuid IS NULL OR po.owner_org_id = sqlc.narg(owner_org_id)::uuid)
ORDER BY po.created_at DESC
LIMIT @lim;

-- name: BookingParties :one
SELECT b.id, b.reference, b.owner_org_id, b.customer_org_id, b.status::text AS status FROM bookings b WHERE b.id = @id;

-- ---- tax rules, payout idempotency, reconciliation (000042) ----------------------------------

-- name: CurrentTaxRate :one
SELECT rate_bps FROM tax_rules WHERE code = @code AND effective_from <= @at ORDER BY effective_from DESC LIMIT 1;

-- name: PayoutByIdempotencyKey :one
SELECT id FROM payouts WHERE owner_org_id = @owner_org_id AND idempotency_key = @key;

-- name: InsertPayoutIdem :one
INSERT INTO payouts (owner_org_id, bank_account_id, linked_account_id, amount_paise, status, batch_date, idempotency_key)
VALUES (@owner_org_id, sqlc.narg(bank_account_id), @linked_account_id, @amount_paise, 'processing', (now() AT TIME ZONE 'Asia/Kolkata')::date, @idempotency_key)
ON CONFLICT (owner_org_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
RETURNING id;

-- name: DeactivatePayoutItems :exec
UPDATE payout_items SET active = false WHERE payout_id = @payout_id;

-- name: InsertReconciliation :exec
INSERT INTO payout_reconciliations (payout_id, gateway_status, gateway_paise, matches, note)
VALUES (@payout_id, @gateway_status, @gateway_paise, @matches, sqlc.narg(note));

-- name: PayoutsToReconcile :many
SELECT id, owner_org_id, amount_paise, status::text AS status FROM payouts
WHERE status IN ('processing', 'paid') AND NOT EXISTS (SELECT 1 FROM payout_reconciliations r WHERE r.payout_id = payouts.id AND r.matches)
ORDER BY created_at LIMIT @lim;

-- name: ListReconciliationMismatches :many
SELECT r.*, p.owner_org_id FROM payout_reconciliations r JOIN payouts p ON p.id = r.payout_id
WHERE NOT r.matches ORDER BY r.checked_at DESC LIMIT @lim;

-- name: SettlementInputs :one
SELECT b.id, b.owner_org_id, b.price_snapshot AS price, b.status_changed_at,
       (o.gstin IS NOT NULL)::bool AS owner_gst, (o.pan_enc IS NOT NULL)::bool AS owner_pan
FROM bookings b JOIN organizations o ON o.id = b.owner_org_id WHERE b.id = @booking_id;

-- name: SetPayoutGatewayID :exec
UPDATE payouts SET gateway_transfer_id = @gateway_transfer_id WHERE id = @id;

-- name: FailedPayoutSettlements :many
SELECT pi.settlement_id FROM payout_items pi WHERE pi.payout_id = @payout_id;

-- name: GetPayoutGatewayID :one
SELECT gateway_transfer_id FROM payouts WHERE id = @id;
