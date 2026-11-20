-- Operator jobs, job evidence (checklists, photos), reported issues and
-- account restrictions.

-- name: InsertInspectionDecisionHistory :one
INSERT INTO inspection_decision_history (issue_id, actor_user_id, action, party_role, note)
VALUES (@issue_id, @actor_user_id, @action, sqlc.narg(party_role), @note)
RETURNING *;

-- name: ListInspectionDecisionHistory :many
SELECT * FROM inspection_decision_history WHERE issue_id = @issue_id ORDER BY created_at;

-- ---- operator jobs ------------------------------------------------------------------------------

-- name: ListOperatorJobsScoped :many
-- scope 'current' = open assignments on bookings still running; 'history'
-- = everything else the operator was assigned (declined, replaced, done).
SELECT a.id AS assignment_id, a.status::text AS assignment_status, b.id AS booking_id, b.reference, b.status::text AS booking_status,
       lower(b.period)::timestamptz AS starts_at, upper(b.period)::timestamptz AS ends_at,
       e.title AS equipment_title, COALESCE(e.registration_no, '')::text AS registration_no,
       ad.line1 AS site_line1, ad.city AS site_city, cu.display_name AS customer_name,
       COALESCE((SELECT c.agreed_amount_paise FROM driver_compensations c WHERE c.booking_id = b.id AND c.driver_user_id = @operator_user_id),0)::bigint AS driver_payment_paise,
       COALESCE((SELECT c.status FROM driver_compensations c WHERE c.booking_id = b.id AND c.driver_user_id = @operator_user_id),'not_set')::text AS driver_payment_status,
       ST_Y(b.site_location::geometry)::float8 AS site_lat, ST_X(b.site_location::geometry)::float8 AS site_lng
FROM operator_assignments a
JOIN bookings b ON b.id = a.booking_id
JOIN equipment e ON e.id = b.equipment_id
JOIN addresses ad ON ad.id = b.site_address_id
JOIN organizations cu ON cu.id = b.customer_org_id
WHERE a.operator_user_id = @operator_user_id
  AND CASE WHEN @scope::text = 'current'
        THEN a.status IN ('assigned', 'accepted') AND b.status IN ('operator_assigned', 'confirmed', 'dispatched', 'on_site', 'active', 'extension_requested', 'return_initiated', 'cancellation_requested')
        WHEN @scope::text = 'history'
        THEN NOT (a.status IN ('assigned', 'accepted') AND b.status IN ('operator_assigned', 'confirmed', 'dispatched', 'on_site', 'active', 'extension_requested', 'return_initiated', 'cancellation_requested'))
        ELSE true END
  AND EXISTS (SELECT 1 FROM operator_profiles op JOIN operator_skills sk ON sk.operator_user_id=op.user_id
              JOIN equipment_models em ON em.category_id=sk.category_id
              WHERE op.user_id=a.operator_user_id AND em.id=e.model_id)
  AND (sqlc.narg(from_at)::timestamptz IS NULL OR upper(b.period) > sqlc.narg(from_at)::timestamptz)
  AND (sqlc.narg(to_at)::timestamptz IS NULL OR lower(b.period) < sqlc.narg(to_at)::timestamptz)
ORDER BY CASE WHEN @scope::text = 'history' THEN -extract(epoch FROM lower(b.period)) ELSE extract(epoch FROM lower(b.period)) END
LIMIT @lim;

