// Package discovery is customer-facing equipment search: PostgreSQL
// full-text search (with trigram typo tolerance), PostGIS distance, map
// bounds and filters over active, approved listings only. Locations are
// the approximate public point of each machine (see migration 000016); the
// exact base location is never read here.
package discovery

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
)

// Sort orders.
const (
	SortRelevance = "relevance"
	SortNewest    = "newest"
	SortPriceAsc  = "price_asc"
	SortPriceDesc = "price_desc"
	SortDistance  = "distance"
	SortRating    = "rating"
)

// Limits.
const (
	DefaultPageSize  = 20
	MaxPageSize      = 50
	MaxMapPageSize   = 300
	DefaultRadiusKm  = 50
	PincodeRadiusKm  = 25
	MaxRadiusKm      = 500
	MaxWindowDays    = 180
	maxQueryTokens   = 8
	maxCategoryCount = 11
)

// Query is GET /discover/equipment.
type Query struct {
	Q             string   `form:"q" binding:"omitempty,max=100" doc:"Free text: machine name, manufacturer, model, area or city"`
	Category      string   `form:"category" binding:"omitempty,max=400" doc:"Category slug, or several separated by commas"`
	Make          string   `form:"make" binding:"omitempty,max=60" doc:"Manufacturer (exact name, case-insensitive)"`
	Model         string   `form:"model" binding:"omitempty,max=80" doc:"Model name (case-insensitive)"`
	City          string   `form:"city" binding:"omitempty,uuid" doc:"City id: only machines based in that city"`
	Pincode       string   `form:"pincode" binding:"omitempty,len=6,numeric" doc:"Search around a PIN code"`
	Lat           *float64 `form:"lat" binding:"omitempty,latitude" doc:"Search around a point (with lng), e.g. the device location"`
	Lng           *float64 `form:"lng" binding:"omitempty,longitude"`
	RadiusKm      *float64 `form:"radius_km" binding:"omitempty,gt=0,max=500" doc:"Distance limit from the search point (default 50 km; 25 km for a PIN code)"`
	MinLat        *float64 `form:"min_lat" binding:"omitempty,latitude" doc:"Map bounds: all four of min_lat, min_lng, max_lat, max_lng"`
	MinLng        *float64 `form:"min_lng" binding:"omitempty,longitude"`
	MaxLat        *float64 `form:"max_lat" binding:"omitempty,latitude"`
	MaxLng        *float64 `form:"max_lng" binding:"omitempty,longitude"`
	Unit          string   `form:"unit" binding:"omitempty,oneof=hour shift day week month" doc:"Price unit for price filters and sorting (default day)"`
	PriceMin      *int64   `form:"price_min" binding:"omitempty,min=0" doc:"Minimum rate per unit, paise"`
	PriceMax      *int64   `form:"price_max" binding:"omitempty,min=0" doc:"Maximum rate per unit, paise"`
	RatingMin     *float64 `form:"rating_min" binding:"omitempty,min=0,max=5"`
	AvailableFrom string   `form:"available_from" binding:"omitempty,max=40" doc:"Free from this date or time (YYYY-MM-DD or RFC 3339)"`
	AvailableTo   string   `form:"available_to" binding:"omitempty,max=40" doc:"Free until (exclusive). A date alone covers that whole day; default from + 1 day"`
	Condition     string   `form:"condition" binding:"omitempty,max=60" doc:"excellent, good, fair, needs_repair (comma separated)"`
	Delivery      bool     `form:"delivery" doc:"Only machines the owner delivers to site"`
	Operator      string   `form:"operator" binding:"omitempty,oneof=with without" doc:"with = rentable with an operator; without = machine only"`
	VerifiedOnly  bool     `form:"verified_only" doc:"Only owners whose business is verified"`
	Sort          string   `form:"sort" binding:"omitempty,oneof=relevance newest price_asc price_desc distance rating"`
	Page          int      `form:"page" binding:"omitempty,min=1,max=500"`
	PageSize      int      `form:"page_size" binding:"omitempty,min=1,max=300" doc:"Up to 50 for lists; up to 300 with map bounds"`
}

