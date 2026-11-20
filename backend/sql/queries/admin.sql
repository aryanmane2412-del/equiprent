-- name: AdminListUsers :many
SELECT id, phone, full_name, COALESCE(email::text, '')::text AS email, status, created_at, last_login_at
FROM users
WHERE (sqlc.narg(status)::text IS NULL OR status::text = sqlc.narg(status))
  AND (sqlc.narg(query)::text IS NULL OR phone LIKE '%' || sqlc.narg(query) || '%' OR full_name ILIKE '%' || sqlc.narg(query) || '%')
  AND (sqlc.narg(after)::uuid IS NULL OR id < sqlc.narg(after))
ORDER BY id DESC
LIMIT @result_limit;

-- name: SetUserStatus :one
UPDATE users SET status = @status
WHERE id = @id AND deleted_at IS NULL AND status <> 'deleted'
RETURNING id, status;

-- name: InsertAuditLog :exec
INSERT INTO audit_logs (actor_user_id, actor_role, action, entity_type, entity_id, reason, request_id, after)
VALUES (sqlc.narg(actor_user_id), sqlc.narg(actor_role), @action, @entity_type, sqlc.narg(entity_id),
        sqlc.narg(reason), sqlc.narg(request_id), sqlc.narg(after));

-- name: ListAuditLogs :many
SELECT id, occurred_at, actor_user_id, actor_role, action, entity_type, entity_id, reason, request_id
FROM audit_logs
WHERE (sqlc.narg(entity_type)::text IS NULL OR entity_type = sqlc.narg(entity_type))
  AND (sqlc.narg(entity_id)::uuid IS NULL OR entity_id = sqlc.narg(entity_id))
  AND (sqlc.narg(actor_user_id)::uuid IS NULL OR actor_user_id = sqlc.narg(actor_user_id))
  AND (sqlc.narg(before_id)::bigint IS NULL OR id < sqlc.narg(before_id))
ORDER BY id DESC
LIMIT @result_limit;

-- name: PlatformStats :one
SELECT
  (SELECT count(*) FROM users WHERE deleted_at IS NULL)::int AS users,
  (SELECT count(*) FROM organizations WHERE deleted_at IS NULL)::int AS organizations,
  (SELECT count(*) FROM equipment WHERE status = 'active' AND deleted_at IS NULL)::int AS active_listings,
  (SELECT count(*) FROM equipment WHERE status = 'pending_review')::int AS listings_pending_review,
  (SELECT count(*) FROM bookings WHERE status = 'requested')::int AS bookings_requested,
  (SELECT count(*) FROM bookings WHERE status IN ('confirmed', 'operator_assigned', 'dispatched', 'on_site', 'active', 'extension_requested', 'return_initiated', 'cancellation_requested'))::int AS bookings_in_progress,
  (SELECT count(*) FROM disputes WHERE status IN ('open', 'under_review'))::int AS open_disputes,
  (SELECT COALESCE(sum(amount_paise), 0) FROM payments
     WHERE status IN ('captured', 'partially_refunded') AND captured_at >= now() - interval '30 days')::bigint AS gmv_30d_paise;

