-- name: UpsertOwnerProfile :one
INSERT INTO owner_profiles (user_id, organization_id)
VALUES (@user_id, @organization_id)
ON CONFLICT (user_id) DO UPDATE SET organization_id = EXCLUDED.organization_id
RETURNING *;

-- name: GetOwnerProfile :one
SELECT * FROM owner_profiles WHERE user_id = @user_id;

-- name: MarkOrgSupplier :exec
UPDATE organizations SET is_supplier = true WHERE id = @id AND NOT is_supplier;

-- name: OwnerDashboard :one
SELECT
  (SELECT count(*) FROM equipment e WHERE e.owner_org_id = @org_id AND e.deleted_at IS NULL)::int AS equipment_total,
  (SELECT count(*) FROM equipment e WHERE e.owner_org_id = @org_id AND e.status = 'active' AND e.deleted_at IS NULL)::int AS equipment_active,
  (SELECT count(*) FROM equipment e WHERE e.owner_org_id = @org_id AND e.status = 'pending_review')::int AS equipment_pending_review,
  (SELECT count(*) FROM bookings b WHERE b.owner_org_id = @org_id AND b.status = 'requested')::int AS bookings_awaiting_response,
  (SELECT count(*) FROM bookings b WHERE b.owner_org_id = @org_id
     AND b.status IN ('confirmed', 'operator_assigned', 'dispatched', 'on_site', 'active', 'extension_requested', 'return_initiated', 'cancellation_requested'))::int AS bookings_in_progress,
  (SELECT count(*) FROM bookings b WHERE b.owner_org_id = @org_id AND b.status IN ('completed', 'closed'))::int AS bookings_completed,
  (SELECT COALESCE(sum(p.amount_paise), 0) FROM payments p JOIN bookings b ON b.id = p.booking_id
     WHERE b.owner_org_id = @org_id AND p.status IN ('captured', 'partially_refunded')
       AND p.captured_at >= now() - interval '30 days')::bigint AS captured_last_30d_paise;

-- name: GetPublicOwner :one
SELECT o.id, o.display_name, o.kyc_status, o.rating_avg, o.rating_count, o.created_at,
       (SELECT count(*) FROM equipment e WHERE e.owner_org_id = o.id AND e.status = 'active' AND e.deleted_at IS NULL)::int AS active_listings,
       op.response_time_minutes, op.acceptance_rate_bps
FROM organizations o
LEFT JOIN LATERAL (SELECT * FROM owner_profiles p WHERE p.organization_id = o.id ORDER BY p.created_at LIMIT 1) op ON true
WHERE o.id = @id AND o.deleted_at IS NULL AND o.is_supplier;