-- name: OperatorJobDetail :one
-- The operator's latest assignment on a booking with what they need on site.
SELECT a.id AS assignment_id, a.status::text AS assignment_status, a.responded_at,
       b.id AS booking_id, b.reference, b.status::text AS booking_status, b.with_operator, b.work_description,
       lower(b.period)::timestamptz AS starts_at, upper(b.period)::timestamptz AS ends_at, b.delivery, b.pickup,
       e.id AS equipment_id, e.title AS equipment_title, COALESCE(e.registration_no, '')::text AS registration_no,
       COALESCE(e.serial_no, '')::text AS serial_no, e.manufacture_year, e.current_hmr, e.condition::text AS condition,
       e.fuel_policy::text AS fuel_policy, m.name AS model_name, mk.name AS make_name, COALESCE(cat.name->>'en', '')::text AS category_name,
       e.timezone,
       cu.display_name AS customer_name, ru.full_name AS customer_contact, ru.phone AS customer_phone,
       ow.display_name AS owner_name, COALESCE(ow.contact_phone, '')::text AS owner_phone,
       COALESCE(sa.label, '')::text AS site_label,
       concat_ws(', ', sa.line1, sa.line2, sa.city, sa.pincode)::text AS site_address,
       COALESCE((SELECT c.agreed_amount_paise FROM driver_compensations c WHERE c.booking_id = b.id AND c.driver_user_id = @operator_user_id),0)::bigint AS driver_payment_paise,
       COALESCE((SELECT c.status FROM driver_compensations c WHERE c.booking_id = b.id AND c.driver_user_id = @operator_user_id),'not_set')::text AS driver_payment_status,
       ST_Y(b.site_location::geometry)::float8 AS site_lat, ST_X(b.site_location::geometry)::float8 AS site_lng,
       b.site_geofence_m,
       ST_Y(e.base_location::geometry)::float8 AS yard_lat, ST_X(e.base_location::geometry)::float8 AS yard_lng
FROM operator_assignments a
JOIN bookings b ON b.id = a.booking_id
JOIN equipment e ON e.id = b.equipment_id
JOIN equipment_models m ON m.id = e.model_id
JOIN equipment_makes mk ON mk.id = m.make_id
JOIN equipment_categories cat ON cat.id = m.category_id
JOIN organizations cu ON cu.id = b.customer_org_id
JOIN organizations ow ON ow.id = b.owner_org_id
JOIN users ru ON ru.id = b.requested_by
LEFT JOIN addresses sa ON sa.id = b.site_address_id
WHERE a.booking_id = @booking_id AND a.operator_user_id = @operator_user_id
ORDER BY a.created_at DESC
LIMIT 1;

-- name: CompleteAssignment :execrows
UPDATE operator_assignments SET status = 'completed'
WHERE booking_id = @booking_id AND operator_user_id = @operator_user_id AND status = 'accepted';

-- name: ReplaceAssignment :execrows
-- The owner swaps the operator: the open assignment is marked replaced.
UPDATE operator_assignments SET status = 'replaced'
WHERE booking_id = @booking_id AND status IN ('assigned', 'accepted');

-- ---- checklists ------------------------------------------------------------------------------

-- name: InsertJobChecklist :one
-- A replay with the same client_id returns no row (the original stands).
INSERT INTO checklists (booking_id, kind, hmr_reading, hmr_photo_object_key, fuel_level_pct, items, photo_object_keys, photo_upload_ids,
                        damage_notes, notes, captured_at, submitted_by, client_id, location)
VALUES (@booking_id, @kind, @hmr_reading, @hmr_photo_object_key, sqlc.narg(fuel_level_pct), @items, @photo_object_keys, @photo_upload_ids,
        sqlc.narg(damage_notes), sqlc.narg(notes), @captured_at, @submitted_by, @client_id,
        CASE WHEN sqlc.narg(lat)::float8 IS NULL THEN NULL
             ELSE ST_SetSRID(ST_MakePoint(sqlc.narg(lng)::float8, sqlc.narg(lat)::float8), 4326)::geography END)
ON CONFLICT DO NOTHING
RETURNING id;

-- name: ListInspectionAcknowledgements :many
SELECT id, booking_id, stage, user_id, role, acknowledged_at, disagreement
FROM booking_inspection_acknowledgements WHERE booking_id = @booking_id
ORDER BY acknowledged_at;

