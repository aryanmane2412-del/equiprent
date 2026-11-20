package users

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/modules/auth"
)

// Repository is the persistence the users service needs.
type Repository interface {
	User(ctx context.Context, id uuid.UUID) (db.User, error)
	UpdateProfile(ctx context.Context, p db.UpdateUserProfileParams) (db.User, error)
	Roles(ctx context.Context, userID uuid.UUID) ([]httpx.Role, error)
	Organization(ctx context.Context, id uuid.UUID) (Organization, error)
	CreateOrganization(ctx context.Context, p db.CreateOrganizationParams, creator uuid.UUID) (Organization, error)
	UpdateOrganization(ctx context.Context, p db.UpdateOrganizationParams) (Organization, error)
	Members(ctx context.Context, orgID uuid.UUID) ([]Member, error)
	AddMember(ctx context.Context, orgID, grantedBy uuid.UUID, req AddMemberRequest) (Member, error)
	RemoveMember(ctx context.Context, orgID, userID uuid.UUID, role string) (bool, error)
	Addresses(ctx context.Context, orgID uuid.UUID) ([]Address, error)
	CreateAddress(ctx context.Context, orgID uuid.UUID, req CreateAddressRequest) (Address, error)
	DeleteAddress(ctx context.Context, orgID, id uuid.UUID) (bool, error)
}

// PGRepository implements Repository on PostgreSQL.
type PGRepository struct {
	pool *pgxpool.Pool
	tx   database.Transactor
}

func NewRepository(pool *pgxpool.Pool, tx database.Transactor) *PGRepository {
	return &PGRepository{pool: pool, tx: tx}
}

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

func (r *PGRepository) User(ctx context.Context, id uuid.UUID) (db.User, error) {
	u, err := r.q(ctx).GetUserByID(ctx, id)
	return u, database.MapError(err)
}

func (r *PGRepository) UpdateProfile(ctx context.Context, p db.UpdateUserProfileParams) (db.User, error) {
	u, err := r.q(ctx).UpdateUserProfile(ctx, p)
	return u, database.MapError(err)
}

func (r *PGRepository) Roles(ctx context.Context, userID uuid.UUID) ([]httpx.Role, error) {
	return auth.LoadRoles(ctx, r.q(ctx), userID)
}

func (r *PGRepository) Organization(ctx context.Context, id uuid.UUID) (Organization, error) {
	o, err := r.q(ctx).GetOrganization(ctx, id)
	if err != nil {
		return Organization{}, database.MapError(err)
	}
	return ToOrganization(o), nil
}

func (r *PGRepository) CreateOrganization(ctx context.Context, p db.CreateOrganizationParams, creator uuid.UUID) (Organization, error) {
	var out Organization
	err := r.tx.InTxAs(ctx, database.Actor{UserID: creator}, func(ctx context.Context) error {
		q := r.q(ctx)
		o, err := q.CreateOrganization(ctx, p)
		if err != nil {
			return err
		}
		for _, role := range []string{httpx.RoleOrgAdmin, httpx.RoleCustomer} {
			if _, err := q.GrantRole(ctx, db.GrantRoleParams{
				UserID: creator, RoleCode: role,
				OrganizationID: uuid.NullUUID{UUID: o.ID, Valid: true},
				GrantedBy:      uuid.NullUUID{UUID: creator, Valid: true},
			}); err != nil {
				return err
			}
		}
		out = ToOrganization(o)
		return nil
	})
	return out, database.MapError(err)
}

func (r *PGRepository) UpdateOrganization(ctx context.Context, p db.UpdateOrganizationParams) (Organization, error) {
	o, err := r.q(ctx).UpdateOrganization(ctx, p)
	if err != nil {
		return Organization{}, database.MapError(err)
	}
	return ToOrganization(o), nil
}

func (r *PGRepository) Members(ctx context.Context, orgID uuid.UUID) ([]Member, error) {
	rows, err := r.q(ctx).ListOrgMembers(ctx, uuid.NullUUID{UUID: orgID, Valid: true})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Member, 0, len(rows))
	for _, m := range rows {
		out = append(out, Member{UserID: m.UserID, FullName: m.FullName, Phone: m.Phone, Role: m.RoleCode, GrantedAt: m.GrantedAt})
	}
	return out, nil
}

