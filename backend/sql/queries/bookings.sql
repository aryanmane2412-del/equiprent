-- name: CreateBooking :one
-- owner_org_id is derived from the equipment by trigger; the value passed here
-- is overwritten. Run inside a transaction (see internal/booking).
INSERT INTO bookings (
  reference, equipment_id, owner_org_id, customer_org_id, requested_by, site_address_id, site_location,
  period, with_operator, pricing_unit, price_snapshot, estimated_total_paise, deposit_paise,
  cancellation_policy_id, payment_mode, status, owner_response_due_at, work_description, delivery, pickup, payment_due_at, customer_type, customer_details)
VALUES (
  @reference, @equipment_id, @customer_org_id, @customer_org_id, @requested_by, @site_address_id,
  ST_SetSRID(ST_MakePoint(@site_lng::float8, @site_lat::float8), 4326)::geography,
  tstzrange(@starts_at::timestamptz, @ends_at::timestamptz, '[)'), @with_operator, @pricing_unit, @price_snapshot,
  @estimated_total_paise, @deposit_paise, sqlc.narg(cancellation_policy_id), @payment_mode, @status,
  sqlc.narg(owner_response_due_at), @work_description, @delivery, @pickup, sqlc.narg(payment_due_at), @customer_type, @customer_details)
RETURNING id, reference, owner_org_id, status, version;

-- name: GetBooking :one
SELECT id, reference, equipment_id, owner_org_id, customer_org_id, status, version,
       lower(period)::timestamptz AS starts_at, upper(period)::timestamptz AS ends_at,
       estimated_total_paise, final_total_paise, deposit_paise, currency, payment_mode
FROM bookings WHERE id = @id;

-- name: TransitionBooking :one
-- Compare-and-set on status AND version: a stale caller gets no row.
UPDATE bookings
SET status = @to_status,
    cancel_reason = COALESCE(sqlc.narg(cancel_reason), cancel_reason),
    payment_due_at = COALESCE(sqlc.narg(payment_due_at), payment_due_at)
WHERE id = @id AND status = @from_status AND version = @version
RETURNING id, status, version, status_changed_at;

-- name: ListBookingEvents :many
SELECT from_status, to_status, actor_user_id, actor_role, reason, created_at
FROM booking_events WHERE booking_id = @booking_id ORDER BY id;

-- name: ListOrgBookings :many
SELECT b.id, b.reference, b.status, lower(b.period)::timestamptz AS starts_at, upper(b.period)::timestamptz AS ends_at,
       b.estimated_total_paise, e.title AS equipment_title
FROM bookings b
JOIN equipment e ON e.id = b.equipment_id
WHERE (b.owner_org_id = @org_id OR b.customer_org_id = @org_id)
  AND (sqlc.narg(status)::booking_status IS NULL OR b.status = sqlc.narg(status))
ORDER BY b.created_at DESC
LIMIT @result_limit;

-- name: AssignOperator :one
INSERT INTO operator_assignments (booking_id, operator_user_id, assigned_by, period)
SELECT b.id, @operator_user_id, @assigned_by, b.period FROM bookings b WHERE b.id = @booking_id
RETURNING id, status;

-- name: RecordDriverCompensation :one
INSERT INTO driver_compensations (booking_id, owner_org_id, driver_user_id, agreed_amount_paise, recorded_by)
SELECT b.id, b.owner_org_id, a.operator_user_id, @agreed_amount_paise, @recorded_by
FROM bookings b JOIN operator_assignments a ON a.booking_id = b.id AND a.status IN ('assigned', 'accepted')
JOIN operator_profiles op ON op.user_id = a.operator_user_id AND op.verified_at IS NOT NULL AND op.licence_expires_on >= CURRENT_DATE
JOIN operator_skills sk ON sk.operator_user_id = op.user_id
JOIN equipment e ON e.id = b.equipment_id
JOIN equipment_models m ON m.id=e.model_id AND m.category_id=sk.category_id
WHERE b.id = @booking_id AND b.owner_org_id = @owner_org_id
ON CONFLICT (booking_id) DO UPDATE SET agreed_amount_paise = EXCLUDED.agreed_amount_paise
WHERE driver_compensations.status = 'agreed'
  AND driver_compensations.driver_user_id = EXCLUDED.driver_user_id
RETURNING driver_compensations.*;

