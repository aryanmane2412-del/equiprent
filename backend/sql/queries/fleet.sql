-- Fleet views. equipment_ids (optional) limits every figure to the machines
-- a fleet manager may see; NULL = the whole organisation (owners).

-- name: FleetOverview :one
SELECT
  (SELECT count(*) FROM equipment e WHERE e.owner_org_id = @org_id AND e.deleted_at IS NULL
     AND (sqlc.narg(equipment_ids)::uuid[] IS NULL OR e.id = ANY (sqlc.narg(equipment_ids)::uuid[])))::int AS equipment_total,
  (SELECT count(*) FROM equipment e WHERE e.owner_org_id = @org_id AND e.status = 'active' AND e.deleted_at IS NULL
     AND (sqlc.narg(equipment_ids)::uuid[] IS NULL OR e.id = ANY (sqlc.narg(equipment_ids)::uuid[])))::int AS listed,
  (SELECT count(DISTINCT b.equipment_id) FROM bookings b
     WHERE b.owner_org_id = @org_id AND b.status IN ('dispatched', 'on_site', 'active', 'extension_requested', 'return_initiated', 'cancellation_requested')
       AND (sqlc.narg(equipment_ids)::uuid[] IS NULL OR b.equipment_id = ANY (sqlc.narg(equipment_ids)::uuid[])))::int AS on_job,
  (SELECT count(*) FROM bookings b WHERE b.owner_org_id = @org_id
     AND b.status IN ('confirmed', 'operator_assigned', 'dispatched', 'on_site', 'active', 'extension_requested', 'return_initiated', 'cancellation_requested')
     AND (sqlc.narg(equipment_ids)::uuid[] IS NULL OR b.equipment_id = ANY (sqlc.narg(equipment_ids)::uuid[])))::int AS active_bookings,
  (SELECT count(DISTINCT ur.user_id) FROM user_roles ur
     WHERE ur.organization_id = @org_id AND ur.role_code = 'operator' AND ur.revoked_at IS NULL)::int AS operators,
  (SELECT count(*) FROM operator_profiles op
     JOIN user_roles ur ON ur.user_id = op.user_id AND ur.organization_id = @org_id AND ur.role_code = 'operator' AND ur.revoked_at IS NULL
     WHERE op.on_duty)::int AS operators_on_duty,
  (SELECT count(*) FROM maintenance_records mr JOIN equipment e ON e.id = mr.equipment_id
     WHERE e.owner_org_id = @org_id AND mr.status IN ('scheduled', 'in_progress')
       AND (sqlc.narg(equipment_ids)::uuid[] IS NULL OR e.id = ANY (sqlc.narg(equipment_ids)::uuid[])))::int AS open_maintenance,
  (SELECT COALESCE(sum(p.amount_paise), 0) FROM payments p JOIN bookings b ON b.id = p.booking_id
     WHERE b.owner_org_id = @org_id AND p.status IN ('captured', 'partially_refunded') AND p.purpose <> 'deposit'
       AND p.captured_at >= now() - interval '30 days'
       AND (sqlc.narg(equipment_ids)::uuid[] IS NULL OR b.equipment_id = ANY (sqlc.narg(equipment_ids)::uuid[])))::bigint AS revenue_30d_paise;

-- name: FleetOperators :many
SELECT u.id AS user_id, u.full_name, u.phone, COALESCE(op.on_duty, false) AS on_duty, op.rating_avg, op.licence_expires_on,
       COALESCE((SELECT b.reference FROM operator_assignments a JOIN bookings b ON b.id = a.booking_id
         WHERE a.operator_user_id = u.id AND a.status IN ('assigned', 'accepted')
         ORDER BY lower(b.period) LIMIT 1), '')::text AS current_booking_reference,
       (op.verified_at IS NOT NULL)::bool AS verified,
       is_restricted(u.id, 'operator_assignments') AS assignments_blocked,
       (u.status::text = 'active')::bool AS active,
       (CASE
         WHEN u.status::text <> 'active' OR op.verified_at IS NULL OR is_restricted(u.id, 'operator_assignments')
              OR op.licence_expires_on IS NULL OR op.licence_expires_on < CURRENT_DATE THEN 'unavailable'
         WHEN NOT COALESCE(op.on_duty, false) THEN 'off_duty'
         WHEN EXISTS (SELECT 1 FROM operator_assignments a JOIN bookings b ON b.id = a.booking_id
              WHERE a.operator_user_id = u.id AND a.status = 'accepted'
                AND b.status IN ('dispatched', 'on_site', 'active', 'return_initiated')) THEN 'on_job'
         WHEN EXISTS (SELECT 1 FROM operator_assignments a
              WHERE a.operator_user_id = u.id AND a.status IN ('assigned', 'accepted')) THEN 'assigned'
         ELSE 'available'
       END)::text AS availability
