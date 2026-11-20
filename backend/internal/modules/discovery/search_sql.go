package discovery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/equiprent/backend/internal/database"
)

// Params are the database-level search parameters (nil = no filter).
type Params struct {
	Tsquery       *string // prefix tsquery built by TSQuery
	QueryText     *string // raw text, for ranking and the fuzzy pass
	Fuzzy         bool    // trigram match instead of full-text (typo fallback)
	PriceUnit     string
	WithOperator  *bool
	CategorySlugs []string
	MakeName      *string
	ModelName     *string // LIKE-escaped
	CityID        uuid.NullUUID
	OriginLat     *float64
	OriginLng     *float64
	MaxKm         *float64
	MinLat        *float64
	MinLng        *float64
	MaxLat        *float64
	MaxLng        *float64
	PriceMin      *int64
	PriceMax      *int64
	RatingMin     *float64
	Conditions    []string
	DeliveryOnly  bool
	VerifiedOnly  bool
	AvailableFrom *time.Time
	AvailableTo   *time.Time
	Sort          string
	ResultLimit   int32
	ResultOffset  int32
}

// Row is one search result row.
type Row struct {
	ID                uuid.UUID
	Title             string
	ManufactureYear   *int16
	Condition         string
	LocationLabel     *string
	DeliveryAvailable bool
	InstantBook       bool
	RatingAvg         *float64
	RatingCount       int32
	WetHire           bool
	DryHire           bool
	ServiceRadiusKm   int32
	CreatedAt         time.Time
	CategorySlug      string
	CategoryName      string
	ModelName         string
	MakeName          string
	OwnerName         string
	OwnerVerified     bool
	CityName          string
	ApproxLat         float64
	ApproxLng         float64
	DistanceKm        float64 // -1 without an origin
	CoverObjectKey    string
	PhotoCount        int32
	PricePaise        int64
	PriceUnit         string
	PriceWithOperator bool
	TotalCount        int64
}

// builder collects SQL fragments and positional arguments.
type builder struct {
	args  []any
	where []string
}

func (b *builder) arg(v any) string {
	b.args = append(b.args, v)
	return fmt.Sprintf("$%d", len(b.args))
}

func (b *builder) and(cond string) { b.where = append(b.where, cond) }

