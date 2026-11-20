// Package httpserver wires Gin: middleware chain, versioned REST routes,
// health endpoints and the http.Server.
package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/equiprent/backend/internal/apidoc"
	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/health"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/ratelimit"
	"github.com/equiprent/backend/internal/version"
)

const (
	// APIVersion is the current major REST version (docs/API_DESIGN.md §1).
	APIVersion = "v1"
	// APIPrefix is where all business endpoints are mounted.
	APIPrefix = "/api/" + APIVersion
	// HeaderAPIVersion is set on every versioned response.
	HeaderAPIVersion = "API-Version"

	PathLiveness  = "/healthz"
	PathReadiness = "/readyz"
	// PathOpenAPI serves the generated OpenAPI 3.1 document.
	PathOpenAPI = APIPrefix + "/openapi.json"
	// PathDocs serves Swagger UI.
	PathDocs = "/docs"
)

// ReadinessChecker is implemented by *health.Service.
type ReadinessChecker interface {
	Ready(ctx context.Context) health.Report
}

// Deps are the collaborators the router needs. Modules, Guards and Limiter
// are optional (unit tests build a bare router).
type Deps struct {
	Config  config.Config
	Logger  *slog.Logger
	Health  ReadinessChecker
	Modules []httpx.Module
	Guards  httpx.Guards
	// Limiter, when set and enabled in config, applies the global per-client
	// budget to every /api/v1 route.
	Limiter *ratelimit.Limiter
}

// NewSpec creates the OpenAPI document header for this service.
func NewSpec(cfg config.Config) *apidoc.Spec {
	return apidoc.New(apidoc.Info{
		Title:   "EQUIPRENT API",
		Version: version.Version,
		Description: "Heavy-equipment rental marketplace API. Money is integer paise (INR); times are RFC 3339 UTC; " +
			"errors are application/problem+json (RFC 9457). Authenticate with POST /auth/otp/request + /auth/otp/verify, " +
			"then send Authorization: Bearer <access_token>. Multi-organisation users select one with X-Org-Id.",
		ServerURL: cfg.Docs.PublicURL,
	})
}

// NewRouter builds the Gin engine with the full middleware chain:
// RequestID → Recovery → Logging → CORS → BodyLimit → Errors → handlers.
func NewRouter(d Deps) *gin.Engine {
	if d.Config.Env.IsDevelopment() {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.RedirectTrailingSlash = false
	// nil means "trust no proxy": ClientIP() uses the socket address unless
	// HTTP_TRUSTED_PROXIES lists the load balancer ranges.
	if err := r.SetTrustedProxies(d.Config.HTTP.TrustedProxies); err != nil {
		d.Logger.Warn("invalid HTTP_TRUSTED_PROXIES, trusting none", "error", err)
		_ = r.SetTrustedProxies(nil)
	}

	r.Use(
		requestIDMiddleware(),
		recoveryMiddleware(d.Logger),
		loggingMiddleware(d.Logger, PathLiveness, PathReadiness),
		corsMiddleware(d.Config.CORS),
		bodyLimitMiddleware(d.Config.HTTP.MaxBodyBytes, map[string]int64{
			// multipart overhead on top of the largest allowed file
			APIPrefix + "/uploads": max(d.Config.Storage.MaxUploadBytes, d.Config.HTTP.MaxBodyBytes) + 256<<10,
		}),
		errorMiddleware(d.Logger),
	)

	r.NoRoute(func(c *gin.Context) { Fail(c, apperror.ErrNotFound) })
	r.NoMethod(func(c *gin.Context) { Fail(c, apperror.ErrMethodNotAllowed) })

	h := &systemHandlers{cfg: d.Config, health: d.Health}
	r.GET(PathLiveness, h.liveness)
	r.GET(PathReadiness, h.readiness)

	v1 := r.Group(APIPrefix, func(c *gin.Context) {
		c.Header(HeaderAPIVersion, APIVersion)
		c.Next()
	})
	if d.Limiter != nil && d.Config.RateLimit.Enabled {
		v1.Use(d.Limiter.Middleware(ratelimit.Rule{Name: "api", Limit: d.Config.RateLimit.PerMinute, Window: time.Minute}, ratelimit.ByIP))
	}

	spec := NewSpec(d.Config)
	api := httpx.NewRouter(v1, spec, d.Guards)
	api.Tag("system").GET("/status", h.status, httpx.Doc{Summary: "Service status and version", Response: statusResponse{}})
	api.Tag("system").GET("/health", h.readiness, httpx.Doc{Summary: "Readiness (dependency checks)", Response: health.Report{}})
	for _, m := range d.Modules {
		m.Routes(api)
	}

	if d.Config.Docs.Enabled {
		doc, err := spec.JSON()
		if err != nil {
			d.Logger.Error("building OpenAPI document failed", "error", err)
		}
		r.GET(PathOpenAPI, func(c *gin.Context) { c.Data(http.StatusOK, "application/json", doc) })
		r.GET(PathDocs, func(c *gin.Context) { c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(swaggerHTML)) })
	}
	return r
}

// BuildSpec registers all modules on a throwaway engine and returns the
// OpenAPI document (used by `api openapi` and tests).
func BuildSpec(d Deps) ([]byte, error) {
	gin.SetMode(gin.ReleaseMode)
	spec := NewSpec(d.Config)
	api := httpx.NewRouter(gin.New().Group(APIPrefix), spec, d.Guards)
	h := &systemHandlers{cfg: d.Config, health: d.Health}
	api.Tag("system").GET("/status", h.status, httpx.Doc{Summary: "Service status and version", Response: statusResponse{}})
	api.Tag("system").GET("/health", h.readiness, httpx.Doc{Summary: "Readiness (dependency checks)", Response: health.Report{}})
	for _, m := range d.Modules {
		m.Routes(api)
	}
	return spec.JSON()
}

// swaggerHTML renders the spec with Swagger UI (assets from a pinned CDN
// version; the page is only served when API_DOCS_ENABLED).
const swaggerHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>EQUIPRENT API</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui-bundle.js" crossorigin></script>
  <script>
    window.ui = SwaggerUIBundle({ url: "` + PathOpenAPI + `", dom_id: "#swagger-ui", persistAuthorization: true,
      deepLinking: true, displayRequestDuration: true, tryItOutEnabled: true });
  </script>
</body>
</html>`

type systemHandlers struct {
	cfg    config.Config
	health ReadinessChecker
}

// liveness says the process is up. It never touches dependencies, so a
// database outage does not make the orchestrator restart healthy pods.
func (h *systemHandlers) liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": health.StatusUp})
}

// readiness checks dependencies; 503 tells load balancers to stop routing.
func (h *systemHandlers) readiness(c *gin.Context) {
	report := h.health.Ready(c.Request.Context())
	status := http.StatusOK
	if report.Status != health.StatusUp {
		status = http.StatusServiceUnavailable
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(status, report)
}

type statusResponse struct {
	Service     string    `json:"service"`
	APIVersion  string    `json:"api_version"`
	Version     string    `json:"version"`
	Commit      string    `json:"commit"`
	Environment string    `json:"environment"`
	ServerTime  time.Time `json:"server_time"`
}

func (h *systemHandlers) status(c *gin.Context) {
	c.JSON(http.StatusOK, statusResponse{
		Service:     h.cfg.ServiceName,
		APIVersion:  APIVersion,
		Version:     version.Version,
		Commit:      version.Commit,
		Environment: string(h.cfg.Env),
		ServerTime:  time.Now().UTC(),
	})
}
