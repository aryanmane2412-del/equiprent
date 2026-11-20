package tracking

import (
	"errors"
	"strings"
	"testing"
)

func TestGenericTrackerPayload(t *testing.T) {
	pts, err := GenericJSON{}.Parse([]byte(`{"device_id":"IMEI-1","positions":[
		{"lat":18.52,"lng":73.85,"recorded_at":"2026-11-15T08:00:00Z","speed_mps":4.5},
		{"lat":18.53,"lng":73.84,"recorded_at":"2026-11-15T08:01:00Z","accuracy_m":6}]}`))
	if err != nil || len(pts) != 2 || pts[0].ExternalID != "IMEI-1" || *pts[0].SpeedMps != 4.5 || *pts[1].AccuracyM != 6 {
		t.Fatalf("parse = %+v %v", pts, err)
	}
	for name, body := range map[string]string{
		"not json":     `{`,
		"no device":    `{"positions":[{"lat":1,"lng":1,"recorded_at":"2026-11-15T08:00:00Z"}]}`,
		"no points":    `{"device_id":"x","positions":[]}`,
		"null island":  `{"device_id":"x","positions":[{"lat":0,"lng":0,"recorded_at":"2026-11-15T08:00:00Z"}]}`,
		"out of range": `{"device_id":"x","positions":[{"lat":95,"lng":1,"recorded_at":"2026-11-15T08:00:00Z"}]}`,
		"no time":      `{"device_id":"x","positions":[{"lat":18,"lng":73}]}`,
		"too many":     `{"device_id":"x","positions":[` + strings.Repeat(`{"lat":18,"lng":73,"recorded_at":"2026-11-15T08:00:00Z"},`, 500) + `{"lat":18,"lng":73,"recorded_at":"2026-11-15T08:00:00Z"}]}`,
	} {
		if _, err := (GenericJSON{}).Parse([]byte(body)); !errors.Is(err, ErrBadPayload) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestProvidersAndPolicy(t *testing.T) {
	if _, ok := Providers(false)["simulated"]; ok {
		t.Fatal("simulated tracker only when simulation is allowed")
	}
	if p := Providers(true)["simulated"]; p == nil || p.Name() != "simulated" {
		t.Fatal("simulated tracker missing")
	}
	pol := policy("v1")
	text := strings.Join(pol.Points, " ")
	for _, want := range []string{"phone", "stops automatically", "logged", "90 days", "not"} {
		if !strings.Contains(text, want) {
			t.Errorf("notice should mention %q", want)
		}
	}
	if sourceNote("app") == sourceNote("telematics") || !strings.Contains(sourceNote("app"), "phone") {
		t.Fatal("source notes must tell phones and trackers apart")
	}
	for status, want := range map[string]string{"dispatched": "mobilisation", "active": "on_site", "return_initiated": "demobilisation"} {
		if got := tripKindFor(status); got != want {
			t.Errorf("tripKindFor(%s) = %s", status, got)
		}
	}
}
