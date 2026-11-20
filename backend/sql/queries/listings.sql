-- Listing system: catalogue resolution, photos, documents, browsing,
-- moderation and category administration.

-- ---- catalogue ------------------------------------------------------------------------

-- name: CategoryBySlug :one
SELECT id, is_active FROM equipment_categories WHERE slug = @slug;

-- name: ModelCategory :one
SELECT m.category_id, c.is_active, c.slug
FROM equipment_models m JOIN equipment_categories c ON c.id = m.category_id
WHERE m.id = @id;

-- name: ResolveMake :one
-- citext makes "jcb" and "JCB" the same make; the first spelling is kept.
INSERT INTO equipment_makes (name) VALUES (@name)
ON CONFLICT (name) DO UPDATE SET name = equipment_makes.name
RETURNING id;

-- name: FindModel :one
SELECT id, category_id FROM equipment_models
WHERE make_id = @make_id AND lower(name) = lower(@name)
LIMIT 1;

-- name: InsertModel :one
INSERT INTO equipment_models (category_id, make_id, name) VALUES (@category_id, @make_id, @name)
RETURNING id;

-- ---- photos ----------------------------------------------------------------------------

-- name: AddListingPhoto :one
-- Appended at the end; the first photo becomes the cover.
INSERT INTO equipment_media (equipment_id, object_key, content_type, size_bytes, sha256, width, height, angle, sort_order, is_cover, uploaded_by)
VALUES (@equipment_id, @object_key, @content_type, @size_bytes, @sha256, sqlc.narg(width), sqlc.narg(height), sqlc.narg(angle),
  (SELECT COALESCE(max(sort_order) + 1, 0) FROM equipment_media WHERE equipment_id = @equipment_id AND deleted_at IS NULL),
  NOT EXISTS (SELECT 1 FROM equipment_media WHERE equipment_id = @equipment_id AND is_cover AND deleted_at IS NULL),
  sqlc.narg(uploaded_by))
RETURNING id;

-- name: CountListingPhotos :one
SELECT count(*)::int FROM equipment_media WHERE equipment_id = @equipment_id AND deleted_at IS NULL;

-- name: DeleteListingPhoto :execrows
UPDATE equipment_media SET deleted_at = now(), is_cover = false
WHERE id = @id AND equipment_id = @equipment_id AND deleted_at IS NULL;

-- name: ClearListingCover :exec
UPDATE equipment_media SET is_cover = false WHERE equipment_id = @equipment_id AND is_cover;

-- name: SetListingCover :execrows
UPDATE equipment_media SET is_cover = true
WHERE id = @id AND equipment_id = @equipment_id AND deleted_at IS NULL;

-- name: EnsureListingCover :exec
-- After the cover is removed, the first remaining photo becomes the cover.
UPDATE equipment_media SET is_cover = true
WHERE id = (SELECT md.id FROM equipment_media md WHERE md.equipment_id = @equipment_id AND md.deleted_at IS NULL ORDER BY md.sort_order LIMIT 1)
  AND NOT EXISTS (SELECT 1 FROM equipment_media md WHERE md.equipment_id = @equipment_id AND md.is_cover AND md.deleted_at IS NULL);

-- ---- documents -------------------------------------------------------------------------

-- name: ListListingDocuments :many
SELECT id, doc_type, document_number, content_type, valid_from, valid_until, status, object_key, created_at
FROM equipment_documents
WHERE equipment_id = @equipment_id AND deleted_at IS NULL
ORDER BY created_at;

-- name: GetListingDocument :one
SELECT id, doc_type, document_number, content_type, valid_from, valid_until, status, object_key, created_at
FROM equipment_documents
WHERE id = @id AND equipment_id = @equipment_id AND deleted_at IS NULL;

-- name: DeleteListingDocument :execrows
UPDATE equipment_documents SET deleted_at = now()
WHERE id = @id AND equipment_id = @equipment_id AND deleted_at IS NULL;

-- name: InsertListingAudit :exec
INSERT INTO audit_logs (actor_user_id, actor_role, action, entity_type, entity_id, reason, request_id)
VALUES (sqlc.narg(actor_user_id), sqlc.narg(actor_role), @action, @entity_type, sqlc.narg(entity_id), sqlc.narg(reason), sqlc.narg(request_id));

-- ---- status ----------------------------------------------------------------------------

-- name: RequireListingReview :execrows
-- Content changes to a live listing send it back to moderation.
UPDATE equipment SET status = 'pending_review', version = version + 1
WHERE id = @id AND deleted_at IS NULL AND status IN ('active', 'paused');

-- name: ModerateListing :one
UPDATE equipment
SET status = @to_status, moderation_note = sqlc.narg(note), moderated_by = sqlc.narg(moderated_by), moderated_at = now(),
    version = version + 1
WHERE id = @id AND deleted_at IS NULL AND status::text = ANY(@from_statuses::text[])
RETURNING id, status, version;

