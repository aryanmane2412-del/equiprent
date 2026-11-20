// Package apperror defines the typed errors that the HTTP layer turns into
// RFC 9457 "problem+json" responses (see docs/API_DESIGN.md §1).
package apperror

import (
	"errors"
	"net/http"
	"strconv"
	"time"
)

// FieldError describes a problem with one input field.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

// Error is an error that knows its HTTP status and stable machine code.
// Values are treated as immutable: the With*/Wrap helpers return copies, so
// the package-level sentinels below are safe to share.
type Error struct {
	Status int
	Code   string
	Title  string
	Detail string
	Fields []FieldError
	Err    error
	// RetryAfter, when set on a 429/503, is sent as the Retry-After header.
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	msg := e.Code + ": " + e.Title
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Err }

// Is matches any *Error with the same Code, so errors.Is(err, ErrNotFound)
// works for customised copies of a sentinel.
func (e *Error) Is(target error) bool {
	if t, ok := errors.AsType[*Error](target); ok {
		return t.Code == e.Code
	}
	return false
}

func (e *Error) clone() *Error {
	c := *e
	c.Fields = append([]FieldError(nil), e.Fields...)
	return &c
}

// WithDetail returns a copy with a human-readable, user-safe detail message.
func (e *Error) WithDetail(detail string) *Error { c := e.clone(); c.Detail = detail; return c }

// WithFields returns a copy carrying field-level validation errors.
func (e *Error) WithFields(fields ...FieldError) *Error {
	c := e.clone()
	c.Fields = append(c.Fields, fields...)
	return c
}

// Wrap returns a copy that records an internal cause. The cause is logged but
// never sent to clients.
func (e *Error) Wrap(err error) *Error { c := e.clone(); c.Err = err; return c }

// WithRetryAfter returns a copy that tells clients when to retry.
func (e *Error) WithRetryAfter(d time.Duration) *Error { c := e.clone(); c.RetryAfter = d; return c }

// RetryAfterHeader returns RetryAfter in whole seconds (at least 1).
func (e *Error) RetryAfterHeader() string {
	secs := int64((e.RetryAfter + time.Second - 1) / time.Second)
	if secs < 1 {
		secs = 1
	}
	return strconv.FormatInt(secs, 10)
}

// New creates a custom application error.
func New(status int, code, title string) *Error {
	return &Error{Status: status, Code: code, Title: title}
}

// Sentinels for common cases. Feature modules add their own codes
// (e.g. BOOKING_OVERLAP) with New.
var (
	ErrBadRequest         = New(http.StatusBadRequest, "BAD_REQUEST", "The request could not be understood")
	ErrValidation         = New(http.StatusBadRequest, "VALIDATION_FAILED", "One or more fields are invalid")
	ErrUnauthenticated    = New(http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required")
	ErrForbidden          = New(http.StatusForbidden, "FORBIDDEN", "You do not have permission to do this")
	ErrNotFound           = New(http.StatusNotFound, "NOT_FOUND", "The requested resource was not found")
	ErrMethodNotAllowed   = New(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "This method is not allowed on this resource")
	ErrConflict           = New(http.StatusConflict, "CONFLICT", "The request conflicts with the current state")
	ErrPayloadTooLarge    = New(http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "The request body is too large")
	ErrRateLimited        = New(http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests, please slow down")
	ErrInternal           = New(http.StatusInternalServerError, "INTERNAL", "Something went wrong on our side")
	ErrServiceUnavailable = New(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "The service is temporarily unavailable")

	// Domain-wide codes from docs/API_DESIGN.md §1.
	ErrInvalidTransition   = New(http.StatusConflict, "INVALID_STATE_TRANSITION", "This action is not allowed in the current state")
	ErrBookingOverlap      = New(http.StatusConflict, "BOOKING_OVERLAP", "Equipment not available for the selected dates")
	ErrVersionMismatch     = New(http.StatusPreconditionFailed, "VERSION_MISMATCH", "The resource was changed by someone else; reload and retry")
	ErrBusinessRule        = New(http.StatusUnprocessableEntity, "BUSINESS_RULE_VIOLATION", "The request breaks a business rule")
	ErrKYCRequired         = New(http.StatusUnprocessableEntity, "KYC_REQUIRED", "Complete verification before doing this")
	ErrOrgContextRequired  = New(http.StatusBadRequest, "ORG_CONTEXT_REQUIRED", "Select the organisation to act for with the X-Org-Id header")
	ErrIdempotencyRequired = New(http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", "This request needs an Idempotency-Key header")
	ErrIdempotencyMismatch = New(http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED", "This Idempotency-Key was used with a different request")
	ErrIdempotencyInFlight = New(http.StatusConflict, "IDEMPOTENCY_IN_PROGRESS", "A request with this Idempotency-Key is still being processed")
)

// From converts any error into an *Error. Unknown errors become ErrInternal
// with the original error kept as the (non-public) cause.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	if appErr, ok := errors.AsType[*Error](err); ok {
		return appErr
	}
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return ErrPayloadTooLarge.Wrap(err)
	}
	return ErrInternal.Wrap(err)
}
