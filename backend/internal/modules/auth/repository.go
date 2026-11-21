package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/events"
	"github.com/equiprent/backend/internal/httpx"
)

// Repository is the persistence the auth service needs.
type Repository interface {
	CreateChallenge(ctx context.Context, phone, purpose string, codeHash []byte, deviceID string, expiresAt time.Time) (Challenge, error)
	LatestChallenge(ctx context.Context, phone, purpose string) (Challenge, error)
	IncrementAttempts(ctx context.Context, id uuid.UUID) (int, error)
	ConsumeChallenge(ctx context.Context, id uuid.UUID) (bool, error)

	AccountByPhone(ctx context.Context, phone string) (Account, error)
	AccountByEmail(ctx context.Context, email string) (Account, error)
	AccountByID(ctx context.Context, id uuid.UUID) (Account, error)
	// CreateUserWithPersonalOrg creates an OTP-signup customer (no password).
	CreateUserWithPersonalOrg(ctx context.Context, phone string) (Account, error)
	// Register creates a password account with its organisation and the
	// roles of the chosen account type, atomically.
	Register(ctx context.Context, a NewAccount) (Account, error)
	MarkLoggedIn(ctx context.Context, userID uuid.UUID) error
	MarkPhoneVerified(ctx context.Context, userID uuid.UUID) error
	MarkEmailVerified(ctx context.Context, userID uuid.UUID, email string) (bool, error)
	SetPassword(ctx context.Context, userID uuid.UUID, hash string) error
	Roles(ctx context.Context, userID uuid.UUID) ([]httpx.Role, error)

	CreateToken(ctx context.Context, userID uuid.UUID, purpose string, hash []byte, email string, expiresAt time.Time) error
	ConsumeToken(ctx context.Context, hash []byte, purpose string) (uuid.UUID, string, error)
	VerifyToken(ctx context.Context, email string, hash []byte, purpose string) (uuid.UUID, bool, error)
	InvalidateTokens(ctx context.Context, userID uuid.UUID, purpose string) error

	CreateSession(ctx context.Context, s NewSession) (Session, error)
	SessionByRefreshHash(ctx context.Context, hash []byte) (Session, error)
	RevokeSession(ctx context.Context, id uuid.UUID) (bool, error)
	RevokeUserSession(ctx context.Context, id, userID uuid.UUID) (bool, error)
	RevokeFamily(ctx context.Context, familyID uuid.UUID) error
	RevokeAllSessions(ctx context.Context, userID uuid.UUID) error
	RevokeOtherSessions(ctx context.Context, userID, keep uuid.UUID) error
	ListSessions(ctx context.Context, userID uuid.UUID) ([]SessionInfo, error)
	SessionState(ctx context.Context, sessionID uuid.UUID) (SessionState, error)

	Audit(ctx context.Context, e AuditEvent) error
}

// PGRepository implements Repository on PostgreSQL.
type PGRepository struct {
	pool *pgxpool.Pool
	tx   database.Transactor
}

// NewRepository returns the PostgreSQL repository.
func NewRepository(pool *pgxpool.Pool, tx database.Transactor) *PGRepository {
	return &PGRepository{pool: pool, tx: tx}
}

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

func (r *PGRepository) CreateChallenge(ctx context.Context, phone, purpose string, codeHash []byte, deviceID string, expiresAt time.Time) (Challenge, error) {
	row, err := r.q(ctx).CreateOTPChallenge(ctx, db.CreateOTPChallengeParams{
		Phone: phone, CodeHash: codeHash, Purpose: purpose, ExpiresAt: expiresAt, DeviceID: optional(deviceID),
	})
	if err != nil {
		return Challenge{}, database.MapError(err)
	}
	return Challenge{ID: row.ID, CodeHash: codeHash, ExpiresAt: row.ExpiresAt}, nil
}

func (r *PGRepository) LatestChallenge(ctx context.Context, phone, purpose string) (Challenge, error) {
	row, err := r.q(ctx).GetLatestOTPChallenge(ctx, db.GetLatestOTPChallengeParams{Phone: phone, Purpose: purpose})
	if err != nil {
		return Challenge{}, database.MapError(err)
	}
	return Challenge{ID: row.ID, CodeHash: row.CodeHash, Attempts: int(row.Attempts), ExpiresAt: row.ExpiresAt}, nil
}

func (r *PGRepository) IncrementAttempts(ctx context.Context, id uuid.UUID) (int, error) {
	n, err := r.q(ctx).IncrementOTPAttempts(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return maxAttempts, nil
	}
	return int(n), database.MapError(err)
}