-- ---- browsing --------------------------------------------------------------------------

-- name: BrowseListings :many
-- Approved (active) machines, newest first, for the public catalogue.
SELECT e.id, e.title, e.manufacture_year, e.condition, e.location_label, e.delivery_available, e.instant_book,
       e.rating_avg, e.rating_count, e.wet_hire, e.dry_hire, e.status, e.version, e.created_at, e.owner_org_id,
       c.slug AS category_slug, (c.name ->> 'en')::text AS category_name, m.name AS model_name, mk.name::text AS make_name,
       o.display_name AS owner_name, (o.kyc_status = 'verified')::bool AS owner_verified,
       COALESCE(ci.name, '')::text AS city_name,
       COALESCE((SELECT md.object_key FROM equipment_media md WHERE md.equipment_id = e.id AND md.deleted_at IS NULL
                 ORDER BY md.is_cover DESC, md.sort_order LIMIT 1), '')::text AS cover_object_key,
       (SELECT count(*) FROM equipment_media md WHERE md.equipment_id = e.id AND md.deleted_at IS NULL)::int AS photo_count,
       COALESCE((SELECT p.rate_paise FROM pricing_plans p WHERE p.equipment_id = e.id AND p.is_active
                 ORDER BY array_position(ARRAY['day','shift','hour','week','month']::text[], p.unit::text), p.rate_paise LIMIT 1), 0)::bigint AS price_paise,
       COALESCE((SELECT p.unit::text FROM pricing_plans p WHERE p.equipment_id = e.id AND p.is_active
                 ORDER BY array_position(ARRAY['day','shift','hour','week','month']::text[], p.unit::text), p.rate_paise LIMIT 1), '')::text AS price_unit,
       e.moderation_note
FROM equipment e
JOIN equipment_models m ON m.id = e.model_id
JOIN equipment_makes mk ON mk.id = m.make_id
JOIN equipment_categories c ON c.id = m.category_id
JOIN organizations o ON o.id = e.owner_org_id
LEFT JOIN cities ci ON ci.id = e.city_id
WHERE e.deleted_at IS NULL
  AND (sqlc.narg(owner_org_id)::uuid IS NULL OR e.owner_org_id = sqlc.narg(owner_org_id))
  AND (sqlc.narg(status)::text IS NULL OR e.status::text = sqlc.narg(status))
  AND (NOT @public_only::bool OR (e.status = 'active' AND c.is_active))
  AND (sqlc.narg(only_ids)::uuid[] IS NULL OR e.id = ANY(sqlc.narg(only_ids)::uuid[]))
  AND (sqlc.narg(category_slug)::text IS NULL OR c.slug = sqlc.narg(category_slug))
  AND (sqlc.narg(city_id)::uuid IS NULL OR e.city_id = sqlc.narg(city_id))
  AND (sqlc.narg(condition)::text IS NULL OR e.condition::text = sqlc.narg(condition))
  AND (NOT @delivery_only::bool OR e.delivery_available)
  AND (NOT @verified_only::bool OR o.kyc_status = 'verified')
  AND (sqlc.narg(query)::text IS NULL OR e.title ILIKE '%' || sqlc.narg(query) || '%'
       OR m.name ILIKE '%' || sqlc.narg(query) || '%' OR mk.name ILIKE '%' || sqlc.narg(query) || '%'
       OR e.location_label ILIKE '%' || sqlc.narg(query) || '%')
  AND (sqlc.narg(after)::uuid IS NULL OR e.id < sqlc.narg(after))
ORDER BY e.id DESC
LIMIT @result_limit;

-- ---- categories ------------------------------------------------------------------------

-- name: ListCategoriesWithCounts :many
SELECT c.id, c.slug, (c.name ->> 'en')::text AS name_en, COALESCE(c.name ->> @lang::text, c.name ->> 'en')::text AS name_local,
       c.parent_id, c.icon_key, c.sort_order, c.is_active, c.spec_schema,
       (SELECT count(*) FROM equipment e JOIN equipment_models m ON m.id = e.model_id
        WHERE m.category_id = c.id AND e.status = 'active' AND e.deleted_at IS NULL)::int AS listing_count,
       (SELECT count(*) FROM equipment_models m WHERE m.category_id = c.id)::int AS model_count
FROM equipment_categories c
WHERE @include_inactive::bool OR c.is_active
ORDER BY c.sort_order, c.slug;

-- name: GetCategory :one
SELECT * FROM equipment_categories WHERE id = @id;

-- name: CreateCategory :one
INSERT INTO equipment_categories (slug, name, sort_order, spec_schema, icon_key)
VALUES (@slug, @name, @sort_order, @spec_schema, sqlc.narg(icon_key))
RETURNING id;

-- name: UpdateCategory :exec
UPDATE equipment_categories
SET name = @name, sort_order = @sort_order, is_active = @is_active, spec_schema = @spec_schema, icon_key = sqlc.narg(icon_key)
WHERE id = @id;
