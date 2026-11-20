-- Equipment discovery. Public results only ever contain active listings of
-- active categories, and only the approximate public_location (never
-- base_location) is read or returned.

-- The main search (DiscoverEquipment) is built dynamically in
-- internal/modules/discovery/search_sql.go: only the filters present are
-- emitted, so PostgreSQL can use the full-text, trigram and spatial indexes.

-- name: SuggestDiscovery :many
-- Type-ahead: categories, manufacturers, models, cities and PIN codes.
SELECT kind, label, value, detail, score::float8 AS score FROM (
  SELECT 'category'::text AS kind, (c.name ->> 'en')::text AS label, c.slug::text AS value, ''::text AS detail,
         word_similarity(@q::text, (c.name ->> 'en') || ' ' || replace(c.slug, '-', ' ')) + 0.3 AS score
  FROM equipment_categories c
  WHERE c.is_active AND ((c.name ->> 'en') ILIKE '%' || @q || '%' OR c.slug ILIKE '%' || @q || '%'
                          OR word_similarity(@q::text, (c.name ->> 'en') || ' ' || replace(c.slug, '-', ' ')) >= 0.5)
  UNION ALL
  SELECT 'make', mk.name::text, mk.name::text, count(e.id)::text || ' listed', word_similarity(@q::text, mk.name::text) + 0.2
  FROM equipment_makes mk
  JOIN equipment_models m ON m.make_id = mk.id
  JOIN equipment e ON e.model_id = m.id AND e.status = 'active' AND e.deleted_at IS NULL
  WHERE mk.name ILIKE @q || '%' OR word_similarity(@q::text, mk.name::text) >= 0.5
  GROUP BY mk.name
  UNION ALL
  SELECT 'model', mk.name::text || ' ' || m.name, m.name, (c.name ->> 'en')::text, word_similarity(@q::text, mk.name::text || ' ' || m.name) + 0.1
  FROM equipment_models m
  JOIN equipment_makes mk ON mk.id = m.make_id
  JOIN equipment_categories c ON c.id = m.category_id
  WHERE EXISTS (SELECT 1 FROM equipment e WHERE e.model_id = m.id AND e.status = 'active' AND e.deleted_at IS NULL)
    AND (m.name ILIKE '%' || @q || '%' OR (mk.name::text || ' ' || m.name) ILIKE '%' || @q || '%'
         OR word_similarity(@q::text, mk.name::text || ' ' || m.name) >= 0.5)
  UNION ALL
  SELECT 'city', ci.name, ci.id::text, '', word_similarity(@q::text, ci.name) + 0.25
  FROM cities ci
  WHERE ci.name ILIKE @q || '%' OR word_similarity(@q::text, ci.name) >= 0.6
  UNION ALL
  SELECT 'pincode', p.pincode || ' · ' || p.area, p.pincode::text, COALESCE(ci.name, ''), 0.4
  FROM pincodes p LEFT JOIN cities ci ON ci.id = p.city_id
  WHERE (@q::text ~ '^[0-9]{3,6}$' AND p.pincode LIKE @q || '%') OR (length(@q::text) >= 3 AND p.area ILIKE @q || '%')
) s
ORDER BY score DESC, label
LIMIT @result_limit;

-- name: SearchLocations :many
-- Cities and PIN codes for the location picker.
SELECT kind, label, value, lat, lng FROM (
  SELECT 'city'::text AS kind, ci.name AS label, ci.id::text AS value,
         ST_Y(ci.center::geometry)::float8 AS lat, ST_X(ci.center::geometry)::float8 AS lng, 0 AS rank
  FROM cities ci WHERE @q::text = '' OR ci.name ILIKE @q || '%'
  UNION ALL
  SELECT 'pincode', p.pincode || ' · ' || p.area || COALESCE(', ' || ci.name, ''), p.pincode::text,
         ST_Y(p.center::geometry)::float8, ST_X(p.center::geometry)::float8, 1
  FROM pincodes p LEFT JOIN cities ci ON ci.id = p.city_id
  WHERE @q::text <> '' AND (p.pincode LIKE @q || '%' OR p.area ILIKE '%' || @q || '%')
) l
ORDER BY rank, label
LIMIT 20;

-- name: GetPincode :one
SELECT p.pincode::text AS pincode, p.area, COALESCE(ci.name, '')::text AS city_name, p.city_id, p.state_code,
       ST_Y(p.center::geometry)::float8 AS lat, ST_X(p.center::geometry)::float8 AS lng
