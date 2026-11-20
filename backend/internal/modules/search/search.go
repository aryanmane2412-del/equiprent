// Package search finds available machines near a site: PostGIS distance
// filtering, the listing's own service radius, availability for the window
// (no held booking, no block), price and verification filters, and sorting by
// distance, price or rating.
package search

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/httpx"
)

// Query is the search request (query string).
type Query struct {
	Category     string    `form:"category" binding:"omitempty,max=60" doc:"Category slug, e.g. excavator"`
	Lat          float64   `form:"lat" binding:"required,latitude"`
	Lng          float64   `form:"lng" binding:"required,longitude"`
	RadiusKm     int32     `form:"radius_km" binding:"omitempty,min=1,max=500"`
	Start        time.Time `form:"start" binding:"required" time_format:"2006-01-02T15:04:05Z07:00"`
	End          time.Time `form:"end" binding:"required,gtfield=Start" time_format:"2006-01-02T15:04:05Z07:00"`
	Unit         string    `form:"unit" binding:"omitempty,oneof=hour shift day week month"`
	WithOperator *bool     `form:"with_operator"`
	MakeID       string    `form:"make" binding:"omitempty,uuid"`
	PriceMax     *int64    `form:"price_max" binding:"omitempty,min=0" doc:"Maximum rate per unit, paise"`
	VerifiedOnly bool      `form:"verified_only"`
	Q            string    `form:"q" binding:"omitempty,max=80" doc:"Free text on title / model"`
	Sort         string    `form:"sort" binding:"omitempty,oneof=distance price rating"`
	Limit        int       `form:"limit" binding:"omitempty,min=1,max=50"`
	Cursor       string    `form:"cursor"`
}

// Result is one available machine.
type Result struct {
	ID             uuid.UUID `json:"id"`
	Title          string    `json:"title"`
	CategorySlug   string    `json:"category_slug"`
	MakeName       string    `json:"make_name"`
	ModelName      string    `json:"model_name"`
	OwnerName      string    `json:"owner_name"`
	OwnerVerified  bool      `json:"owner_verified"`
	InstantBook    bool      `json:"instant_book"`
	RatingAvg      *float64  `json:"rating_avg,omitempty"`
	RatingCount    int32     `json:"rating_count"`
	RatePaise      int64     `json:"rate_paise"`
	Unit           string    `json:"unit"`
	WithOperator   bool      `json:"with_operator"`
	DistanceKm     float64   `json:"distance_km"`
	CoverURL       string    `json:"cover_url,omitempty" doc:"Signed, time-limited photo link"`
	CoverObjectKey string    `json:"-"`
}

// Results is a page of results. The cursor is an opaque offset because
// results are ranked, not keyed.
type Results struct {
	Data       []Result `json:"data"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

// Repository runs the search query.
type Repository interface {
	Search(ctx context.Context, p db.SearchListingsParams) ([]Result, error)
}

type PGRepository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *PGRepository { return &PGRepository{pool: pool} }

func (r *PGRepository) Search(ctx context.Context, p db.SearchListingsParams) ([]Result, error) {
	rows, err := database.Queries(ctx, r.pool).SearchListings(ctx, p)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Result, 0, len(rows))
	for _, e := range rows {
		out = append(out, Result{
			ID: e.ID, Title: e.Title, CategorySlug: e.CategorySlug, MakeName: e.MakeName, ModelName: e.ModelName,
			OwnerName: e.OwnerName, OwnerVerified: e.OwnerVerified, InstantBook: e.InstantBook, RatingAvg: e.RatingAvg,
			RatingCount: e.RatingCount, RatePaise: e.RatePaise, Unit: string(e.Unit), WithOperator: e.WithOperator,
			DistanceKm: e.DistanceKm, CoverObjectKey: e.CoverObjectKey,
		})
	}
	return out, nil
}

// Service validates and runs searches.
type Service struct {
	repo Repository
	sign func(key string) string // signs cover photo links; nil = no links
}

func NewService(repo Repository) *Service { return &Service{repo: repo} }

// WithPhotoLinks makes results carry signed cover photo links.
func (s *Service) WithPhotoLinks(sign func(key string) string) *Service {
	s.sign = sign
	return s
}

// Params converts a query into database parameters (exported for tests).
func Params(q Query) (db.SearchListingsParams, int, error) {
	if q.End.Sub(q.Start) > 180*24*time.Hour {
		return db.SearchListingsParams{}, 0, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "end", Code: "max", Message: "rentals are limited to 180 days"})
	}
	offset := 0
	if q.Cursor != "" {
		n, err := strconv.Atoi(q.Cursor)
		if err != nil || n < 0 || n > 10_000 {
			return db.SearchListingsParams{}, 0, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "cursor", Code: "invalid", Message: "is not a valid cursor"})
		}
		offset = n
	}
	limit := q.Limit
	if limit == 0 {
		limit = 20
	}
	p := db.SearchListingsParams{
		Unit: db.PriceUnit(orDefault(q.Unit, "day")), WithOperator: q.WithOperator == nil || *q.WithOperator,
		Lat: q.Lat, Lng: q.Lng, MaxKm: q.RadiusKm, StartsAt: q.Start.UTC(), EndsAt: q.End.UTC(),
		Sort: orDefault(q.Sort, "distance"), VerifiedOnly: q.VerifiedOnly, PriceMaxPaise: q.PriceMax,
		ResultLimit: int32(limit + 1), ResultOffset: int32(offset),
	}
	if p.MaxKm == 0 {
		p.MaxKm = 100
	}
	if q.Category != "" {
		p.CategorySlug = &q.Category
	}
	if q.MakeID != "" {
		p.MakeID = uuid.NullUUID{UUID: uuid.MustParse(q.MakeID), Valid: true}
	}
	if t := strings.TrimSpace(q.Q); t != "" {
		escaped := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(t)
		p.Query = &escaped
	}
	return p, limit, nil
}

// Search returns available machines.
func (s *Service) Search(ctx context.Context, q Query) (Results, error) {
	p, limit, err := Params(q)
	if err != nil {
		return Results{}, err
	}
	rows, err := s.repo.Search(ctx, p)
	if err != nil {
		return Results{}, err
	}
	for i := range rows {
		if s.sign != nil && rows[i].CoverObjectKey != "" {
			rows[i].CoverURL = s.sign(rows[i].CoverObjectKey)
		}
	}
	out := Results{Data: rows}
	if len(rows) > limit {
		out.Data = rows[:limit]
		out.NextCursor = strconv.Itoa(int(p.ResultOffset) + limit)
	}
	return out, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// Handler exposes GET /search/equipment.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Name() string { return "search" }

func (h *Handler) Routes(r *httpx.Router) {
	r.Tag("search").GET("/search/equipment", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q Query) (any, error) {
		return h.svc.Search(c.Request.Context(), q)
	}), httpx.Doc{Summary: "Search available equipment near a site", Query: Query{}, Response: Results{},
		Description: "Only machines free for the whole [start, end) window and whose service radius reaches the site are returned."})
}