-- name: AdminReadOnlyDashboard :one
WITH ledger AS (
  SELECT
    COALESCE(sum(e.amount_paise) FILTER (WHERE a.code IN ('platform_revenue:commission','platform_revenue:convenience_fee') AND e.direction='C'),0)::bigint AS platform_revenue_paise,
    COALESCE(sum(e.amount_paise) FILTER (WHERE a.type='expense' AND e.direction='D'),0)::bigint AS platform_expenses_paise
  FROM ledger_entries e JOIN ledger_accounts a ON a.id=e.account_id JOIN ledger_transactions t ON t.id=e.transaction_id
  WHERE t.created_at >= @from_at AND t.created_at < @to_at
), counts AS (
  SELECT
    (SELECT count(DISTINCT ur.organization_id) FROM user_roles ur JOIN users u ON u.id=ur.user_id WHERE ur.role_code='owner' AND ur.revoked_at IS NULL AND u.deleted_at IS NULL)::int AS owners,
    (SELECT count(DISTINCT ur.organization_id) FROM user_roles ur JOIN users u ON u.id=ur.user_id WHERE ur.role_code='customer' AND ur.revoked_at IS NULL AND u.deleted_at IS NULL)::int AS customers,
    (SELECT count(DISTINCT ur.user_id) FROM user_roles ur JOIN users u ON u.id=ur.user_id WHERE ur.role_code='operator' AND ur.revoked_at IS NULL AND u.deleted_at IS NULL)::int AS drivers,
    (SELECT count(*) FROM equipment WHERE deleted_at IS NULL)::int AS equipment,
    (SELECT count(*) FROM bookings WHERE created_at >= @from_at AND created_at < @to_at)::int AS bookings,
    (SELECT count(*) FROM bookings WHERE status IN ('requested','payment_pending','accepted','quoted') AND created_at >= @from_at AND created_at < @to_at)::int AS pending_bookings,
    (SELECT count(*) FROM bookings WHERE status IN ('completed','closed') AND status_changed_at >= @from_at AND status_changed_at < @to_at)::int AS completed_bookings,
    (SELECT COALESCE(sum(amount_paise),0) FROM payments WHERE status IN ('captured','partially_refunded','refunded') AND captured_at >= @from_at AND captured_at < @to_at)::bigint AS successful_customer_payments_paise,
    (SELECT count(*) FROM payments WHERE created_at >= @from_at AND created_at < @to_at)::int AS transactions,
    (SELECT COALESCE(sum(agreed_amount_paise),0) FROM driver_compensations WHERE status='paid' AND paid_at >= @from_at AND paid_at < @to_at)::bigint AS driver_payouts_paise,
    (SELECT COALESCE(sum(amount_paise),0) FROM refunds WHERE status='processed' AND processed_at >= @from_at AND processed_at < @to_at)::bigint AS refunds_paise,
    (SELECT COALESCE(sum(net_payable_paise),0) FROM settlements WHERE created_at >= @from_at AND created_at < @to_at)::bigint AS owner_settlements_paise,
    (SELECT count(*) FROM settlements WHERE status IN ('pending','eligible','on_hold'))::int AS pending_settlements
)
SELECT counts.*, ledger.platform_revenue_paise, ledger.platform_expenses_paise,
       (ledger.platform_revenue_paise-ledger.platform_expenses_paise-counts.refunds_paise-counts.driver_payouts_paise)::bigint AS net_platform_profit_paise
FROM counts CROSS JOIN ledger;

-- name: AdminBookingCategoryStats :many
SELECT COALESCE(c.name->>'en', 'Other')::text AS category, count(*)::int AS bookings
FROM bookings b
JOIN equipment e ON e.id = b.equipment_id
LEFT JOIN equipment_models m ON m.id = e.model_id
LEFT JOIN equipment_categories c ON c.id = m.category_id
WHERE b.created_at >= @from_at AND b.created_at < @to_at
GROUP BY 1 ORDER BY bookings DESC, category;

-- name: AdminBookingTrend :many
SELECT date_trunc('month', b.created_at)::date AS month, count(*)::int AS bookings
FROM bookings b
WHERE b.created_at >= @from_at AND b.created_at < @to_at
GROUP BY 1 ORDER BY 1;

-- name: AdminReadOnlyUsers :many
SELECT u.id, u.phone, u.full_name, COALESCE(u.email::text,'')::text AS email, u.status::text AS status,
       u.created_at, COALESCE(array_agg(DISTINCT ur.role_code ORDER BY ur.role_code) FILTER (WHERE ur.revoked_at IS NULL), '{}')::text[] AS roles
FROM users u LEFT JOIN user_roles ur ON ur.user_id=u.id
WHERE u.deleted_at IS NULL AND (sqlc.narg(after)::uuid IS NULL OR u.id < sqlc.narg(after))
GROUP BY u.id ORDER BY u.id DESC LIMIT @result_limit;

