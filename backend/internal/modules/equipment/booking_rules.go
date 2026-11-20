package equipment

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/modules/bookings"
)

// BookingRules are the owner's rental rules for one machine. Amounts are paise.
type BookingRules struct {
	MinRentalHours       *int32               `json:"min_rental_hours,omitempty" doc:"Shortest rental (hours)"`
	MaxRentalDays        *int32               `json:"max_rental_days,omitempty" doc:"Longest rental (days, up to 180)"`
	SecurityDepositPaise *int64               `json:"security_deposit_paise,omitempty" doc:"Refundable deposit; empty = platform default"`
	FuelPolicy           string               `json:"fuel_policy" doc:"customer (customer fuels), owner (owner supplies, charged per working hour) or shared"`
	FuelRatePerHourPaise *int64               `json:"fuel_rate_per_hour_paise,omitempty"`
	LateFeePerHourPaise  *int64               `json:"late_fee_per_hour_paise,omitempty" doc:"Empty = the hourly rate (or 1/8 of the daily rate)"`
	LateGraceMinutes     int32                `json:"late_grace_minutes"`
	PickupFixedPaise     int64                `json:"pickup_fixed_paise" doc:"Return-trip charge (0 = included in delivery)"`
	PickupPerKmPaise     int64                `json:"pickup_per_km_paise"`
	Timezone             string               `json:"timezone" doc:"IANA zone, e.g. Asia/Kolkata: customers pick dates in it"`
	Cancellation         CancellationSettings `json:"cancellation"`
}

// CancellationSettings is a preset or the owner's own rules.
type CancellationSettings struct {
	Policy string                      `json:"policy" doc:"A preset code (flexible, standard, strict) or custom; empty = platform default"`
	Name   string                      `json:"name,omitempty"`
	Rules  []bookings.CancellationRule `json:"rules,omitempty" doc:"Required for custom: fee in basis points of the estimate by hours of notice"`
}

// BookingRulesRequest replaces the rules (send every field).
type BookingRulesRequest struct {
	MinRentalHours       *int32               `json:"min_rental_hours" binding:"omitempty,min=1,max=4320"`
	MaxRentalDays        *int32               `json:"max_rental_days" binding:"omitempty,min=1,max=180"`
	SecurityDepositPaise *int64               `json:"security_deposit_paise" binding:"omitempty,min=0,max=100000000"`
	FuelPolicy           string               `json:"fuel_policy" binding:"required,oneof=customer owner shared"`
	FuelRatePerHourPaise *int64               `json:"fuel_rate_per_hour_paise" binding:"omitempty,min=0,max=10000000"`
	LateFeePerHourPaise  *int64               `json:"late_fee_per_hour_paise" binding:"omitempty,min=0,max=10000000"`
	LateGraceMinutes     *int32               `json:"late_grace_minutes" binding:"omitempty,min=0,max=720"`
	PickupFixedPaise     int64                `json:"pickup_fixed_paise" binding:"min=0,max=100000000"`
	PickupPerKmPaise     int64                `json:"pickup_per_km_paise" binding:"min=0,max=1000000"`
	Timezone             string               `json:"timezone" binding:"omitempty,max=60"`
	Cancellation         CancellationSettings `json:"cancellation"`
}

// CancellationPreset is a platform policy owners can choose.
type CancellationPreset struct {
	Code      string                      `json:"code"`
	Name      string                      `json:"name"`
	Rules     []bookings.CancellationRule `json:"rules"`
	IsDefault bool                        `json:"is_default"`
}

type CancellationPresetList struct {
	Data []CancellationPreset `json:"data"`
}

// RulesRepository is the persistence for booking rules (PGRepository).
type RulesRepository interface {
	BookingRules(ctx context.Context, id uuid.UUID) (db.GetBookingRulesRow, error)
	SaveBookingRules(ctx context.Context, id, owner uuid.UUID, req BookingRulesRequest) error
	CancellationPresets(ctx context.Context) ([]db.ListCancellationPresetsRow, error)
}

func (r *PGRepository) BookingRules(ctx context.Context, id uuid.UUID) (db.GetBookingRulesRow, error) {
	row, err := r.q(ctx).GetBookingRules(ctx, id)
	return row, database.MapError(err)
}

func (r *PGRepository) CancellationPresets(ctx context.Context) ([]db.ListCancellationPresetsRow, error) {
	rows, err := r.q(ctx).ListCancellationPresets(ctx)
	return rows, database.MapError(err)
}

