package users

import (
	"context"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/httpx"
)

var gstinPattern = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$`)

// Service implements profile and organisation use cases.
type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

// Me returns the caller's profile with roles.
func (s *Service) Me(ctx context.Context, p *httpx.Principal) (Profile, error) {
	u, err := s.repo.User(ctx, p.UserID)
	if err != nil {
		return Profile{}, err
	}
	return toProfile(u, p.Roles), nil
}

// UpdateMe changes profile fields.
func (s *Service) UpdateMe(ctx context.Context, p *httpx.Principal, req UpdateProfileRequest) (Profile, error) {
	if req.FullName != nil {
		trimmed := strings.TrimSpace(*req.FullName)
		req.FullName = &trimmed
	}
	if req.Email != nil {
		lower := strings.ToLower(strings.TrimSpace(*req.Email))
		req.Email = &lower
	}
	u, err := s.repo.UpdateProfile(ctx, db.UpdateUserProfileParams{
		ID: p.UserID, FullName: req.FullName, Email: req.Email, PreferredLanguage: req.PreferredLanguage,
	})
	if err != nil {
		return Profile{}, err
	}
	return toProfile(u, p.Roles), nil
}

// MyOrgs lists the caller's organisations with their roles in each.
func (s *Service) MyOrgs(ctx context.Context, p *httpx.Principal) (OrgList, error) {
	out := OrgList{Data: []OrgMembership{}}
	for _, id := range p.Orgs() {
		org, err := s.repo.Organization(ctx, id)
		if err != nil {
			return OrgList{}, err
		}
		m := OrgMembership{Organization: org}
		for _, r := range p.Roles {
			if r.OrgID == id {
				m.Roles = append(m.Roles, r.Code)
			}
		}
		out.Data = append(out.Data, m)
	}
	return out, nil
}

// CreateOrg creates a business; the caller becomes its admin and customer.
func (s *Service) CreateOrg(ctx context.Context, p *httpx.Principal, req CreateOrgRequest) (Organization, error) {
	params := db.CreateOrganizationParams{
		LegalName: strings.TrimSpace(req.LegalName), DisplayName: strings.TrimSpace(req.DisplayName),
		OrgType: db.OrgType(req.OrgType), IsBuyer: true, CreatedBy: uuid.NullUUID{UUID: p.UserID, Valid: true},
	}
	if req.GSTIN != nil {
		gstin, state, err := normaliseGSTIN(*req.GSTIN)
		if err != nil {
			return Organization{}, err
		}
		params.Gstin, params.GstStateCode = &gstin, &state
	}
	return s.repo.CreateOrganization(ctx, params, p.UserID)
}

// Org returns an organisation the caller belongs to.
func (s *Service) Org(ctx context.Context, p *httpx.Principal, orgID uuid.UUID) (Organization, error) {
	if !p.IsMember(orgID) && !p.IsAdmin() {
		return Organization{}, apperror.ErrNotFound
	}
	return s.repo.Organization(ctx, orgID)
}

// UpdateOrg edits business details (org_admin).
func (s *Service) UpdateOrg(ctx context.Context, p *httpx.Principal, orgID uuid.UUID, req UpdateOrgRequest) (Organization, error) {
	if err := requireOrgRole(p, orgID, httpx.RoleOrgAdmin); err != nil {
		return Organization{}, err
	}
	params := db.UpdateOrganizationParams{ID: orgID, LegalName: req.LegalName, DisplayName: req.DisplayName}
	if req.GSTIN != nil {
		gstin, state, err := normaliseGSTIN(*req.GSTIN)
		if err != nil {
			return Organization{}, err
		}
		params.Gstin, params.GstStateCode = &gstin, &state
	}
	return s.repo.UpdateOrganization(ctx, params)
}

// Members lists the organisation's members (org_admin, fleet_manager).
func (s *Service) Members(ctx context.Context, p *httpx.Principal, orgID uuid.UUID) (MemberList, error) {
	if err := requireOrgRole(p, orgID, httpx.RoleOrgAdmin, httpx.RoleFleetManager); err != nil {
		return MemberList{}, err
	}
	list, err := s.repo.Members(ctx, orgID)
	return MemberList{Data: list}, err
}

// AddMember invites a person by phone with a role (org_admin; fleet
// managers may add operators).
func (s *Service) AddMember(ctx context.Context, p *httpx.Principal, orgID uuid.UUID, req AddMemberRequest) (Member, error) {
	allowed := p.HasOrgRole(orgID, httpx.RoleOrgAdmin) ||
		(req.Role == httpx.RoleOperator && p.HasOrgRole(orgID, httpx.RoleFleetManager))
	if !allowed && !p.HasPlatformRole(httpx.RoleAdminSupport) {
		return Member{}, apperror.ErrForbidden.WithDetail("only organisation admins can add members")
	}
	return s.repo.AddMember(ctx, orgID, p.UserID, req)
}

// RemoveMember revokes one role. Admins cannot remove their own admin role,
// which would leave the organisation without one.
func (s *Service) RemoveMember(ctx context.Context, p *httpx.Principal, orgID, userID uuid.UUID, role string) error {
	if err := requireOrgRole(p, orgID, httpx.RoleOrgAdmin); err != nil {
		return err
	}
	if userID == p.UserID && role == httpx.RoleOrgAdmin {
		return apperror.ErrBusinessRule.WithDetail("you cannot remove your own admin role")
	}
	ok, err := s.repo.RemoveMember(ctx, orgID, userID, role)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.ErrNotFound.WithDetail("this member does not hold that role")
	}
	return nil
}

// Addresses lists saved addresses (members).
func (s *Service) Addresses(ctx context.Context, p *httpx.Principal, orgID uuid.UUID) (AddressList, error) {
	if !p.IsMember(orgID) {
		return AddressList{}, apperror.ErrNotFound
	}
	list, err := s.repo.Addresses(ctx, orgID)
	return AddressList{Data: list}, err
}

// CreateAddress saves a site or yard (members).
func (s *Service) CreateAddress(ctx context.Context, p *httpx.Principal, orgID uuid.UUID, req CreateAddressRequest) (Address, error) {
	if !p.IsMember(orgID) {
		return Address{}, apperror.ErrNotFound
	}
	if req.IsSite && req.Location == nil {
		return Address{}, apperror.ErrValidation.WithFields(apperror.FieldError{
			Field: "location", Code: "required", Message: "work sites need a map location",
		})
	}
	return s.repo.CreateAddress(ctx, orgID, req)
}

// DeleteAddress removes a saved address (members).
func (s *Service) DeleteAddress(ctx context.Context, p *httpx.Principal, orgID, id uuid.UUID) error {
	if !p.IsMember(orgID) {
		return apperror.ErrNotFound
	}
	ok, err := s.repo.DeleteAddress(ctx, orgID, id)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.ErrNotFound
	}
	return nil
}

func requireOrgRole(p *httpx.Principal, orgID uuid.UUID, roles ...string) error {
	if p.HasOrgRole(orgID, roles...) || p.HasPlatformRole(httpx.RoleAdminSupport) {
		return nil
	}
	if !p.IsMember(orgID) {
		return apperror.ErrNotFound
	}
	return apperror.ErrForbidden.WithDetail("your role in this organisation does not allow this")
}

// normaliseGSTIN upper-cases and validates a GSTIN and returns its state code.
func normaliseGSTIN(raw string) (gstin, state string, err error) {
	gstin = strings.ToUpper(strings.TrimSpace(raw))
	if !gstinPattern.MatchString(gstin) {
		return "", "", apperror.ErrValidation.WithFields(apperror.FieldError{
			Field: "gstin", Code: "invalid", Message: "is not a valid GSTIN",
		})
	}
	return gstin, gstin[:2], nil
}

func toProfile(u db.User, roles []httpx.Role) Profile {
	if roles == nil {
		roles = []httpx.Role{}
	}
	return Profile{
		ID: u.ID, Phone: u.Phone, Email: u.Email, FullName: u.FullName, PreferredLanguage: u.PreferredLanguage,
		Status: string(u.Status), CreatedAt: u.CreatedAt, Roles: roles, AppRoles: httpx.AppRoles(roles),
		PhoneVerified: u.PhoneVerifiedAt != nil, EmailVerified: u.EmailVerifiedAt != nil,
		HasPassword: u.PasswordHash != nil && *u.PasswordHash != "",
	}
}
