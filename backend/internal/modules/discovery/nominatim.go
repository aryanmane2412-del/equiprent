package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/equiprent/backend/internal/apperror"
)

// reverseOSM resolves a coordinate with OpenStreetMap Nominatim. It needs no API
// key, so "use my location" still works when Google Geocoding is not configured.
func reverseOSM(ctx context.Context, lat, lng float64) (LocationDetails, error) {
	q := url.Values{"format": {"jsonv2"}, "addressdetails": {"1"}, "zoom": {"18"},
		"lat": {fmt.Sprintf("%.7f", lat)}, "lon": {fmt.Sprintf("%.7f", lng)}, "accept-language": {"en"}}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://nominatim.openstreetmap.org/reverse?"+q.Encode(), nil)
	if err != nil {
		return LocationDetails{}, err
	}
	req.Header.Set("User-Agent", "equiprent-backend/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return LocationDetails{}, apperror.ErrServiceUnavailable.WithDetail("could not reach the address service; enter the address manually")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return LocationDetails{}, apperror.ErrServiceUnavailable.WithDetail("the address service is busy; try again or enter the address manually")
	}
	var body struct {
		DisplayName string            `json:"display_name"`
		Address     map[string]string `json:"address"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || len(body.Address) == 0 {
		return LocationDetails{}, apperror.ErrNotFound
	}
	a := body.Address
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(a[k]); v != "" {
				return v
			}
		}
		return ""
	}
	out := LocationDetails{
		Pincode:          strings.ReplaceAll(first("postcode"), " ", ""),
		Area:             first("suburb", "neighbourhood", "village", "hamlet"),
		City:             first("city", "town"),
		District:         first("state_district", "county", "city_district"),
		State:            first("state", "union_territory"),
		FormattedAddress: body.DisplayName,
		AddressLine:      strings.Join(nonEmpty(first("house_number"), first("road"), first("suburb", "neighbourhood")), ", "),
		Lat:              lat, Lng: lng, Source: "openstreetmap",
	}
	if out.City == "" {
		out.City = strings.TrimSpace(strings.TrimSuffix(first("state_district", "county", "village", "municipality"), " District"))
	}
	out.District = strings.TrimSpace(strings.TrimSuffix(out.District, " District"))
	if out.AddressLine == "" {
		out.AddressLine = out.Area
	}
	for code, name := range indianStates {
		if strings.EqualFold(name, out.State) {
			out.StateCode = code
			break
		}
	}
	return out, nil
}

func nonEmpty(v ...string) []string {
	var r []string
	for _, s := range v {
		if s != "" {
			r = append(r, s)
		}
	}
	return r
}