// SaveBookingRules writes the rules, the pickup rates and (for custom
// cancellation rules) a new policy owned by the organisation, atomically.
func (r *PGRepository) SaveBookingRules(ctx context.Context, id, owner uuid.UUID, req BookingRulesRequest) error {
	return r.tx.InTx(ctx, func(ctx context.Context) error {
		q := r.q(ctx)
		policy := uuid.NullUUID{}
		switch req.Cancellation.Policy {
		case "":
		case "custom":
			raw, _ := json.Marshal(req.Cancellation.Rules)
			name := strings.TrimSpace(req.Cancellation.Name)
			if name == "" {
				name = "Owner's policy"
			}
			pid, err := q.CreateCustomCancellationPolicy(ctx, db.CreateCustomCancellationPolicyParams{
				Code: "custom_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16], Name: name, Rules: raw,
				OwnerOrgID: uuid.NullUUID{UUID: owner, Valid: true},
			})
			if err != nil {
				return database.MapError(err)
			}
			policy = uuid.NullUUID{UUID: pid, Valid: true}
		default:
			pid, err := q.GetCancellationPresetByCode(ctx, req.Cancellation.Policy)
			if err != nil {
				if errors.Is(database.MapError(err), apperror.ErrNotFound) {
					return apperror.ErrValidation.WithFields(apperror.FieldError{Field: "cancellation.policy", Code: "unknown", Message: "unknown cancellation policy"})
				}
				return database.MapError(err)
			}
			policy = uuid.NullUUID{UUID: pid, Valid: true}
		}
		grace := int32(60)
		if req.LateGraceMinutes != nil {
			grace = *req.LateGraceMinutes
		}
		n, err := q.SetBookingRules(ctx, db.SetBookingRulesParams{
			ID: id, MinRentalHours: req.MinRentalHours, MaxRentalDays: req.MaxRentalDays, SecurityDepositPaise: req.SecurityDepositPaise,
			FuelRatePerHourPaise: req.FuelRatePerHourPaise, LateFeePerHourPaise: req.LateFeePerHourPaise, LateGraceMinutes: grace,
			Timezone: req.Timezone, FuelPolicy: db.FuelPolicy(req.FuelPolicy), CancellationPolicyID: policy,
		})
		if err != nil {
			return database.MapError(err)
		}
		if n == 0 {
			return apperror.ErrNotFound
		}
		return database.MapError(q.UpsertPickupRates(ctx, db.UpsertPickupRatesParams{
			EquipmentID: id, PickupFixedPaise: req.PickupFixedPaise, PickupPerKmPaise: req.PickupPerKmPaise,
		}))
	})
}

func (s *Service) rules() (RulesRepository, error) {
	r, ok := s.repo.(RulesRepository)
	if !ok {
		return nil, errors.New("equipment: repository has no booking rules")
	}
	return r, nil
}

// BookingRules returns a machine's rules (owner).
func (s *Service) BookingRules(ctx context.Context, owner, id uuid.UUID) (BookingRules, error) {
	if _, err := s.Mine(ctx, owner, id); err != nil {
		return BookingRules{}, err
	}
	r, err := s.rules()
	if err != nil {
		return BookingRules{}, err
	}
	row, err := r.BookingRules(ctx, id)
	if err != nil {
		return BookingRules{}, err
	}
	out := BookingRules{
		MinRentalHours: row.MinRentalHours, MaxRentalDays: row.MaxRentalDays, SecurityDepositPaise: row.SecurityDepositPaise,
		FuelPolicy: row.FuelPolicy, FuelRatePerHourPaise: row.FuelRatePerHourPaise, LateFeePerHourPaise: row.LateFeePerHourPaise,
		LateGraceMinutes: row.LateGraceMinutes, PickupFixedPaise: row.PickupFixedPaise, PickupPerKmPaise: row.PickupPerKmPaise,
		Timezone: row.Timezone,
	}
	if row.PolicyCode != "" {
		out.Cancellation.Name = row.PolicyName
		out.Cancellation.Policy = row.PolicyCode
		if row.PolicyCustom {
			out.Cancellation.Policy = "custom"
		}
		_ = json.Unmarshal(row.PolicyRules, &out.Cancellation.Rules)
	}
	return out, nil
}

// SetBookingRules validates and saves a machine's rules (owner).
func (s *Service) SetBookingRules(ctx context.Context, owner, id uuid.UUID, req BookingRulesRequest) (BookingRules, error) {
	if _, err := s.Mine(ctx, owner, id); err != nil {
		return BookingRules{}, err
	}
	bad := func(f, code, msg string) error {
		return apperror.ErrValidation.WithFields(apperror.FieldError{Field: f, Code: code, Message: msg})
	}
	if req.Timezone == "" {
		req.Timezone = "Asia/Kolkata"
	}
	if _, err := time.LoadLocation(req.Timezone); err != nil || !strings.Contains(req.Timezone, "/") {
		return BookingRules{}, bad("timezone", "timezone", "use an IANA time zone such as Asia/Kolkata")
	}
	if req.MinRentalHours != nil && req.MaxRentalDays != nil && *req.MinRentalHours > *req.MaxRentalDays*24 {
		return BookingRules{}, bad("min_rental_hours", "lte", "the minimum is longer than the maximum")
	}
	if req.FuelPolicy == "owner" && (req.FuelRatePerHourPaise == nil || *req.FuelRatePerHourPaise == 0) {
		return BookingRules{}, bad("fuel_rate_per_hour_paise", "required", "set a fuel charge per working hour when you supply fuel")
	}
	switch req.Cancellation.Policy {
	case "custom":
		if err := bookings.ValidateCancellationRules(req.Cancellation.Rules); err != nil {
			return BookingRules{}, bad("cancellation.rules", "invalid", err.Error())
		}
	default:
		if len(req.Cancellation.Rules) > 0 {
			return BookingRules{}, bad("cancellation.rules", "custom_only", "rules are only sent with policy custom")
		}
	}
	r, err := s.rules()
	if err != nil {
		return BookingRules{}, err
	}
	if err := r.SaveBookingRules(ctx, id, owner, req); err != nil {
		return BookingRules{}, err
	}
	return s.BookingRules(ctx, owner, id)
}

// CancellationPresets lists the platform policies.
func (s *Service) CancellationPresets(ctx context.Context) (CancellationPresetList, error) {
	r, err := s.rules()
	if err != nil {
		return CancellationPresetList{}, err
	}
	rows, err := r.CancellationPresets(ctx)
	if err != nil {
		return CancellationPresetList{}, err
	}
	out := CancellationPresetList{Data: make([]CancellationPreset, 0, len(rows))}
	for _, p := range rows {
		x := CancellationPreset{Code: p.Code, Name: p.Name, IsDefault: p.IsDefault}
		_ = json.Unmarshal(p.Rules, &x.Rules)
		out.Data = append(out.Data, x)
	}
	return out, nil
}
