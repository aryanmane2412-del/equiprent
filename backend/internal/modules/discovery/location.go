package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/equiprent/backend/internal/apperror"
)

var indianPIN = regexp.MustCompile(`^[1-9][0-9]{5}$`)

// LocationDetails is a postal address or actual device point resolved by the backend.
type LocationDetails struct {
	Pincode          string  `json:"pincode,omitempty"`
	Area             string  `json:"area,omitempty"`
	AddressLine      string  `json:"address_line,omitempty"`
	FormattedAddress string  `json:"formatted_address,omitempty"`
	City             string  `json:"city,omitempty"`
	District         string  `json:"district,omitempty"`
	State            string  `json:"state,omitempty"`
	StateCode        string  `json:"state_code,omitempty"`
	Lat              float64 `json:"lat"`
	Lng              float64 `json:"lng"`
	Source           string  `json:"source" doc:"google or local_reference"`
}

type detailedGeocoder interface {
	PincodeDetails(context.Context, string) (LocationDetails, error)
	Reverse(context.Context, float64, float64) (LocationDetails, error)
}

var indianStates = map[string]string{
	"01": "Jammu and Kashmir", "02": "Himachal Pradesh", "03": "Punjab", "04": "Chandigarh", "05": "Uttarakhand", "06": "Haryana", "07": "Delhi", "08": "Rajasthan", "09": "Uttar Pradesh", "10": "Bihar", "11": "Sikkim", "12": "Arunachal Pradesh", "13": "Nagaland", "14": "Manipur", "15": "Mizoram", "16": "Tripura", "17": "Meghalaya", "18": "Assam", "19": "West Bengal", "20": "Jharkhand", "21": "Odisha", "22": "Chhattisgarh", "23": "Madhya Pradesh", "24": "Gujarat", "26": "Dadra and Nagar Haveli and Daman and Diu", "27": "Maharashtra", "28": "Andhra Pradesh", "29": "Karnataka", "30": "Goa", "31": "Lakshadweep", "32": "Kerala", "33": "Tamil Nadu", "34": "Puducherry", "35": "Andaman and Nicobar Islands", "36": "Telangana", "37": "Andhra Pradesh", "38": "Ladakh",
}

// LookupPincode verifies the PIN and uses Google when configured, falling back
// only to a matching curated reference record when the external service fails.
func (s *Service) LookupPincode(ctx context.Context, pin string) (LocationDetails, error) {
	pin = strings.TrimSpace(pin)
	if !indianPIN.MatchString(pin) {
		return LocationDetails{}, invalid("pincode", "format", "enter a valid six-digit Indian PIN code")
	}
	if geo, ok := s.geo.(detailedGeocoder); ok {
		if result, err := geo.PincodeDetails(ctx, pin); err == nil {
			return result, nil
		} else if !errors.Is(err, apperror.ErrNotFound) {
			row, dbErr := s.repo.Pincode(ctx, pin)
			if dbErr == nil {
				return localPincodeDetails(row.Pincode, row.Area, row.CityName, row.StateCode, row.Lat, row.Lng), nil
			}
			return LocationDetails{}, err
		}
	}
	row, err := s.repo.Pincode(ctx, pin)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return LocationDetails{}, invalid("pincode", "unknown", "this PIN code could not be verified")
		}
		return LocationDetails{}, err
	}
	return localPincodeDetails(row.Pincode, row.Area, row.CityName, row.StateCode, row.Lat, row.Lng), nil
}

func localPincodeDetails(pin, area, city, code string, lat, lng float64) LocationDetails {
	return LocationDetails{Pincode: strings.TrimSpace(pin), Area: area, City: city, State: indianStates[code], StateCode: code, Lat: lat, Lng: lng, Source: "local_reference"}
}

// ReverseGeocode resolves the real device coordinates; it has no fabricated fallback.
func (s *Service) ReverseGeocode(ctx context.Context, lat, lng float64) (LocationDetails, error) {
	if math.IsNaN(lat) || math.IsInf(lat, 0) || lat < -90 || lat > 90 || math.IsNaN(lng) || math.IsInf(lng, 0) || lng < -180 || lng > 180 {
		return LocationDetails{}, invalid("location", "coordinates", "device coordinates are invalid")
	}
	if geo, ok := s.geo.(detailedGeocoder); ok {
		if g, isGoogle := s.geo.(*GoogleGeocoder); !isGoogle || g.Key != "" {
			if res, err := geo.Reverse(ctx, lat, lng); err == nil {
				return res, nil
			}
		}
	}
	return reverseOSM(ctx, lat, lng)
}

