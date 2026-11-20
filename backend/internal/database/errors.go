package database

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/equiprent/backend/internal/apperror"
)

// MapError converts PostgreSQL errors into API errors. Repositories call it
// on every returned error, so services and handlers only see *apperror.Error
// for expected conditions. Unknown errors are returned unchanged (and become
// 500s with the cause logged, never sent).
//
// Custom SQLSTATEs raised by EQUIPRENT triggers (migration 000002):
//
//	EQ001 modification of an append-only / immutable row
//	EQ002 invalid booking/payment status transition
//	EQ003 unbalanced ledger transaction
//	EQ004 business rule violation (message is user-safe)
func MapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.ErrNotFound.Wrap(err)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23505": // unique_violation
		return apperror.ErrConflict.WithDetail(uniqueDetail(pgErr.ConstraintName)).Wrap(err)
	case "23P01": // exclusion_violation
		switch pgErr.ConstraintName {
		case "bookings_no_overlap":
			return apperror.ErrBookingOverlap.Wrap(err)
		case "availability_blocks_no_overlap":
			return apperror.ErrConflict.WithDetail("this period overlaps an existing block").Wrap(err)
		case "operator_assignments_no_overlap":
			return apperror.ErrConflict.WithDetail("the operator is already assigned to an overlapping job").Wrap(err)
		}
		return apperror.ErrConflict.Wrap(err)
	case "23503": // foreign_key_violation
		return apperror.ErrValidation.WithDetail("a referenced record does not exist").Wrap(err)
	case "23514", "23502": // check_violation, not_null_violation
		return apperror.ErrValidation.WithDetail(checkDetail(pgErr)).Wrap(err)
	case "22P02", "22007", "22008", "22003": // invalid text, datetime, numeric out of range
		return apperror.ErrBadRequest.WithDetail("a value has an invalid format").Wrap(err)
	case "EQ001":
		return apperror.ErrConflict.WithDetail("this record can no longer be changed").Wrap(err)
	case "EQ002":
		return apperror.ErrInvalidTransition.Wrap(err)
	case "EQ004":
		return businessRule(pgErr).Wrap(err)
	case "40P01", "40001": // deadlock_detected, serialization_failure: safe to retry
		return apperror.ErrConflict.WithDetail("the request collided with another change; please retry").Wrap(err)
	case "57014": // query_canceled (statement timeout / client gone)
		return apperror.ErrServiceUnavailable.Wrap(err)
	}
	return err
}

func businessRule(pgErr *pgconn.PgError) *apperror.Error {
	msg := pgErr.Message
	switch {
	case strings.Contains(msg, "is blocked during"), strings.Contains(msg, "cannot block"):
		return apperror.ErrBookingOverlap.WithDetail("the equipment is unavailable for part of this period")
	case strings.Contains(msg, "is not available for booking"):
		return apperror.ErrBusinessRule.WithDetail("this equipment is not available for booking")
	case strings.Contains(msg, "not a participant"):
		return apperror.ErrForbidden.WithDetail("you are not part of this conversation")
	case strings.Contains(msg, "cannot be reviewed yet"):
		return apperror.ErrBusinessRule.WithDetail("reviews open once the booking is completed")
	case strings.Contains(msg, "exceed payment amount"):
		return apperror.ErrBusinessRule.WithDetail("refunds cannot exceed the captured amount")
	}
	return apperror.ErrBusinessRule
}

func uniqueDetail(constraint string) string {
	switch constraint {
	case "operator_profiles_licence_uq":
		return "this licence is already registered to another operator"
	case "organizations_pan_uq":
		return "an organisation with this PAN already exists"
	case "users_phone_uq":
		return "an account with this phone number already exists"
	case "users_email_uq":
		return "an account with this email already exists"
	case "equipment_registration_uq":
		return "equipment with this registration number is already listed"
	case "organizations_gstin_uq":
		return "an organisation with this GSTIN already exists"
	case "user_roles_active_uq":
		return "this role is already granted"
	case "pricing_plans_active_uq":
		return "an active price for this unit already exists"
	case "reviews_booking_id_reviewer_org_id_subject_type_key":
		return "you have already reviewed this booking"
	case "disputes_open_uq":
		return "an open dispute of this category already exists for this booking"
	case "rental_agreements_booking_id_key":
		return "an agreement already exists for this booking"
	case "operator_assignments_one_active_uq":
		return "an operator is already assigned to this booking"
	case "trips_one_open_uq":
		return "this equipment already has an open trip"
	}
	return "the record already exists"
}

func checkDetail(pgErr *pgconn.PgError) string {
	if pgErr.ColumnName != "" {
		return "invalid value for " + pgErr.ColumnName
	}
	if pgErr.ConstraintName != "" {
		return "constraint " + pgErr.ConstraintName + " failed"
	}
	return "a value is invalid"
}
