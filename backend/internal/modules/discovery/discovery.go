package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/modules/equipment"
)

// Result is one machine in search results. Location fields are the
// approximate public point (about 1 km); exact addresses are never public.
type Result struct {
	ID                uuid.UUID `json:"id"`
	Title             string    `json:"title"`
	CategorySlug      string    `json:"category_slug"`
	CategoryName      string    `json:"category_name"`
	MakeName          string    `json:"make_name"`
	ModelName         string    `json:"model_name"`
	ManufactureYear   *int16    `json:"manufacture_year,omitempty"`
	Condition         string    `json:"condition"`
	LocationLabel     *string   `json:"location_label,omitempty" doc:"Area chosen by the owner, e.g. Hinjewadi, Pune"`
	CityName          string    `json:"city_name,omitempty"`
	ApproxLocation    Point     `json:"approx_location" doc:"Centre of the ~1 km grid cell holding the machine"`
	DistanceKm        *float64  `json:"distance_km,omitempty" doc:"From the search point, approximate (0.1 km)"`
	DeliversToYou     *bool     `json:"delivers_to_you,omitempty" doc:"Search point is inside the owner's service radius and delivery is offered"`
	DeliveryAvailable bool      `json:"delivery_available"`
	WetHire           bool      `json:"wet_hire" doc:"Rentable with an operator"`
	DryHire           bool      `json:"dry_hire" doc:"Rentable without an operator"`
	InstantBook       bool      `json:"instant_book"`
	OwnerName         string    `json:"owner_name"`
	OwnerVerified     bool      `json:"owner_verified"`
	RatingAvg         *float64  `json:"rating_avg,omitempty"`
	RatingCount       int32     `json:"rating_count"`
	CoverURL          string    `json:"cover_url,omitempty"`
	PhotoCount        int32     `json:"photo_count"`
	PricePaise        int64     `json:"price_paise" doc:"Rate in the requested unit when listed, else the main rate"`
	PriceUnit         string    `json:"price_unit,omitempty"`
	PriceWithOperator bool      `json:"price_with_operator"`
	CreatedAt         time.Time `json:"created_at"`
	coverKey          string
}