// Origin is the resolved search point.
type Origin struct {
	Source  string  `json:"source" doc:"point, pincode or city"`
	Label   string  `json:"label"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	CityID  string  `json:"city_id,omitempty"`
	Pincode string  `json:"pincode,omitempty"`
}

// Letters with their combining marks (Devanagari vowel signs) and digits.
var tokenRe = regexp.MustCompile(`[\p{L}\p{M}\p{N}]+`)

// TSQuery turns free text into a prefix tsquery ("jcb 3dx" ->
// "jcb:* & 3dx:*"). Only letters and digits survive, so user input can
// never inject tsquery operators. Returns "" when nothing is searchable.
func TSQuery(text string) string {
	var parts []string
	seen := map[string]bool{}
	for _, t := range tokenRe.FindAllString(strings.ToLower(text), -1) {
		if seen[t] || (len([]rune(t)) == 1 && !unicode.IsDigit([]rune(t)[0])) {
			continue
		}
		seen[t] = true
		parts = append(parts, t+":*")
		if len(parts) == maxQueryTokens {
			break
		}
	}
	return strings.Join(parts, " & ")
}

func splitList(s string) []string {
	var out []string
	for v := range strings.SplitSeq(s, ",") {
		if v = strings.TrimSpace(strings.ToLower(v)); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

var slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func invalid(field, code, msg string) error {
	return apperror.ErrValidation.WithFields(apperror.FieldError{Field: field, Code: code, Message: msg})
}

// parseTime accepts YYYY-MM-DD (midnight IST) or RFC 3339.
func parseTime(field, v string) (time.Time, bool, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC(), false, nil
	}
	ist := time.FixedZone("IST", 5*3600+1800)
	if t, err := time.ParseInLocation("2006-01-02", v, ist); err == nil {
		return t.UTC(), true, nil
	}
	return time.Time{}, false, invalid(field, "datetime", "use YYYY-MM-DD or an RFC 3339 time")
}

// Plan is a validated query: database parameters plus what the service
// still has to resolve (PIN code / city centre) and paging.
type Plan struct {
	Params   Params
	Page     int
	PageSize int
	Pincode  string
	CityID   uuid.NullUUID
	Sort     string
	Point    *Origin
	MapView  bool
}

// Build validates q and turns it into a Plan (exported for tests).
func Build(q Query) (Plan, error) {
	p := Params{PriceUnit: "day"}
	plan := Plan{Page: max(q.Page, 1), PageSize: q.PageSize}

	if t := strings.TrimSpace(q.Q); t != "" {
		if ts := TSQuery(t); ts != "" {
			p.Tsquery, p.QueryText = &ts, &t
		}
	}
	if cats := splitList(q.Category); len(cats) > 0 {
		if len(cats) > maxCategoryCount {
			return plan, invalid("category", "max", "too many categories")
		}
		for _, c := range cats {
			if !slugRe.MatchString(c) {
				return plan, invalid("category", "slug", "is not a category slug")
			}
		}
		p.CategorySlugs = cats
	}
	if m := strings.TrimSpace(q.Make); m != "" {
		p.MakeName = &m
	}
	if m := strings.TrimSpace(q.Model); m != "" {
		escaped := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(m)
		p.ModelName = &escaped
	}
	if q.City != "" {
		plan.CityID = uuid.NullUUID{UUID: uuid.MustParse(q.City), Valid: true}
		p.CityID = plan.CityID
	}

	// Location: a point beats a PIN code beats a city centre.
	if (q.Lat == nil) != (q.Lng == nil) {
		return plan, invalid("lat", "required_with", "lat and lng go together")
	}
	switch {
	case q.Lat != nil:
		plan.Point = &Origin{Source: "point", Label: "Your location", Lat: *q.Lat, Lng: *q.Lng}
	case q.Pincode != "":
		if q.Pincode[0] == '0' {
			return plan, invalid("pincode", "pincode", "is not a valid PIN code")
		}
		plan.Pincode = q.Pincode
	}
	if q.RadiusKm != nil {
		r := math.Min(*q.RadiusKm, MaxRadiusKm)
		p.MaxKm = &r
	}

	bounds := []*float64{q.MinLat, q.MinLng, q.MaxLat, q.MaxLng}
	set := 0
	for _, b := range bounds {
		if b != nil {
			set++
		}
	}
	switch set {
	case 0:
	case 4:
		if *q.MinLat >= *q.MaxLat || *q.MinLng >= *q.MaxLng {
			return plan, invalid("min_lat", "bounds", "min must be below max")
		}
		if *q.MaxLat-*q.MinLat > 30 || *q.MaxLng-*q.MinLng > 40 {
			return plan, invalid("min_lat", "bounds", "zoom in: the map area is too large")
		}
		p.MinLat, p.MinLng, p.MaxLat, p.MaxLng = q.MinLat, q.MinLng, q.MaxLat, q.MaxLng
		plan.MapView = true
	default:
		return plan, invalid("min_lat", "bounds", "send all four map bounds")
	}

	if q.Unit != "" {
		p.PriceUnit = q.Unit
	}
	if q.PriceMin != nil && q.PriceMax != nil && *q.PriceMin > *q.PriceMax {
		return plan, invalid("price_min", "lte", "must not exceed price_max")
	}
	p.PriceMin, p.PriceMax, p.RatingMin = q.PriceMin, q.PriceMax, q.RatingMin

	if q.AvailableFrom != "" || q.AvailableTo != "" {
		if q.AvailableFrom == "" {
			return plan, invalid("available_from", "required_with", "is required with available_to")
		}
		from, _, err := parseTime("available_from", q.AvailableFrom)
		if err != nil {
			return plan, err
		}
		to := from.Add(24 * time.Hour)
		if q.AvailableTo != "" {
			t, dateOnly, err := parseTime("available_to", q.AvailableTo)
			if err != nil {
				return plan, err
			}
			if dateOnly {
				t = t.Add(24 * time.Hour) // a date covers that whole day
			}
			to = t
		}
		if !to.After(from) {
			return plan, invalid("available_to", "gt", "must be after available_from")
		}
		if to.Sub(from) > MaxWindowDays*24*time.Hour {
			return plan, invalid("available_to", "max", fmt.Sprintf("the window is limited to %d days", MaxWindowDays))
		}
		p.AvailableFrom, p.AvailableTo = &from, &to
	}

	if conds := splitList(q.Condition); len(conds) > 0 {
		for _, c := range conds {
			if !slices.Contains([]string{"excellent", "good", "fair", "needs_repair"}, c) {
				return plan, invalid("condition", "oneof", "use excellent, good, fair or needs_repair")
			}
		}
		p.Conditions = conds
	}
	p.DeliveryOnly, p.VerifiedOnly = q.Delivery, q.VerifiedOnly
	switch q.Operator {
	case "with":
		yes := true
		p.WithOperator = &yes
	case "without":
		no := false
		p.WithOperator = &no
	}

	limit := DefaultPageSize
	if plan.PageSize > 0 {
		limit = plan.PageSize
	}
	if plan.MapView {
		limit = min(max(limit, 100), MaxMapPageSize)
	} else if limit > MaxPageSize {
		return plan, invalid("page_size", "max", fmt.Sprintf("is at most %d (or %d with map bounds)", MaxPageSize, MaxMapPageSize))
	}
	plan.PageSize = limit
	if (plan.Page-1)*limit > 10_000 {
		return plan, invalid("page", "max", "narrow the search instead of paging this far")
	}
	p.ResultLimit, p.ResultOffset = int32(limit), int32((plan.Page-1)*limit)

	plan.Sort = q.Sort
	plan.Params = p
	return plan, nil
}

// finishSort picks the default order once the origin is known and rejects
// a distance sort without one.
func (pl *Plan) finishSort(hasOrigin bool) error {
	switch {
	case pl.Sort == SortDistance && !hasOrigin:
		return invalid("sort", "requires", "choose a location (city, PIN code or your position) to sort by distance")
	case pl.Sort != "":
	case pl.Params.Tsquery != nil:
		pl.Sort = SortRelevance
	case hasOrigin:
		pl.Sort = SortDistance
	default:
		pl.Sort = SortNewest
	}
	pl.Params.Sort = pl.Sort
	return nil
}

// Signature identifies a search for history de-duplication: the
// meaningful parameters in a canonical order (paging and map bounds are
// excluded).
func Signature(q Query) string {
	v := url.Values{}
	add := func(k, s string) {
		if s = strings.TrimSpace(strings.ToLower(s)); s != "" {
			v.Set(k, s)
		}
	}
	f := func(x *float64, prec int) string {
		if x == nil {
			return ""
		}
		return strconv.FormatFloat(*x, 'f', prec, 64)
	}
	i := func(x *int64) string {
		if x == nil {
			return ""
		}
		return strconv.FormatInt(*x, 10)
	}
	add("q", strings.Join(strings.Fields(q.Q), " "))
	add("category", strings.Join(splitList(q.Category), ","))
	add("make", q.Make)
	add("model", q.Model)
	add("city", q.City)
	add("pincode", q.Pincode)
	add("lat", f(q.Lat, 2))
	add("lng", f(q.Lng, 2))
	add("radius_km", f(q.RadiusKm, 0))
	add("unit", q.Unit)
	add("price_min", i(q.PriceMin))
	add("price_max", i(q.PriceMax))
	add("rating_min", f(q.RatingMin, 1))
	add("available_from", q.AvailableFrom)
	add("available_to", q.AvailableTo)
	add("condition", strings.Join(splitList(q.Condition), ","))
	if q.Delivery {
		v.Set("delivery", "true")
	}
	add("operator", q.Operator)
	if q.VerifiedOnly {
		v.Set("verified_only", "true")
	}
	add("sort", q.Sort)
	return v.Encode()
}

// worthRecording reports whether a search says something about intent
// (plain browsing and map panning are not recorded).
func worthRecording(q Query, plan Plan) bool {
	if plan.MapView || plan.Page > 1 {
		return false
	}
	return strings.TrimSpace(q.Q) != "" || q.Category != "" || q.Make != "" || q.Model != "" ||
		q.City != "" || q.Pincode != "" || q.Lat != nil
}
