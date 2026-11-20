// Package owners covers the supplier side of an organisation: onboarding as
// an equipment owner, the owner dashboard and the public owner profile.
package owners

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/httpx"
)

// ---- domain ------------------------------------------------------------------------

// Profile is the owner profile of the caller.
type Profile struct {
	UserID              uuid.UUID `json:"user_id"`
	OrganizationID      uuid.UUID `json:"organization_id"`
	ResponseTimeMinutes *int32    `json:"response_time_minutes,omitempty"`
	AcceptanceRateBps   *int32    `json:"acceptance_rate_bps,omitempty"`
	PayoutHoldDays      int16     `json:"payout_hold_days"`
	CreatedAt           time.Time `json:"created_at"`
}

// Dashboard summarises an owner organisation's business.
type Dashboard struct {
	EquipmentTotal           int32 `json:"equipment_total"`
	EquipmentActive          int32 `json:"equipment_active"`
	EquipmentPendingReview   int32 `json:"equipment_pending_review"`
	BookingsAwaitingResponse int32 `json:"bookings_awaiting_response"`
	BookingsInProgress       int32 `json:"bookings_in_progress"`
	BookingsCompleted        int32 `json:"bookings_completed"`
	CapturedLast30dPaise     int64 `json:"captured_last_30d_paise"`
}

// Earnings contains settlement totals computed from the organisation's
// verified settlement ledger, never estimated booking values.
type Earnings struct {
	db.OwnerEarningsRow
	Monthly []db.OwnerMonthlyEarningsRow `json:"monthly"`
}

// PublicOwner is what customers see about a supplier.
type PublicOwner struct {
	ID                  uuid.UUID `json:"id"`
	DisplayName         string    `json:"display_name"`
	Verified            bool      `json:"verified"`
	RatingAvg           *float64  `json:"rating_avg,omitempty"`
	RatingCount         int32     `json:"rating_count"`
	ActiveListings      int32     `json:"active_listings"`
	ResponseTimeMinutes *int32    `json:"response_time_minutes,omitempty"`
	AcceptanceRateBps   *int32    `json:"acceptance_rate_bps,omitempty"`
	MemberSince         time.Time `json:"member_since"`
}

// ---- repository --------------------------------------------------------------------

type Repository interface {
	Onboard(ctx context.Context, userID, orgID uuid.UUID) (Profile, error)
	Dashboard(ctx context.Context, orgID uuid.UUID) (Dashboard, error)
	Earnings(ctx context.Context, orgID uuid.UUID) (Earnings, error)
	Public(ctx context.Context, orgID uuid.UUID) (PublicOwner, error)
}

type PGRepository struct {
	pool *pgxpool.Pool
	tx   database.Transactor
}

func NewRepository(pool *pgxpool.Pool, tx database.Transactor) *PGRepository {
	return &PGRepository{pool: pool, tx: tx}
}

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

// Onboard grants the owner role, records the owner profile and flags the
// organisation as a supplier, atomically. Re-running it is harmless.
func (r *PGRepository) Onboard(ctx context.Context, userID, orgID uuid.UUID) (Profile, error) {
	var out Profile
	err := r.tx.InTxAs(ctx, database.Actor{UserID: userID, Role: httpx.RoleOrgAdmin}, func(ctx context.Context) error {
		q := r.q(ctx)
		has, err := q.UserHasOrgRole(ctx, db.UserHasOrgRoleParams{
			UserID: userID, OrganizationID: uuid.NullUUID{UUID: orgID, Valid: true}, Roles: []string{httpx.RoleOwner},
		})
		if err != nil {
			return err
		}
		if !has {
			if _, err := q.GrantRole(ctx, db.GrantRoleParams{
				UserID: userID, RoleCode: httpx.RoleOwner,
				OrganizationID: uuid.NullUUID{UUID: orgID, Valid: true}, GrantedBy: uuid.NullUUID{UUID: userID, Valid: true},
			}); err != nil {
				return err
			}
		}
		if err := q.MarkOrgSupplier(ctx, orgID); err != nil {
			return err
		}
		p, err := q.UpsertOwnerProfile(ctx, db.UpsertOwnerProfileParams{UserID: userID, OrganizationID: orgID})
		if err != nil {
			return err
		}
		out = Profile{
			UserID: p.UserID, OrganizationID: p.OrganizationID, ResponseTimeMinutes: p.ResponseTimeMinutes,
			AcceptanceRateBps: p.AcceptanceRateBps, PayoutHoldDays: p.PayoutHoldDays, CreatedAt: p.CreatedAt,
		}
		return nil
	})
	return out, database.MapError(err)
}

