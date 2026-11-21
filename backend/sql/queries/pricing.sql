-- Pricing engine inputs and owner booking rules.

-- name: GetPricingTerms :one
-- Listing terms for pricing. distance_km is 0 when no site is given.
SELECT e.id AS equipment_id, e.owner_org_id, e.status AS equipment_status, e.instant_book, e.city_id, m.category_id,
       e.service_radius_km, e.fuel_policy::text AS fuel_policy, e.timezone,
       COALESCE(e.fuel_rate_per_hour_paise, 0)::bigint AS fuel_rate_per_hour_paise,
       COALESCE(e.security_deposit_paise, 0)::bigint AS security_deposit_paise,
       COALESCE(e.late_fee_per_hour_paise, 0)::bigint AS late_fee_per_hour_paise,
       e.late_grace_minutes, COALESCE(e.min_rental_hours, 0)::int AS min_rental_hours,
       COALESCE(e.max_rental_days, 0)::int AS max_rental_days,
       COALESCE(mr.fixed_paise, 0)::bigint AS delivery_fixed_paise, COALESCE(mr.per_km_paise, 0)::bigint AS delivery_per_km_paise,
       COALESCE(mr.free_km, 0)::int AS free_km,
       COALESCE(mr.pickup_fixed_paise, 0)::bigint AS pickup_fixed_paise, COALESCE(mr.pickup_per_km_paise, 0)::bigint AS pickup_per_km_paise,
       COALESCE(cp.id, dcp.id) AS cancellation_policy_id,
       COALESCE(cp.code, dcp.code, '')::text AS cancellation_code,
       COALESCE(cp.rules, dcp.rules, '[]'::jsonb) AS cancellation_rules,
       (CASE WHEN sqlc.narg(lat)::float8 IS NULL THEN 0
             ELSE ST_Distance(e.base_location, ST_SetSRID(ST_MakePoint(sqlc.narg(lng)::float8, sqlc.narg(lat)::float8), 4326)::geography) / 1000.0
        END)::float8 AS distance_km
FROM equipment e
JOIN equipment_models m ON m.id = e.model_id
LEFT JOIN mobilisation_rates mr ON mr.equipment_id = e.id
LEFT JOIN cancellation_policies cp ON cp.id = e.cancellation_policy_id
LEFT JOIN cancellation_policies dcp ON dcp.is_default
WHERE e.id = @equipment_id AND e.deleted_at IS NULL;

-- name: ActivePricingPlans :many
SELECT unit::text AS unit, rate_paise, min_units, included_hours, extra_hour_rate_paise, operator_bata_per_day_paise
FROM pricing_plans
WHERE equipment_id = @equipment_id AND is_active AND with_operator = @with_operator
  AND valid_from <= @on_date::date AND (valid_until IS NULL OR valid_until >= @on_date::date)
ORDER BY unit;

