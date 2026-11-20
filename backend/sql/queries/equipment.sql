-- name: ListMakes :many
SELECT id, name::text AS name FROM equipment_makes ORDER BY name;

-- name: ListModels :many
SELECT m.id, m.name, mk.name::text AS make_name, c.slug AS category_slug
FROM equipment_models m
JOIN equipment_makes mk ON mk.id = m.make_id
JOIN equipment_categories c ON c.id = m.category_id
WHERE (sqlc.narg(category_slug)::text IS NULL OR c.slug = sqlc.narg(category_slug))
  AND (sqlc.narg(make_id)::uuid IS NULL OR m.make_id = sqlc.narg(make_id))
ORDER BY mk.name, m.name;

-- name: ListCities :many
SELECT id, name, state_code, is_live,
       ST_Y(center::geometry)::float8 AS lat, ST_X(center::geometry)::float8 AS lng
FROM cities ORDER BY name;

-- name: GetEquipmentDetail :one
SELECT e.id, e.owner_org_id, e.model_id, e.title, e.description, e.manufacture_year, e.registration_no,
       e.current_hmr, e.service_radius_km, e.wet_hire, e.dry_hire, e.fuel_policy, e.instant_book,
       e.status, e.rating_avg, e.rating_count, e.version, e.created_at, e.updated_at, e.city_id,
       ST_Y(e.base_location::geometry)::float8 AS lat, ST_X(e.base_location::geometry)::float8 AS lng,
       m.name AS model_name, mk.name::text AS make_name, c.slug AS category_slug, c.id AS category_id,
       o.display_name AS owner_name, o.kyc_status AS owner_kyc_status,
       e.condition, e.delivery_available, e.location_label, e.moderation_note, e.moderated_at, e.serial_no,
       (c.name ->> 'en')::text AS category_name, COALESCE(ci.name, '')::text AS city_name
FROM equipment e
JOIN equipment_models m ON m.id = e.model_id
JOIN equipment_makes mk ON mk.id = m.make_id
JOIN equipment_categories c ON c.id = m.category_id
JOIN organizations o ON o.id = e.owner_org_id
LEFT JOIN cities ci ON ci.id = e.city_id
WHERE e.id = @id AND e.deleted_at IS NULL;

-- name: ListOwnerEquipment :many
SELECT e.id, e.title, e.status, e.version, e.instant_book, e.rating_avg, e.rating_count, e.created_at,
       m.name AS model_name, c.slug AS category_slug
FROM equipment e
JOIN equipment_models m ON m.id = e.model_id
JOIN equipment_categories c ON c.id = m.category_id
WHERE e.owner_org_id = @owner_org_id AND e.deleted_at IS NULL
  AND (sqlc.narg(status)::text IS NULL OR e.status::text = sqlc.narg(status))
  AND (sqlc.narg(only_ids)::uuid[] IS NULL OR e.id = ANY(sqlc.narg(only_ids)::uuid[]))
  AND (sqlc.narg(after)::uuid IS NULL OR e.id < sqlc.narg(after))
ORDER BY e.id DESC
LIMIT @result_limit;

-- name: ListEquipmentByStatus :many
SELECT e.id, e.title, e.status, e.version, e.instant_book, e.rating_avg, e.rating_count, e.created_at,
       m.name AS model_name, c.slug AS category_slug, e.owner_org_id
FROM equipment e
JOIN equipment_models m ON m.id = e.model_id
JOIN equipment_categories c ON c.id = m.category_id
WHERE e.deleted_at IS NULL AND e.status::text = @status::text
  AND (sqlc.narg(after)::uuid IS NULL OR e.id < sqlc.narg(after))
ORDER BY e.id DESC
LIMIT @result_limit;

-- name: CreateEquipmentListing :one
INSERT INTO equipment (
  owner_org_id, model_id, title, description, manufacture_year, registration_no, serial_no,
  current_hmr, base_location, base_address_id, city_id, service_radius_km, wet_hire, dry_hire, fuel_policy, instant_book,
  condition, delivery_available, location_label)
VALUES (
  @owner_org_id, @model_id, @title, @description, sqlc.narg(manufacture_year), sqlc.narg(registration_no), sqlc.narg(serial_no),
  @current_hmr, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography, sqlc.narg(base_address_id),
  sqlc.narg(city_id), @service_radius_km, @wet_hire, @dry_hire, @fuel_policy, @instant_book,
  @condition, @delivery_available, sqlc.narg(location_label))
RETURNING id;

-- name: UpdateEquipmentListing :one
-- Optimistic locking: the caller passes the version it read (If-Match).
UPDATE equipment
SET title             = COALESCE(sqlc.narg(title), title),
    description       = COALESCE(sqlc.narg(description), description),
    current_hmr       = COALESCE(sqlc.narg(current_hmr), current_hmr),
    service_radius_km = COALESCE(sqlc.narg(service_radius_km), service_radius_km),
    wet_hire          = COALESCE(sqlc.narg(wet_hire), wet_hire),
    dry_hire          = COALESCE(sqlc.narg(dry_hire), dry_hire),
    instant_book      = COALESCE(sqlc.narg(instant_book), instant_book),
    model_id          = COALESCE(sqlc.narg(model_id), model_id),
    manufacture_year  = COALESCE(sqlc.narg(manufacture_year), manufacture_year),
    registration_no   = COALESCE(sqlc.narg(registration_no), registration_no),
    fuel_policy       = COALESCE(sqlc.narg(fuel_policy), fuel_policy),
    condition         = COALESCE(sqlc.narg(condition), condition),
    delivery_available = COALESCE(sqlc.narg(delivery_available), delivery_available),
    location_label    = COALESCE(sqlc.narg(location_label), location_label),
    city_id           = COALESCE(sqlc.narg(city_id), city_id),
    base_location     = CASE WHEN sqlc.narg(lat)::float8 IS NULL THEN base_location
                             ELSE ST_SetSRID(ST_MakePoint(sqlc.narg(lng)::float8, sqlc.narg(lat)::float8), 4326)::geography END,
    version           = version + 1
