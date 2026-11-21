package httpserver

import (
	"bytes"
	"io"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/equiprent/backend/internal/apperror"
)

// rejectNULInURL answers 400 when the path or query string contains a NUL
// byte. PostgreSQL text cannot hold NUL, so such input would otherwise reach a
// query and surface as a 500.
func rejectNULInURL() gin.HandlerFunc {
	return func(c *gin.Context) {
		u := c.Request.URL
		if strings.ContainsRune(u.Path, 0) || strings.ContainsRune(u.RawQuery, 0) || strings.Contains(strings.ToLower(u.RawQuery+u.RawPath), "%00") {
			Fail(c, apperror.ErrBadRequest.WithDetail("the request contains an invalid character"))
			c.Abort()
			return
		}
		c.Next()
	}
}

// rejectNULInBody answers 400 when a JSON body contains a NUL byte, raw or as
// the escape \u0000, for the same reason as rejectNULInURL: PostgreSQL text
// columns cannot store it. Bodies stay readable by the handlers.
func rejectNULInBody() gin.HandlerFunc {
	nulEscape := []byte(`\u0000`)
	return func(c *gin.Context) {
		if c.Request.Body == nil || c.Request.ContentLength == 0 || !strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "json") {
			c.Next()
			return
		}
		b, err := io.ReadAll(c.Request.Body) // already capped by bodyLimitMiddleware
		if err != nil {
			Fail(c, err)
			c.Abort()
			return
		}
		if bytes.IndexByte(b, 0) >= 0 || bytes.Contains(b, nulEscape) {
			Fail(c, apperror.ErrBadRequest.WithDetail("the request contains an invalid character"))
			c.Abort()
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(b))
		c.Next()
	}
}
