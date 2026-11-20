package httpx

import (
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
)

// Role is one active role assignment. OrgID is uuid.Nil for platform roles.
type Role struct {
	Code    string    `json:"role"`
	OrgID   uuid.UUID `json:"organization_id,omitempty"`
	OrgName string    `json:"organization_name,omitempty"`
}

// Platform role codes (docs/SECURITY.md).
const (
	RoleAdminSuper   = "admin_super"
	RoleAdminOps     = "admin_ops"
	RoleAdminKYC     = "admin_kyc"
	RoleAdminFinance = "admin_finance"
	RoleAdminSupport = "admin_support"
)

// Organisation role codes.
const (
	RoleCustomer     = "customer"
	RoleOwner        = "owner"
	RoleFleetManager = "fleet_manager"
	RoleOperator     = "operator"
	RoleOrgAdmin     = "org_admin"
)

// Principal is the authenticated caller.
type Principal struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
	Roles     []Role
	// ActingOrg is the organisation selected with X-Org-Id (or the only one
	// the user belongs to). uuid.Nil when none applies.
	ActingOrg uuid.UUID
}

// HasPlatformRole reports whether the user holds any of the platform roles.
// admin_super implicitly holds all of them.
func (p *Principal) HasPlatformRole(codes ...string) bool {
	for _, r := range p.Roles {
		if r.OrgID != uuid.Nil {
			continue
		}
		if r.Code == RoleAdminSuper || slices.Contains(codes, r.Code) {
			return true
		}
	}
	return false
}

// IsAdmin reports whether the user holds any platform (admin) role.
func (p *Principal) IsAdmin() bool {
	for _, r := range p.Roles {
		if r.OrgID == uuid.Nil {
			return true
		}
	}
	return false
}

// HasOrgRole reports whether the user holds any of codes in org.
//
// org_admin administers the organisation (members, addresses, details) and
// may buy on its behalf, so it also satisfies RoleCustomer. It does NOT imply
// owner, fleet_manager or operator: supplier capabilities need those roles
// explicitly (granted at owner registration / onboarding or by invitation),
// otherwise every customer would be an equipment owner of their own org.
func (p *Principal) HasOrgRole(org uuid.UUID, codes ...string) bool {
	if org == uuid.Nil {
		return false
	}
	adminCounts := slices.Contains(codes, RoleCustomer)
	for _, r := range p.Roles {
		if r.OrgID != org {
			continue
		}
		if slices.Contains(codes, r.Code) || (r.Code == RoleOrgAdmin && adminCounts) {
			return true
		}
	}
	return false
}

// IsMember reports whether the user holds any role in org.
func (p *Principal) IsMember(org uuid.UUID) bool {
	for _, r := range p.Roles {
		if r.OrgID == org && org != uuid.Nil {
			return true
		}
	}
	return false
}

// Orgs lists the organisations the user belongs to, without duplicates.
func (p *Principal) Orgs() []uuid.UUID {
	var out []uuid.UUID
	for _, r := range p.Roles {
		if r.OrgID != uuid.Nil && !slices.Contains(out, r.OrgID) {
			out = append(out, r.OrgID)
		}
	}
	return out
}

// RoleIn returns the most specific role code the user holds in org, used for
// audit "actor_role".
func (p *Principal) RoleIn(org uuid.UUID) string {
	for _, pref := range []string{RoleOwner, RoleFleetManager, RoleCustomer, RoleOperator, RoleOrgAdmin} {
		if slices.ContainsFunc(p.Roles, func(r Role) bool { return r.OrgID == org && r.Code == pref }) {
			return pref
		}
	}
	if p.IsAdmin() {
		return "admin"
	}
	return ""
}

const principalKey = "equiprent.principal"

// SetPrincipal stores the authenticated caller on the request.
func SetPrincipal(c *gin.Context, p *Principal) { c.Set(principalKey, p) }

// CurrentPrincipal returns the authenticated caller, if any.
func CurrentPrincipal(c *gin.Context) (*Principal, bool) {
	v, ok := c.Get(principalKey)
	if !ok {
		return nil, false
	}
	p, ok := v.(*Principal)
	return p, ok && p != nil
}

// MustPrincipal returns the caller or records 401. Use on authenticated
// routes, where the auth middleware guarantees a principal.
func MustPrincipal(c *gin.Context) (*Principal, bool) {
	p, ok := CurrentPrincipal(c)
	if !ok {
		Fail(c, apperror.ErrUnauthenticated)
	}
	return p, ok
}

// ActingOrg returns the caller and their selected organisation, requiring
// that they hold one of roles there (any role when roles is empty).
func ActingOrg(c *gin.Context, roles ...string) (*Principal, uuid.UUID, bool) {
	p, ok := MustPrincipal(c)
	if !ok {
		return nil, uuid.Nil, false
	}
	if p.ActingOrg == uuid.Nil {
		Fail(c, apperror.ErrOrgContextRequired)
		return nil, uuid.Nil, false
	}
	if len(roles) > 0 && !p.HasOrgRole(p.ActingOrg, roles...) {
		Fail(c, apperror.ErrForbidden.WithDetail("your role in this organisation does not allow this"))
		return nil, uuid.Nil, false
	}
	return p, p.ActingOrg, true
}

// App roles are the five user-facing roles. They are always derived on the
// server from the stored role assignments; a role sent by a client (header,
// body field or token claim) is never trusted. Access tokens deliberately
// carry no roles: they are loaded from the database on every request.
const (
	AppRoleCustomer       = "CUSTOMER"
	AppRoleEquipmentOwner = "EQUIPMENT_OWNER"
	AppRoleOperator       = "OPERATOR"
	AppRoleFleetManager   = "FLEET_MANAGER"
	AppRoleAdmin          = "ADMIN"
)

// AppRoles maps role assignments to app roles, in a stable order.
func AppRoles(roles []Role) []string {
	has := map[string]bool{}
	for _, r := range roles {
		switch {
		case r.OrgID == uuid.Nil:
			has[AppRoleAdmin] = true // every platform role is an admin role
		case r.Code == RoleCustomer:
			has[AppRoleCustomer] = true
		case r.Code == RoleOwner:
			has[AppRoleEquipmentOwner] = true
		case r.Code == RoleOperator:
			has[AppRoleOperator] = true
		case r.Code == RoleFleetManager:
			has[AppRoleFleetManager] = true
		}
	}
	out := []string{}
	for _, r := range []string{AppRoleCustomer, AppRoleEquipmentOwner, AppRoleOperator, AppRoleFleetManager, AppRoleAdmin} {
		if has[r] {
			out = append(out, r)
		}
	}
	return out
}
