-- Finance visibility into owner payouts: what needs a person.

-- name: AdminListPayouts :many
SELECT po.id, po.owner_org_id, o.display_name AS owner_name, po.amount_paise, po.status::text AS status,
       po.gateway_transfer_id, po.failure_reason, po.created_at, po.paid_at
FROM payouts po JOIN organizations o ON o.id = po.owner_org_id
WHERE (sqlc.narg(status)::text IS NULL OR po.status::text = sqlc.narg(status)::text)
ORDER BY po.created_at DESC
LIMIT @result_limit;

-- name: AdminStuckPayouts :many
-- Sent to the gateway (or recorded) but still not finished long after.
SELECT po.id, po.owner_org_id, o.display_name AS owner_name, po.amount_paise, po.status::text AS status,
       po.gateway_transfer_id, po.failure_reason, po.created_at, po.paid_at
FROM payouts po JOIN organizations o ON o.id = po.owner_org_id
WHERE po.status = 'processing' AND po.created_at < now() - make_interval(secs => @stuck_secs::float8)
ORDER BY po.created_at
LIMIT @result_limit;

-- name: AdminRecentFailedPayouts :many
SELECT po.id, po.owner_org_id, o.display_name AS owner_name, po.amount_paise, po.status::text AS status,
       po.gateway_transfer_id, po.failure_reason, po.created_at, po.paid_at
FROM payouts po JOIN organizations o ON o.id = po.owner_org_id
WHERE po.status IN ('failed', 'reversed') AND po.created_at > now() - interval '7 days'
ORDER BY po.created_at DESC
LIMIT @result_limit;

-- name: AdminPayoutMismatches :many
-- The latest mismatch per payout between our books and the gateway (including a payout the bank
-- reversed after it was marked paid).
SELECT DISTINCT ON (r.payout_id) r.payout_id, p.owner_org_id, o.display_name AS owner_name, p.amount_paise,
       p.status::text AS our_status, r.gateway_status, r.gateway_paise, r.note, r.checked_at
FROM payout_reconciliations r
JOIN payouts p ON p.id = r.payout_id
JOIN organizations o ON o.id = p.owner_org_id
WHERE NOT r.matches
  AND NOT EXISTS (SELECT 1 FROM payout_reconciliations ok WHERE ok.payout_id = r.payout_id AND ok.matches AND ok.checked_at > r.checked_at)
ORDER BY r.payout_id, r.checked_at DESC
LIMIT @result_limit;

-- name: AdminUnpayableOwners :many
-- Owners with money waiting that cannot be paid because they have no verified payout account.
SELECT s.owner_org_id, o.display_name AS owner_name, count(*)::int AS eligible_count, COALESCE(sum(s.net_payable_paise), 0)::bigint AS eligible_paise
FROM settlements s JOIN organizations o ON o.id = s.owner_org_id
WHERE s.status = 'eligible' AND s.net_payable_paise > 0
  AND NOT EXISTS (SELECT 1 FROM payout_linked_accounts l WHERE l.owner_org_id = s.owner_org_id AND l.activation_status = 'activated'
                  AND l.provider = @provider::text AND l.mode = @mode::text)
GROUP BY s.owner_org_id, o.display_name
ORDER BY eligible_paise DESC
LIMIT @result_limit;