// searchSQL builds the two-stage search: stage one finds the matching ids
// with only the columns needed to filter, sort and count; stage two loads
// display columns (cover photo, prices) for the page alone.
func searchSQL(p Params) (string, []any) {
	b := &builder{}
	b.and("e.status = 'active'")
	b.and("e.deleted_at IS NULL")
	b.and("c.is_active")

	origin := ""
	if p.OriginLat != nil {
		origin = fmt.Sprintf("ST_SetSRID(ST_MakePoint(%s::float8, %s::float8), 4326)::geography", b.arg(*p.OriginLng), b.arg(*p.OriginLat))
	}

	rank := "0::float8"
	switch {
	case p.Tsquery != nil && !p.Fuzzy:
		ts := fmt.Sprintf("to_tsquery('simple', %s)", b.arg(*p.Tsquery))
		b.and("e.search_document @@ " + ts)
		rank = fmt.Sprintf("ts_rank_cd(e.search_document, %s)", ts)
	case p.Tsquery != nil && p.Fuzzy:
		// Indexed word-similarity (threshold set per transaction) on the
		// title, or a make/model whose name is close.
		q := b.arg(*p.QueryText)
		b.and(fmt.Sprintf(`(%[1]s <%% e.title OR e.model_id IN (
		  SELECT m2.id FROM equipment_models m2 JOIN equipment_makes mk2 ON mk2.id = m2.make_id
		  WHERE %[1]s <%% m2.name OR %[1]s <%% mk2.name::text))`, q))
		rank = fmt.Sprintf("word_similarity(%[1]s, e.title || ' ' || mk.name || ' ' || m.name)", q)
	}
	if len(p.CategorySlugs) > 0 {
		b.and("c.slug = ANY(" + b.arg(p.CategorySlugs) + "::text[])")
	}
	if p.MakeName != nil {
		b.and("mk.name = " + b.arg(*p.MakeName) + "::citext")
	}
	if p.ModelName != nil {
		b.and("m.name ILIKE " + b.arg(*p.ModelName))
	}
	if p.CityID.Valid {
		b.and("e.city_id = " + b.arg(p.CityID.UUID))
	}
	if origin != "" && p.MaxKm != nil {
		b.and(fmt.Sprintf("ST_DWithin(e.public_location, %s, %s::float8 * 1000.0)", origin, b.arg(*p.MaxKm)))
	}
	if p.MinLat != nil {
		b.and(fmt.Sprintf("ST_Intersects(e.public_location, ST_MakeEnvelope(%s, %s, %s, %s, 4326)::geography)",
			b.arg(*p.MinLng), b.arg(*p.MinLat), b.arg(*p.MaxLng), b.arg(*p.MaxLat)))
	}
	if p.RatingMin != nil {
		b.and("e.rating_avg >= " + b.arg(*p.RatingMin))
	}
	if len(p.Conditions) > 0 {
		b.and("e.condition::text = ANY(" + b.arg(p.Conditions) + "::text[])")
	}
	if p.DeliveryOnly {
		b.and("e.delivery_available")
	}
	if p.WithOperator != nil {
		if *p.WithOperator {
			b.and("e.wet_hire")
		} else {
			b.and("e.dry_hire")
		}
	}
	if p.VerifiedOnly {
		b.and("o.kyc_status = 'verified'")
	}
	if p.AvailableFrom != nil {
		window := fmt.Sprintf("tstzrange(%s::timestamptz, %s::timestamptz, '[)')", b.arg(*p.AvailableFrom), b.arg(*p.AvailableTo))
		b.and(fmt.Sprintf(`NOT EXISTS (SELECT 1 FROM bookings bk WHERE bk.equipment_id = e.id AND booking_holds_slot(bk.status) AND bk.period && %[1]s)
		  AND NOT EXISTS (SELECT 1 FROM availability_blocks ab WHERE ab.equipment_id = e.id AND ab.period && %[1]s)`, window))
	}

	// The requested-unit price is only joined in stage one when it filters or sorts.
	priceNeeded := p.PriceMin != nil || p.PriceMax != nil || p.Sort == SortPriceAsc || p.Sort == SortPriceDesc
	unitArg := b.arg(p.PriceUnit)
	opFilter := ""
	if p.WithOperator != nil {
		opFilter = " AND pp.with_operator = " + b.arg(*p.WithOperator)
	}
	unitLateral := fmt.Sprintf(`LEFT JOIN LATERAL (
	    SELECT pp.rate_paise, pp.unit::text AS unit, pp.with_operator FROM pricing_plans pp
	    WHERE pp.equipment_id = e.id AND pp.is_active AND pp.unit::text = %s%s
	    ORDER BY pp.rate_paise LIMIT 1) up ON true`, unitArg, opFilter)
	stage1Join := ""
	unitPrice := "NULL::bigint"
	if priceNeeded {
		stage1Join = unitLateral
		unitPrice = "up.rate_paise"
		if p.PriceMin != nil {
			b.and("up.rate_paise >= " + b.arg(*p.PriceMin))
		}
		if p.PriceMax != nil {
			b.and("up.rate_paise <= " + b.arg(*p.PriceMax))
		}
	}

	dist := "-1::float8"
	if origin != "" {
		dist = fmt.Sprintf("ST_Distance(e.public_location, %s) / 1000.0", origin)
	}
	order := map[string]string{
		SortRelevance: "rank DESC, e.created_at DESC",
		SortPriceAsc:  "unit_price ASC NULLS LAST, e.created_at DESC",
		SortPriceDesc: "unit_price DESC NULLS LAST, e.created_at DESC",
		SortDistance:  "dist ASC, e.created_at DESC",
		SortRating:    "e.rating_avg DESC NULLS LAST, e.rating_count DESC, e.created_at DESC",
		SortNewest:    "e.created_at DESC",
	}[p.Sort]
	if order == "" {
		order = "e.created_at DESC"
	}
	// ORDER BY may reference output aliases only as bare names.
	order = strings.NewReplacer("rank", "s_rank", "unit_price", "s_unit_price", "dist", "s_dist").Replace(order)

	limit, offset := b.arg(p.ResultLimit), b.arg(p.ResultOffset)
	sql := fmt.Sprintf(`
WITH hits AS (
  SELECT e.id, (%[1]s)::float8 AS s_rank, (%[2]s)::bigint AS s_unit_price, (%[3]s)::float8 AS s_dist,
         e.created_at, e.rating_avg, e.rating_count, count(*) OVER () AS total_count
  FROM equipment e
  JOIN equipment_models m ON m.id = e.model_id
  JOIN equipment_makes mk ON mk.id = m.make_id
  JOIN equipment_categories c ON c.id = m.category_id
  JOIN organizations o ON o.id = e.owner_org_id
  %[4]s
  WHERE %[5]s
), page AS (
  SELECT h.*, row_number() OVER (ORDER BY %[6]s, h.id DESC) AS ord
  FROM hits h
  ORDER BY %[6]s, h.id DESC
  LIMIT %[7]s OFFSET %[8]s
)
SELECT e.id, e.title, e.manufacture_year, e.condition::text, e.location_label, e.delivery_available, e.instant_book,
       e.rating_avg, e.rating_count, e.wet_hire, e.dry_hire, e.service_radius_km, e.created_at,
       c.slug, (c.name ->> 'en')::text, m.name, mk.name::text,
       o.display_name, (o.kyc_status = 'verified'), COALESCE(ci.name, ''),
       ST_Y(e.public_location::geometry), ST_X(e.public_location::geometry), page.s_dist,
       COALESCE((SELECT md.object_key FROM equipment_media md WHERE md.equipment_id = e.id AND md.deleted_at IS NULL
                 ORDER BY md.is_cover DESC, md.sort_order LIMIT 1), ''),
       (SELECT count(*) FROM equipment_media md WHERE md.equipment_id = e.id AND md.deleted_at IS NULL)::int4,
       COALESCE(up.rate_paise, dp.rate_paise, 0)::int8, COALESCE(up.unit, dp.unit, ''), COALESCE(up.with_operator, dp.with_operator, false),
       page.total_count
FROM page
JOIN equipment e ON e.id = page.id
JOIN equipment_models m ON m.id = e.model_id
JOIN equipment_makes mk ON mk.id = m.make_id
JOIN equipment_categories c ON c.id = m.category_id
JOIN organizations o ON o.id = e.owner_org_id
LEFT JOIN cities ci ON ci.id = e.city_id
%[9]s
LEFT JOIN LATERAL (
  SELECT pp.rate_paise, pp.unit::text AS unit, pp.with_operator FROM pricing_plans pp
  WHERE pp.equipment_id = e.id AND pp.is_active
  ORDER BY array_position(ARRAY['day','shift','hour','week','month']::text[], pp.unit::text), pp.rate_paise LIMIT 1) dp ON true
ORDER BY page.ord`,
		rank, unitPrice, dist, stage1Join, strings.Join(b.where, "\n    AND "),
		strings.ReplaceAll(order, "e.", "h."), limit, offset, unitLateral)
	return sql, b.args
}

