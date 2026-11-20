package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/httpx"
)

// HeaderRequestID is accepted from clients (if well-formed) and always echoed.
const HeaderRequestID = httpx.HeaderRequestID

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{8,128}$`)

// RequestID returns the request ID assigned by the RequestIDMiddleware.
func RequestID(c *gin.Context) string { return httpx.RequestID(c) }

// RequestIDFromContext returns the request ID stored in ctx, or "".
func RequestIDFromContext(ctx context.Context) string { return httpx.RequestIDFromContext(ctx) }

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(HeaderRequestID)
		if !requestIDPattern.MatchString(id) {
			id = uuid.Must(uuid.NewV7()).String()
		}
		c.Request = c.Request.WithContext(httpx.WithRequestID(c.Request.Context(), id))
		c.Header(HeaderRequestID, id)
		c.Next()
	}
}

// recoveryMiddleware converts panics into a 500 problem response and logs the
// stack trace.
func recoveryMiddleware(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler { //nolint:errorlint // sentinel comparison per net/http docs
				panic(rec)
			}
			log.ErrorContext(c.Request.Context(), "panic recovered",
				"request_id", RequestID(c), "panic", rec, "stack", string(debug.Stack()))
			if !c.Writer.Written() {
				writeProblem(c, log, apperror.ErrInternal)
			}
			c.Abort()
		}()
		c.Next()
	}
}

// errorMiddleware renders the last error recorded via Fail / c.Error.
func errorMiddleware(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if len(c.Errors) == 0 || c.Writer.Written() {
			return
		}
		writeProblem(c, log, c.Errors.Last().Err)
	}
}

// loggingMiddleware writes one structured line per request.
func loggingMiddleware(log *slog.Logger, quietPaths ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}

		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case status >= 400:
			level = slog.LevelWarn
		case slices.Contains(quietPaths, route):
			level = slog.LevelDebug
		}

		log.Log(c.Request.Context(), level, "http request",
			"request_id", RequestID(c),
			"method", c.Request.Method,
			"route", route,
			"path", c.Request.URL.Path,
			"status", status,
			"latency_ms", time.Since(start).Milliseconds(),
			"bytes", c.Writer.Size(),
			"client_ip", c.ClientIP(),
			"user_agent", c.Request.UserAgent(),
		)
	}
}

// bodyLimitMiddleware caps request bodies; oversized bodies fail when read
// and are reported as 413 by apperror.From.
// Per-path overrides allow larger bodies only where needed (file uploads).
func bodyLimitMiddleware(maxBytes int64, overrides map[string]int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := maxBytes
		if o, ok := overrides[c.Request.URL.Path]; ok {
			limit = o
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}

var (
	corsAllowMethods = strings.Join([]string{
		http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions,
	}, ", ")
	corsAllowHeaders = strings.Join([]string{
		"Authorization", "Content-Type", "Accept", "Accept-Language", "Idempotency-Key", "If-Match",
		HeaderRequestID, "X-Org-Id", "X-Role", "X-App-Version", "X-Platform",
	}, ", ")
	corsExposeHeaders = strings.Join([]string{HeaderRequestID, HeaderAPIVersion, "Retry-After", "ETag",
		"RateLimit-Limit", "RateLimit-Remaining", "Idempotency-Replayed"}, ", ")
)

// corsMiddleware implements an allow-list CORS policy for the web apps.
func corsMiddleware(cfg config.CORSConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}
		c.Writer.Header().Add("Vary", "Origin")

		preflight := c.Request.Method == http.MethodOptions && c.GetHeader("Access-Control-Request-Method") != ""
		if !originAllowed(cfg, origin) {
			if preflight {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
			return
		}

		h := c.Writer.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Set("Access-Control-Allow-Credentials", "true")
		h.Set("Access-Control-Expose-Headers", corsExposeHeaders)
		if preflight {
			h.Set("Access-Control-Allow-Methods", corsAllowMethods)
			h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
			h.Set("Access-Control-Max-Age", strconv.Itoa(600))
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func originAllowed(cfg config.CORSConfig, origin string) bool {
	if slices.Contains(cfg.AllowedOrigins, origin) {
		return true
	}
	if !cfg.AllowLocalhost {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}
