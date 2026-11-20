package apperror

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestCustomisedSentinelStillMatches(t *testing.T) {
	err := ErrNotFound.WithDetail("equipment 42 not found")
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("customised copy should match its sentinel")
	}
	if errors.Is(err, ErrConflict) {
		t.Fatal("must not match a different code")
	}
}

func TestHelpersDoNotMutateSentinels(t *testing.T) {
	_ = ErrValidation.WithFields(FieldError{Field: "phone", Code: "invalid"}).WithDetail("x")
	if ErrValidation.Detail != "" || len(ErrValidation.Fields) != 0 {
		t.Fatalf("sentinel was mutated: %+v", ErrValidation)
	}
}

func TestFromWrapsUnknownErrorsAsInternal(t *testing.T) {
	cause := errors.New("db exploded")
	got := From(fmt.Errorf("query: %w", cause))
	if got.Status != http.StatusInternalServerError || got.Code != "INTERNAL" {
		t.Fatalf("got %+v", got)
	}
	if !errors.Is(got, cause) {
		t.Fatal("cause should be preserved for logging")
	}
}

func TestFromKeepsAppErrors(t *testing.T) {
	in := ErrForbidden.WithDetail("not your booking")
	if got := From(fmt.Errorf("wrapped: %w", in)); got != in {
		t.Fatalf("expected same *Error back, got %+v", got)
	}
}

func TestFromMapsMaxBytesError(t *testing.T) {
	got := From(&http.MaxBytesError{Limit: 10})
	if got.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", got.Status)
	}
}
