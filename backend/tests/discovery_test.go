package tests

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// Known places (exact machine yards). Customers must only ever see the
// ~1 km grid point, never these coordinates.
var (
	hinjewadi  = [2]float64{18.5912, 73.7389}
	hadapsar   = [2]float64{18.5089, 73.9260}
	andheri    = [2]float64{19.1136, 72.8697}
	nagpur     = [2]float64{21.1458, 79.0882}
	shivajinag = [2]float64{18.5308, 73.8475} // search point
)

// haversineKm is the great-circle distance (independent of PostGIS).
func haversineKm(a, b [2]float64) float64 {
	const r = 6371.0088
	rad := math.Pi / 180
	dLat, dLng := (b[0]-a[0])*rad, (b[1]-a[1])*rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(a[0]*rad)*math.Cos(b[0]*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Asin(math.Sqrt(h))
}

type price struct {
	unit  string
	paise int
	wet   bool
}

// publishAt creates and approves a listing at an exact point.
func (e *env) publishAt(owner, ops *user, extra map[string]any, at [2]float64, prices ...price) string {
	t := e.t
	t.Helper()
	body := listingBody(extra)
	body["base_location"] = map[string]float64{"lat": at[0], "lng": at[1]}
	id := must(t, owner.do(http.MethodPost, "/owner/equipment", body), 201, "create listing").str("id")
	if len(prices) == 0 {
		prices = []price{{"day", 950000, true}}
	}
	var plans []map[string]any
	for _, p := range prices {
		plans = append(plans, map[string]any{"unit": p.unit, "with_operator": p.wet, "rate_paise": p.paise})
	}
	must(t, owner.do(http.MethodPut, "/owner/equipment/"+id+"/pricing", map[string]any{"plans": plans}), 200, "pricing")
	owner.attachPhoto(id)
	owner.attachEquipmentDocument(id)
	must(t, owner.do(http.MethodPost, "/owner/equipment/"+id+"/submit", nil), 200, "submit")
	return id
}

func (e *env) discover(query string) resp {
	e.t.Helper()
	return must(e.t, e.call(http.MethodGet, "/discover/equipment?"+query, "", "", nil, nil), 200, "discover "+query)
}

func ids(r resp) []string {
	var out []string
	for _, d := range r.list("data") {
		out = append(out, d.(map[string]any)["id"].(string))
	}
	return out
}

func same(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	g, w := slices.Clone(got), slices.Clone(want)
	sort.Strings(g)
	sort.Strings(w)
	if !slices.Equal(g, w) {
		t.Fatalf("%s: got %v, want %v", what, got, want)
	}
}

func cityID(t *testing.T, e *env, name string) string {
	t.Helper()
	for _, c := range must(t, e.call(http.MethodGet, "/cities", "", "", nil, nil), 200, "cities").list("data") {
		if m := c.(map[string]any); m["name"] == name {
			return m["id"].(string)
		}
	}
	t.Fatalf("no city %s", name)
	return ""
}

type fleet struct {
	e                         *env
	owner, mumbai, ops        *user
	jcb, exc, crane, nag, dft string
}

// discoveryFleet publishes four machines at known places on a fresh database.
func discoveryFleet(t *testing.T) fleet {
	e := newIsolatedEnv(t)
	kyc := e.kycAdmin()
	ops := e.opsAdmin()
	owner := e.verifiedOwner(kyc, "Pune Earthmovers")
	mumbai := e.verifiedOwner(kyc, "Andheri Cranes")
	pune, mum, ngp := cityID(t, e, "Pune"), cityID(t, e, "Mumbai"), cityID(t, e, "Nagpur")

	f := fleet{e: e, owner: owner, mumbai: mumbai, ops: ops}
	f.jcb = e.publishAt(owner, ops, map[string]any{
		"title": "JCB 3DX Super backhoe loader", "category": "backhoe-loader", "manufacturer": "JCB", "model": "3DX Super",
		"condition": "excellent", "delivery_available": true, "dry_hire": false, "location_label": "Hinjewadi, Pune", "city_id": pune,
		"service_radius_km": 30,
	}, hinjewadi, price{"day", 950000, true}, price{"hour", 120000, true})
	f.exc = e.publishAt(owner, ops, map[string]any{
		"title": "Tata Hitachi EX200 excavator", "category": "excavator", "manufacturer": "Tata Hitachi", "model": "EX 200LC",
		"condition": "good", "delivery_available": false, "dry_hire": true, "location_label": "Hadapsar, Pune", "city_id": pune,
	}, hadapsar, price{"day", 1600000, true}, price{"day", 1300000, false})
	f.crane = e.publishAt(mumbai, ops, map[string]any{
		"title": "ACE NX 360 pick and carry crane", "category": "crane", "manufacturer": "ACE", "model": "NX 360",
		"condition": "good", "delivery_available": true, "dry_hire": false, "location_label": "Andheri East, Mumbai", "city_id": mum,
	}, andheri, price{"day", 1400000, true})
	f.nag = e.publishAt(owner, ops, map[string]any{
		"title": "Komatsu PC210 excavator", "category": "excavator", "manufacturer": "Komatsu", "model": "PC210",
		"condition": "fair", "delivery_available": true, "dry_hire": false, "location_label": "Hingna MIDC, Nagpur", "city_id": ngp,
	}, nagpur, price{"day", 1500000, true})
	// Not public: a draft right next to the search point.
	f.dft = must(t, owner.do(http.MethodPost, "/owner/equipment", listingBody(map[string]any{
		"title": "Draft JCB excavator", "category": "excavator", "manufacturer": "JCB", "model": "JS205", "base_location": map[string]float64{"lat": shivajinag[0], "lng": shivajinag[1]},
	})), 201, "draft").str("id")
	return f
}

func TestDiscoverySearchAndFilters(t *testing.T) {
	f := discoveryFleet(t)
	e := f.e

	all := e.discover("")
	same(t, "only approved listings", ids(all), f.jcb, f.exc, f.crane, f.nag)
	if all.str("sort") != "newest" || ids(all)[0] != f.nag {
		t.Fatalf("default order is newest first: %s", all.raw)
	}

	// Full-text: name, manufacturer, model, area, city; prefixes; typos.
	same(t, "jcb 3dx", ids(e.discover("q=jcb+3dx")), f.jcb)
	same(t, "excavator", ids(e.discover("q=excavator")), f.exc, f.nag)
	same(t, "typo", ids(e.discover("q=excavtor")), f.exc, f.nag)
	same(t, "prefix", ids(e.discover("q=komat")), f.nag)
	same(t, "area", ids(e.discover("q=hinjewadi")), f.jcb)
	same(t, "city", ids(e.discover("q=mumbai")), f.crane)
	same(t, "category words", ids(e.discover("q=pick+and+carry")), f.crane)
	same(t, "make filter", ids(e.discover("make=tata+hitachi")), f.exc)
	same(t, "model filter", ids(e.discover("model=pc210")), f.nag)
	same(t, "categories", ids(e.discover("category=crane,backhoe-loader")), f.jcb, f.crane)
	same(t, "city filter", ids(e.discover("city="+cityID(t, e, "Pune"))), f.jcb, f.exc)

	// Filters.
	same(t, "condition", ids(e.discover("condition=excellent,fair")), f.jcb, f.nag)
	same(t, "delivery", ids(e.discover("delivery=true&q=excavator")), f.nag)
	same(t, "machine only", ids(e.discover("operator=without")), f.exc)
	same(t, "with operator", ids(e.discover("operator=with&category=excavator")), f.exc, f.nag)
	same(t, "price max", ids(e.discover("price_max=1400000")), f.jcb, f.exc, f.crane) // excavator: Rs 13,000 machine only
	same(t, "price range", ids(e.discover("price_min=1000000&price_max=1500000")), f.exc, f.crane, f.nag)
	same(t, "machine-only price", ids(e.discover("operator=without&price_max=1300000")), f.exc)
	same(t, "hourly price", ids(e.discover("unit=hour&price_max=200000")), f.jcb)

	e.exec(`UPDATE equipment SET rating_avg = 4.7, rating_count = 12 WHERE id = $1`, f.exc)
	e.exec(`UPDATE equipment SET rating_avg = 4.1, rating_count = 3 WHERE id = $1`, f.crane)
	same(t, "rating", ids(e.discover("rating_min=4.5")), f.exc)
	if got := ids(e.discover("sort=rating")); got[0] != f.exc || got[1] != f.crane {
		t.Fatalf("rating sort = %v", got)
	}

	// Availability: a maintenance block and a held booking window.
	e.exec(`INSERT INTO availability_blocks (equipment_id, period, reason)
	        VALUES ($1, tstzrange('2027-03-10 00:00+05:30', '2027-03-12 00:00+05:30'), 'maintenance')`, f.jcb)
	same(t, "blocked day", ids(e.discover("available_from=2027-03-11&category=backhoe-loader,crane")), f.crane)
	same(t, "free day", ids(e.discover("available_from=2027-03-12&category=backhoe-loader,crane")), f.jcb, f.crane)
	same(t, "window before", ids(e.discover("available_from=2027-03-01&available_to=2027-03-09&category=backhoe-loader")), f.jcb)
	// A date-only end includes that whole day, which is blocked.
	same(t, "window into block", ids(e.discover("available_from=2027-03-01&available_to=2027-03-10&category=backhoe-loader")))

	// Sorting by price.
	if got := ids(e.discover("sort=price_asc")); !slices.Equal(got, []string{f.jcb, f.exc, f.crane, f.nag}) {
		t.Fatalf("price_asc = %v", got)
	}
	if got := ids(e.discover("sort=price_desc")); !slices.Equal(got, []string{f.nag, f.crane, f.exc, f.jcb}) {
		t.Fatalf("price_desc = %v", got)
	}

	// Verified owners only.
	e.exec(`UPDATE organizations SET kyc_status = 'expired', kyc_verified_at = NULL WHERE id = (SELECT owner_org_id FROM equipment WHERE id = $1)`, f.crane)
	same(t, "verified only", ids(e.discover("verified_only=true")), f.jcb, f.exc, f.nag)

	// Pausing hides a machine immediately.
	must(t, f.owner.do(http.MethodPost, "/owner/equipment/"+f.nag+"/pause", nil), 200, "pause")
	same(t, "paused hidden", ids(e.discover("q=excavator")), f.exc)
	must(t, f.owner.do(http.MethodPost, "/owner/equipment/"+f.nag+"/resume", nil), 200, "resume")

	// Pagination.
	p1, p2 := e.discover("page_size=3"), e.discover("page_size=3&page=2")
	if p1.num("total") != 4 || p1.num("pages") != 2 || len(ids(p1)) != 3 || len(ids(p2)) != 1 || slices.Contains(ids(p1), ids(p2)[0]) {
		t.Fatalf("pages: %s / %s", p1.raw, p2.raw)
	}
	if p3 := e.discover("page_size=3&page=3"); len(ids(p3)) != 0 {
		t.Fatalf("past the end: %s", p3.raw)
	}

	// Validation.
	for _, bad := range []string{"sort=distance", "lat=18.5", "min_lat=18&max_lat=19", "price_min=5&price_max=1",
		"available_to=2027-01-01", "available_from=2027-01-02&available_to=2027-01-01", "condition=shiny", "page_size=51",
		"pincode=012345", "category=Bad_Slug"} {
		if r := e.call(http.MethodGet, "/discover/equipment?"+bad, "", "", nil, nil); r.code != 400 {
			t.Fatalf("%s: %d %s", bad, r.code, r.raw)
		}
	}
}

func TestDiscoveryGeospatialAccuracy(t *testing.T) {
	f := discoveryFleet(t)
	e := f.e
	exact := map[string][2]float64{f.jcb: hinjewadi, f.exc: hadapsar, f.crane: andheri, f.nag: nagpur}

	// Distances from a point, nearest first, within the 1 km privacy error.
	r := e.discover(fmt.Sprintf("lat=%v&lng=%v&radius_km=500", shivajinag[0], shivajinag[1]))
	if r.str("sort") != "distance" || r.str("origin.source") != "point" {
		t.Fatalf("origin/sort: %s", r.raw)
	}
	got := ids(r)
	if !slices.Equal(got, []string{f.exc, f.jcb, f.crane}) { // Nagpur is ~600 km away
		t.Fatalf("order = %v", got)
	}
	prev := -1.0
	for _, d := range r.list("data") {
		m := d.(map[string]any)
		dist := m["distance_km"].(float64)
		want := haversineKm(shivajinag, exact[m["id"].(string)])
		// Grid error (< 0.8 km) plus spheroid vs sphere (~0.5%).
		if math.Abs(dist-want) > 1.0+0.005*want {
			t.Fatalf("%s: distance %.2f km, true %.2f km", m["title"], dist, want)
		}
		if dist < prev {
			t.Fatal("not sorted by distance")
		}
		prev = dist
		// The public point is the centre of the grid cell, never the yard.
		loc := m["approx_location"].(map[string]any)
		pt := [2]float64{loc["lat"].(float64), loc["lng"].(float64)}
		if off := haversineKm(pt, exact[m["id"].(string)]); off < 0.05 || off > 0.8 {
			t.Fatalf("%s: public point %.3f km from the yard", m["title"], off)
		}
	}
	// Mumbai crane: 120 km by road-free distance, check against an independent value.
	if want := haversineKm(shivajinag, andheri); want < 115 || want > 125 {
		t.Fatalf("reference distance %.1f", want)
	}

	// Radius filter.
	same(t, "10 km", ids(e.discover(fmt.Sprintf("lat=%v&lng=%v&radius_km=10", shivajinag[0], shivajinag[1]))), f.exc)
	same(t, "15 km", ids(e.discover(fmt.Sprintf("lat=%v&lng=%v&radius_km=15", shivajinag[0], shivajinag[1]))), f.exc, f.jcb)
	same(t, "default 50 km", ids(e.discover(fmt.Sprintf("lat=%v&lng=%v", shivajinag[0], shivajinag[1]))), f.exc, f.jcb)

	// PIN code: Hinjewadi 411057 is ~0 km from the JCB; default 25 km radius.
	pin := e.discover("pincode=411057")
	if pin.str("origin.source") != "pincode" || !strings.Contains(pin.str("origin.label"), "Hinjewadi") {
		t.Fatalf("pincode origin: %s", pin.raw)
	}
	if ids(pin)[0] != f.jcb || pin.list("data")[0].(map[string]any)["distance_km"].(float64) > 1.0 {
		t.Fatalf("pincode nearest: %s", pin.raw)
	}
	same(t, "pincode radius", ids(pin), f.jcb, f.exc) // Hadapsar ~22 km
	if r := e.call(http.MethodGet, "/discover/equipment?pincode=999999", "", "", nil, nil); r.code != 400 {
		t.Fatalf("unknown pincode without Google: %d %s", r.code, r.raw)
	}
	// Delivery reach: JCB delivers 30 km; Hadapsar excavator does not deliver.
	for _, d := range pin.list("data") {
		m := d.(map[string]any)
		if want := m["id"] == f.jcb; m["delivers_to_you"] != want {
			t.Fatalf("delivers_to_you for %s = %v", m["title"], m["delivers_to_you"])
		}
	}

	// City: centre only orders; the city filter selects.
	city := e.discover("city=" + cityID(t, e, "Mumbai"))
	same(t, "city", ids(city), f.crane)
	if city.str("origin.source") != "city" || city.str("sort") != "distance" {
		t.Fatalf("city origin: %s", city.raw)
	}

	// Map bounds: Mumbai area only, then all of Maharashtra.
	same(t, "mumbai box", ids(e.discover("min_lat=18.9&min_lng=72.7&max_lat=19.3&max_lng=73.1")), f.crane)
	same(t, "maharashtra box", ids(e.discover("min_lat=15.6&min_lng=72.6&max_lat=22.1&max_lng=80.9")), f.jcb, f.exc, f.crane, f.nag)
	box := e.discover("min_lat=18.4&min_lng=73.6&max_lat=18.7&max_lng=74.0&page_size=10")
	if box.num("page_size") < 100 {
		t.Fatalf("map pages are large: %s", box.raw)
	}
	same(t, "pune box", ids(box), f.jcb, f.exc)
}

func TestDiscoveryNeverRevealsExactLocation(t *testing.T) {
	f := discoveryFleet(t)
	e := f.e
	exactStrings := []string{"18.5912", "73.7389", "18.5089", "73.926", "19.1136", "72.8697"}
	check := func(what, raw string) {
		t.Helper()
		for _, s := range exactStrings {
			if strings.Contains(raw, s) {
				t.Fatalf("%s leaks exact coordinate %s: %s", what, s, raw)
			}
		}
	}
	check("search", e.discover(fmt.Sprintf("lat=%v&lng=%v&radius_km=500", shivajinag[0], shivajinag[1])).raw)
	check("map", e.discover("min_lat=15.6&min_lng=72.6&max_lat=22.1&max_lng=80.9").raw)

	customer := e.registerAndVerify("customer", "")
	for _, who := range []*user{nil, customer} {
		var r resp
		if who == nil {
			r = e.call(http.MethodGet, "/equipment/"+f.jcb, "", "", nil, nil)
		} else {
			r = customer.do(http.MethodGet, "/equipment/"+f.jcb, nil)
		}
		must(t, r, 200, "detail")
		check("detail", r.raw)
		if r.num("base_location.lat") != 18.595 || r.num("base_location.lng") != 73.735 {
			t.Fatalf("detail shows %v,%v; want the grid centre 18.595,73.735", r.num("base_location.lat"), r.num("base_location.lng"))
		}
	}
	check("compare", must(t, e.call(http.MethodGet, "/discover/compare?ids="+f.jcb+","+f.exc, "", "", nil, nil), 200, "compare").raw)

	// The owner still sees (and edits) the exact yard.
	own := must(t, f.owner.do(http.MethodGet, "/equipment/"+f.jcb, nil), 200, "owner detail")
	if own.num("base_location.lat") != hinjewadi[0] || own.num("base_location.lng") != hinjewadi[1] {
		t.Fatalf("owner sees %s", own.raw)
	}
	// Even the drafts' exact position stays out of search.
	if strings.Contains(e.discover("q=draft").raw, f.dft) {
		t.Fatal("draft in search")
	}
}

func TestDiscoverySuggestionsAndLocations(t *testing.T) {
	f := discoveryFleet(t)
	e := f.e
	sug := func(q string) string {
		return must(t, e.call(http.MethodGet, "/discover/suggest?q="+url.QueryEscape(q), "", "", nil, nil), 200, "suggest").raw
	}
	if s := sug("jc"); !strings.Contains(s, `"kind":"make","label":"JCB"`) {
		t.Fatalf("make suggestion: %s", s)
	}
	if s := sug("excav"); !strings.Contains(s, `"kind":"category"`) || !strings.Contains(s, "Excavator") {
		t.Fatalf("category suggestion: %s", s)
	}
	if s := sug("pc21"); !strings.Contains(s, `"kind":"model"`) || !strings.Contains(s, "Komatsu PC210") {
		t.Fatalf("model suggestion: %s", s)
	}
	if s := sug("41105"); !strings.Contains(s, `"kind":"pincode"`) || !strings.Contains(s, "411057") {
		t.Fatalf("pincode suggestion: %s", s)
	}
	if s := sug("pun"); !strings.Contains(s, `"kind":"city","label":"Pune"`) {
		t.Fatalf("city suggestion: %s", s)
	}
	if s := sug("x"); s != `{"data":[]}` {
		t.Fatalf("one letter: %s", s)
	}
	// Makes with no live machine are not suggested (the draft's catalogue entries).
	locs := must(t, e.call(http.MethodGet, "/discover/locations?q=hinj", "", "", nil, nil), 200, "locations")
	if !strings.Contains(locs.raw, `"value":"411057"`) {
		t.Fatalf("locations: %s", locs.raw)
	}
	if n := len(must(t, e.call(http.MethodGet, "/discover/locations", "", "", nil, nil), 200, "cities").list("data")); n < 10 {
		t.Fatalf("cities: %d", n)
	}
	if must(t, e.call(http.MethodGet, "/discover/maps", "", "", nil, nil), 200, "maps").str("provider") != "builtin" {
		t.Fatal("no Google key configured")
	}
}

func TestDiscoverySavedHistoryAndCompare(t *testing.T) {
	f := discoveryFleet(t)
	e := f.e
	c := e.registerAndVerify("customer", "")

	// Saved equipment.
	must(t, c.do(http.MethodPut, "/me/saved-equipment/"+f.jcb, nil), 204, "save")
	must(t, c.do(http.MethodPut, "/me/saved-equipment/"+f.jcb, nil), 204, "save again")
	must(t, c.do(http.MethodPut, "/me/saved-equipment/"+f.crane, nil), 204, "save crane")
	must(t, c.do(http.MethodPut, "/me/saved-equipment/"+f.dft, nil), 404, "drafts cannot be saved")
	saved := must(t, c.do(http.MethodGet, "/me/saved-equipment", nil), 200, "saved")
	same(t, "saved", ids(saved), f.jcb, f.crane)
	same(t, "saved ids", func() []string {
		var out []string
		for _, v := range must(t, c.do(http.MethodGet, "/me/saved-equipment/ids", nil), 200, "ids").list("data") {
			out = append(out, v.(string))
		}
		return out
	}(), f.jcb, f.crane)
	// A paused machine stays saved but is marked unavailable.
	must(t, f.mumbai.do(http.MethodPost, "/owner/equipment/"+f.crane+"/pause", nil), 200, "pause")
	for _, d := range must(t, c.do(http.MethodGet, "/me/saved-equipment", nil), 200, "saved").list("data") {
		m := d.(map[string]any)
		if (m["id"] == f.crane) == (m["available"] == true) {
			t.Fatalf("availability flag: %v", m)
		}
	}
	must(t, c.do(http.MethodDelete, "/me/saved-equipment/"+f.crane, nil), 204, "unsave")
	same(t, "after unsave", ids(must(t, c.do(http.MethodGet, "/me/saved-equipment", nil), 200, "saved")), f.jcb)
	other := e.registerAndVerify("customer", "")
	if n := len(must(t, other.do(http.MethodGet, "/me/saved-equipment", nil), 200, "other").list("data")); n != 0 {
		t.Fatal("saved lists are per user")
	}
	if r := e.call(http.MethodGet, "/me/saved-equipment", "", "", nil, nil); r.code != 401 {
		t.Fatalf("anonymous saved: %d", r.code)
	}

	// Search history: signed-in searches are recorded once per distinct search.
	must(t, c.do(http.MethodGet, "/discover/equipment?q=excavator", nil), 200, "search")
	must(t, c.do(http.MethodGet, "/discover/equipment?q=Excavator&page=1", nil), 200, "same search")
	must(t, c.do(http.MethodGet, "/discover/equipment?pincode=411057&category=backhoe-loader", nil), 200, "search 2")
	must(t, c.do(http.MethodGet, "/discover/equipment?q=excavator&page=2", nil), 200, "paging is not a new search")
	must(t, c.do(http.MethodGet, "/discover/equipment?min_lat=18&min_lng=73&max_lat=19&max_lng=74", nil), 200, "map panning")
	must(t, c.do(http.MethodGet, "/discover/equipment", nil), 200, "browsing")
	e.discover("q=crane") // anonymous
	h := must(t, c.do(http.MethodGet, "/me/search-history", nil), 200, "history")
	if n := len(h.list("data")); n != 2 {
		t.Fatalf("history: %s", h.raw)
	}
	first := h.list("data")[0].(map[string]any)
	if !strings.Contains(first["label"].(string), "near 411057 · Hinjewadi") || first["params"].(map[string]any)["pincode"] != "411057" {
		t.Fatalf("history entry: %v", first)
	}
	if h.list("data")[1].(map[string]any)["result_count"].(float64) != 2 {
		t.Fatalf("result count: %v", h.list("data")[1])
	}
	must(t, c.do(http.MethodDelete, "/me/search-history/"+first["id"].(string), nil), 204, "forget")
	must(t, other.do(http.MethodDelete, "/me/search-history/"+h.list("data")[1].(map[string]any)["id"].(string), nil), 404, "not yours")
	must(t, c.do(http.MethodDelete, "/me/search-history", nil), 204, "clear")
	if n := len(must(t, c.do(http.MethodGet, "/me/search-history", nil), 200, "history").list("data")); n != 0 {
		t.Fatal("history not cleared")
	}
	for i := range 35 {
		must(t, c.do(http.MethodGet, fmt.Sprintf("/discover/equipment?q=machine%d", i), nil), 200, "search")
	}
	if n := len(must(t, c.do(http.MethodGet, "/me/search-history", nil), 200, "history").list("data")); n != 30 {
		t.Fatalf("history keeps 30, got %d", n)
	}

	// Comparison.
	cmp := must(t, e.call(http.MethodGet, "/discover/compare?ids="+f.jcb+","+f.exc+","+f.nag, "", "", nil, nil), 200, "compare")
	if len(cmp.list("data")) != 3 || cmp.str("data.0.id") != f.jcb || len(cmp.list("data.1.pricing")) != 2 {
		t.Fatalf("compare: %s", cmp.raw)
	}
	for _, bad := range []string{f.jcb, f.jcb + "," + f.jcb, "x,y", strings.Join([]string{f.jcb, f.exc, f.crane, f.nag, f.dft}, ",")} {
		if r := e.call(http.MethodGet, "/discover/compare?ids="+bad, "", "", nil, nil); r.code != 400 {
			t.Fatalf("compare %q: %d", bad, r.code)
		}
	}
	if r := e.call(http.MethodGet, "/discover/compare?ids="+f.jcb+","+f.dft, "", "", nil, nil); r.code != 404 {
		t.Fatalf("compare draft: %d", r.code)
	}
}

// TestDiscoveryPerformance loads 20,000 active machines across India and
// checks typical searches stay fast (indexes in use).
func TestDiscoveryPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("performance test")
	}
	e := newIsolatedEnv(t)
	kyc := e.kycAdmin()
	ops := e.opsAdmin()
	owner := e.verifiedOwner(kyc, "Bulk Fleet")
	seed := e.publishAt(owner, ops, map[string]any{"title": "Seed machine"}, hinjewadi)
	start := time.Now()
	e.exec(`
	  WITH src AS (SELECT * FROM equipment WHERE id = $1),
	  models AS (SELECT m.id, row_number() OVER () AS n FROM equipment_models m),
	  nm AS (SELECT count(*) AS c FROM models)
	  INSERT INTO equipment (owner_org_id, model_id, title, description, base_location, city_id, service_radius_km,
	                         wet_hire, dry_hire, status, condition, delivery_available, location_label, rating_avg, rating_count, created_at)
	  SELECT src.owner_org_id, models.id,
	         (ARRAY['JCB backhoe','Excavator 20 t','Hydra crane','Tipper truck','Road roller','Forklift 3 t','Transit mixer','Tractor'])[1 + g % 8] || ' unit ' || g,
	         'Bulk performance listing ' || g,
	         ST_SetSRID(ST_MakePoint(72.6 + random() * 8.0, 12.8 + random() * 15.5), 4326)::geography,
	         src.city_id, 50, true, g % 3 = 0, 'active',
	         (ARRAY['excellent','good','fair','needs_repair']::equipment_condition[])[1 + g % 4], g % 2 = 0,
	         'Area ' || (g % 400), round((3 + random() * 2)::numeric, 2), g % 50, now() - (g || ' minutes')::interval
	  FROM generate_series(1, 20000) g CROSS JOIN src CROSS JOIN nm
	  JOIN models ON models.n = 1 + g % nm.c`, seed)
	e.exec(`INSERT INTO pricing_plans (equipment_id, unit, with_operator, rate_paise)
	        SELECT id, 'day', true, 400000 + (abs(hashtext(id::text)) % 4000000) FROM equipment WHERE description LIKE 'Bulk performance%'`)
	e.exec(`ANALYZE equipment`)
	e.exec(`ANALYZE pricing_plans`)
	t.Logf("loaded 20,000 listings in %s", time.Since(start).Round(time.Millisecond))

	queries := []string{
		"",
		"q=excavator",
		"q=jcb+backhoe&sort=price_asc",
		"q=excavtor",
		"lat=18.53&lng=73.85&radius_km=25",
		"pincode=400069&category=crane,excavator",
		"min_lat=18.4&min_lng=73.6&max_lat=18.7&max_lng=74.0",
		"price_min=500000&price_max=1500000&condition=excellent,good&delivery=true&sort=price_desc",
		"rating_min=4.5&sort=rating&page=5",
		"lat=19.07&lng=72.87&radius_km=50&operator=without&available_from=2027-02-01&available_to=2027-02-05",
	}
	var worst time.Duration
	for _, q := range queries {
		e.discover(q) // warm
		var times []time.Duration
		for range 5 {
			s := time.Now()
			r := e.discover(q)
			times = append(times, time.Since(s))
			if r.num("total") == 0 && q != "q=excavtor" {
				t.Fatalf("%q found nothing", q)
			}
		}
		slices.Sort(times)
		median := times[len(times)/2]
		t.Logf("%-110q median %s", q, median.Round(100*time.Microsecond))
		worst = max(worst, median)
	}
	// Generous bound for laptops and CI; measured medians are 5-90 ms here.
	if worst > 250*time.Millisecond {
		t.Fatalf("slowest median %s exceeds 250 ms", worst)
	}

	// The full-text and map searches use their indexes.
	plan := func(sql string) string {
		var lines []string
		rows, err := e.pool.Query(t.Context(), "EXPLAIN "+sql)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var l string
			_ = rows.Scan(&l)
			lines = append(lines, l)
		}
		return strings.Join(lines, "\n")
	}
	if p := plan(`SELECT id FROM equipment WHERE status = 'active' AND deleted_at IS NULL AND search_document @@ to_tsquery('simple', '7771:*')`); !strings.Contains(p, "equipment_search_document_gin") {
		t.Fatalf("FTS plan:\n%s", p)
	}
	if p := plan(`SELECT id FROM equipment WHERE status = 'active' AND deleted_at IS NULL
	              AND ST_DWithin(public_location, ST_SetSRID(ST_MakePoint(73.85, 18.53), 4326)::geography, 10000)`); !strings.Contains(p, "equipment_public_location_gix") {
		t.Fatalf("geo plan:\n%s", p)
	}
}
