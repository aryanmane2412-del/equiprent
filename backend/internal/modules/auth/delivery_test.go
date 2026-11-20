package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSMSHTTPPostsAuthenticatedMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("request method/auth = %s/%q", r.Method, r.Header.Get("Authorization"))
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if body["to"] != "+919876543210" || !strings.Contains(body["message"], "583104") || !strings.Contains(body["message"], "Never share") {
			t.Errorf("unexpected SMS payload: %+v", body)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	sender := SMSHTTP{HTTPJSON{Endpoint: server.URL, Token: "test-token", Client: server.Client()}}
	if err := sender.SendOTP(context.Background(), "+919876543210", "583104", "login"); err != nil {
		t.Fatal(err)
	}
}

func TestEmailHTTPRejectsProviderFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["from"] != "verify@example.com" || body["to"] != "user@example.com" || !strings.Contains(body["body"], "123456") {
			t.Errorf("unexpected email payload: %+v", body)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	sender := EmailHTTP{HTTPJSON: HTTPJSON{Endpoint: server.URL, Token: "test-token", Client: server.Client()}, From: "verify@example.com"}
	if err := sender.Send(context.Background(), "user@example.com", "Verify", "Code 123456"); err == nil {
		t.Fatal("expected provider failure")
	}
}
