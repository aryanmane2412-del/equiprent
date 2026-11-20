package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/modules/equipment"
	"github.com/google/uuid"
)

func TestAdminMutationServicesAreReadOnly(t *testing.T) {
	s := &Service{}
	checks := []struct {
		name string
		call func() error
	}{
		{"change user status", func() error {
			_, err := s.SetUserStatus(context.Background(), nil, uuid.Nil, "suspended", "reason")
			return err
		}},
		{"grant role", func() error {
			_, err := s.GrantRole(context.Background(), nil, uuid.Nil, GrantRoleRequest{})
			return err
		}},
		{"moderate listing", func() error {
			_, err := s.Moderate(context.Background(), nil, uuid.Nil, equipment.ActionApprove, "reason")
			return err
		}},
		{"transition booking", func() error {
			_, err := s.TransitionBooking(context.Background(), nil, uuid.Nil, BookingTransitionRequest{})
			return err
		}},
		{"create catalogue model", func() error { _, err := s.CreateModel(context.Background(), nil, CreateModelRequest{}); return err }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); !errors.Is(err, apperror.ErrForbidden) {
				t.Fatalf("mutation returned %v, want forbidden", err)
			}
		})
	}
}

func TestReportRangeRequiresValidUTCWindow(t *testing.T) {
	from, to, err := reportRange(ReportRangeQuery{From: "2026-01-01T05:30:00+05:30", To: "2026-01-02T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if from.Location() != time.UTC || to.Location() != time.UTC {
		t.Fatalf("range locations must be UTC, got %s and %s", from.Location(), to.Location())
	}
	if got := from.Format(time.RFC3339); got != "2026-01-01T00:00:00Z" {
		t.Fatalf("normalized from = %s", got)
	}
}

func TestReportRangeRejectsPartialReversedAndLongRanges(t *testing.T) {
	for name, q := range map[string]ReportRangeQuery{
		"partial":  {From: "2026-01-01T00:00:00Z"},
		"reversed": {From: "2026-01-02T00:00:00Z", To: "2026-01-01T00:00:00Z"},
		"too long": {From: "2025-01-01T00:00:00Z", To: "2026-01-10T00:00:00Z"},
		"invalid":  {From: "yesterday", To: "2026-01-02T00:00:00Z"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := reportRange(q); err == nil {
				t.Fatal("expected an invalid date range error")
			}
		})
	}
}