-- name: ListBookingChecklists :many
SELECT c.id, c.kind, c.hmr_reading, c.fuel_level_pct, c.items, c.photo_upload_ids, c.photo_object_keys, c.hmr_photo_object_key, c.damage_notes, c.notes,
       c.captured_at, c.received_at, u.full_name AS submitted_by_name
FROM checklists c JOIN users u ON u.id = c.submitted_by
WHERE c.booking_id = @booking_id
ORDER BY c.captured_at;

-- name: GetChecklistByClient :one
SELECT id, booking_id, kind FROM checklists WHERE client_id = @client_id;

-- name: HasChecklist :one
SELECT EXISTS (SELECT 1 FROM checklists WHERE booking_id = @booking_id AND kind = @kind)::bool;

-- name: SetEquipmentHMR :exec
UPDATE equipment SET current_hmr = GREATEST(current_hmr, @hmr::numeric) WHERE id = @id;

-- ---- reported issues ---------------------------------------------------------------------------

-- name: InsertCustomerDeliveryPhoto :one
INSERT INTO customer_delivery_photos (booking_id, upload_id, client_id, captured_at, submitted_by)
VALUES (@booking_id, @upload_id, @client_id, @captured_at, @submitted_by)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: GetCustomerDeliveryPhotoByClient :one
SELECT * FROM customer_delivery_photos WHERE client_id = @client_id;

-- name: ListCustomerDeliveryPhotos :many
SELECT p.id, p.booking_id, p.upload_id, p.client_id, p.captured_at, p.submitted_at, p.submitted_by, u.full_name AS submitted_by_name,
       ap.approved_at, COALESCE(au.full_name, '')::text AS approved_by_name
FROM customer_delivery_photos p JOIN users u ON u.id = p.submitted_by
LEFT JOIN customer_delivery_photo_approvals ap ON ap.photo_id = p.id LEFT JOIN users au ON au.id = ap.approved_by
WHERE p.booking_id = @booking_id ORDER BY p.captured_at, p.id;

-- name: ApproveCustomerDeliveryPhoto :one
INSERT INTO customer_delivery_photo_approvals (photo_id, booking_id, approved_by)
SELECT p.id, p.booking_id, @user_id FROM customer_delivery_photos p WHERE p.id = @id AND p.booking_id = @booking_id
ON CONFLICT DO NOTHING
RETURNING photo_id;

-- name: InsertIssueReport :one
INSERT INTO issue_reports (booking_id, equipment_id, kind, stage, severity, description, photo_upload_ids, reported_by, reporter_role)
VALUES (@booking_id, @equipment_id, @kind, @stage, @severity, @description, @photo_upload_ids, @reported_by, @reporter_role)
RETURNING *;

-- name: ListBookingIssues :many
SELECT i.*, u.full_name AS reporter_name FROM issue_reports i JOIN users u ON u.id = i.reported_by
WHERE i.booking_id = @booking_id ORDER BY i.created_at DESC;

-- name: ListIssueQueue :many
SELECT i.*, u.full_name AS reporter_name, b.reference AS booking_reference, e.title AS equipment_title,
       ow.display_name AS owner_name, cu.display_name AS customer_name
FROM issue_reports i
JOIN users u ON u.id = i.reported_by
JOIN bookings b ON b.id = i.booking_id
JOIN equipment e ON e.id = i.equipment_id
JOIN organizations ow ON ow.id = b.owner_org_id
JOIN organizations cu ON cu.id = b.customer_org_id
WHERE (sqlc.narg(status)::text IS NULL OR i.status::text = sqlc.narg(status)::text)
  AND (sqlc.narg(severity)::text IS NULL OR i.severity = sqlc.narg(severity)::text)
ORDER BY CASE i.severity WHEN 'critical' THEN 0 WHEN 'major' THEN 1 ELSE 2 END, i.created_at
LIMIT @lim;

