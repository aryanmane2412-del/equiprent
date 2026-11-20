-- name: ListCategories :many
SELECT id, slug, (name ->> 'en')::text AS name_en, parent_id, icon_key, COALESCE(name ->> @lang::text, name ->> 'en')::text AS name_local, sort_order
FROM equipment_categories
WHERE is_active
ORDER BY sort_order;

-- name: GetCategoryBySlug :one
SELECT * FROM equipment_categories WHERE slug = @slug;

-- name: UpsertMake :one
INSERT INTO equipment_makes (name) VALUES (@name)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING id;

-- name: UpsertModel :one
INSERT INTO equipment_models (category_id, make_id, name, default_specs)
VALUES (@category_id, @make_id, @name, @default_specs)
ON CONFLICT (make_id, name) DO UPDATE SET default_specs = EXCLUDED.default_specs
RETURNING id;

-- name: CreateEquipment :one
INSERT INTO equipment (
  owner_org_id, model_id, title, description, manufacture_year, registration_no,
  current_hmr, base_location, city_id, service_radius_km, wet_hire, dry_hire, instant_book, status)
VALUES (
  @owner_org_id, @model_id, @title, @description, sqlc.narg(manufacture_year), sqlc.narg(registration_no),
  @current_hmr, ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography, sqlc.narg(city_id),
  @service_radius_km, @wet_hire, @dry_hire, @instant_book, @status)
RETURNING id, version;

-- name: GetEquipment :one
SELECT e.id, e.owner_org_id, e.title, e.status, e.version, e.current_hmr, e.instant_book,
       ST_Y(e.base_location::geometry)::float8 AS lat, ST_X(e.base_location::geometry)::float8 AS lng,
       m.name AS model_name, mk.name::text AS make_name, c.slug AS category_slug
FROM equipment e
JOIN equipment_models m ON m.id = e.model_id
JOIN equipment_makes mk ON mk.id = m.make_id
JOIN equipment_categories c ON c.id = m.category_id
WHERE e.id = @id AND e.deleted_at IS NULL;

-- name: SetEquipmentStatus :one
-- Optimistic locking: fails (no rows) if someone else changed the listing.
UPDATE equipment SET status = @status, version = version + 1
WHERE id = @id AND version = @version AND deleted_at IS NULL
RETURNING id, status, version;

-- name: RetireEquipment :execrows
UPDATE equipment SET status = 'retired', deleted_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: UpsertEquipmentSpec :exec
INSERT INTO equipment_specifications (equipment_id, spec_key, value_numeric, value_text, unit)
VALUES (@equipment_id, @spec_key, sqlc.narg(value_numeric), sqlc.narg(value_text), sqlc.narg(unit))
ON CONFLICT (equipment_id, spec_key) DO UPDATE
SET value_numeric = EXCLUDED.value_numeric, value_text = EXCLUDED.value_text, unit = EXCLUDED.unit;

-- name: ListEquipmentSpecs :many
SELECT spec_key, value_numeric, value_text, unit
FROM equipment_specifications WHERE equipment_id = @equipment_id ORDER BY spec_key;

-- name: AddEquipmentPhoto :one
INSERT INTO equipment_media (equipment_id, object_key, content_type, size_bytes, sha256, width, height, angle, sort_order, is_cover, uploaded_by)
VALUES (@equipment_id, @object_key, @content_type, @size_bytes, @sha256, sqlc.narg(width), sqlc.narg(height), sqlc.narg(angle), @sort_order, @is_cover, sqlc.narg(uploaded_by))
RETURNING id;

-- name: ListEquipmentPhotos :many
SELECT id, object_key, content_type, width, height, angle, is_cover
FROM equipment_media
WHERE equipment_id = @equipment_id AND deleted_at IS NULL
ORDER BY is_cover DESC, sort_order;

-- name: AddEquipmentDocument :one
INSERT INTO equipment_documents (equipment_id, doc_type, document_number, object_key, content_type, size_bytes, sha256, valid_from, valid_until, uploaded_by)
VALUES (@equipment_id, @doc_type, sqlc.narg(document_number), @object_key, @content_type, @size_bytes, @sha256, sqlc.narg(valid_from), sqlc.narg(valid_until), sqlc.narg(uploaded_by))
RETURNING id;

-- name: ListExpiringDocuments :many
-- Verified documents expiring before a date (daily expiry scan).
SELECT d.id, d.equipment_id, d.doc_type, d.valid_until, e.owner_org_id, e.title
FROM equipment_documents d
JOIN equipment e ON e.id = d.equipment_id AND e.deleted_at IS NULL
WHERE d.status = 'verified' AND d.deleted_at IS NULL AND d.valid_until <= @before::date
ORDER BY d.valid_until;

-- name: CreatePricingPlan :one
INSERT INTO pricing_plans (equipment_id, unit, with_operator, rate_paise, min_units, included_hours, extra_hour_rate_paise, operator_bata_per_day_paise)
VALUES (@equipment_id, @unit, @with_operator, @rate_paise, @min_units, sqlc.narg(included_hours), sqlc.narg(extra_hour_rate_paise), @operator_bata_per_day_paise)
RETURNING id;

-- name: CreateAvailabilityBlock :one
INSERT INTO availability_blocks (equipment_id, period, reason, note, created_by)
VALUES (@equipment_id, tstzrange(@starts_at::timestamptz, @ends_at::timestamptz, '[)'), @reason, sqlc.narg(note), sqlc.narg(created_by))
RETURNING id;

-- name: SearchEquipment :many
-- Active, non-deleted machines of a category within reach of a site that are
-- free (no slot-holding booking, no block) for the requested window.
SELECT e.id, e.title, e.instant_book, e.rating_avg, e.rating_count,
       c.slug AS category_slug,
       p.rate_paise, p.unit,
       (ST_Distance(e.base_location, site.g) / 1000.0)::float8 AS distance_km
FROM equipment e
JOIN equipment_models m ON m.id = e.model_id
JOIN equipment_categories c ON c.id = m.category_id
JOIN pricing_plans p ON p.equipment_id = e.id AND p.is_active AND p.unit = @unit AND p.with_operator = @with_operator
CROSS JOIN (SELECT ST_SetSRID(ST_MakePoint(@lng::float8, @lat::float8), 4326)::geography AS g) site
WHERE e.status = 'active'
  AND e.deleted_at IS NULL
  AND c.slug = @category_slug
  AND ST_DWithin(e.base_location, site.g, LEAST(e.service_radius_km, @max_km::int) * 1000.0)
  AND NOT EXISTS (
    SELECT 1 FROM bookings b
    WHERE b.equipment_id = e.id AND booking_holds_slot(b.status)
      AND b.period && tstzrange(@starts_at::timestamptz, @ends_at::timestamptz, '[)'))
  AND NOT EXISTS (
    SELECT 1 FROM availability_blocks a
    WHERE a.equipment_id = e.id AND a.period && tstzrange(@starts_at::timestamptz, @ends_at::timestamptz, '[)'))
ORDER BY distance_km
LIMIT @result_limit;