FROM user_roles ur
JOIN users u ON u.id = ur.user_id AND u.deleted_at IS NULL
LEFT JOIN operator_profiles op ON op.user_id = u.id
WHERE ur.organization_id = @org_id AND ur.role_code = 'operator' AND ur.revoked_at IS NULL
ORDER BY u.full_name;

-- name: FleetLivePositions :many
-- Last known positions of machines that are out on a job (tracking runs
-- only during trips and rentals), limited to the caller's machines.
SELECT e.id AS equipment_id, e.title, ST_Y(l.location::geometry)::float8 AS lat, ST_X(l.location::geometry)::float8 AS lng,
       l.recorded_at
FROM equipment e
JOIN equipment_last_locations l ON l.equipment_id = e.id
WHERE e.owner_org_id = @org_id AND e.deleted_at IS NULL
  AND (sqlc.narg(equipment_ids)::uuid[] IS NULL OR e.id = ANY (sqlc.narg(equipment_ids)::uuid[]))
ORDER BY e.title;

-- name: FleetUtilisation :many
-- Booked hours per machine within [from, to), counting bookings that held
-- the machine or ran to completion, and the rental earnings captured in the
-- window (deposits excluded, refunds deducted).
SELECT e.id AS equipment_id, e.title,
       COALESCE((SELECT sum(EXTRACT(EPOCH FROM (
           LEAST(upper(b.period), @to_at::timestamptz) - GREATEST(lower(b.period), @from_at::timestamptz))) / 3600.0)
         FROM bookings b
         WHERE b.equipment_id = e.id AND b.status NOT IN ('requested', 'quoted', 'rejected', 'expired', 'cancelled', 'draft')
           AND b.period && tstzrange(@from_at::timestamptz, @to_at::timestamptz, '[)')), 0)::float8 AS booked_hours,
       (SELECT count(*) FROM bookings b
         WHERE b.equipment_id = e.id AND b.status NOT IN ('requested', 'quoted', 'rejected', 'expired', 'cancelled', 'draft')
           AND b.period && tstzrange(@from_at::timestamptz, @to_at::timestamptz, '[)'))::int AS bookings,
       COALESCE((SELECT sum(p.amount_paise - COALESCE((SELECT sum(r.amount_paise) FROM refunds r WHERE r.payment_id = p.id AND r.status = 'processed'), 0))
         FROM payments p JOIN bookings b ON b.id = p.booking_id
         WHERE b.equipment_id = e.id AND p.purpose <> 'deposit' AND p.status IN ('captured', 'partially_refunded', 'refunded')
           AND p.captured_at >= @from_at::timestamptz AND p.captured_at < @to_at::timestamptz), 0)::bigint AS earnings_paise
FROM equipment e
WHERE e.owner_org_id = @org_id AND e.deleted_at IS NULL
  AND (sqlc.narg(equipment_ids)::uuid[] IS NULL OR e.id = ANY (sqlc.narg(equipment_ids)::uuid[]))
ORDER BY booked_hours DESC, e.title;

-- name: FleetEquipmentBoard :many
-- Status of each machine: listing, condition, the job it is on, the next
-- booking, open maintenance and the last position.
SELECT e.id, e.title, e.registration_no, e.status::text AS listing_status, e.condition::text AS condition, e.current_hmr,
       COALESCE(f.name, '')::text AS fleet_name, f.id AS fleet_id,
       COALESCE(cur.id::text, '')::text AS current_booking_id, COALESCE(cur.reference, '')::text AS current_reference, COALESCE(cur.status::text, '')::text AS current_status,
       COALESCE(cu_op.full_name, '')::text AS current_operator,
       COALESCE(nxt.id::text, '')::text AS next_booking_id, COALESCE(nxt.reference, '')::text AS next_reference,
       COALESCE(to_char(lower(nxt.period) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), '')::text AS next_start,
       (SELECT count(*) FROM maintenance_records mr WHERE mr.equipment_id = e.id AND mr.status IN ('scheduled', 'in_progress'))::int AS open_maintenance,
       EXISTS (SELECT 1 FROM availability_blocks ab WHERE ab.equipment_id = e.id AND ab.period @> now()) AS blocked_now,
       COALESCE(ST_Y(l.location::geometry), 0)::float8 AS last_lat, COALESCE(ST_X(l.location::geometry), 0)::float8 AS last_lng,
       (l.equipment_id IS NOT NULL)::bool AS has_location, l.recorded_at AS last_seen_at