// Point is a latitude/longitude pair.
type Point struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Results is a page of results.
type Results struct {
	Data     []Result `json:"data"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
	Total    int64    `json:"total"`
	Pages    int64    `json:"pages"`
	Sort     string   `json:"sort"`
	Origin   *Origin  `json:"origin,omitempty"`
	Fuzzy    bool     `json:"fuzzy" doc:"No exact match: results are for similar spellings"`
	TookMs   int64    `json:"took_ms"`
}

// Suggestion is a type-ahead entry.
type Suggestion struct {
	Kind   string `json:"kind" doc:"category, make, model, city or pincode"`
	Label  string `json:"label"`
	Value  string `json:"value" doc:"Slug, name, city id or PIN code to search with"`
	Detail string `json:"detail,omitempty"`
}

type SuggestionList struct {
	Data []Suggestion `json:"data"`
}

// Location is a place in the location picker.
type Location struct {
	Kind  string  `json:"kind" doc:"city or pincode"`
	Label string  `json:"label"`
	Value string  `json:"value" doc:"City id or PIN code"`
	Lat   float64 `json:"lat"`
	Lng   float64 `json:"lng"`
}

type LocationList struct {
	Data []Location `json:"data"`
}

// SavedItem is a saved machine.
type SavedItem struct {
	Result
	Available bool      `json:"available" doc:"false once the listing is paused or removed"`
	SavedAt   time.Time `json:"saved_at"`
}

type SavedList struct {
	Data []SavedItem `json:"data"`
}

type SavedIDs struct {
	Data []uuid.UUID `json:"data"`
}

// HistoryEntry is a recent search.
type HistoryEntry struct {
	ID          uuid.UUID       `json:"id"`
	Label       string          `json:"label"`
	Params      json.RawMessage `json:"params" swaggertype:"object" doc:"The query parameters to repeat the search"`
	ResultCount int32           `json:"result_count"`
	SearchedAt  time.Time       `json:"searched_at"`
}

type HistoryList struct {
	Data []HistoryEntry `json:"data"`
}

// Comparison is several listings side by side.
type Comparison struct {
	Data     []equipment.Listing `json:"data"`
	SpecKeys []string            `json:"spec_keys" doc:"Every specification key across the listings, in a stable order"`
}

// MapsInfo tells clients which map to draw.
type MapsInfo struct {
	Provider string `json:"provider" doc:"google when credentials are configured, otherwise builtin"`
}

// ---- dependencies ------------------------------------------------------------------------

// Repository is the data access the service needs.
type Repository interface {
	Discover(ctx context.Context, p Params) ([]Row, error)
	Suggest(ctx context.Context, q string, limit int32) ([]db.SuggestDiscoveryRow, error)
	Locations(ctx context.Context, q string) ([]db.SearchLocationsRow, error)
	Pincode(ctx context.Context, pin string) (db.GetPincodeRow, error)
	City(ctx context.Context, id uuid.UUID) (db.GetCityCenterRow, error)
	Save(ctx context.Context, user, id uuid.UUID) (int64, error)
	IsActive(ctx context.Context, id uuid.UUID) (bool, error)
	Unsave(ctx context.Context, user, id uuid.UUID) (int64, error)
	SavedIDs(ctx context.Context, user uuid.UUID) ([]uuid.UUID, error)
	Saved(ctx context.Context, user uuid.UUID) ([]db.ListSavedEquipmentRow, error)
	Record(ctx context.Context, p db.RecordSearchParams) error
	History(ctx context.Context, user uuid.UUID) ([]db.ListSearchHistoryRow, error)
	DeleteHistory(ctx context.Context, user, id uuid.UUID) (int64, error)
	ClearHistory(ctx context.Context, user uuid.UUID) error
}

// ListingReader returns a public listing (the equipment service).
type ListingReader interface {
	Public(ctx context.Context, p *httpx.Principal, id uuid.UUID) (equipment.Listing, error)
}

// Geocoder resolves PIN codes missing from the reference table (Google
// Geocoding when configured).
type Geocoder interface {
	Pincode(ctx context.Context, pin string) (lat, lng float64, label string, err error)
}

type PGRepository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *PGRepository { return &PGRepository{pool: pool} }

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

func (r *PGRepository) Suggest(ctx context.Context, q string, limit int32) ([]db.SuggestDiscoveryRow, error) {
	rows, err := r.q(ctx).SuggestDiscovery(ctx, db.SuggestDiscoveryParams{Q: q, ResultLimit: limit})
	return rows, database.MapError(err)
}
func (r *PGRepository) Locations(ctx context.Context, q string) ([]db.SearchLocationsRow, error) {
	rows, err := r.q(ctx).SearchLocations(ctx, q)
	return rows, database.MapError(err)
}
func (r *PGRepository) Pincode(ctx context.Context, pin string) (db.GetPincodeRow, error) {
	row, err := r.q(ctx).GetPincode(ctx, pin)
	return row, database.MapError(err)
}
func (r *PGRepository) City(ctx context.Context, id uuid.UUID) (db.GetCityCenterRow, error) {
	row, err := r.q(ctx).GetCityCenter(ctx, id)
	return row, database.MapError(err)
}
func (r *PGRepository) Save(ctx context.Context, user, id uuid.UUID) (int64, error) {
	n, err := r.q(ctx).SaveEquipment(ctx, db.SaveEquipmentParams{UserID: user, EquipmentID: id})
	return n, database.MapError(err)
}
func (r *PGRepository) IsActive(ctx context.Context, id uuid.UUID) (bool, error) {
	ok, err := r.q(ctx).IsEquipmentActive(ctx, id)
	return ok, database.MapError(err)
}
func (r *PGRepository) Unsave(ctx context.Context, user, id uuid.UUID) (int64, error) {
	n, err := r.q(ctx).UnsaveEquipment(ctx, db.UnsaveEquipmentParams{UserID: user, EquipmentID: id})
	return n, database.MapError(err)
}
func (r *PGRepository) SavedIDs(ctx context.Context, user uuid.UUID) ([]uuid.UUID, error) {
	ids, err := r.q(ctx).SavedEquipmentIDs(ctx, user)
	return ids, database.MapError(err)
}
func (r *PGRepository) Saved(ctx context.Context, user uuid.UUID) ([]db.ListSavedEquipmentRow, error) {
	rows, err := r.q(ctx).ListSavedEquipment(ctx, user)
	return rows, database.MapError(err)
}
func (r *PGRepository) Record(ctx context.Context, p db.RecordSearchParams) error {
	q := r.q(ctx)
	if _, err := q.RecordSearch(ctx, p); err != nil {
		return database.MapError(err)
	}
	return database.MapError(q.TrimSearchHistory(ctx, p.UserID))
}
func (r *PGRepository) History(ctx context.Context, user uuid.UUID) ([]db.ListSearchHistoryRow, error) {
	rows, err := r.q(ctx).ListSearchHistory(ctx, user)
	return rows, database.MapError(err)
}
func (r *PGRepository) DeleteHistory(ctx context.Context, user, id uuid.UUID) (int64, error) {
	n, err := r.q(ctx).DeleteSearchHistoryEntry(ctx, db.DeleteSearchHistoryEntryParams{UserID: user, ID: id})
	return n, database.MapError(err)
}
func (r *PGRepository) ClearHistory(ctx context.Context, user uuid.UUID) error {
	return database.MapError(r.q(ctx).ClearSearchHistory(ctx, user))
}

// ---- service -----------------------------------------------------------------------------

// Service runs discovery.
type Service struct {
	repo     Repository
	listings ListingReader
	geo      Geocoder
	sign     func(key string) string
	now      func() time.Time
	// OnRecordError reports search-history failures (they never fail a search).
	OnRecordError func(error)
}

func NewService(repo Repository, listings ListingReader, geo Geocoder, sign func(string) string) *Service {
	return &Service{repo: repo, listings: listings, geo: geo, sign: sign, now: time.Now}
}

// Maps reports the map provider.
func (s *Service) Maps() MapsInfo {
	if s.geo != nil {
		return MapsInfo{Provider: "google"}
	}
	return MapsInfo{Provider: "builtin"}
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// resolveOrigin turns a PIN code or city into a search point.
func (s *Service) resolveOrigin(ctx context.Context, plan *Plan) (*Origin, error) {
	if plan.Point != nil {
		if plan.Params.MaxKm == nil {
			r := float64(DefaultRadiusKm)
			plan.Params.MaxKm = &r
		}
		return plan.Point, nil
	}
	if plan.Pincode != "" {
		row, err := s.repo.Pincode(ctx, plan.Pincode)
		var o *Origin
		switch {
		case err == nil:
			o = &Origin{Source: "pincode", Label: row.Pincode + " · " + row.Area, Lat: row.Lat, Lng: row.Lng, Pincode: row.Pincode}
			if row.CityName != "" {
				o.Label += ", " + row.CityName
			}
		case errors.Is(err, apperror.ErrNotFound) && s.geo != nil:
			lat, lng, label, gerr := s.geo.Pincode(ctx, plan.Pincode)
			if gerr != nil {
				return nil, gerr
			}
			o = &Origin{Source: "pincode", Label: plan.Pincode + " · " + label, Lat: lat, Lng: lng, Pincode: plan.Pincode}
		case errors.Is(err, apperror.ErrNotFound):
			return nil, invalid("pincode", "unknown", "we don't have this PIN code yet; search by city instead")
		default:
			return nil, err
		}
		if plan.Params.MaxKm == nil {
			r := float64(PincodeRadiusKm)
			plan.Params.MaxKm = &r
		}
		return o, nil
	}
	if plan.CityID.Valid {
		c, err := s.repo.City(ctx, plan.CityID.UUID)
		if errors.Is(err, apperror.ErrNotFound) {
			return nil, invalid("city", "unknown", "unknown city")
		}
		if err != nil {
			return nil, err
		}
		// The city filter selects machines; the centre only orders and
		// measures distance (no radius on top of the city filter).
		return &Origin{Source: "city", Label: c.Name, Lat: c.Lat, Lng: c.Lng, CityID: c.ID.String()}, nil
	}
	return nil, nil
}

// Search runs a discovery query. p (optional) records the search in the
// user's history.
func (s *Service) Search(ctx context.Context, p *httpx.Principal, q Query) (Results, error) {
	started := s.now()
	plan, err := Build(q)
	if err != nil {
		return Results{}, err
	}
	origin, err := s.resolveOrigin(ctx, &plan)
	if err != nil {
		return Results{}, err
	}
	if origin != nil {
		plan.Params.OriginLat, plan.Params.OriginLng = &origin.Lat, &origin.Lng
	}
	if err := plan.finishSort(origin != nil); err != nil {
		return Results{}, err
	}
	rows, err := s.repo.Discover(ctx, plan.Params)
	if err != nil {
		return Results{}, err
	}
	fuzzy := false
	if len(rows) == 0 && plan.Page == 1 && plan.Params.Tsquery != nil {
		// Nothing matched exactly: retry tolerating typos ("excavtor").
		plan.Params.Fuzzy = true
		if rows, err = s.repo.Discover(ctx, plan.Params); err != nil {
			return Results{}, err
		}
		fuzzy = len(rows) > 0
	}
	out := Results{Data: make([]Result, 0, len(rows)), Page: plan.Page, PageSize: plan.PageSize, Sort: plan.Sort, Origin: origin, Fuzzy: fuzzy}
	for _, r := range rows {
		out.Total = r.TotalCount
		out.Data = append(out.Data, s.result(r))
	}
	if len(rows) == 0 && plan.Page > 1 {
		// Past the end: report the total without fetching page one again.
		out.Total = -1
	}
	if out.Total > 0 {
		out.Pages = (out.Total + int64(plan.PageSize) - 1) / int64(plan.PageSize)
	} else if out.Total < 0 {
		out.Total = 0
	}
	out.TookMs = s.now().Sub(started).Milliseconds()

	if p != nil && worthRecording(q, plan) {
		s.record(ctx, p.UserID, q, origin, out.Total)
	}
	return out, nil
}

func (s *Service) result(r Row) Result {
	res := Result{
		ID: r.ID, Title: r.Title, CategorySlug: r.CategorySlug, CategoryName: r.CategoryName, MakeName: r.MakeName,
		ModelName: r.ModelName, ManufactureYear: r.ManufactureYear, Condition: r.Condition, LocationLabel: r.LocationLabel,
		CityName: r.CityName, ApproxLocation: Point{Lat: round4(r.ApproxLat), Lng: round4(r.ApproxLng)},
		DeliveryAvailable: r.DeliveryAvailable, WetHire: r.WetHire, DryHire: r.DryHire, InstantBook: r.InstantBook,
		OwnerName: r.OwnerName, OwnerVerified: r.OwnerVerified, RatingAvg: r.RatingAvg, RatingCount: r.RatingCount,
		PhotoCount: r.PhotoCount, PricePaise: r.PricePaise, PriceUnit: r.PriceUnit, PriceWithOperator: r.PriceWithOperator,
		CreatedAt: r.CreatedAt, coverKey: r.CoverObjectKey,
	}
	if r.DistanceKm >= 0 {
		d := round1(r.DistanceKm)
		reach := r.DeliveryAvailable && r.DistanceKm <= float64(r.ServiceRadiusKm)
		res.DistanceKm, res.DeliversToYou = &d, &reach
	}
	if s.sign != nil && r.CoverObjectKey != "" {
		res.CoverURL = s.sign(r.CoverObjectKey)
	}
	return res
}

func round4(v float64) float64 { return math.Round(v*1e4) / 1e4 }

// label describes a search for the history list.
func label(q Query, origin *Origin) string {
	var parts []string
	if t := strings.Join(strings.Fields(q.Q), " "); t != "" {
		parts = append(parts, `"`+t+`"`)
	}
	if q.Category != "" {
		parts = append(parts, strings.ReplaceAll(strings.Join(splitList(q.Category), ", "), "-", " "))
	}
	if q.Make != "" || q.Model != "" {
		parts = append(parts, strings.TrimSpace(q.Make+" "+q.Model))
	}
	if origin != nil {
		parts = append(parts, "near "+origin.Label)
	}
	l := strings.Join(parts, " · ")
	if l == "" {
		l = "All equipment"
	}
	if len(l) > 200 {
		l = l[:200]
	}
	return l
}

func (s *Service) record(ctx context.Context, user uuid.UUID, q Query, origin *Origin, total int64) {
	params, _ := json.Marshal(historyParams(q))
	err := s.repo.Record(ctx, db.RecordSearchParams{
		UserID: user, Signature: Signature(q), Label: label(q, origin), Params: params, ResultCount: int32(min(total, math.MaxInt32)),
	})
	if err != nil && s.OnRecordError != nil {
		s.OnRecordError(err)
	}
}

// historyParams is the query to repeat, without paging or map bounds.
func historyParams(q Query) map[string]any {
	m := map[string]any{}
	put := func(k string, v any, ok bool) {
		if ok {
			m[k] = v
		}
	}
	put("q", strings.TrimSpace(q.Q), strings.TrimSpace(q.Q) != "")
	put("category", q.Category, q.Category != "")
	put("make", q.Make, q.Make != "")
	put("model", q.Model, q.Model != "")
	put("city", q.City, q.City != "")
	put("pincode", q.Pincode, q.Pincode != "")
	if q.Lat != nil {
		m["lat"], m["lng"] = math.Round(*q.Lat*100)/100, math.Round(*q.Lng*100)/100
	}
	if q.RadiusKm != nil {
		m["radius_km"] = *q.RadiusKm
	}
	put("unit", q.Unit, q.Unit != "")
	if q.PriceMin != nil {
		m["price_min"] = *q.PriceMin
	}
	if q.PriceMax != nil {
		m["price_max"] = *q.PriceMax
	}
	if q.RatingMin != nil {
		m["rating_min"] = *q.RatingMin
	}
	put("available_from", q.AvailableFrom, q.AvailableFrom != "")
	put("available_to", q.AvailableTo, q.AvailableTo != "")
	put("condition", q.Condition, q.Condition != "")
	put("delivery", true, q.Delivery)
	put("operator", q.Operator, q.Operator != "")
	put("verified_only", true, q.VerifiedOnly)
	put("sort", q.Sort, q.Sort != "")
	return m
}

// Suggest returns type-ahead suggestions.
func (s *Service) Suggest(ctx context.Context, q string) (SuggestionList, error) {
	q = strings.TrimSpace(q)
	if len([]rune(q)) < 2 {
		return SuggestionList{Data: []Suggestion{}}, nil
	}
	rows, err := s.repo.Suggest(ctx, q, 10)
	if err != nil {
		return SuggestionList{}, err
	}
	out := SuggestionList{Data: make([]Suggestion, 0, len(rows))}
	for _, r := range rows {
		out.Data = append(out.Data, Suggestion{Kind: r.Kind, Label: r.Label, Value: r.Value, Detail: r.Detail})
	}
	return out, nil
}

// Locations lists cities and PIN codes for the location picker.
func (s *Service) Locations(ctx context.Context, q string) (LocationList, error) {
	rows, err := s.repo.Locations(ctx, strings.TrimSpace(q))
	if err != nil {
		return LocationList{}, err
	}
	out := LocationList{Data: make([]Location, 0, len(rows))}
	for _, r := range rows {
		out.Data = append(out.Data, Location{Kind: r.Kind, Label: r.Label, Value: r.Value, Lat: r.Lat, Lng: r.Lng})
	}
	return out, nil
}

// MaxCompare is how many machines can be compared at once.
const MaxCompare = 4

// Compare loads 2-4 public listings with the union of their spec keys.
func (s *Service) Compare(ctx context.Context, p *httpx.Principal, raw string) (Comparison, error) {
	var ids []uuid.UUID
	for v := range strings.SplitSeq(raw, ",") {
		id, err := uuid.Parse(strings.TrimSpace(v))
		if err != nil {
			return Comparison{}, invalid("ids", "uuid", "must be listing ids separated by commas")
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) < 2 || len(ids) > MaxCompare {
		return Comparison{}, invalid("ids", "count", "compare 2 to 4 machines")
	}
	out := Comparison{Data: make([]equipment.Listing, 0, len(ids)), SpecKeys: []string{}}
	for _, id := range ids {
		// Anonymous view: comparison is a public, customer-facing page.
		l, err := s.listings.Public(ctx, nil, id)
		if err != nil {
			return Comparison{}, err
		}
		l.Documents = nil
		out.Data = append(out.Data, l)
		for _, sp := range l.Specs {
			if !slices.Contains(out.SpecKeys, sp.Key) {
				out.SpecKeys = append(out.SpecKeys, sp.Key)
			}
		}
	}
	return out, nil
}

// ---- saved equipment ------------------------------------------------------------------------

func (s *Service) Save(ctx context.Context, user, id uuid.UUID) error {
	n, err := s.repo.Save(ctx, user, id)
	if err != nil || n > 0 {
		return err
	}
	ok, err := s.repo.IsActive(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.ErrNotFound.WithDetail("this listing is not available")
	}
	return nil // already saved
}

func (s *Service) Unsave(ctx context.Context, user, id uuid.UUID) error {
	_, err := s.repo.Unsave(ctx, user, id)
	return err
}

func (s *Service) SavedIDs(ctx context.Context, user uuid.UUID) (SavedIDs, error) {
	ids, err := s.repo.SavedIDs(ctx, user)
	return SavedIDs{Data: ids}, err
}

func (s *Service) Saved(ctx context.Context, user uuid.UUID) (SavedList, error) {
	rows, err := s.repo.Saved(ctx, user)
	if err != nil {
		return SavedList{}, err
	}
	out := SavedList{Data: make([]SavedItem, 0, len(rows))}
	for _, r := range rows {
		item := SavedItem{Available: r.Available, SavedAt: r.SavedAt, Result: Result{
			ID: r.ID, Title: r.Title, CategorySlug: r.CategorySlug, CategoryName: r.CategoryName, MakeName: r.MakeName,
			ModelName: r.ModelName, ManufactureYear: r.ManufactureYear, Condition: r.Condition, LocationLabel: r.LocationLabel,
			CityName: r.CityName, DeliveryAvailable: r.DeliveryAvailable, WetHire: r.WetHire, DryHire: r.DryHire,
			OwnerName: r.OwnerName, OwnerVerified: r.OwnerVerified, RatingAvg: r.RatingAvg, RatingCount: r.RatingCount,
			PhotoCount: r.PhotoCount, PricePaise: r.PricePaise, PriceUnit: r.PriceUnit,
		}}
		if s.sign != nil && r.CoverObjectKey != "" && r.Available {
			item.CoverURL = s.sign(r.CoverObjectKey)
		}
		out.Data = append(out.Data, item)
	}
	return out, nil
}

// ---- history --------------------------------------------------------------------------------

func (s *Service) History(ctx context.Context, user uuid.UUID) (HistoryList, error) {
	rows, err := s.repo.History(ctx, user)
	if err != nil {
		return HistoryList{}, err
	}
	out := HistoryList{Data: make([]HistoryEntry, 0, len(rows))}
	for _, r := range rows {
		out.Data = append(out.Data, HistoryEntry{ID: r.ID, Label: r.Label, Params: r.Params, ResultCount: r.ResultCount, SearchedAt: r.SearchedAt})
	}
	return out, nil
}

func (s *Service) DeleteHistory(ctx context.Context, user, id uuid.UUID) error {
	n, err := s.repo.DeleteHistory(ctx, user, id)
	if err == nil && n == 0 {
		return apperror.ErrNotFound
	}
	return err
}

func (s *Service) ClearHistory(ctx context.Context, user uuid.UUID) error {
	return s.repo.ClearHistory(ctx, user)
}
