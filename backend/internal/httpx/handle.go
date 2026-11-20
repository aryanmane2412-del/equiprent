package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
)

// HandlerFunc is a module handler: it returns the response body or an error.
// p is nil on public routes.
type HandlerFunc func(c *gin.Context, p *Principal) (any, error)

// Handle adapts fn to Gin, rendering the result with status (204 ignores the
// body).
func Handle(status int, fn HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, _ := CurrentPrincipal(c)
		v, err := fn(c, p)
		Reply(c, status, v, err)
	}
}

// HandleJSON binds and validates a JSON body of type T before calling fn.
func HandleJSON[T any](status int, fn func(c *gin.Context, p *Principal, req T) (any, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req T
		if err := c.ShouldBindJSON(&req); err != nil {
			Fail(c, BindError(err))
			return
		}
		p, _ := CurrentPrincipal(c)
		v, err := fn(c, p, req)
		Reply(c, status, v, err)
	}
}

// HandleQuery binds and validates query parameters of type T before calling fn.
func HandleQuery[T any](status int, fn func(c *gin.Context, p *Principal, q T) (any, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		var q T
		if err := c.ShouldBindQuery(&q); err != nil {
			Fail(c, BindError(err))
			return
		}
		p, _ := CurrentPrincipal(c)
		v, err := fn(c, p, q)
		Reply(c, status, v, err)
	}
}

// Param parses a UUID path parameter.
func Param(c *gin.Context, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		return uuid.Nil, apperror.ErrValidation.WithFields(apperror.FieldError{Field: name, Code: "uuid", Message: "must be a UUID"})
	}
	return id, nil
}

// Params parses several UUID path parameters in order.
func Params(c *gin.Context, names ...string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, len(names))
	for i, n := range names {
		id, err := Param(c, n)
		if err != nil {
			return nil, err
		}
		out[i] = id
	}
	return out, nil
}

// Status codes re-exported for terse route tables.
const (
	StatusOK        = http.StatusOK
	StatusCreated   = http.StatusCreated
	StatusAccepted  = http.StatusAccepted
	StatusNoContent = http.StatusNoContent
)

// Nothing is returned by handlers of 204 routes.
var Nothing any

// Scope is an authenticated caller acting for an organisation.
type Scope struct {
	P   *Principal
	Org uuid.UUID
}

// OrgHandle requires the caller to act for an organisation (X-Org-Id) in
// which they hold one of roles (org_admin always qualifies), then calls fn.
func OrgHandle(status int, roles []string, fn func(c *gin.Context, s Scope) (any, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, org, ok := ActingOrg(c, roles...)
		if !ok {
			return
		}
		v, err := fn(c, Scope{P: p, Org: org})
		Reply(c, status, v, err)
	}
}

// Bind decodes and validates the JSON body into dst, returning an API error.
func Bind(c *gin.Context, dst any) error {
	if err := c.ShouldBindJSON(dst); err != nil {
		return BindError(err)
	}
	return nil
}

// BindQ decodes and validates query parameters into dst, returning an API error.
func BindQ(c *gin.Context, dst any) error {
	if err := c.ShouldBindQuery(dst); err != nil {
		return BindError(err)
	}
	return nil
}