FROM equipment e
LEFT JOIN fleet_equipment fe ON fe.equipment_id = e.id
LEFT JOIN fleets f ON f.id = fe.fleet_id AND f.status = 'active'
LEFT JOIN LATERAL (
  SELECT b.id, b.reference, b.status FROM bookings b
  WHERE b.equipment_id = e.id AND b.status IN ('operator_assigned', 'dispatched', 'on_site', 'active', 'extension_requested', 'return_initiated', 'cancellation_requested')
  ORDER BY lower(b.period) LIMIT 1) cur ON true
LEFT JOIN LATERAL (
  SELECT u.full_name FROM operator_assignments a JOIN users u ON u.id = a.operator_user_id
  WHERE a.booking_id = cur.id AND a.status IN ('assigned', 'accepted') LIMIT 1) cu_op ON true
LEFT JOIN LATERAL (
  SELECT b.id, b.reference, b.period FROM bookings b
  WHERE b.equipment_id = e.id AND b.status IN ('accepted', 'payment_pending', 'payment_processing', 'confirmed', 'operator_assigned')
    AND lower(b.period) > now()
  ORDER BY lower(b.period) LIMIT 1) nxt ON true
LEFT JOIN equipment_last_locations l ON l.equipment_id = e.id
WHERE e.owner_org_id = @org_id AND e.deleted_at IS NULL
  AND (sqlc.narg(equipment_ids)::uuid[] IS NULL OR e.id = ANY (sqlc.narg(equipment_ids)::uuid[]))
  AND (sqlc.narg(fleet_id)::uuid IS NULL OR fe.fleet_id = sqlc.narg(fleet_id)::uuid)
ORDER BY e.title;

-- name: FleetSchedule :many
-- Bookings, availability blocks and maintenance downtime on the machines in
-- [from, to), for a timeline per machine.
SELECT * FROM (
  SELECT 'booking'::text AS kind, b.equipment_id, e.title AS equipment_title, b.id AS item_id, b.reference AS label,
         b.status::text AS status, lower(b.period)::timestamptz AS starts_at, upper(b.period)::timestamptz AS ends_at,
         COALESCE((SELECT u.full_name FROM operator_assignments a JOIN users u ON u.id = a.operator_user_id
                    WHERE a.booking_id = b.id AND a.status IN ('assigned', 'accepted') LIMIT 1), '')::text AS operator_name,
         b.with_operator
  FROM bookings b JOIN equipment e ON e.id = b.equipment_id
  WHERE b.owner_org_id = @org_id
    AND b.status NOT IN ('draft', 'requested', 'quoted', 'rejected', 'expired', 'cancelled', 'refunded', 'refund_pending')
    AND b.period && tstzrange(@from_at::timestamptz, @to_at::timestamptz, '[)')
    AND (sqlc.narg(equipment_ids)::uuid[] IS NULL OR b.equipment_id = ANY (sqlc.narg(equipment_ids)::uuid[]))
  UNION ALL
  SELECT 'block', ab.equipment_id, e.title, ab.id, COALESCE(NULLIF(ab.note, ''), ab.reason::text), ab.reason::text,
         lower(ab.period), upper(ab.period), '', false
  FROM availability_blocks ab JOIN equipment e ON e.id = ab.equipment_id AND e.owner_org_id = @org_id AND e.deleted_at IS NULL
  WHERE ab.period && tstzrange(@from_at::timestamptz, @to_at::timestamptz, '[)')
    AND (sqlc.narg(equipment_ids)::uuid[] IS NULL OR ab.equipment_id = ANY (sqlc.narg(equipment_ids)::uuid[]))
) x
ORDER BY starts_at, equipment_title;

-- ---- fleets ---------------------------------------------------------------------------------

-- name: CreateFleet :one
INSERT INTO fleets (owner_org_id, name, description, created_by) VALUES (@owner_org_id, @name, sqlc.narg(description), @created_by)
RETURNING *;