-- name: ListDriverCompensations :many
SELECT c.*, u.full_name AS driver_name, u.phone AS driver_phone
FROM driver_compensations c JOIN users u ON u.id = c.driver_user_id
WHERE c.owner_org_id = @owner_org_id ORDER BY c.created_at DESC LIMIT @result_limit;

-- name: PayDriverCompensation :one
UPDATE driver_compensations SET status = 'paid', paid_at = now(), transaction_reference = @transaction_reference
WHERE driver_compensations.id = @id AND driver_compensations.owner_org_id = @owner_org_id AND driver_compensations.status = 'agreed'
  AND EXISTS (SELECT 1 FROM bookings b WHERE b.id = driver_compensations.booking_id AND b.status IN ('completed', 'closed'))
  AND EXISTS (SELECT 1 FROM operator_assignments a WHERE a.booking_id = driver_compensations.booking_id
              AND a.operator_user_id = driver_compensations.driver_user_id AND a.status = 'completed')
RETURNING driver_compensations.*;

-- name: GetDriverCompensationByID :one
SELECT c.*, u.full_name AS driver_name, u.phone AS driver_phone
FROM driver_compensations c JOIN users u ON u.id = c.driver_user_id
WHERE c.id = @id AND c.owner_org_id = @owner_org_id;

-- name: ListOperatorDriverCompensations :many
SELECT c.* FROM driver_compensations c
WHERE c.driver_user_id = @driver_user_id
ORDER BY c.created_at DESC LIMIT @result_limit;

-- name: DriverCompensationHistoryForOperator :many
SELECT e.id, e.compensation_id, e.status, e.amount_paise, e.transaction_reference, e.actor_user_id, e.created_at
FROM driver_compensation_events e JOIN driver_compensations c ON c.id=e.compensation_id
WHERE c.driver_user_id = @driver_user_id::uuid ORDER BY e.created_at DESC LIMIT @result_limit;

-- name: DriverCompensationHistory :many
SELECT e.* FROM driver_compensation_events e JOIN driver_compensations c ON c.id = e.compensation_id
WHERE c.id = @compensation_id AND c.owner_org_id = @owner_org_id ORDER BY e.created_at DESC;

-- name: RespondToAssignment :one
UPDATE operator_assignments SET status = @status, responded_at = now()
WHERE id = @id AND status = 'assigned'
RETURNING id, status;

-- name: CreateRentalAgreement :one
INSERT INTO rental_agreements (booking_id, template_version, terms_sha256, document_object_key)
VALUES (@booking_id, @template_version, @terms_sha256, sqlc.narg(document_object_key))
RETURNING id;

-- name: AcceptAgreementAsCustomer :execrows
UPDATE rental_agreements
SET customer_accepted_by = @user_id, customer_accepted_at = now(), customer_accept_ip = sqlc.narg(ip)
WHERE booking_id = @booking_id AND customer_accepted_at IS NULL AND superseded_at IS NULL;

-- name: AcceptAgreementAsOwner :execrows
UPDATE rental_agreements
SET owner_accepted_by = @user_id, owner_accepted_at = now()
WHERE booking_id = @booking_id AND owner_accepted_at IS NULL AND superseded_at IS NULL;

-- name: CreateChecklist :one
INSERT INTO checklists (booking_id, kind, hmr_reading, hmr_photo_object_key, fuel_level_pct, items, captured_at, submitted_by, customer_otp_verified_at, client_id, location)
VALUES (@booking_id, @kind, @hmr_reading, @hmr_photo_object_key, sqlc.narg(fuel_level_pct), @items, @captured_at, @submitted_by,
        sqlc.narg(customer_otp_verified_at), @client_id, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography)
ON CONFLICT (client_id) DO UPDATE SET client_id = EXCLUDED.client_id  -- idempotent replay returns the same row
RETURNING id;

-- name: SubmitWorkLog :one
INSERT INTO work_logs (booking_id, work_date, start_hmr, end_hmr, idle_hours, breakdown_hours, submitted_by, client_id)
VALUES (@booking_id, @work_date, @start_hmr, @end_hmr, @idle_hours, @breakdown_hours, @submitted_by, @client_id)
RETURNING id, billable_hours;

-- name: ApproveWorkLog :one
UPDATE work_logs SET status = 'approved', approved_by = @approved_by, approved_at = now()
WHERE id = @id AND status = 'submitted'
RETURNING id, billable_hours;

-- name: BillableHours :one
SELECT COALESCE(sum(billable_hours), 0)::numeric AS hours
FROM work_logs WHERE booking_id = @booking_id AND status IN ('approved', 'auto_approved');
