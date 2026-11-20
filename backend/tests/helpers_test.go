package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/app"
	"github.com/equiprent/backend/internal/cache"
	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/httpserver"
	"github.com/equiprent/backend/internal/logger"
)

// env is a fully wired API on the test database.
type env struct {
	t      *testing.T
	router http.Handler
	pool   *pgxpool.Pool
	app    *app.App
	cfg    config.Config
}

func newEnv(t *testing.T, opts ...func(*config.Config)) *env {
	t.Helper()
	return newEnvAt(t, databaseURL(t), opts...)
}

// newIsolatedEnv runs against a throwaway database (exact counts, timing).
func newIsolatedEnv(t *testing.T, opts ...func(*config.Config)) *env {
	t.Helper()
	return newEnvAt(t, freshDatabase(t, databaseURL(t)), opts...)
}

func newEnvAt(t *testing.T, url string, opts ...func(*config.Config)) *env {
	t.Helper()
	if err := newMigrator(t, url).Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrate up: %v", err)
	}
	cfg, err := config.Load(func(k string) (string, bool) {
		switch k {
		case "DATABASE_URL":
			return url, true
		case "APP_ENV":
			return "test", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg.RateLimit.Enabled = false
	cfg.Storage.Dir = t.TempDir() // private document store
	for _, o := range opts {
		o(&cfg)
	}
	pool := newPool(t, url)
	log := logger.Discard()
	if os.Getenv("TEST_LOG") != "" { // TEST_LOG=1 shows server logs (500 causes)
		log = logger.New(os.Stderr, config.LogConfig{Level: slog.LevelDebug, Format: "text"}, "test", config.EnvTest)
	}
	a := app.New(cfg, log, pool, cache.NewMemoryStore())
	router := httpserver.NewRouter(httpserver.Deps{
		Config: cfg, Logger: log, Health: nil, Modules: a.Modules, Guards: a.Guards, Limiter: a.Limiter,
	})
	return &env{t: t, router: router, pool: pool, app: a, cfg: cfg}
}

// user is a signed-in API client.
type user struct {
	e      *env
	phone  string
	token  string
	id     string
	org    string // acting organisation (X-Org-Id), "" = none
	refesh string
}

// resp is a decoded response.
type resp struct {
	code   int
	body   map[string]any
	raw    string
	header http.Header
}

func (r resp) str(path string) string { return fmt.Sprint(dig(r.body, path)) }

func (r resp) num(path string) float64 {
	v, _ := dig(r.body, path).(float64)
	return v
}

func (r resp) list(path string) []any {
	v, _ := dig(r.body, path).([]any)
	return v
}

// dig walks "a.b.0.c" paths through decoded JSON.
func dig(v any, path string) any {
	if path == "" {
		return v
	}
	for _, part := range strings.Split(path, ".") {
		switch node := v.(type) {
		case map[string]any:
			v = node[part]
		case []any:
			var i int
			if _, err := fmt.Sscan(part, &i); err != nil || i >= len(node) {
				return nil
			}
			v = node[i]
		default:
			return nil
		}
	}
	return v
}

func (e *env) call(method, path, token, org string, body any, headers map[string]string) resp {
	e.t.Helper()
	if m, ok := body.(map[string]any); ok && path == "/auth/register" {
		if _, set := m["accept_inspection_policy"]; !set {
			m["accept_inspection_policy"], m["inspection_policy_version"] = true, "2026-11-18-v1"
		}
	}
	var rd *bytes.Reader
	switch b := body.(type) {
	case nil:
		rd = bytes.NewReader(nil)
	case string:
		rd = bytes.NewReader([]byte(b))
	case []byte:
		rd = bytes.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, "/api/v1"+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if org != "" {
		req.Header.Set("X-Org-Id", org)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	out := resp{code: w.Code, raw: w.Body.String(), header: w.Header()}
	_ = json.Unmarshal(w.Body.Bytes(), &out.body)
	return out
}

func (u *user) do(method, path string, body any, headers ...string) resp {
	u.e.t.Helper()
	h := map[string]string{}
	for i := 0; i+1 < len(headers); i += 2 {
		h[headers[i]] = headers[i+1]
	}
	return u.e.call(method, path, u.token, u.org, body, h)
}

// must asserts the status code and returns the response.
func must(t *testing.T, r resp, want int, what string) resp {
	t.Helper()
	if r.code != want {
		t.Fatalf("%s: status %d, want %d: %s", what, r.code, want, r.raw)
	}
	return r
}

func randomPhone() string { return fmt.Sprintf("+9198%08d", rand.IntN(100_000_000)) }

func randomLetters(n int) string {
	const letters = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.IntN(len(letters))]
	}
	return string(b)
}

func randomGSTIN(state string) string {
	return fmt.Sprintf("%s%s%04d%s1Z%d", state, randomLetters(5), rand.IntN(10000), randomLetters(1), rand.IntN(10))
}

// login runs the OTP flow (dev echo) and returns a signed-in user.
func (e *env) login(phone string) *user {
	e.t.Helper()
	otp := must(e.t, e.call(http.MethodPost, "/auth/otp/request", "", "", map[string]any{"phone": phone}, nil), 202, "otp request")
	tok := must(e.t, e.call(http.MethodPost, "/auth/otp/verify", "", "", map[string]any{
		"phone": phone, "code": otp.str("dev_code"), "platform": "android",
	}, nil), 200, "otp verify")
	return &user{e: e, phone: phone, token: tok.str("access_token"), id: tok.str("user.id"), refesh: tok.str("refresh_token")}
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatalf("exec %q: %v", sql, err)
	}
}

// query1 returns the first column of the first row as text.
func (e *env) query1(sql string, args ...any) string {
	e.t.Helper()
	var out string
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&out); err != nil {
		e.t.Fatalf("query %q: %v", sql, err)
	}
	return out
}