func (r *PGRepository) ConsumeChallenge(ctx context.Context, id uuid.UUID) (bool, error) {
	n, err := r.q(ctx).ConsumeOTPChallenge(ctx, id)
	return n == 1, database.MapError(err)
}

func (r *PGRepository) AccountByPhone(ctx context.Context, phone string) (Account, error) {
	u, err := r.q(ctx).GetUserByPhone(ctx, phone)
	return toAccount(u), database.MapError(err)
}

func (r *PGRepository) AccountByEmail(ctx context.Context, email string) (Account, error) {
	u, err := r.q(ctx).GetUserByEmail(ctx, email)
	return toAccount(u), database.MapError(err)
}

func (r *PGRepository) AccountByID(ctx context.Context, id uuid.UUID) (Account, error) {
	u, err := r.q(ctx).GetUser(ctx, id)
	if err == nil && u.DeletedAt != nil {
		return Account{}, apperror.ErrNotFound
	}
	return toAccount(u), database.MapError(err)
}

func (r *PGRepository) CreateUserWithPersonalOrg(ctx context.Context, phone string) (Account, error) {
	var out Account
	err := r.tx.InTx(ctx, func(ctx context.Context) error {
		q := r.q(ctx)
		u, err := q.CreateUser(ctx, db.CreateUserParams{Phone: phone, PreferredLanguage: "en"})
		if err != nil {
			return err
		}
		if err := r.createOrg(ctx, q, u.ID, "Personal account", "Personal account "+phone, db.OrgTypeIndividual, false,
			[]string{httpx.RoleCustomer, httpx.RoleOrgAdmin}); err != nil {
			return err
		}
		out = toAccount(u)
		return nil
	})
	return out, database.MapError(err)
}

func (r *PGRepository) Register(ctx context.Context, a NewAccount) (Account, error) {
	var out Account
	err := r.tx.InTx(ctx, func(ctx context.Context) error {
		q := r.q(ctx)
		u, err := q.CreatePasswordUser(ctx, db.CreatePasswordUserParams{
			Phone: a.Phone, Email: a.Email, FullName: a.FullName, PasswordHash: &a.PasswordHash,
		})
		if err != nil {
			return err
		}
		if _, err := q.InsertEquipmentInspectionPolicyConsent(ctx, db.InsertEquipmentInspectionPolicyConsentParams{UserID: u.ID, PolicyVersion: a.InspectionPolicyVersion}); err != nil {
			return err
		}
		switch a.AccountType {
		case AccountTypeEquipmentOwner:
			if err := r.createOrg(ctx, q, u.ID, a.OrgName, a.OrgName, db.OrgTypeProprietorship, true,
				[]string{httpx.RoleOwner, httpx.RoleOrgAdmin}); err != nil {
				return err
			}
			if err := events.Emit(ctx, q, "user", u.ID, events.OwnerRegistered, events.Payload{ActorUserID: u.ID, Title: a.OrgName}); err != nil {
				return err
			}
		case AccountTypeDriver:
			org, err := q.CreateOrganization(ctx, db.CreateOrganizationParams{
				LegalName: a.FullName, DisplayName: a.FullName, OrgType: db.OrgTypeIndividual, IsBuyer: true,
				CreatedBy: uuid.NullUUID{UUID: u.ID, Valid: true},
			})
			if err != nil {
				return err
			}
			for _, role := range []string{httpx.RoleOperator, httpx.RoleOrgAdmin} {
				if _, err := q.GrantRole(ctx, db.GrantRoleParams{UserID: u.ID, RoleCode: role,
					OrganizationID: uuid.NullUUID{UUID: org.ID, Valid: true}}); err != nil {
					return err
				}
			}
			if err := q.CreateOperatorProfile(ctx, db.CreateOperatorProfileParams{UserID: u.ID, OnDuty: false}); err != nil {
				return database.MapError(err)
			}
		default:
			if err := r.createOrg(ctx, q, u.ID, "Personal account", a.FullName, db.OrgTypeIndividual, false,
				[]string{httpx.RoleCustomer, httpx.RoleOrgAdmin}); err != nil {
				return err
			}
		}
		out = toAccount(u)
		return nil
	})
	return out, database.MapError(err)
}

