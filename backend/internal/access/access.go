// Package access answers "may this caller manage this machine?" for the
// owner-side equipment endpoints, bookings, fleet views and notifications.
// Owners manage all of their organisation's machines; fleet managers only the
// machines of the fleets they manage and those granted to them directly (or
// all of them when granted all_equipment), and nothing while a
// fleet-management restriction is in force (view fleet_manager_scope).
package access

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/httpx"
)

// Equipment checks owner-side equipment access.
type Equipment interface {
	// CanManage returns nil when p may manage equipmentID of org. A fleet
	// manager without a grant gets ErrNotFound (the machine is not disclosed).
	CanManage(ctx context.Context, p *httpx.Principal, org, equipmentID uuid.UUID) error
	// Scope reports what p may see in org: all machines, or only ids.
	Scope(ctx context.Context, p *httpx.Principal, org uuid.UUID) (all bool, ids map[uuid.UUID]bool, err error)
	// Grant gives a fleet manager access to one machine (one they created).
	Grant(ctx context.Context, userID, org, equipmentID uuid.UUID) error
	// ManagementBlocked reports a fleet-management restriction on the user.
	ManagementBlocked(ctx context.Context, userID uuid.UUID) (bool, error)
}

func (c *Checker) ManagementBlocked(ctx context.Context, userID uuid.UUID) (bool, error) {
	ok, err := database.Queries(ctx, c.pool).IsRestricted(ctx, db.IsRestrictedParams{UserID: userID, Kind: "fleet_management"})
	return ok, database.MapError(err)
}

// Checker implements Equipment on PostgreSQL.
type Checker struct{ pool *pgxpool.Pool }

func NewChecker(pool *pgxpool.Pool) *Checker { return &Checker{pool: pool} }

func (c *Checker) CanManage(ctx context.Context, p *httpx.Principal, org, equipmentID uuid.UUID) error {
	if p.HasOrgRole(org, httpx.RoleOwner) {
		return nil
	}
	if !p.HasOrgRole(org, httpx.RoleFleetManager) {
		return apperror.ErrForbidden
	}
	row, err := database.Queries(ctx, c.pool).FleetManagerEquipmentAccess(ctx, db.FleetManagerEquipmentAccessParams{
		UserID: p.UserID, OrganizationID: org, EquipmentID: equipmentID,
	})
	if err != nil {
		return database.MapError(err)
	}
	if row.AllEquipment || row.Granted {
		return nil
	}
	return apperror.ErrNotFound.WithDetail("this machine is not assigned to you")
}

func (c *Checker) Scope(ctx context.Context, p *httpx.Principal, org uuid.UUID) (bool, map[uuid.UUID]bool, error) {
	if p.HasOrgRole(org, httpx.RoleOwner) {
		return true, nil, nil
	}
	if !p.HasOrgRole(org, httpx.RoleFleetManager) {
		return false, map[uuid.UUID]bool{}, nil
	}
	q := database.Queries(ctx, c.pool)
	// all_equipment, unless a fleet-management restriction is in force.
	all, err := q.FleetManagerAllEquipment(ctx, db.FleetManagerAllEquipmentParams{UserID: p.UserID, OrganizationID: org})
	if err != nil {
		return false, nil, database.MapError(err)
	}
	if all {
		return true, nil, nil
	}
	// Direct grants plus the machines of the fleets they manage.
	rows, err := q.FleetManagerScopeIDs(ctx, db.FleetManagerScopeIDsParams{UserID: p.UserID, OrganizationID: org})
	if err != nil {
		return false, nil, database.MapError(err)
	}
	ids := make(map[uuid.UUID]bool, len(rows))
	for _, id := range rows {
		ids[id] = true
	}
	return false, ids, nil
}

// Grant gives a fleet manager access to a machine they created.
func (c *Checker) Grant(ctx context.Context, userID, org, equipmentID uuid.UUID) error {
	q := database.Queries(ctx, c.pool)
	if err := q.EnsureFleetManagerProfile(ctx, db.EnsureFleetManagerProfileParams{UserID: userID, OrganizationID: org}); err != nil {
		return database.MapError(err)
	}
	_, err := q.AddFleetGrants(ctx, db.AddFleetGrantsParams{
		UserID: userID, OrganizationID: org, EquipmentIds: []uuid.UUID{equipmentID}, GrantedBy: uuid.NullUUID{UUID: userID, Valid: true},
	})
	return database.MapError(err)
}

// Guard is middleware for owner-side machine routes (":id" is the machine).
// It runs after authentication and refuses callers who may not manage the
// machine in their acting organisation. Requests without an acting
// organisation or with a malformed id fall through to the handler, which
// rejects them with the usual errors.
func Guard(eq Equipment) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := httpx.CurrentPrincipal(c)
		if !ok || p.ActingOrg == uuid.Nil {
			c.Next()
			return
		}
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.Next()
			return
		}
		if !p.HasOrgRole(p.ActingOrg, httpx.RoleOwner, httpx.RoleFleetManager) {
			c.Next() // the handler's role check answers
			return
		}
		if err := eq.CanManage(c.Request.Context(), p, p.ActingOrg, id); err != nil {
			httpx.Fail(c, err)
			c.Abort()
			return
		}
		c.Next()
	}
}

// Filter returns nil when p may see every machine of org, else the ids
// they were granted (possibly empty).
func Filter(ctx context.Context, eq Equipment, p *httpx.Principal, org uuid.UUID) ([]uuid.UUID, error) {
	all, ids, err := eq.Scope(ctx, p, org)
	if err != nil || all {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	return out, nil
}