-- name: AdminReadOnlyEquipment :many
SELECT e.id, e.title, e.status::text AS status, e.created_at, COALESCE(m.name,'')::text AS model_name,
       COALESCE(o.display_name,'')::text AS owner_name, COALESCE(c.name->>'en','')::text AS category_name
FROM equipment e JOIN organizations o ON o.id=e.owner_org_id
LEFT JOIN equipment_models m ON m.id=e.model_id LEFT JOIN equipment_categories c ON c.id=m.category_id
WHERE e.deleted_at IS NULL AND (sqlc.narg(after)::uuid IS NULL OR e.id < sqlc.narg(after))
ORDER BY e.id DESC LIMIT @result_limit;

-- name: AdminReadOnlyBookings :many
SELECT b.id, b.reference, b.status::text AS status, b.estimated_total_paise, b.created_at,
       e.title AS equipment_title, oc.display_name AS owner_name, cc.display_name AS customer_name
FROM bookings b JOIN equipment e ON e.id=b.equipment_id
JOIN organizations oc ON oc.id=b.owner_org_id JOIN organizations cc ON cc.id=b.customer_org_id
WHERE b.created_at >= @from_at AND b.created_at < @to_at
 AND (sqlc.narg(after)::uuid IS NULL OR b.id < sqlc.narg(after))
ORDER BY b.id DESC LIMIT @result_limit;

-- name: AdminReadOnlyPayments :many
SELECT p.id, b.reference AS booking_reference, p.amount_paise, p.status::text AS status,
       p.purpose::text AS purpose, p.gateway, p.gateway_mode, p.captured_at, p.created_at
FROM payments p JOIN bookings b ON b.id=p.booking_id
WHERE COALESCE(p.captured_at,p.created_at) >= @from_at AND COALESCE(p.captured_at,p.created_at) < @to_at
 AND (sqlc.narg(after)::uuid IS NULL OR p.id < sqlc.narg(after))
ORDER BY p.id DESC LIMIT @result_limit;

-- name: AdminRevenueSeries :many
SELECT date_trunc('month', t.created_at)::date AS month,
 COALESCE(sum(e.amount_paise) FILTER (WHERE a.code IN ('platform_revenue:commission','platform_revenue:convenience_fee') AND e.direction='C'),0)::bigint AS revenue_paise,
 COALESCE(sum(e.amount_paise) FILTER (WHERE a.type='expense' AND e.direction='D'),0)::bigint AS expenses_paise
FROM ledger_transactions t JOIN ledger_entries e ON e.transaction_id=t.id JOIN ledger_accounts a ON a.id=e.account_id
WHERE t.created_at >= @from_at AND t.created_at < @to_at
GROUP BY 1 ORDER BY 1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = @id AND deleted_at IS NULL;

-- name: AdminEquipmentOwners :many
-- One row per owner organisation (never per user/role row); equipment counted from the equipment table.
SELECT o.id AS org_id, o.display_name::text AS company_name,
       COALESCE(p.full_name,'')::text AS owner_name, COALESCE(p.phone,'')::text AS phone,
       COALESCE(p.email::text,'')::text AS email,
       (SELECT count(*) FROM equipment e WHERE e.owner_org_id=o.id AND e.deleted_at IS NULL)::bigint AS equipment_count
FROM organizations o
LEFT JOIN LATERAL (
  SELECT u.full_name, u.phone, u.email FROM user_roles ur JOIN users u ON u.id=ur.user_id
  WHERE ur.organization_id=o.id AND ur.role_code='owner' AND ur.revoked_at IS NULL AND u.deleted_at IS NULL
  ORDER BY ur.granted_at, ur.id LIMIT 1) p ON true
WHERE o.deleted_at IS NULL
  AND EXISTS (SELECT 1 FROM user_roles x WHERE x.organization_id=o.id AND x.role_code='owner' AND x.revoked_at IS NULL)
  AND (@search::text = '' OR o.display_name ILIKE '%' || @search::text || '%' OR p.full_name ILIKE '%' || @search::text || '%')
ORDER BY lower(o.display_name), o.id
LIMIT @result_limit;
