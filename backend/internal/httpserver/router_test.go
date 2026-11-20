package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/health"
	"github.com/equiprent/backend/internal/logger"
)

func testConfig() config.Config {
	return config.Config{
		Env:         config.EnvTest,
		ServiceName: "equiprent-api",
		HTTP:        config.HTTPConfig{MaxBodyBytes: 64},
		CORS: config.CORSConfig{
			AllowedOrigins: []string{"https://app.equiprent.in"},
			AllowLocalhost: true,
		},
	}
}

func newTestRouter(t *testing.T, checkers ...health.Checker) *gin.Engine {
	t.Helper()
	return NewRouter(Deps{
		Config: testConfig(),
		Logger: logger.Discard(),
		Health: health.NewService(time.Second, true, checkers...).Disable("redis"),
	})
}

func do(r http.Handler, method, path string, headers map[string]string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeProblem(t *testing.T, w *httptest.ResponseRecorder) Problem {
	t.Helper()
	if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content-type = %q, body=%s", ct, w.Body)
	}
	var p Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	return p
}

var upChecker = health.CheckFunc{CheckName: "postgres", Fn: func(context.Context) error { return nil }}

func TestLiveness(t *testing.T) {
	w := do(newTestRouter(t, upChecker), http.MethodGet, PathLiveness, nil, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"up"`) {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	if w.Header().Get(HeaderRequestID) == "" {
		t.Error("missing request id header")
	}
}

func TestReadinessUpAndDown(t *testing.T) {
	w := do(newTestRouter(t, upChecker), http.MethodGet, PathReadiness, nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("ready: got %d %s", w.Code, w.Body)
	}
	var rep health.Report
	_ = json.Unmarshal(w.Body.Bytes(), &rep)
	if rep.Checks["postgres"].Status != health.StatusUp || rep.Checks["redis"].Status != health.StatusDisabled {
		t.Fatalf("checks = %+v", rep.Checks)
	}

	down := health.CheckFunc{CheckName: "postgres", Fn: func(context.Context) error { return errors.New("refused") }}
	w = do(newTestRouter(t, down), http.MethodGet, PathReadiness, nil, "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("not ready: got %d", w.Code)
	}
}

func TestVersionedStatus(t *testing.T) {
	w := do(newTestRouter(t, upChecker), http.MethodGet, APIPrefix+"/status", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	if w.Header().Get(HeaderAPIVersion) != "v1" {
		t.Errorf("API-Version header = %q", w.Header().Get(HeaderAPIVersion))
	}
	var s statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if s.APIVersion != "v1" || s.Service != "equiprent-api" || s.Environment != "test" {
		t.Errorf("status = %+v", s)
	}
}

func TestUnknownRouteIsProblemJSON(t *testing.T) {
	w := do(newTestRouter(t, upChecker), http.MethodGet, APIPrefix+"/nope", nil, "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d", w.Code)
	}
	p := decodeProblem(t, w)
	if p.Code != "NOT_FOUND" || p.Type != "https://docs.equiprent.in/errors/not-found" || p.RequestID == "" {
		t.Errorf("problem = %+v", p)
	}
}

func TestWrongMethodIs405(t *testing.T) {
	w := do(newTestRouter(t, upChecker), http.MethodPost, PathLiveness, nil, "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d", w.Code)
	}
	if p := decodeProblem(t, w); p.Code != "METHOD_NOT_ALLOWED" {
		t.Errorf("code = %s", p.Code)
	}
}

func TestPanicIsRecoveredWithoutLeakingDetails(t *testing.T) {
	r := newTestRouter(t, upChecker)
	r.GET("/boom", func(*gin.Context) { panic("secret internals") })
	w := do(r, http.MethodGet, "/boom", nil, "")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatal("panic message leaked to client")
	}
	if p := decodeProblem(t, w); p.Code != "INTERNAL" {
		t.Errorf("code = %s", p.Code)
	}
}

func TestFailRendersAppErrorWithFields(t *testing.T) {
	r := newTestRouter(t, upChecker)
	r.POST("/validate", func(c *gin.Context) {
		Fail(c, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "phone", Code: "invalid"}))
	})
	w := do(r, http.MethodPost, "/validate", nil, "")
	p := decodeProblem(t, w)
	if w.Code != http.StatusBadRequest || len(p.Errors) != 1 || p.Errors[0].Field != "phone" {
		t.Fatalf("got %d %+v", w.Code, p)
	}
}

func TestBodyLimit(t *testing.T) {
	r := newTestRouter(t, upChecker)
	r.POST("/echo", func(c *gin.Context) {
		var v map[string]any
		if err := c.ShouldBindJSON(&v); err != nil {
			Fail(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
	w := do(r, http.MethodPost, "/echo", map[string]string{"Content-Type": "application/json"},
		`{"data":"`+strings.Repeat("x", 200)+`"}`)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}

func TestClientRequestIDIsEchoedWhenValid(t *testing.T) {
	r := newTestRouter(t, upChecker)
	w := do(r, http.MethodGet, PathLiveness, map[string]string{HeaderRequestID: "client-req-12345"}, "")
	if got := w.Header().Get(HeaderRequestID); got != "client-req-12345" {
		t.Errorf("request id = %q", got)
	}
	w = do(r, http.MethodGet, PathLiveness, map[string]string{HeaderRequestID: "bad id\n<script>"}, "")
	if got := w.Header().Get(HeaderRequestID); got == "" || strings.Contains(got, "<") {
		t.Errorf("unsafe request id accepted: %q", got)
	}
}

func TestCORS(t *testing.T) {
	r := newTestRouter(t, upChecker)
	preflight := func(origin string) *httptest.ResponseRecorder {
		return do(r, http.MethodOptions, APIPrefix+"/status", map[string]string{
			"Origin": origin, "Access-Control-Request-Method": "GET",
		}, "")
	}

	for _, origin := range []string{"https://app.equiprent.in", "http://localhost:5000", "http://127.0.0.1:61234"} {
		w := preflight(origin)
		if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != origin {
			t.Errorf("%s: got %d allow=%q", origin, w.Code, w.Header().Get("Access-Control-Allow-Origin"))
		}
	}

	w := preflight("https://evil.example")
	if w.Code != http.StatusForbidden || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("evil origin: got %d", w.Code)
	}

	w = do(r, http.MethodGet, APIPrefix+"/status", map[string]string{"Origin": "https://evil.example"}, "")
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("simple request from disallowed origin got CORS headers")
	}
}