// Discover runs the search. The fuzzy pass lowers the word-similarity
// threshold for its own transaction only.
func (r *PGRepository) Discover(ctx context.Context, p Params) ([]Row, error) {
	sql, args := searchSQL(p)
	run := func(q interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	}) ([]Row, error) {
		rows, err := q.Query(ctx, sql, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []Row
		for rows.Next() {
			var x Row
			if err := rows.Scan(&x.ID, &x.Title, &x.ManufactureYear, &x.Condition, &x.LocationLabel, &x.DeliveryAvailable,
				&x.InstantBook, &x.RatingAvg, &x.RatingCount, &x.WetHire, &x.DryHire, &x.ServiceRadiusKm, &x.CreatedAt,
				&x.CategorySlug, &x.CategoryName, &x.ModelName, &x.MakeName, &x.OwnerName, &x.OwnerVerified, &x.CityName,
				&x.ApproxLat, &x.ApproxLng, &x.DistanceKm, &x.CoverObjectKey, &x.PhotoCount, &x.PricePaise, &x.PriceUnit,
				&x.PriceWithOperator, &x.TotalCount); err != nil {
				return nil, err
			}
			out = append(out, x)
		}
		return out, rows.Err()
	}
	if !p.Fuzzy {
		out, err := run(database.Conn(ctx, r.pool))
		return out, database.MapError(err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT set_config('pg_trgm.word_similarity_threshold', '0.5', true)"); err != nil {
		return nil, database.MapError(err)
	}
	out, err := run(tx)
	if err != nil {
		return nil, database.MapError(err)
	}
	return out, database.MapError(tx.Commit(ctx))
}