// createOrg creates an organisation owned by userID with the given roles.
// Suppliers also get an owner profile.
func (r *PGRepository) createOrg(ctx context.Context, q *db.Queries, userID uuid.UUID, display, legal string, kind db.OrgType, supplier bool, roles []string) error {
	org, err := q.CreateOrganization(ctx, db.CreateOrganizationParams{
		LegalName: legal, DisplayName: display, OrgType: kind, IsBuyer: true, IsSupplier: supplier,
		CreatedBy: uuid.NullUUID{UUID: userID, Valid: true},
	})
	if err != nil {
		return err
	}
	for _, role := range roles {
		if _, err := q.GrantRole(ctx, db.GrantRoleParams{
			UserID: userID, RoleCode: role, OrganizationID: uuid.NullUUID{UUID: org.ID, Valid: true},
		}); err != nil {
			return err
		}
	}
	if supplier {
		if _, err := q.UpsertOwnerProfile(ctx, db.UpsertOwnerProfileParams{UserID: userID, OrganizationID: org.ID}); err != nil {
			return err
		}
	}
	return nil
}

func (r *PGRepository) MarkLoggedIn(ctx context.Context, userID uuid.UUID) error {
	return database.MapError(r.q(ctx).MarkUserLoggedIn(ctx, userID))
}

func (r *PGRepository) MarkPhoneVerified(ctx context.Context, userID uuid.UUID) error {
	return database.MapError(r.q(ctx).MarkPhoneVerified(ctx, userID))
}

func (r *PGRepository) MarkEmailVerified(ctx context.Context, userID uuid.UUID, email string) (bool, error) {
	n, err := r.q(ctx).MarkEmailVerified(ctx, db.MarkEmailVerifiedParams{ID: userID, Email: email})
	return n == 1, database.MapError(err)
}

func (r *PGRepository) SetPassword(ctx context.Context, userID uuid.UUID, hash string) error {
	return database.MapError(r.q(ctx).SetPasswordHash(ctx, db.SetPasswordHashParams{ID: userID, PasswordHash: &hash}))
}

func (r *PGRepository) Roles(ctx context.Context, userID uuid.UUID) ([]httpx.Role, error) {
	return LoadRoles(ctx, r.q(ctx), userID)
}

// LoadRoles reads a user's active role assignments.
func LoadRoles(ctx context.Context, q *db.Queries, userID uuid.UUID) ([]httpx.Role, error) {
	rows, err := q.ListUserRoles(ctx, userID)
	if err != nil {
		return nil, database.MapError(err)
	}
	roles := make([]httpx.Role, 0, len(rows))
	for _, row := range rows {
		r := httpx.Role{Code: row.RoleCode}
		if row.OrganizationID.Valid {
			r.OrgID = row.OrganizationID.UUID
		}
		if row.OrganizationName != nil {
			r.OrgName = *row.OrganizationName
		}
		roles = append(roles, r)
	}
	return roles, nil
}

func (r *PGRepository) CreateToken(ctx context.Context, userID uuid.UUID, purpose string, hash []byte, email string, expiresAt time.Time) error {
	return database.MapError(r.q(ctx).CreateAuthToken(ctx, db.CreateAuthTokenParams{
		UserID: userID, Purpose: purpose, TokenHash: hash, Email: optional(email), ExpiresAt: expiresAt,
	}))
}

func (r *PGRepository) ConsumeToken(ctx context.Context, hash []byte, purpose string) (uuid.UUID, string, error) {
	row, err := r.q(ctx).ConsumeAuthToken(ctx, db.ConsumeAuthTokenParams{TokenHash: hash, Purpose: purpose})
	return row.UserID, row.Email, database.MapError(err)
}

// VerifyToken checks a code against the newest live token of the address. found is false
// when there is no live token (none sent, expired, used or too many wrong attempts).
func (r *PGRepository) VerifyToken(ctx context.Context, email string, hash []byte, purpose string) (uuid.UUID, bool, error) {
	row, err := r.q(ctx).VerifyAuthToken(ctx, db.VerifyAuthTokenParams{Email: email, Purpose: purpose, TokenHash: hash})
	if err != nil {
		err = database.MapError(err)
		if errors.Is(err, apperror.ErrNotFound) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, err
	}
	if !row.Ok {
		return uuid.Nil, false, nil
	}
	return row.UserID, true, nil
}

func (r *PGRepository) InvalidateTokens(ctx context.Context, userID uuid.UUID, purpose string) error {
	return database.MapError(r.q(ctx).InvalidateAuthTokens(ctx, db.InvalidateAuthTokensParams{UserID: userID, Purpose: purpose}))
}

