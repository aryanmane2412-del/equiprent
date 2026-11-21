package httpserver

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// securityHeaders sets defence-in-depth response headers on every response.
// The API serves JSON only, so the CSP forbids everything; the /docs page
// (Swagger UI) gets its own relaxed policy. HSTS is sent only in deployed
// environments so local http:// development keeps working.
func securityHeaders(hsts bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Resource-Policy", "same-site")
		h.Set("Permissions-Policy", "geolocation=(), camera=(), microphone=()")
		if c.Request.URL.Path == PathDocs {
			h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' https://unpkg.com; "+
				"style-src 'self' 'unsafe-inline' https://unpkg.com; img-src 'self' data:; frame-ancestors 'none'")
		} else {
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		}
		if hsts {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		// Tokens and personal data must never be cached by browsers or proxies.
		// (File downloads set their own Cache-Control later and override this.)
		if strings.HasPrefix(c.Request.URL.Path, APIPrefix+"/") {
			h.Set("Cache-Control", "no-store")
		}
		c.Next()
	}
}