-- name: ActiveTaxRates :many
-- Taxes in force on a date (the rental start, in the listing's time zone).
SELECT code, name, rate_bps, applies_to, sac_code FROM tax_rates
WHERE is_active AND valid_from <= @on_date::date AND (valid_until IS NULL OR valid_until >= @on_date::date)
ORDER BY code;

-- name: ListTaxRates :many
SELECT id, code, name, rate_bps, applies_to, valid_from, valid_until, is_active, updated_at, sac_code, legal_basis, reviewed_by_name, reviewed_at
FROM tax_rates ORDER BY code, valid_from;

-- name: UpsertTaxRate :one
-- Any change to a rule clears its review: the new wording must be reviewed again.
INSERT INTO tax_rates (code, name, rate_bps, applies_to, valid_from, valid_until, is_active, sac_code, legal_basis)
VALUES (@code, @name, @rate_bps, @applies_to, @valid_from, sqlc.narg(valid_until), @is_active, @sac_code, sqlc.narg(legal_basis))
ON CONFLICT (code, valid_from) DO UPDATE
  SET name = EXCLUDED.name, rate_bps = EXCLUDED.rate_bps, applies_to = EXCLUDED.applies_to,
      valid_until = EXCLUDED.valid_until, is_active = EXCLUDED.is_active, sac_code = EXCLUDED.sac_code,
      legal_basis = EXCLUDED.legal_basis, reviewed_by_name = NULL, reviewed_at = NULL
RETURNING id;

-- name: ListCancellationPresets :many
SELECT id, code, name, rules, is_default FROM cancellation_policies WHERE owner_org_id IS NULL ORDER BY is_default DESC, code;

-- name: GetCancellationPresetByCode :one
SELECT id FROM cancellation_policies WHERE code = @code AND owner_org_id IS NULL;

-- name: CreateCustomCancellationPolicy :one
INSERT INTO cancellation_policies (code, name, rules, owner_org_id)
VALUES (@code, @name, @rules, @owner_org_id)
RETURNING id;

-- name: GetBookingRules :one
SELECT e.min_rental_hours, e.max_rental_days, e.security_deposit_paise, e.fuel_rate_per_hour_paise, e.late_fee_per_hour_paise,
       e.late_grace_minutes, e.timezone, e.fuel_policy::text AS fuel_policy,
       COALESCE(mr.pickup_fixed_paise, 0)::bigint AS pickup_fixed_paise, COALESCE(mr.pickup_per_km_paise, 0)::bigint AS pickup_per_km_paise,
       COALESCE(cp.code, '')::text AS policy_code, COALESCE(cp.name, '')::text AS policy_name,
       COALESCE(cp.rules, '[]'::jsonb) AS policy_rules, (cp.owner_org_id IS NOT NULL)::bool AS policy_custom
FROM equipment e
LEFT JOIN mobilisation_rates mr ON mr.equipment_id = e.id
LEFT JOIN cancellation_policies cp ON cp.id = e.cancellation_policy_id
WHERE e.id = @id AND e.deleted_at IS NULL;

-- name: SetBookingRules :execrows
UPDATE equipment
SET min_rental_hours = sqlc.narg(min_rental_hours), max_rental_days = sqlc.narg(max_rental_days),
    security_deposit_paise = sqlc.narg(security_deposit_paise), fuel_rate_per_hour_paise = sqlc.narg(fuel_rate_per_hour_paise),
    late_fee_per_hour_paise = sqlc.narg(late_fee_per_hour_paise), late_grace_minutes = @late_grace_minutes,
    timezone = @timezone, fuel_policy = @fuel_policy::fuel_policy, cancellation_policy_id = sqlc.narg(cancellation_policy_id)
WHERE id = @id AND deleted_at IS NULL;

-- name: UpsertPickupRates :exec
INSERT INTO mobilisation_rates (equipment_id, pickup_fixed_paise, pickup_per_km_paise)
VALUES (@equipment_id, @pickup_fixed_paise, @pickup_per_km_paise)
ON CONFLICT (equipment_id) DO UPDATE
  SET pickup_fixed_paise = EXCLUDED.pickup_fixed_paise, pickup_per_km_paise = EXCLUDED.pickup_per_km_paise;

-- name: SlotConflicts :many
-- Held bookings and owner blocks overlapping [from, to).
SELECT lower(b.period)::timestamptz AS starts_at, upper(b.period)::timestamptz AS ends_at, 'booked'::text AS kind
FROM bookings b
WHERE b.equipment_id = @equipment_id AND booking_holds_slot(b.status, b.resume_status)
  AND b.period && tstzrange(@from_at::timestamptz, @to_at::timestamptz, '[)')
UNION ALL
SELECT lower(a.period)::timestamptz, upper(a.period)::timestamptz, 'blocked'::text
FROM availability_blocks a
WHERE a.equipment_id = @equipment_id AND a.period && tstzrange(@from_at::timestamptz, @to_at::timestamptz, '[)')
ORDER BY 1;

-- name: NextFreeStart :one
-- Earliest start at or after @from_at at which [start, start + @hours) is free
-- (candidates: the requested start and the end of every busy range).
WITH busy AS (
  SELECT b.period FROM bookings b
  WHERE b.equipment_id = @equipment_id AND booking_holds_slot(b.status, b.resume_status) AND upper(b.period) > @from_at::timestamptz
  UNION ALL
  SELECT a.period FROM availability_blocks a WHERE a.equipment_id = @equipment_id AND upper(a.period) > @from_at::timestamptz
), candidates AS (
  SELECT @from_at::timestamptz AS s
  UNION SELECT upper(period) FROM busy
)
SELECT c.s::timestamptz AS start_at FROM candidates c
WHERE c.s >= @from_at::timestamptz
  AND NOT EXISTS (SELECT 1 FROM busy WHERE busy.period && tstzrange(c.s, c.s + make_interval(secs => sqlc.arg(hours)::float8 * 3600), '[)'))
ORDER BY c.s LIMIT 1;

-- name: SetBookingFinalCharges :one
UPDATE bookings
SET final_charges = @final_charges, final_total_paise = @final_total_paise, finalized_at = now(),
    actual_period = tstzrange(@actual_start::timestamptz, @actual_end::timestamptz, '[)')
WHERE id = @id AND version = @version AND finalized_at IS NULL
RETURNING id, version;
