-- name: GetQuoteInputs :one
-- Everything needed to price a request for one machine and site.
SELECT e.id AS equipment_id, e.owner_org_id, e.status AS equipment_status, e.instant_book, e.city_id,
       m.category_id, e.service_radius_km,
       p.id AS plan_id, p.unit, p.rate_paise, p.min_units, p.included_hours, p.extra_hour_rate_paise,
       p.operator_bata_per_day_paise,
       COALESCE(mr.fixed_paise, 0)::bigint AS mob_fixed_paise, COALESCE(mr.per_km_paise, 0)::bigint AS mob_per_km_paise,
       COALESCE(mr.free_km, 0)::int AS mob_free_km,
       (ST_Distance(e.base_location, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography) / 1000.0)::float8 AS distance_km
FROM equipment e
JOIN equipment_models m ON m.id = e.model_id
JOIN pricing_plans p ON p.equipment_id = e.id AND p.is_active AND p.unit = @unit AND p.with_operator = @with_operator
LEFT JOIN mobilisation_rates mr ON mr.equipment_id = e.id
WHERE e.id = @equipment_id AND e.deleted_at IS NULL;

-- name: GetCommissionRule :one
SELECT commission_bps, customer_fee_bps
FROM commission_rules
WHERE (category_id IS NULL OR category_id = sqlc.narg(category_id))
  AND (city_id IS NULL OR city_id = sqlc.narg(city_id))
  AND (owner_org_id IS NULL OR owner_org_id = @owner_org_id)
  AND valid_from <= now() AND (valid_until IS NULL OR valid_until > now())
ORDER BY priority DESC, valid_from DESC
LIMIT 1;

-- name: GetDefaultCancellationPolicy :one
SELECT id, code, rules FROM cancellation_policies WHERE is_default;

-- name: GetBookingDetail :one
SELECT b.id, b.reference, b.equipment_id, b.owner_org_id, b.customer_org_id, b.requested_by, b.site_address_id,
       b.status, b.version, b.with_operator, b.pricing_unit, b.price_snapshot, b.estimated_total_paise,
       b.final_total_paise, b.deposit_paise, b.currency, b.payment_mode, b.work_description, b.cancel_reason,
       b.owner_response_due_at, b.status_changed_at, b.created_at, b.site_geofence_m,
       lower(b.period)::timestamptz AS starts_at, upper(b.period)::timestamptz AS ends_at,
       ST_Y(b.site_location::geometry)::float8 AS site_lat, ST_X(b.site_location::geometry)::float8 AS site_lng,
       e.title AS equipment_title, ow.display_name AS owner_name, cu.display_name AS customer_name,
       cp.rules AS cancellation_rules, b.delivery, b.pickup, b.final_charges, b.finalized_at,
       b.resume_status, b.payment_due_at, e.timezone AS equipment_timezone,
       ru.full_name AS requester_name, ru.phone AS requester_phone, cu.kyc_status::text AS customer_kyc_status,
       (SELECT count(*) FROM bookings x WHERE x.customer_org_id = b.customer_org_id
          AND x.status IN ('completed', 'closed'))::int AS customer_completed_rentals,
       b.customer_type, b.customer_details,
       COALESCE(sa.label, '')::text AS site_label,
       concat_ws(', ', sa.line1, sa.line2, sa.city, sa.pincode)::text AS site_address
FROM bookings b
JOIN equipment e ON e.id = b.equipment_id
JOIN organizations ow ON ow.id = b.owner_org_id
JOIN organizations cu ON cu.id = b.customer_org_id
JOIN users ru ON ru.id = b.requested_by
LEFT JOIN addresses sa ON sa.id = b.site_address_id
LEFT JOIN cancellation_policies cp ON cp.id = b.cancellation_policy_id
WHERE b.id = @id;

-- name: ListBookingsForOrg :many
-- side = 'customer' or 'owner'; statuses (optional) filters by any of them.
SELECT b.id, b.reference, b.status, b.version, lower(b.period)::timestamptz AS starts_at, upper(b.period)::timestamptz AS ends_at,
       b.estimated_total_paise, b.final_total_paise, b.currency, b.created_at, e.title AS equipment_title, b.equipment_id,
       b.owner_org_id, b.customer_org_id, b.with_operator, b.payment_due_at, b.owner_response_due_at,
       ow.display_name AS owner_name, cu.display_name AS customer_name, e.timezone AS equipment_timezone
FROM bookings b
JOIN equipment e ON e.id = b.equipment_id
JOIN organizations ow ON ow.id = b.owner_org_id
JOIN organizations cu ON cu.id = b.customer_org_id
WHERE ((@side::text = 'customer' AND b.customer_org_id = @org_id) OR (@side::text = 'owner' AND b.owner_org_id = @org_id AND b.status <> 'draft'))
  AND (sqlc.narg(statuses)::text[] IS NULL OR b.status::text = ANY (sqlc.narg(statuses)::text[]))
  AND (sqlc.narg(equipment_id)::uuid IS NULL OR b.equipment_id = sqlc.narg(equipment_id))
  AND (sqlc.narg(equipment_ids)::uuid[] IS NULL OR b.equipment_id = ANY (sqlc.narg(equipment_ids)::uuid[]))
  AND (sqlc.narg(after)::uuid IS NULL OR b.id < sqlc.narg(after))
ORDER BY b.id DESC
LIMIT @result_limit;

-- name: SetBookingFinalTotal :exec
UPDATE bookings SET final_total_paise = @final_total_paise WHERE id = @id;

-- name: GetActiveAssignment :one
SELECT a.id, a.operator_user_id, a.status, u.full_name AS operator_name, u.phone AS operator_phone
FROM operator_assignments a
JOIN users u ON u.id = a.operator_user_id
WHERE a.booking_id = @booking_id AND a.status IN ('assigned', 'accepted');

-- name: CompleteAssignments :exec
UPDATE operator_assignments SET status = 'completed'
WHERE booking_id = @booking_id AND status IN ('assigned', 'accepted');

-- name: ExpireStaleBookingRequests :many
-- Owners that miss the response SLA lose the request (worker job).
UPDATE bookings SET status = 'expired'
WHERE id IN (
  SELECT id FROM bookings
  WHERE status = 'requested' AND owner_response_due_at < now()
  ORDER BY owner_response_due_at
  LIMIT @batch_size
  FOR UPDATE SKIP LOCKED)
RETURNING id, reference, customer_org_id, owner_org_id;

-- name: AutoApproveWorkLogs :execrows
-- Customers have 48 hours to approve or dispute a daily log.
UPDATE work_logs SET status = 'auto_approved', approved_at = now()
WHERE status = 'submitted' AND created_at < now() - interval '48 hours';

-- name: InsertOutboxEvent :exec
INSERT INTO outbox_events (aggregate_type, aggregate_id, event_type, payload)
VALUES (@aggregate_type, @aggregate_id, @event_type, @payload);

-- name: ClaimOutboxEvents :many
SELECT id, aggregate_type, aggregate_id, event_type, payload, created_at
FROM outbox_events
WHERE published_at IS NULL
ORDER BY created_at
LIMIT @batch_size
FOR UPDATE SKIP LOCKED;

-- name: MarkOutboxPublished :exec
UPDATE outbox_events SET published_at = now() WHERE id = @id;