type geocodeComponent struct {
	LongName  string   `json:"long_name"`
	ShortName string   `json:"short_name"`
	Types     []string `json:"types"`
}
type geocodeResult struct {
	FormattedAddress  string             `json:"formatted_address"`
	AddressComponents []geocodeComponent `json:"address_components"`
	Geometry          struct {
		Location struct {
			Lat float64 `json:"lat"`
			Lng float64 `json:"lng"`
		} `json:"location"`
	} `json:"geometry"`
}
type geocodeResponse struct {
	Status  string          `json:"status"`
	Results []geocodeResult `json:"results"`
}

func (g *GoogleGeocoder) geocode(ctx context.Context, params url.Values) (geocodeResponse, error) {
	if g.Key == "" {
		return geocodeResponse{}, apperror.ErrServiceUnavailable.WithDetail("Google Geocoding is not configured")
	}
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	endpoint := g.Endpoint
	if endpoint == "" {
		endpoint = "https://maps.googleapis.com/maps/api/geocode/json"
	}
	params.Set("key", g.Key)
	params.Set("region", "in")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return geocodeResponse{}, apperror.ErrServiceUnavailable.WithDetail("could not start the location lookup")
	}
	res, err := client.Do(req)
	if err != nil {
		return geocodeResponse{}, apperror.ErrServiceUnavailable.WithDetail("location service is unreachable; check internet connectivity and try again")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return geocodeResponse{}, apperror.ErrServiceUnavailable.WithDetail(fmt.Sprintf("location service returned HTTP %d", res.StatusCode))
	}
	var body geocodeResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return geocodeResponse{}, apperror.ErrServiceUnavailable.WithDetail("location service returned an unreadable response")
	}
	if body.Status == "ZERO_RESULTS" || len(body.Results) == 0 {
		return geocodeResponse{}, invalid("location", "not_found", "no address was found for this location")
	}
	if body.Status != "OK" {
		return geocodeResponse{}, apperror.ErrServiceUnavailable.WithDetail(fmt.Sprintf("location service failed (%s); check the Geocoding API key and billing", body.Status))
	}
	return body, nil
}

func componentValue(cs []geocodeComponent, kind string) string {
	for _, c := range cs {
		for _, t := range c.Types {
			if t == kind {
				return c.LongName
			}
		}
	}
	return ""
}

func googleLocation(r geocodeResult) LocationDetails {
	city := componentValue(r.AddressComponents, "locality")
	if city == "" {
		city = componentValue(r.AddressComponents, "postal_town")
	}
	if city == "" {
		city = componentValue(r.AddressComponents, "sublocality_level_1")
	}
	if city == "" {
		city = componentValue(r.AddressComponents, "administrative_area_level_3")
	}
	state := componentValue(r.AddressComponents, "administrative_area_level_1")
	line := strings.TrimSpace(componentValue(r.AddressComponents, "street_number") + " " + componentValue(r.AddressComponents, "route"))
	area := componentValue(r.AddressComponents, "neighborhood")
	if line == "" {
		line = area
	}
	stateCode := ""
	for code, name := range indianStates {
		if strings.EqualFold(name, state) {
			stateCode = code
			break
		}
	}
	return LocationDetails{Pincode: componentValue(r.AddressComponents, "postal_code"), Area: area, AddressLine: line,
		FormattedAddress: r.FormattedAddress, City: city, District: componentValue(r.AddressComponents, "administrative_area_level_2"),
		State: state, StateCode: stateCode, Lat: r.Geometry.Location.Lat, Lng: r.Geometry.Location.Lng, Source: "google"}
}

func (g *GoogleGeocoder) PincodeDetails(ctx context.Context, pin string) (LocationDetails, error) {
	body, err := g.geocode(ctx, url.Values{"components": {"postal_code:" + pin + "|country:IN"}})
	if err != nil {
		return LocationDetails{}, err
	}
	result := googleLocation(body.Results[0])
	if result.Pincode != "" && result.Pincode != pin {
		return LocationDetails{}, invalid("pincode", "mismatch", "the location service returned a different PIN code")
	}
	result.Pincode = pin
	return result, nil
}

func (g *GoogleGeocoder) Reverse(ctx context.Context, lat, lng float64) (LocationDetails, error) {
	body, err := g.geocode(ctx, url.Values{"latlng": {fmt.Sprintf("%.7f,%.7f", lat, lng)}})
	if err != nil {
		return LocationDetails{}, err
	}
	result := googleLocation(body.Results[0])
	result.Lat, result.Lng = lat, lng
	return result, nil
}
