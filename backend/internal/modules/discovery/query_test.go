package discovery

import (
	"errors"
	"strings"
	"testing"

	"github.com/equiprent/backend/internal/apperror"
)

func ptr[T any](v T) *T { return &v }

func TestTSQueryIsPrefixAndSafe(t *testing.T) {
	cases := map[string]string{
		"JCB 3DX":                                      "jcb:* & 3dx:*",
		"  excavator  ":                                "excavator:*",
		"a 20 t crane":                                 "20:* & crane:*", // single letters dropped, digits kept
		"x' | !y & (z:*)":                              "",
		"crane crane CRANE":                            "crane:*",
		"हाइड्रा क्रेन":                                "हाइड्रा:* & क्रेन:*",
		"pc210' & ! | <-> : * ( ) drop":                "pc210:* & drop:*",
		"one two three four five six seven eight nine": "one:* & two:* & three:* & four:* & five:* & six:* & seven:* & eight:*",
	}
	for in, want := range cases {
		if got := TSQuery(in); got != want {
			t.Errorf("TSQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildDefaults(t *testing.T) {
	pl, err := Build(Query{})
	if err != nil {
		t.Fatal(err)
	}
	if pl.Page != 1 || pl.PageSize != DefaultPageSize || pl.Params.PriceUnit != "day" || pl.Params.ResultOffset != 0 || pl.MapView {
		t.Fatalf("plan = %+v", pl)
	}
	if err := pl.finishSort(false); err != nil || pl.Params.Sort != SortNewest {
		t.Fatalf("default sort %q %v", pl.Params.Sort, err)
	}
	q, _ := Build(Query{Q: "jcb"})
	_ = q.finishSort(true)
	if q.Sort != SortRelevance {
		t.Fatal("text search sorts by relevance")
	}
	o, _ := Build(Query{Lat: ptr(18.5), Lng: ptr(73.8)})
	_ = o.finishSort(true)
	if o.Sort != SortDistance {
		t.Fatal("location search sorts by distance")
	}
}

func TestBuildFilters(t *testing.T) {
	pl, err := Build(Query{
		Category: "Excavator, crane,excavator", Condition: "good,fair", Operator: "without", Delivery: true,
		Model: "100%_x", PriceMin: ptr[int64](100), PriceMax: ptr[int64](900), Page: 3, PageSize: 10,
		AvailableFrom: "2027-01-10", AvailableTo: "2027-01-12",
	})
	if err != nil {
		t.Fatal(err)
	}
	p := pl.Params
	if strings.Join(p.CategorySlugs, ",") != "excavator,crane" || strings.Join(p.Conditions, ",") != "good,fair" {
		t.Fatalf("lists: %v %v", p.CategorySlugs, p.Conditions)
	}
	if p.WithOperator == nil || *p.WithOperator || !p.DeliveryOnly || *p.ModelName != `100\%\_x` {
		t.Fatalf("filters: %+v", p)
	}
	if p.ResultLimit != 10 || p.ResultOffset != 20 {
		t.Fatalf("paging: %d %d", p.ResultLimit, p.ResultOffset)
	}
	// Dates are IST days; a date-only end covers that whole day.
	if p.AvailableFrom.Format("2006-01-02T15:04Z07:00") != "2027-01-09T18:30Z" || p.AvailableTo.Format("2006-01-02T15:04Z07:00") != "2027-01-12T18:30Z" {
		t.Fatalf("window %v - %v", p.AvailableFrom, p.AvailableTo)
	}
	m, err := Build(Query{MinLat: ptr(18.0), MinLng: ptr(73.0), MaxLat: ptr(19.0), MaxLng: ptr(74.0), PageSize: 20})
	if err != nil || !m.MapView || m.PageSize != 100 {
		t.Fatalf("map plan %+v %v", m, err)
	}
}

func TestBuildRejects(t *testing.T) {
	bad := []Query{
		{Lat: ptr(18.5)},
		{MinLat: ptr(18.0), MaxLat: ptr(19.0)},
		{MinLat: ptr(19.0), MinLng: ptr(73.0), MaxLat: ptr(18.0), MaxLng: ptr(74.0)},
		{MinLat: ptr(5.0), MinLng: ptr(60.0), MaxLat: ptr(40.0), MaxLng: ptr(100.0)},
		{PriceMin: ptr[int64](10), PriceMax: ptr[int64](5)},
		{AvailableTo: "2027-01-01"},
		{AvailableFrom: "01/02/2027"},
		{AvailableFrom: "2027-01-01", AvailableTo: "2027-09-01"},
		{Condition: "new"},
		{Category: "not a slug"},
		{PageSize: 51},
		{Pincode: "012345"},
		{Page: 500, PageSize: 50},
	}
	for _, q := range bad {
		if _, err := Build(q); !errors.Is(err, apperror.ErrValidation) {
			t.Errorf("Build(%+v) = %v, want validation error", q, err)
		}
	}
	pl, _ := Build(Query{Sort: SortDistance})
	if err := pl.finishSort(false); !errors.Is(err, apperror.ErrValidation) {
		t.Fatal("distance sort needs a location")
	}
}

func TestSignatureIgnoresPagingAndCase(t *testing.T) {
	a := Signature(Query{Q: "JCB  3dx", Category: "crane,excavator", Page: 2})
	b := Signature(Query{Q: "jcb 3dx", Category: "Crane, excavator", PageSize: 10})
	if a != b || a == Signature(Query{Q: "jcb"}) {
		t.Fatalf("%q vs %q", a, b)
	}
	if worthRecording(Query{}, Plan{Page: 1}) || worthRecording(Query{Q: "x"}, Plan{Page: 2}) || worthRecording(Query{Q: "x"}, Plan{Page: 1, MapView: true}) {
		t.Fatal("browsing, paging and map panning are not recorded")
	}
}

func TestSearchSQLOnlyEmitsPresentFilters(t *testing.T) {
	sql, args := searchSQL(Params{PriceUnit: "day", Sort: SortNewest, ResultLimit: 20})
	for _, absent := range []string{"search_document @@", "ST_DWithin", "ST_Intersects", "up.rate_paise >=", "<%"} {
		if strings.Contains(sql, absent) {
			t.Errorf("unexpected %q in browse SQL", absent)
		}
	}
	if !strings.Contains(sql, "e.status = 'active'") || !strings.Contains(sql, "c.is_active") || strings.Contains(sql, "base_location") {
		t.Fatal("must restrict to active listings and never read base_location")
	}
	if len(args) != 3 { // unit, limit, offset
		t.Fatalf("args = %v", args)
	}
	ts := "crane:*"
	full, _ := searchSQL(Params{Tsquery: &ts, QueryText: ptr("crane"), PriceUnit: "day", Sort: SortPriceAsc, PriceMax: ptr[int64](5),
		OriginLat: ptr(18.5), OriginLng: ptr(73.8), MaxKm: ptr(10.0), ResultLimit: 20})
	for _, want := range []string{"search_document @@ to_tsquery", "ST_DWithin(e.public_location", "up.rate_paise <=", "s_unit_price ASC NULLS LAST"} {
		if !strings.Contains(full, want) {
			t.Errorf("missing %q", want)
		}
	}
	fz, _ := searchSQL(Params{Tsquery: &ts, QueryText: ptr("crane"), Fuzzy: true, PriceUnit: "day", Sort: SortRelevance, ResultLimit: 20})
	if !strings.Contains(fz, "<% e.title") || strings.Contains(fz, "search_document @@") {
		t.Fatal("fuzzy pass uses trigram similarity instead of full-text")
	}
}

func TestHistoryLabel(t *testing.T) {
	if l := label(Query{Q: " jcb ", Category: "backhoe-loader"}, &Origin{Label: "411057 · Hinjewadi, Pune"}); l != `"jcb" · backhoe loader · near 411057 · Hinjewadi, Pune` {
		t.Fatalf("label = %q", l)
	}
}