// AddMember grants a role to the user with this phone, creating an account
// (activated on first OTP login) if needed. Operators also get a profile
// with this organisation as employer.
func (r *PGRepository) AddMember(ctx context.Context, orgID, grantedBy uuid.UUID, req AddMemberRequest) (Member, error) {
	var out Member
	err := r.tx.InTxAs(ctx, database.Actor{UserID: grantedBy, Role: httpx.RoleOrgAdmin}, func(ctx context.Context) error {
		q := r.q(ctx)
		u, err := q.GetUserByPhone(ctx, req.Phone)
		if err != nil {
			if !errors.Is(database.MapError(err), apperror.ErrNotFound) {
				return err
			}
			u, err = q.CreateUser(ctx, db.CreateUserParams{Phone: req.Phone, FullName: req.FullName, PreferredLanguage: "en"})
			if err != nil {
				return err
			}
		}
		g, err := q.GrantRole(ctx, db.GrantRoleParams{
			UserID: u.ID, RoleCode: req.Role,
			OrganizationID: uuid.NullUUID{UUID: orgID, Valid: true},
			GrantedBy:      uuid.NullUUID{UUID: grantedBy, Valid: true},
		})
		if err != nil {
			return err
		}
		if req.Role == httpx.RoleOperator {
			if _, err := q.UpsertOperatorProfile(ctx, db.UpsertOperatorProfileParams{
				UserID: u.ID, EmployerOrganizationID: uuid.NullUUID{UUID: orgID, Valid: true},
			}); err != nil {
				return err
			}
		}
		if req.Role == httpx.RoleOwner || req.Role == httpx.RoleFleetManager {
			if err := q.MarkOrgSupplier(ctx, orgID); err != nil {
				return err
			}
		}
		out = Member{UserID: u.ID, FullName: u.FullName, Phone: u.Phone, Role: g.RoleCode, GrantedAt: g.GrantedAt}
		return nil
	})
	return out, database.MapError(err)
}

func (r *PGRepository) RemoveMember(ctx context.Context, orgID, userID uuid.UUID, role string) (bool, error) {
	n, err := r.q(ctx).RevokeRole(ctx, db.RevokeRoleParams{
		UserID: userID, RoleCode: role, OrganizationID: uuid.NullUUID{UUID: orgID, Valid: true},
	})
	return n > 0, database.MapError(err)
}

func (r *PGRepository) Addresses(ctx context.Context, orgID uuid.UUID) ([]Address, error) {
	rows, err := r.q(ctx).ListOrgAddresses(ctx, orgID)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Address, 0, len(rows))
	for _, a := range rows {
		out = append(out, ToAddress(db.GetAddressRow(a)))
	}
	return out, nil
}

func (r *PGRepository) CreateAddress(ctx context.Context, orgID uuid.UUID, req CreateAddressRequest) (Address, error) {
	p := db.InsertAddressParams{
		OrganizationID: orgID, Label: req.Label, Line1: req.Line1, Line2: req.Line2, Locality: req.Locality,
		City: req.City, District: req.District, StateCode: req.StateCode, Pincode: req.Pincode, IsSite: req.IsSite,
	}
	if req.Location != nil {
		p.Lat, p.Lng = &req.Location.Lat, &req.Location.Lng
	}
	row, err := r.q(ctx).InsertAddress(ctx, p)
	if err != nil {
		return Address{}, database.MapError(err)
	}
	a, err := r.q(ctx).GetAddress(ctx, row.ID)
	if err != nil {
		return Address{}, database.MapError(err)
	}
	return ToAddress(a), nil
}

func (r *PGRepository) DeleteAddress(ctx context.Context, orgID, id uuid.UUID) (bool, error) {
	n, err := r.q(ctx).DeleteAddress(ctx, db.DeleteAddressParams{ID: id, OrganizationID: orgID})
	return n == 1, database.MapError(err)
}

// ToOrganization maps the database row to the API model.
func ToOrganization(o db.Organization) Organization {
	return Organization{
		ID: o.ID, LegalName: o.LegalName, DisplayName: o.DisplayName, OrgType: string(o.OrgType),
		IsBuyer: o.IsBuyer, IsSupplier: o.IsSupplier, GSTIN: o.Gstin, GSTStateCode: o.GstStateCode,
		KYCStatus: string(o.KycStatus), RatingAvg: o.RatingAvg, RatingCount: o.RatingCount, CreatedAt: o.CreatedAt,
	}
}

// ToAddress maps the database row to the API model.
func ToAddress(a db.GetAddressRow) Address {
	out := Address{
		ID: a.ID, OrganizationID: a.OrganizationID, Label: a.Label, Line1: a.Line1, Line2: a.Line2,
		Locality: a.Locality, City: a.City, District: a.District, StateCode: a.StateCode, Pincode: a.Pincode,
		IsSite: a.IsSite, CreatedAt: a.CreatedAt,
	}
	if a.HasLocation {
		out.Location = &GeoPoint{Lat: a.Lat, Lng: a.Lng}
	}
	return out
}