-- name: UpdateFleet :one
UPDATE fleets SET name = @name, description = sqlc.narg(description) WHERE id = @id AND owner_org_id = @owner_org_id AND status = 'active'
RETURNING *;

-- name: ArchiveFleet :one
UPDATE fleets SET status = 'archived', archived_reason = sqlc.narg(reason) WHERE id = @id AND status = 'active'
RETURNING *;

-- name: GetFleet :one
SELECT * FROM fleets WHERE id = @id;

-- name: ListFleets :many
-- An organisation's fleets (owner view), or every fleet (admin, org_id NULL).
SELECT f.*, o.display_name AS organization_name,
       (SELECT count(*) FROM fleet_equipment fe JOIN equipment e ON e.id = fe.equipment_id AND e.deleted_at IS NULL WHERE fe.fleet_id = f.id)::int AS equipment_count,
       COALESCE((SELECT array_agg(u.full_name ORDER BY u.full_name) FROM fleet_managers fm JOIN users u ON u.id = fm.user_id WHERE fm.fleet_id = f.id), '{}')::text[] AS manager_names
FROM fleets f JOIN organizations o ON o.id = f.owner_org_id
WHERE (sqlc.narg(org_id)::uuid IS NULL OR f.owner_org_id = sqlc.narg(org_id)::uuid)
  AND (sqlc.narg(status)::text IS NULL OR f.status = sqlc.narg(status)::text)
  AND (sqlc.narg(manager_id)::uuid IS NULL OR EXISTS (SELECT 1 FROM fleet_managers fm WHERE fm.fleet_id = f.id AND fm.user_id = sqlc.narg(manager_id)::uuid))
ORDER BY f.status, f.name
LIMIT @lim;

-- name: ListFleetMembers :many
SELECT 'equipment'::text AS kind, e.id AS member_id, e.title AS name, COALESCE(e.registration_no, '')::text AS detail
FROM fleet_equipment fe JOIN equipment e ON e.id = fe.equipment_id AND e.deleted_at IS NULL WHERE fe.fleet_id = @fleet_id
UNION ALL
SELECT 'manager', u.id, u.full_name, u.phone
FROM fleet_managers fm JOIN users u ON u.id = fm.user_id WHERE fm.fleet_id = @fleet_id
ORDER BY 1, 3;

-- name: RemoveFromAnyFleet :exec
DELETE FROM fleet_equipment WHERE equipment_id = ANY (@equipment_ids::uuid[]);

-- name: AddFleetEquipment :execrows
INSERT INTO fleet_equipment (fleet_id, equipment_id, added_by)
SELECT @fleet_id, e.id, @added_by FROM equipment e
WHERE e.id = ANY (@equipment_ids::uuid[]) AND e.owner_org_id = @owner_org_id AND e.deleted_at IS NULL;

-- name: RemoveFleetEquipment :execrows
DELETE FROM fleet_equipment WHERE fleet_id = @fleet_id AND equipment_id = @equipment_id;

-- name: AddFleetManager :execrows
INSERT INTO fleet_managers (fleet_id, user_id, added_by) VALUES (@fleet_id, @user_id, @added_by)
ON CONFLICT (fleet_id, user_id) DO NOTHING;

-- name: RemoveFleetManager :execrows
DELETE FROM fleet_managers WHERE fleet_id = @fleet_id AND user_id = @user_id;

-- name: IsOrgFleetManager :one
SELECT EXISTS (SELECT 1 FROM user_roles ur WHERE ur.user_id = @user_id AND ur.organization_id = @organization_id
                 AND ur.role_code = 'fleet_manager' AND ur.revoked_at IS NULL)::bool;

-- name: FleetManagerAllEquipment :one
SELECT (COALESCE((SELECT f.all_equipment FROM fleet_manager_profiles f WHERE f.user_id = @user_id AND f.organization_id = @organization_id), false)
        AND NOT is_restricted(@user_id, 'fleet_management'))::bool;

-- name: EquipmentFleets :many
SELECT fe.equipment_id, fe.fleet_id FROM fleet_equipment fe WHERE fe.equipment_id = ANY (@equipment_ids::uuid[]);

-- name: IsFleetManagerOf :one
SELECT EXISTS (SELECT 1 FROM fleet_managers WHERE fleet_id = @fleet_id AND user_id = @user_id)::bool;
