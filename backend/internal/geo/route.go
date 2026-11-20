// Package geo estimates road routes and arrival times. Google Directions is
// used when a server key is configured; otherwise (or when Google fails)
// a straight-line estimate is returned and labelled as such, so the app
// never claims more precision than it has.
package geo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Point is a WGS84 coordinate.
type Point struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Estimate is a route from a position to a destination.
type Estimate struct {
	DistanceM float64 `json:"distance_m"`
	DurationS float64 `json:"duration_s"`
	Method    string  `json:"method" doc:"google_directions (road route, traffic-aware) or straight_line (rough, no road data)"`
	Polyline  []Point `json:"polyline,omitempty" doc:"Road route to draw (google_directions only)"`
}

// Router estimates a route.
type Router interface {
	Route(ctx context.Context, from, to Point) (Estimate, error)
}

// DistanceM is the great-circle distance in metres (haversine).
func DistanceM(a, b Point) float64 {
	const r = 6371008.8
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat, dLng := rad(b.Lat-a.Lat), rad(b.Lng-a.Lng)
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(rad(a.Lat))*math.Cos(rad(b.Lat))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Asin(math.Min(1, math.Sqrt(h)))
}

// StraightLine estimates heavy-machinery transport on a low-bed trailer:
// roads are ~1.3x the straight line, at ~30 km/h average.
type StraightLine struct {
	RoadFactor float64
	SpeedKmh   float64
}

func (s StraightLine) Route(_ context.Context, from, to Point) (Estimate, error) {
	f, v := s.RoadFactor, s.SpeedKmh
	if f <= 0 {
		f = 1.3
	}
	if v <= 0 {
		v = 30
	}
	d := DistanceM(from, to) * f
	return Estimate{DistanceM: math.Round(d), DurationS: math.Round(d / (v * 1000 / 3600)), Method: "straight_line"}, nil
}

// Google calls the Directions API (driving, departure now) with a server
// key, caching results briefly per origin cell and destination.
type Google struct {
	Key      string
	BaseURL  string
	HTTP     *http.Client
	Fallback Router
	TTL      time.Duration

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	at time.Time
	e  Estimate
}

// NewGoogle builds the router with a straight-line fallback.
func NewGoogle(key string) *Google {
	return &Google{Key: key, BaseURL: "https://maps.googleapis.com/maps/api/directions/json", HTTP: &http.Client{Timeout: 4 * time.Second},
		Fallback: StraightLine{}, TTL: 2 * time.Minute}
}

// ErrNoRoute is returned when Google finds no road route.
var ErrNoRoute = errors.New("no route")

func (g *Google) Route(ctx context.Context, from, to Point) (Estimate, error) {
	// ~100 m origin cells keep the cache useful while a machine moves.
	k := fmt.Sprintf("%.3f,%.3f>%.5f,%.5f", from.Lat, from.Lng, to.Lat, to.Lng)
	g.mu.Lock()
	if c, ok := g.cache[k]; ok && time.Since(c.at) < g.TTL {
		g.mu.Unlock()
		return c.e, nil
	}
	g.mu.Unlock()
	e, err := g.fetch(ctx, from, to)
	if err != nil {
		if g.Fallback != nil {
			return g.Fallback.Route(ctx, from, to)
		}
		return Estimate{}, err
	}
	g.mu.Lock()
	if g.cache == nil {
		g.cache = map[string]cached{}
	}
	g.cache[k] = cached{at: time.Now(), e: e}
	g.mu.Unlock()
	return e, nil
}

func (g *Google) fetch(ctx context.Context, from, to Point) (Estimate, error) {
	q := url.Values{
		"origin":         {fmt.Sprintf("%.6f,%.6f", from.Lat, from.Lng)},
		"destination":    {fmt.Sprintf("%.6f,%.6f", to.Lat, to.Lng)},
		"mode":           {"driving"},
		"departure_time": {"now"},
		"key":            {g.Key},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.BaseURL+"?"+q.Encode(), nil)
	if err != nil {
		return Estimate{}, err
	}
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return Estimate{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode != http.StatusOK {
		return Estimate{}, fmt.Errorf("directions: status %d", resp.StatusCode)
	}
	var out struct {
		Status string `json:"status"`
		Routes []struct {
			OverviewPolyline struct {
				Points string `json:"points"`
			} `json:"overview_polyline"`
			Legs []struct {
				Distance struct {
					Value float64 `json:"value"`
				} `json:"distance"`
				Duration struct {
					Value float64 `json:"value"`
				} `json:"duration"`
				DurationInTraffic *struct {
					Value float64 `json:"value"`
				} `json:"duration_in_traffic"`
			} `json:"legs"`
		} `json:"routes"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Estimate{}, err
	}
	if out.Status != "OK" || len(out.Routes) == 0 || len(out.Routes[0].Legs) == 0 {
		return Estimate{}, fmt.Errorf("%w: %s", ErrNoRoute, out.Status)
	}
	leg := out.Routes[0].Legs[0]
	dur := leg.Duration.Value
	if leg.DurationInTraffic != nil && leg.DurationInTraffic.Value > 0 {
		dur = leg.DurationInTraffic.Value
	}
	// Heavy transport is slower than cars: add a third.
	return Estimate{DistanceM: leg.Distance.Value, DurationS: math.Round(dur * 4 / 3), Method: "google_directions",
		Polyline: DecodePolyline(out.Routes[0].OverviewPolyline.Points)}, nil
}

// DecodePolyline decodes Google's encoded polyline format.
func DecodePolyline(s string) []Point {
	var out []Point
	var lat, lng int
	for i := 0; i < len(s); {
		for _, v := range []*int{&lat, &lng} {
			shift, result := 0, 0
			for i < len(s) {
				b := int(s[i]) - 63
				i++
				result |= (b & 0x1f) << shift
				shift += 5
				if b < 0x20 {
					break
				}
			}
			if result&1 != 0 {
				*v += ^(result >> 1)
			} else {
				*v += result >> 1
			}
		}
		out = append(out, Point{Lat: float64(lat) / 1e5, Lng: float64(lng) / 1e5})
	}
	return out
}

// Redact removes the key from an error message (keys never reach logs).
func Redact(msg, key string) string {
	if key == "" {
		return msg
	}
	return strings.ReplaceAll(msg, key, "***")
}