func (r *PGRepository) Dashboard(ctx context.Context, orgID uuid.UUID) (Dashboard, error) {
	d, err := r.q(ctx).OwnerDashboard(ctx, orgID)
	return Dashboard(d), database.MapError(err)
}

func (r *PGRepository) Earnings(ctx context.Context, orgID uuid.UUID) (Earnings, error) {
	q := r.q(ctx)
	totals, err := q.OwnerEarnings(ctx, orgID)
	if err != nil {
		return Earnings{}, database.MapError(err)
	}
	monthly, err := q.OwnerMonthlyEarnings(ctx, orgID)
	if err != nil {
		return Earnings{}, database.MapError(err)
	}
	return Earnings{OwnerEarningsRow: totals, Monthly: monthly}, nil
}

func (r *PGRepository) Public(ctx context.Context, orgID uuid.UUID) (PublicOwner, error) {
	o, err := r.q(ctx).GetPublicOwner(ctx, orgID)
	if err != nil {
		return PublicOwner{}, database.MapError(err)
	}
	return PublicOwner{
		ID: o.ID, DisplayName: o.DisplayName, Verified: o.KycStatus == db.VerificationStatusVerified,
		RatingAvg: o.RatingAvg, RatingCount: o.RatingCount, ActiveListings: o.ActiveListings,
		ResponseTimeMinutes: o.ResponseTimeMinutes, AcceptanceRateBps: o.AcceptanceRateBps, MemberSince: o.CreatedAt,
	}, nil
}

// ---- service -----------------------------------------------------------------------

type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) Onboard(ctx context.Context, sc httpx.Scope) (Profile, error) {
	return s.repo.Onboard(ctx, sc.P.UserID, sc.Org)
}

func (s *Service) Dashboard(ctx context.Context, org uuid.UUID) (Dashboard, error) {
	return s.repo.Dashboard(ctx, org)
}

func (s *Service) Earnings(ctx context.Context, org uuid.UUID) (Earnings, error) {
	return s.repo.Earnings(ctx, org)
}

func (s *Service) Public(ctx context.Context, org uuid.UUID) (PublicOwner, error) {
	return s.repo.Public(ctx, org)
}

// ---- handler -----------------------------------------------------------------------

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Name() string { return "owners" }

func (h *Handler) Routes(r *httpx.Router) {
	r = r.Tag("owners")
	r.GET("/owners/:org_id", httpx.Handle(200, func(c *gin.Context, _ *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "org_id")
		if err != nil {
			return nil, err
		}
		return h.svc.Public(c.Request.Context(), id)
	}), httpx.Doc{Summary: "Public supplier profile", Response: PublicOwner{}})

	a := r.Authed()
	a.POST("/owners/onboard", httpx.OrgHandle(200, []string{httpx.RoleOrgAdmin}, func(c *gin.Context, s httpx.Scope) (any, error) {
		return h.svc.Onboard(c.Request.Context(), s)
	}), httpx.Doc{Summary: "Become an equipment owner (org admin)", Response: Profile{}, OrgScoped: true,
		Description: "Grants you the owner role in the acting organisation and marks it as a supplier."})
	// Organisation-wide figures: owners only (fleet managers use the scoped
	// GET /fleet/{org_id}/overview).
	a.GET("/owners/me/dashboard", httpx.OrgHandle(200, []string{httpx.RoleOwner}, func(c *gin.Context, s httpx.Scope) (any, error) {
		return h.svc.Dashboard(c.Request.Context(), s.Org)
	}), httpx.Doc{Summary: "Owner dashboard", Response: Dashboard{}, OrgScoped: true})
	a.GET("/owners/me/earnings", httpx.OrgHandle(200, []string{httpx.RoleOwner}, func(c *gin.Context, s httpx.Scope) (any, error) {
		return h.svc.Earnings(c.Request.Context(), s.Org)
	}), httpx.Doc{Summary: "Owner settlement earnings", Response: Earnings{}, OrgScoped: true,
		Description: "Amounts and monthly history are read from verified settlement records for the acting owner organisation."})
}