func (r *PGRepository) CreateSession(ctx context.Context, s NewSession) (Session, error) {
	row, err := r.q(ctx).CreateSession(ctx, db.CreateSessionParams{
		UserID: s.UserID, FamilyID: s.FamilyID, RefreshTokenHash: s.RefreshHash,
		DeviceID: optional(s.DeviceID), Platform: optional(s.Platform), UserAgent: optional(truncate(s.UserAgent, 300)),
		ExpiresAt: s.ExpiresAt,
	})
	if err != nil {
		return Session{}, database.MapError(err)
	}
	return Session{ID: row.ID, UserID: s.UserID, FamilyID: s.FamilyID, ExpiresAt: row.ExpiresAt}, nil
}

func (r *PGRepository) SessionByRefreshHash(ctx context.Context, hash []byte) (Session, error) {
	s, err := r.q(ctx).GetSessionByRefreshHash(ctx, hash)
	if err != nil {
		return Session{}, database.MapError(err)
	}
	return Session{ID: s.ID, UserID: s.UserID, FamilyID: s.FamilyID, ExpiresAt: s.ExpiresAt, RevokedAt: s.RevokedAt}, nil
}

func (r *PGRepository) RevokeSession(ctx context.Context, id uuid.UUID) (bool, error) {
	n, err := r.q(ctx).RevokeSession(ctx, id)
	return n == 1, database.MapError(err)
}

func (r *PGRepository) RevokeUserSession(ctx context.Context, id, userID uuid.UUID) (bool, error) {
	n, err := r.q(ctx).RevokeUserSession(ctx, db.RevokeUserSessionParams{ID: id, UserID: userID})
	return n == 1, database.MapError(err)
}

func (r *PGRepository) RevokeFamily(ctx context.Context, familyID uuid.UUID) error {
	_, err := r.q(ctx).RevokeSessionFamily(ctx, familyID)
	return database.MapError(err)
}

func (r *PGRepository) RevokeAllSessions(ctx context.Context, userID uuid.UUID) error {
	_, err := r.q(ctx).RevokeAllUserSessions(ctx, userID)
	return database.MapError(err)
}

func (r *PGRepository) RevokeOtherSessions(ctx context.Context, userID, keep uuid.UUID) error {
	_, err := r.q(ctx).RevokeOtherUserSessions(ctx, db.RevokeOtherUserSessionsParams{UserID: userID, KeepSessionID: keep})
	return database.MapError(err)
}

func (r *PGRepository) ListSessions(ctx context.Context, userID uuid.UUID) ([]SessionInfo, error) {
	rows, err := r.q(ctx).ListUserSessions(ctx, userID)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]SessionInfo, 0, len(rows))
	for _, s := range rows {
		out = append(out, SessionInfo{
			ID: s.ID, DeviceID: s.DeviceID, Platform: s.Platform, UserAgent: s.UserAgent,
			CreatedAt: s.CreatedAt, LastUsedAt: s.LastUsedAt, ExpiresAt: s.ExpiresAt,
		})
	}
	return out, nil
}

func (r *PGRepository) SessionState(ctx context.Context, sessionID uuid.UUID) (SessionState, error) {
	row, err := r.q(ctx).GetSessionPrincipal(ctx, sessionID)
	if err != nil {
		return SessionState{}, database.MapError(err)
	}
	return SessionState{
		UserID: row.UserID, UserActive: row.UserStatus == db.AccountStatusActive,
		Revoked: row.RevokedAt != nil, ExpiresAt: row.ExpiresAt,
	}, nil
}

func (r *PGRepository) Audit(ctx context.Context, e AuditEvent) error {
	p := db.InsertAuthAuditParams{Action: e.Action}
	if e.Actor != uuid.Nil {
		p.ActorUserID = uuid.NullUUID{UUID: e.Actor, Valid: true}
	}
	if e.UserID != uuid.Nil {
		p.EntityID = uuid.NullUUID{UUID: e.UserID, Valid: true}
	}
	p.Reason = optional(e.Reason)
	p.RequestID = optional(httpx.RequestIDFromContext(ctx))
	if ip, err := netip.ParseAddr(e.IP); err == nil {
		p.Ip = &ip
	}
	if len(e.Data) > 0 {
		p.After, _ = json.Marshal(e.Data)
	}
	return database.MapError(r.q(ctx).InsertAuthAudit(ctx, p))
}

func toAccount(u db.User) Account {
	return Account{
		ID: u.ID, Phone: u.Phone, Email: u.Email, FullName: u.FullName, Language: u.PreferredLanguage, Status: string(u.Status),
		PasswordHash: u.PasswordHash, PhoneVerifiedAt: u.PhoneVerifiedAt, EmailVerifiedAt: u.EmailVerifiedAt,
	}
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