FROM pincodes p LEFT JOIN cities ci ON ci.id = p.city_id
WHERE p.pincode = @pincode;

-- name: GetCityCenter :one
SELECT ci.id, ci.name, ST_Y(ci.center::geometry)::float8 AS lat, ST_X(ci.center::geometry)::float8 AS lng
FROM cities ci WHERE ci.id = @id;

-- ---- saved equipment ----------------------------------------------------------------------

-- name: SaveEquipment :execrows
INSERT INTO saved_equipment (user_id, equipment_id)
SELECT @user_id, e.id FROM equipment e WHERE e.id = @equipment_id AND e.status = 'active' AND e.deleted_at IS NULL
ON CONFLICT DO NOTHING;

-- name: IsEquipmentActive :one
SELECT EXISTS (SELECT 1 FROM equipment e WHERE e.id = @equipment_id AND e.status = 'active' AND e.deleted_at IS NULL)::bool;

-- name: UnsaveEquipment :execrows
DELETE FROM saved_equipment WHERE user_id = @user_id AND equipment_id = @equipment_id;

-- name: SavedEquipmentIDs :many
SELECT equipment_id FROM saved_equipment WHERE user_id = @user_id ORDER BY created_at DESC;

-- name: ListSavedEquipment :many
-- Saved machines, newest first; ones no longer listed come back with
-- available = false so the customer can remove them.
SELECT e.id, e.title, (e.status = 'active' AND e.deleted_at IS NULL AND c.is_active)::bool AS available,
       c.slug AS category_slug, (c.name ->> 'en')::text AS category_name, m.name AS model_name, mk.name::text AS make_name,
       e.manufacture_year, e.condition::text AS condition, e.location_label, COALESCE(ci.name, '')::text AS city_name,
       e.delivery_available, e.wet_hire, e.dry_hire, e.rating_avg, e.rating_count,
       o.display_name AS owner_name, (o.kyc_status = 'verified')::bool AS owner_verified,
       COALESCE((SELECT md.object_key FROM equipment_media md WHERE md.equipment_id = e.id AND md.deleted_at IS NULL
                 ORDER BY md.is_cover DESC, md.sort_order LIMIT 1), '')::text AS cover_object_key,
       (SELECT count(*) FROM equipment_media md WHERE md.equipment_id = e.id AND md.deleted_at IS NULL)::int AS photo_count,
       COALESCE(dp.rate_paise, 0)::bigint AS price_paise, COALESCE(dp.unit, '')::text AS price_unit,
       s.created_at AS saved_at
FROM saved_equipment s
JOIN equipment e ON e.id = s.equipment_id
JOIN equipment_models m ON m.id = e.model_id
JOIN equipment_makes mk ON mk.id = m.make_id
JOIN equipment_categories c ON c.id = m.category_id
JOIN organizations o ON o.id = e.owner_org_id
LEFT JOIN cities ci ON ci.id = e.city_id
LEFT JOIN LATERAL (
  SELECT p.rate_paise, p.unit::text AS unit FROM pricing_plans p
  WHERE p.equipment_id = e.id AND p.is_active
  ORDER BY array_position(ARRAY['day','shift','hour','week','month']::text[], p.unit::text), p.rate_paise LIMIT 1) dp ON true
WHERE s.user_id = @user_id
ORDER BY s.created_at DESC
LIMIT 200;

-- ---- search history --------------------------------------------------------------------------

-- name: RecordSearch :one
INSERT INTO search_history (user_id, signature, label, params, result_count)
VALUES (@user_id, @signature, @label, @params, @result_count)
ON CONFLICT (user_id, signature) DO UPDATE
  SET searched_at = now(), result_count = EXCLUDED.result_count, label = EXCLUDED.label
RETURNING id;

-- name: TrimSearchHistory :exec
DELETE FROM search_history h
WHERE h.user_id = @user_id
  AND h.id NOT IN (SELECT x.id FROM search_history x WHERE x.user_id = @user_id ORDER BY x.searched_at DESC LIMIT 30);

-- name: ListSearchHistory :many
SELECT id, label, params, result_count, searched_at FROM search_history
WHERE user_id = @user_id ORDER BY searched_at DESC LIMIT 30;

-- name: DeleteSearchHistoryEntry :execrows
DELETE FROM search_history WHERE user_id = @user_id AND id = @id;

-- name: ClearSearchHistory :exec
DELETE FROM search_history WHERE user_id = @user_id;