-- name: ReviewIssue :one
UPDATE issue_reports SET status = @status, resolution_note = sqlc.narg(resolution_note), reviewed_by = @reviewed_by,
       reviewed_at = CASE WHEN @status::issue_status IN ('resolved', 'dismissed') THEN now() ELSE reviewed_at END
WHERE id = @id AND status IN ('open', 'under_review')
RETURNING *;

-- name: GetIssue :one
SELECT * FROM issue_reports WHERE id = @id;

-- ---- account restrictions ---------------------------------------------------------------------

-- name: InsertRestriction :one
INSERT INTO account_restrictions (user_id, kind, reason, created_by, expires_at)
VALUES (@user_id, @kind, @reason, @created_by, sqlc.narg(expires_at))
RETURNING *;

-- name: LiftRestriction :one
UPDATE account_restrictions SET lifted_at = now(), lifted_by = @lifted_by, lift_reason = @lift_reason
WHERE id = @id AND lifted_at IS NULL
RETURNING *;

-- name: ListRestrictions :many
SELECT r.*, u.full_name AS user_name, u.phone AS user_phone,
       (r.lifted_at IS NULL AND (r.expires_at IS NULL OR r.expires_at > now()))::bool AS active
FROM account_restrictions r JOIN users u ON u.id = r.user_id
WHERE (sqlc.narg(user_id)::uuid IS NULL OR r.user_id = sqlc.narg(user_id)::uuid)
  AND (NOT @active_only::bool OR (r.lifted_at IS NULL AND (r.expires_at IS NULL OR r.expires_at > now())))
ORDER BY r.created_at DESC
LIMIT @lim;

-- name: IsRestricted :one
SELECT is_restricted(@user_id, @kind)::bool;

-- ---- notifications: owner-side recipients ---------------------------------------------------------

-- name: OwnerSideRecipients :many
-- Owners and org admins always; fleet managers only when the booking's (or
-- the named) machine is in their scope, or they manage every machine.
SELECT DISTINCT ur.user_id
FROM user_roles ur
WHERE ur.organization_id = @org_id AND ur.revoked_at IS NULL
  AND (ur.role_code IN ('owner', 'org_admin')
       OR (ur.role_code = 'fleet_manager' AND (
             COALESCE((SELECT f.all_equipment FROM fleet_manager_profiles f WHERE f.user_id = ur.user_id AND f.organization_id = @org_id), false)
             AND NOT is_restricted(ur.user_id, 'fleet_management')
             OR EXISTS (SELECT 1 FROM fleet_manager_scope s
                         WHERE s.user_id = ur.user_id AND s.organization_id = @org_id
                           AND s.equipment_id = COALESCE(sqlc.narg(equipment_id)::uuid,
                                                         (SELECT b.equipment_id FROM bookings b WHERE b.id = sqlc.narg(booking_id)::uuid))))));

-- name: UploadKeys :many
SELECT id, object_key FROM uploads WHERE id = ANY (@ids::uuid[]);
-- name: AcknowledgeBookingInspection :one
INSERT INTO booking_inspection_acknowledgements (booking_id, stage, user_id, role, disagreement)
VALUES (@booking_id, @stage, @user_id, @role, sqlc.narg(disagreement))
ON CONFLICT (booking_id, stage, user_id) DO NOTHING
RETURNING id, acknowledged_at;

-- name: CountInspectionAcknowledgements :one
SELECT CASE WHEN EXISTS (
  SELECT 1
  FROM bookings b
  WHERE b.id = @booking_id
    AND EXISTS (SELECT 1 FROM booking_inspection_acknowledgements a
                WHERE a.booking_id = b.id AND a.stage = @stage
                  AND a.role = 'customer' AND a.disagreement IS NULL)
    AND EXISTS (SELECT 1 FROM booking_inspection_acknowledgements a
                WHERE a.booking_id = b.id AND a.stage = @stage
                  AND a.role = CASE WHEN b.with_operator THEN 'operator' ELSE 'owner' END
                  AND a.disagreement IS NULL)
) THEN 2 ELSE 0 END::int;

