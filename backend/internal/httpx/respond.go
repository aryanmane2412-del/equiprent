// Package httpx holds the HTTP helpers shared by the router and every feature
// module: problem+json errors, request IDs, binding/validation, pagination,
// the authenticated principal and documented route registration.
package httpx

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/equiprent/backend/internal/apperror"
)

// HeaderRequestID is accepted from clients (if well-formed) and always echoed.
const HeaderRequestID = "X-Request-ID"

const problemTypeBase = "https://docs.equiprent.in/errors/"

type requestIDKey struct{}

// WithRequestID stores id in ctx.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the request ID assigned by the request-ID middleware.
func RequestID(c *gin.Context) string { return RequestIDFromContext(c.Request.Context()) }

// RequestIDFromContext returns the request ID stored in ctx, or "".
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// Problem is the RFC 9457 error body used by every endpoint.
type Problem struct {
	Type      string                `json:"type"`
	Title     string                `json:"title"`
	Status    int                   `json:"status"`
	Code      string                `json:"code"`
	Detail    string                `json:"detail,omitempty"`
	Instance  string                `json:"instance,omitempty"`
	RequestID string                `json:"request_id,omitempty"`
	Errors    []apperror.FieldError `json:"errors,omitempty"`
}

// Fail records err on the context and aborts the chain. The error middleware
// renders it once all handlers have returned. Handlers use this rather than
// writing error JSON themselves.
func Fail(c *gin.Context, err error) {
	_ = c.Error(err)
	c.Abort()
}

// WriteProblem renders err as application/problem+json. Internal causes are
// logged with the request ID but never sent to the client.
func WriteProblem(c *gin.Context, log *slog.Logger, err error) {
	appErr := apperror.From(err)
	reqID := RequestID(c)

	if appErr.Status >= http.StatusInternalServerError {
		log.ErrorContext(c.Request.Context(), "request failed",
			"request_id", reqID, "code", appErr.Code, "error", err)
	}
	if appErr.Status == http.StatusTooManyRequests && appErr.RetryAfter > 0 {
		c.Header("Retry-After", appErr.RetryAfterHeader())
	}

	body, mErr := json.Marshal(Problem{
		Type:      problemTypeBase + strings.ToLower(strings.ReplaceAll(appErr.Code, "_", "-")),
		Title:     appErr.Title,
		Status:    appErr.Status,
		Code:      appErr.Code,
		Detail:    appErr.Detail,
		Instance:  c.Request.URL.Path,
		RequestID: reqID,
		Errors:    appErr.Fields,
	})
	if mErr != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Data(appErr.Status, "application/problem+json", body)
}

// OK writes a 200 JSON response.
func OK(c *gin.Context, v any) { c.JSON(http.StatusOK, v) }

// Created writes a 201 JSON response.
func Created(c *gin.Context, v any) { c.JSON(http.StatusCreated, v) }

// NoContent writes a 204 response.
func NoContent(c *gin.Context) { c.Status(http.StatusNoContent) }

// Reply renders v with status, or the error. It keeps handlers to one line
// after binding: httpx.Reply(c, http.StatusOK, svc.Do(ctx, req)).
func Reply(c *gin.Context, status int, v any, err error) {
	if err != nil {
		Fail(c, err)
		return
	}
	if status == http.StatusNoContent {
		c.Status(status)
		return
	}
	c.JSON(status, v)
}

// Done renders 204 No Content, or the error.
func Done(c *gin.Context, err error) {
	if err != nil {
		Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Send is the curried form of Reply, so a service call can be passed straight
// through: httpx.Send(c, http.StatusOK)(h.svc.Get(ctx, id)).
func Send(c *gin.Context, status int) func(v any, err error) {
	return func(v any, err error) { Reply(c, status, v, err) }
}