WHERE id = @id AND owner_org_id = @owner_org_id AND version = @version AND deleted_at IS NULL
RETURNING id, version;

-- name: TransitionEquipmentStatus :one
-- Compare-and-set on the current status. owner_org_id NULL = admin action.
UPDATE equipment SET status = @to_status, version = version + 1
WHERE id = @id AND deleted_at IS NULL
  AND status::text = ANY(@from_statuses::text[])
  AND (sqlc.narg(owner_org_id)::uuid IS NULL OR owner_org_id = sqlc.narg(owner_org_id))
RETURNING id, status, version;

-- name: RetireOwnerEquipment :execrows
UPDATE equipment SET status = 'retired', deleted_at = now(), version = version + 1
WHERE equipment.id = @id AND equipment.owner_org_id = @owner_org_id AND equipment.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM bookings b WHERE b.equipment_id = equipment.id AND booking_holds_slot(b.status));

-- name: EquipmentOwner :one
SELECT owner_org_id, status FROM equipment WHERE id = @id AND deleted_at IS NULL;

-- name: DeactivatePricingPlans :exec
UPDATE pricing_plans SET is_active = false WHERE equipment_id = @equipment_id AND is_active;

-- name: ListPricingPlans :many
SELECT id, unit, with_operator, rate_paise, currency, min_units, included_hours, extra_hour_rate_paise, operator_bata_per_day_paise
FROM pricing_plans WHERE equipment_id = @equipment_id AND is_active
ORDER BY with_operator DESC, unit;

-- name: UpsertMobilisationRate :exec
INSERT INTO mobilisation_rates (equipment_id, fixed_paise, per_km_paise, free_km, needs_trailer)
VALUES (@equipment_id, @fixed_paise, @per_km_paise, @free_km, @needs_trailer)
ON CONFLICT (equipment_id) DO UPDATE
SET fixed_paise = EXCLUDED.fixed_paise, per_km_paise = EXCLUDED.per_km_paise,
    free_km = EXCLUDED.free_km, needs_trailer = EXCLUDED.needs_trailer;

-- name: GetMobilisationRate :one
SELECT fixed_paise, per_km_paise, free_km, needs_trailer FROM mobilisation_rates WHERE equipment_id = @equipment_id;

-- name: SearchListings :many
-- Active machines within reach of the site that are free for the window.
SELECT e.id, e.title, e.instant_book, e.rating_avg, e.rating_count, e.wet_hire, e.dry_hire,
       c.slug AS category_slug, m.name AS model_name, mk.name::text AS make_name,
       o.display_name AS owner_name, (o.kyc_status = 'verified')::bool AS owner_verified,
       p.rate_paise, p.unit, p.with_operator,
       -- Approximate (public point): exact positions are never revealed.
       (ST_Distance(e.public_location, site.g) / 1000.0)::float8 AS distance_km,
       COALESCE((SELECT md.object_key FROM equipment_media md
         WHERE md.equipment_id = e.id AND md.deleted_at IS NULL
         ORDER BY md.is_cover DESC, md.sort_order LIMIT 1), '')::text AS cover_object_key
FROM equipment e
JOIN equipment_models m ON m.id = e.model_id
JOIN equipment_makes mk ON mk.id = m.make_id
JOIN equipment_categories c ON c.id = m.category_id
JOIN organizations o ON o.id = e.owner_org_id
JOIN pricing_plans p ON p.equipment_id = e.id AND p.is_active AND p.unit = @unit AND p.with_operator = @with_operator
CROSS JOIN (SELECT ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography AS g) site
WHERE e.status = 'active'
  AND e.deleted_at IS NULL
  AND (sqlc.narg(category_slug)::text IS NULL OR c.slug = sqlc.narg(category_slug))
  AND (sqlc.narg(make_id)::uuid IS NULL OR mk.id = sqlc.narg(make_id))
  AND (sqlc.narg(price_max_paise)::bigint IS NULL OR p.rate_paise <= sqlc.narg(price_max_paise))
  AND (NOT @verified_only::bool OR o.kyc_status = 'verified')
  AND (sqlc.narg(query)::text IS NULL OR e.title ILIKE '%' || sqlc.narg(query) || '%' OR m.name ILIKE '%' || sqlc.narg(query) || '%')
  AND ST_DWithin(e.base_location, site.g, LEAST(e.service_radius_km, @max_km::int) * 1000.0)
  AND NOT EXISTS (
    SELECT 1 FROM bookings b
    WHERE b.equipment_id = e.id AND booking_holds_slot(b.status)
      AND b.period && tstzrange(@starts_at::timestamptz, @ends_at::timestamptz, '[)'))
  AND NOT EXISTS (
    SELECT 1 FROM availability_blocks a
    WHERE a.equipment_id = e.id AND a.period && tstzrange(@starts_at::timestamptz, @ends_at::timestamptz, '[)'))
ORDER BY
  CASE WHEN @sort::text = 'price' THEN p.rate_paise END ASC,
  CASE WHEN @sort::text = 'rating' THEN e.rating_avg END DESC NULLS LAST,
  distance_km ASC, e.id
LIMIT @result_limit OFFSET @result_offset;
