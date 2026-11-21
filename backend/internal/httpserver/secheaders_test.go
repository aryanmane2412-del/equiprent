package httpserver

import (
	"net/http"
	"testing"
)

func TestSecurityHeadersOnAPIResponses(t *testing.T) {
	w := do(newTestRouter(t, upChecker), http.MethodGet, APIPrefix+"/status", nil, "")
	want := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
		"Cache-Control":           "no-store",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
	}
	for k, v := range want {
		if got := w.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS must not be sent in the test/development environment, got %q", got)
	}
}
