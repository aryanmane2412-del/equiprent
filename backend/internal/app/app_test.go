package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/equiprent/backend/internal/apidoc"
	"github.com/equiprent/backend/internal/app"
	"github.com/equiprent/backend/internal/cache"
	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/health"
	"github.com/equiprent/backend/internal/httpserver"
	"github.com/equiprent/backend/internal/logger"
)

var update = flag.Bool("update", false, "rewrite api/openapi.json")

// wired builds the full router on a pool that never connects: enough for
// everything that happens before a query (routing, auth, validation, docs).
func wired(t *testing.T) (*gin.Engine, httpserver.Deps) {
	t.Helper()
	cfg, err := config.Load(func(k string) (string, bool) {
		switch k {
		case "DATABASE_URL":
			return "postgres://nobody@127.0.0.1:1/none?connect_timeout=1", true
		case "APP_ENV":
			return "test", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.NewPool(context.Background(), cfg.Database, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	a := app.New(cfg, logger.Discard(), pool, cache.NewMemoryStore())
	deps := httpserver.Deps{
		Config: cfg, Logger: logger.Discard(), Modules: a.Modules, Guards: a.Guards, Limiter: a.Limiter,
		Health: health.NewService(time.Second, true).Disable("postgres"),
	}
	return httpserver.NewRouter(deps), deps
}

var param = regexp.MustCompile(`:[a-z_]+`)

// Every served /api/v1 route is in the OpenAPI document and vice versa.
func TestEveryRouteIsDocumented(t *testing.T) {
	r, _ := wired(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, httpserver.PathOpenAPI, nil))
	if w.Code != 200 {
		t.Fatalf("openapi.json: %d", w.Code)
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	served := map[string]bool{}
	for _, rt := range r.Routes() {
		if !strings.HasPrefix(rt.Path, httpserver.APIPrefix) || rt.Path == httpserver.PathOpenAPI {
			continue
		}
		key := rt.Method + " " + apidoc.OpenAPIPath(rt.Path)
		served[key] = true
		if _, ok := doc.Paths[apidoc.OpenAPIPath(rt.Path)][strings.ToLower(rt.Method)]; !ok {
			t.Errorf("undocumented route %s", key)
		}
	}
	for path, ops := range doc.Paths {
		for m := range ops {
			if !served[strings.ToUpper(m)+" "+path] {
				t.Errorf("documented but not served: %s %s", m, path)
			}
		}
	}
	if len(served) < 100 {
		t.Errorf("only %d routes registered; modules missing?", len(served))
	}
}

// Every operation marked as secured rejects anonymous callers with 401
// before touching the database.
func TestSecuredRoutesRequireAuthentication(t *testing.T) {
	r, deps := wired(t)
	raw, err := httpserver.BuildSpec(deps)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			Security []map[string][]string `json:"security"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for path, ops := range doc.Paths {
		concrete := regexp.MustCompile(`\{[a-z_]+\}`).ReplaceAllString(path, "0193f6c2-0000-7000-8000-000000000001")
		for method, op := range ops {
			if len(op.Security) == 0 {
				continue
			}
			w := httptest.NewRecorder()
			req := httptest.NewRequest(strings.ToUpper(method), concrete, bytes.NewReader([]byte(`{}`)))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s %s without token: %d, want 401", strings.ToUpper(method), path, w.Code)
			}
			checked++
		}
	}
	if checked < 80 {
		t.Errorf("only %d secured operations checked", checked)
	}
	// A garbage token is rejected the same way.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Header().Get("WWW-Authenticate"), "invalid_token") {
		t.Errorf("bad token: %d %v", w.Code, w.Header())
	}
}

// api/openapi.json is the committed contract (the Flutter client is
// generated from it). Regenerate with: go test ./internal/app -run OpenAPI -update
func TestCommittedOpenAPIIsCurrent(t *testing.T) {
	_, deps := wired(t)
	deps.Config.Docs.PublicURL = "http://localhost:8080"
	got, err := httpserver.BuildSpec(deps)
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const path = "../../api/openapi.json"
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Fatal("api/openapi.json is stale: run `go test ./internal/app -run OpenAPI -update` (or `go run ./cmd/api openapi > api/openapi.json`)")
	}
}

func TestSwaggerUIServed(t *testing.T) {
	r, _ := wired(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, httpserver.PathDocs, nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "swagger-ui") {
		t.Fatalf("docs: %d", w.Code)
	}
}

func TestValidationErrorsNameJSONFields(t *testing.T) {
	r, _ := wired(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/otp/request", strings.NewReader(`{"phone":"98765"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	var p httpserver.Problem
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if w.Code != 400 || len(p.Errors) != 1 || p.Errors[0].Field != "phone" || p.Errors[0].Code != "e164" {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}
