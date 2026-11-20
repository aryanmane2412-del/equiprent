package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGoogleGeocoderResolvesPostalAddressAndCoordinates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("components") != "postal_code:411057|country:IN" {
			t.Errorf("unexpected components: %s", r.URL.Query().Get("components"))
		}
		if r.URL.Query().Get("key") != "test-key" {
			t.Error("Google key missing")
		}
		_, _ = w.Write([]byte(`{"status":"OK","results":[{"formatted_address":"Hinjewadi, Pune, Maharashtra 411057, India","address_components":[{"long_name":"411057","short_name":"411057","types":["postal_code"]},{"long_name":"Pune","short_name":"Pune","types":["locality","political"]},{"long_name":"Pune","short_name":"Pune","types":["administrative_area_level_2","political"]},{"long_name":"Maharashtra","short_name":"MH","types":["administrative_area_level_1","political"]},{"long_name":"India","short_name":"IN","types":["country","political"]}],"geometry":{"location":{"lat":18.5912,"lng":73.7389}}}]}`))
	}))
	defer server.Close()
	geo := &GoogleGeocoder{Key: "test-key", Client: server.Client(), Endpoint: server.URL}
	got, err := geo.PincodeDetails(context.Background(), "411057")
	if err != nil {
		t.Fatal(err)
	}
	if got.Pincode != "411057" || got.City != "Pune" || got.District != "Pune" || got.StateCode != "27" || got.Lat != 18.5912 || got.Lng != 73.7389 {
		t.Fatalf("unexpected location: %+v", got)
	}
}

func TestGoogleGeocoderReverseUsesInputGPSPointAndParsedAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("latlng") != "18.5912000,73.7389000" {
			t.Errorf("unexpected latlng: %s", r.URL.Query().Get("latlng"))
		}
		_, _ = w.Write([]byte(`{"status":"OK","results":[{"formatted_address":"Phase 2, Hinjewadi, Pune, Maharashtra 411057, India","address_components":[{"long_name":"Phase 2","short_name":"Phase 2","types":["route"]},{"long_name":"Pune","short_name":"Pune","types":["locality"]},{"long_name":"Pune","short_name":"Pune","types":["administrative_area_level_2"]},{"long_name":"Maharashtra","short_name":"MH","types":["administrative_area_level_1"]},{"long_name":"411057","short_name":"411057","types":["postal_code"]}],"geometry":{"location":{"lat":18.59,"lng":73.73}}}]}`))
	}))
	defer server.Close()
	geo := &GoogleGeocoder{Key: "test-key", Client: server.Client(), Endpoint: server.URL}
	got, err := geo.Reverse(context.Background(), 18.5912, 73.7389)
	if err != nil {
		t.Fatal(err)
	}
	if got.Pincode != "411057" || got.City != "Pune" || got.AddressLine != "Phase 2" || got.Lat != 18.5912 || got.Lng != 73.7389 {
		t.Fatalf("unexpected reverse result: %+v", got)
	}
}

func TestGoogleGeocoderReportsUnavailableService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer server.Close()
	geo := &GoogleGeocoder{Key: "test-key", Client: server.Client(), Endpoint: server.URL}
	if _, err := geo.PincodeDetails(context.Background(), "411057"); err == nil {
		t.Fatal("expected upstream error")
	}
}
