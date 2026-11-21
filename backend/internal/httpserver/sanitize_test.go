package httpserver

import (
	"net/http"
	"testing"
)

func TestNULByteInQueryIsBadRequest(t *testing.T) {
	for _, path := range []string{"/status?q=%00", "/status?q=a%00b", "/status?x=%00"} {
		w := do(newTestRouter(t, upChecker), http.MethodGet, APIPrefix+path, nil, "")
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", path, w.Code)
		}
	}
	if w := do(newTestRouter(t, upChecker), http.MethodGet, APIPrefix+"/status?q=ok", nil, ""); w.Code != http.StatusOK {
		t.Errorf("normal query got %d", w.Code)
	}
}

var jsonHdr = map[string]string{"Content-Type": "application/json"}

func TestNULByteInJSONBodyIsBadRequest(t *testing.T) {
	r := newTestRouter(t, upChecker)
	for _, body := range []string{`{"name":"a\u0000b"}`, "{\"name\":\"a\u0000b\"}"} {
		w := do(r, http.MethodPost, APIPrefix+"/status", jsonHdr, body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %q: got %d, want 400", body, w.Code)
		}
	}
	// A normal body is passed through untouched (this route then answers 405).
	if w := do(r, http.MethodPost, APIPrefix+"/status", jsonHdr, `{"name":"ok"}`); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("normal body got %d, want 405", w.Code)
	}
}
