package tracking

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Hardware trackers and telematics units report the MACHINE's position
// (unlike a phone, which reports where the phone is). Each vendor posts its
// own payload format; a TelematicsProvider adapts it to DevicePosition.
// Devices authenticate with a per-device bearer token issued at
// registration (only its SHA-256 is stored).
//
// To add a vendor: implement TelematicsProvider, register it in Providers,
// and point the vendor's webhook at POST /api/v1/telematics/{name}/positions.

// DevicePosition is one fix from a device.
type DevicePosition struct {
	ExternalID string    // the device's id at the vendor (checked against the token's device)
	Lat, Lng   float64   //
	RecordedAt time.Time //
	AccuracyM  *float32
	SpeedMps   *float32
	Heading    *int16
}

// TelematicsProvider parses a vendor's position payload.
type TelematicsProvider interface {
	Name() string
	Parse(body []byte) ([]DevicePosition, error)
}

// ErrBadPayload rejects payloads a provider cannot read.
var ErrBadPayload = errors.New("unreadable telematics payload")

// GenericJSON is EQUIPRENT's own format, for trackers that can be
// configured to post JSON:
//
//	{"device_id": "IMEI-123", "positions": [{"lat": .., "lng": .., "recorded_at": "RFC3339",
//	  "accuracy_m": .., "speed_mps": .., "heading": ..}]}
type GenericJSON struct{}

func (GenericJSON) Name() string { return "generic" }

func (GenericJSON) Parse(body []byte) ([]DevicePosition, error) {
	var in struct {
		DeviceID  string `json:"device_id"`
		Positions []struct {
			Lat        float64   `json:"lat"`
			Lng        float64   `json:"lng"`
			RecordedAt time.Time `json:"recorded_at"`
			AccuracyM  *float32  `json:"accuracy_m"`
			SpeedMps   *float32  `json:"speed_mps"`
			Heading    *int16    `json:"heading"`
		} `json:"positions"`
	}
	if err := json.Unmarshal(body, &in); err != nil || in.DeviceID == "" || len(in.Positions) == 0 || len(in.Positions) > 500 {
		return nil, ErrBadPayload
	}
	out := make([]DevicePosition, 0, len(in.Positions))
	for _, p := range in.Positions {
		if err := validFix(p.Lat, p.Lng, p.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, DevicePosition{ExternalID: in.DeviceID, Lat: p.Lat, Lng: p.Lng, RecordedAt: p.RecordedAt,
			AccuracyM: p.AccuracyM, SpeedMps: p.SpeedMps, Heading: p.Heading})
	}
	return out, nil
}

// SimulatedTracker accepts the generic format for development and tests,
// recorded with source "simulated" so it is never mistaken for a real fix.
type SimulatedTracker struct{ GenericJSON }

func (SimulatedTracker) Name() string { return "simulated" }

func validFix(lat, lng float64, at time.Time) error {
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 || (lat == 0 && lng == 0) || at.IsZero() {
		return fmt.Errorf("%w: position out of range", ErrBadPayload)
	}
	return nil
}

// Providers are the registered vendors by name.
func Providers(simulated bool) map[string]TelematicsProvider {
	m := map[string]TelematicsProvider{"generic": GenericJSON{}}
	if simulated {
		m["simulated"] = SimulatedTracker{}
	}
	return m
}
