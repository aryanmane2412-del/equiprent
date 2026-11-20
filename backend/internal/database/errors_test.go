package database

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/equiprent/backend/internal/apperror"
)

func TestMapError(t *testing.T) {
	cases := []struct {
		name   string
		in     error
		code   string
		status int
	}{
		{"no rows", fmt.Errorf("q: %w", pgx.ErrNoRows), "NOT_FOUND", 404},
		{"unique", &pgconn.PgError{Code: "23505", ConstraintName: "users_phone_uq"}, "CONFLICT", 409},
		{"booking overlap", &pgconn.PgError{Code: "23P01", ConstraintName: "bookings_no_overlap"}, "BOOKING_OVERLAP", 409},
		{"fk", &pgconn.PgError{Code: "23503"}, "VALIDATION_FAILED", 400},
		{"check", &pgconn.PgError{Code: "23514", ConstraintName: "x_ck"}, "VALIDATION_FAILED", 400},
		{"immutable", &pgconn.PgError{Code: "EQ001"}, "CONFLICT", 409},
		{"transition", &pgconn.PgError{Code: "EQ002"}, "INVALID_STATE_TRANSITION", 409},
		{"blocked", &pgconn.PgError{Code: "EQ004", Message: "equipment x is blocked during [a,b)"}, "BOOKING_OVERLAP", 409},
		{"rule", &pgconn.PgError{Code: "EQ004", Message: "something else"}, "BUSINESS_RULE_VIOLATION", 422},
		{"timeout", &pgconn.PgError{Code: "57014"}, "SERVICE_UNAVAILABLE", 503},
	}
	for _, tc := range cases {
		got := apperror.From(MapError(tc.in))
		if got.Code != tc.code || got.Status != tc.status {
			t.Errorf("%s: %s/%d, want %s/%d", tc.name, got.Code, got.Status, tc.code, tc.status)
		}
		if !errors.Is(got, tc.in) && tc.name != "no rows" {
			t.Errorf("%s: cause not kept for logging", tc.name)
		}
	}
	if d := apperror.From(MapError(&pgconn.PgError{Code: "23505", ConstraintName: "users_phone_uq"})).Detail; d == "" {
		t.Error("known constraints get a friendly detail")
	}
	if MapError(nil) != nil {
		t.Error("nil stays nil")
	}
	plain := errors.New("network")
	if MapError(plain) != plain || apperror.From(MapError(plain)).Status != http.StatusInternalServerError {
		t.Error("unknown errors pass through (become 500)")
	}
}

func TestIsRetryable(t *testing.T) {
	if !IsRetryable(&pgconn.PgError{Code: "40001"}) || !IsRetryable(fmt.Errorf("x: %w", &pgconn.PgError{Code: "40P01"})) {
		t.Error("serialization failures and deadlocks are retryable")
	}
	if IsRetryable(&pgconn.PgError{Code: "23505"}) || IsRetryable(errors.New("x")) {
		t.Error("other errors are not")
	}
}
