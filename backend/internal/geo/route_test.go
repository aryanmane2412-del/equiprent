package geo

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

var (
	yard = Point{Lat: 18.5204, Lng: 73.8567} // Pune
	site = Point{Lat: 18.5913, Lng: 73.7389} // Hinjewadi
)

func TestDistanceAndStraightLine(t *testing.T) {
	d := DistanceM(yard, site)
	if d < 14_500 || d > 15_000 { // ~14.7 km
		t.Fatalf("distance = %.0f", d)
	}
	e, _ := StraightLine{}.Route(context.Background(), yard, site)
	if e.Method != "straight_line" || math.Abs(e.DistanceM-d*1.3) > 1 || math.Abs(e.DurationS-e.DistanceM/(30_000.0/3600)) > 1 || e.Polyline != nil {
		t.Fatalf("estimate = %+v", e)
	}
}

func TestDecodePolyline(t *testing.T) {
	// Google's documented example.
	pts := DecodePolyline("_p~iF~ps|U_ulLnnqC_mqNvxq`@")
	want := []Point{{38.5, -120.2}, {40.7, -120.95}, {43.252, -126.453}}
	if len(pts) != len(want) {
		t.Fatalf("points = %v", pts)
	}
	for i := range want {
		if math.Abs(pts[i].Lat-want[i].Lat) > 1e-6 || math.Abs(pts[i].Lng-want[i].Lng) > 1e-6 {
			t.Fatalf("point %d = %v, want %v", i, pts[i], want[i])
		}
	}
}

func TestGoogleDirections(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		q := r.URL.Query()
		if q.Get("key") != "server-key" || q.Get("mode") != "driving" || !strings.HasPrefix(q.Get("origin"), "18.52") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"status":"OK","routes":[{"overview_polyline":{"points":"_p~iF~ps|U_ulLnnqC"},
			"legs":[{"distance":{"value":18200},"duration":{"value":1800},"duration_in_traffic":{"value":2400}}]}]}`))
	}))
	defer srv.Close()
	g := NewGoogle("server-key")
	g.BaseURL = srv.URL
	e, err := g.Route(context.Background(), yard, site)
	if err != nil || e.Method != "google_directions" || e.DistanceM != 18200 || e.DurationS != 3200 || len(e.Polyline) != 2 {
		t.Fatalf("google = %+v %v", e, err)
	}
	// Cached for the same origin cell.
	if _, err := g.Route(context.Background(), Point{Lat: yard.Lat + 0.0001, Lng: yard.Lng}, site); err != nil || calls.Load() != 1 {
		t.Fatalf("cache: calls = %d %v", calls.Load(), err)
	}
}

func TestGoogleFallsBackGracefully(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"denied": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"status":"REQUEST_DENIED","routes":[]}`))
		},
		"no route": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"status":"ZERO_RESULTS","routes":[]}`))
		},
		"outage":  func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) },
		"garbage": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<html>`)) },
	} {
		srv := httptest.NewServer(handler)
		g := NewGoogle("k")
		g.BaseURL = srv.URL
		e, err := g.Route(context.Background(), yard, site)
		srv.Close()
		if err != nil || e.Method != "straight_line" {
			t.Errorf("%s: %+v %v", name, e, err)
		}
	}
	if got := Redact("GET https://x?key=secret123: timeout", "secret123"); strings.Contains(got, "secret123") {
		t.Fatalf("redact = %s", got)
	}
}
