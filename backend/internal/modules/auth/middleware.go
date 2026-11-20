package auth

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/httpx"
)

// HeaderOrgID selects the organisation a multi-org user acts for.
const HeaderOrgID = "X-Org-Id"

// Authenticator turns a bearer token into a Principal.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (*httpx.Principal, error)
}

// Middleware requires a valid bearer token, then resolves the acting
// organisation: X-Org-Id must name an organisation the user belongs to
// (platform admins may act for any); without the header, a user with exactly
// one organisation acts for it.
func Middleware(a Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			c.Header("WWW-Authenticate", `Bearer realm="equiprent"`)
			httpx.Fail(c, apperror.ErrUnauthenticated)
			return
		}
		p, err := a.Authenticate(c.Request.Context(), strings.TrimSpace(token))
		if err != nil {
			c.Header("WWW-Authenticate", `Bearer realm="equiprent", error="invalid_token"`)
			httpx.Fail(c, err)
			return
		}

		if raw := c.GetHeader(HeaderOrgID); raw != "" {
			org, err := uuid.Parse(raw)
			if err != nil {
				httpx.Fail(c, apperror.ErrValidation.WithFields(apperror.FieldError{Field: HeaderOrgID, Code: "uuid", Message: "must be a UUID"}))
				return
			}
			if !p.IsMember(org) && !p.IsAdmin() {
				httpx.Fail(c, apperror.ErrForbidden.WithDetail("you are not a member of this organisation"))
				return
			}
			p.ActingOrg = org
		} else if orgs := p.Orgs(); len(orgs) == 1 {
			p.ActingOrg = orgs[0]
		}
		httpx.SetPrincipal(c, p)
		c.Next()
	}
}

// RequirePlatformRole allows only callers holding one of roles (admin_super
// always passes). Must run after Middleware.
func RequirePlatformRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := httpx.MustPrincipal(c)
		if !ok {
			return
		}
		if !p.HasPlatformRole(roles...) {
			httpx.Fail(c, apperror.ErrForbidden.WithDetail("an administrator role is required"))
			return
		}
		c.Next()
	}
}

// OptionalMiddleware sets the Principal when a valid bearer token is
// present and otherwise lets the request through anonymously. Used on public
// endpoints that show more to the owner (e.g. an unpublished listing).
func OptionalMiddleware(a Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if ok && strings.TrimSpace(token) != "" {
			if p, err := a.Authenticate(c.Request.Context(), strings.TrimSpace(token)); err == nil {
				httpx.SetPrincipal(c, p)
			}
		}
		c.Next()
	}
}
