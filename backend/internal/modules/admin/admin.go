// Package admin provides read-only platform monitoring and financial reports.
// Legacy write routes return 403 at the router and service layers.
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/modules/auth"
	"github.com/equiprent/backend/internal/modules/bookings"
	"github.com/equiprent/backend/internal/modules/equipment"
)

type Stats struct {
	Users                 int32 `json:"users"`
	Organizations         int32 `json:"organizations"`
	ActiveListings        int32 `json:"active_listings"`
	ListingsPendingReview int32 `json:"listings_pending_review"`
	BookingsRequested     int32 `json:"bookings_requested"`
	BookingsInProgress    int32 `json:"bookings_in_progress"`
	OpenDisputes          int32 `json:"open_disputes"`
	GMV30dPaise           int64 `json:"gmv_30d_paise"`
}

// ReadOnlyDashboard is a snapshot of counts and recorded ledger activity. All
// money values are in paise and originate from payment, refund and ledger rows.
type ReadOnlyDashboard struct {
	Owners                          int32 `json:"owners"`
	Customers                       int32 `json:"customers"`
	Drivers                         int32 `json:"drivers"`
	Equipment                       int32 `json:"equipment"`
	Bookings                        int32 `json:"bookings"`
	PendingBookings                 int32 `json:"pending_bookings"`
	CompletedBookings               int32 `json:"completed_bookings"`
	SuccessfulCustomerPaymentsPaise int64 `json:"successful_customer_payments_paise"`
	Transactions                    int32 `json:"transactions"`
	PlatformRevenuePaise            int64 `json:"platform_revenue_paise"`
	DriverPayoutsPaise              int64 `json:"driver_payouts_paise"`
	RefundsPaise                    int64 `json:"refunds_paise"`
	OwnerSettlementsPaise           int64 `json:"owner_settlements_paise"`
	PlatformExpensesPaise           int64 `json:"platform_expenses_paise"`
	NetPlatformProfitPaise          int64 `json:"net_platform_profit_paise"`
	PendingSettlements              int32 `json:"pending_settlements"`
}

type ReportRangeQuery struct {
	From   string `form:"from" binding:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
	To     string `form:"to" binding:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
	Limit  int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Cursor string `form:"cursor" binding:"omitempty,uuid"`
}

type PaymentReportPage struct {
	Data       []AdminPaymentRecord `json:"data"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

type OwnersQuery struct {
	Search string `form:"q" binding:"omitempty,max=100"`
	Limit  int    `form:"limit" binding:"omitempty,min=1,max=200"`
}
type OwnersReport struct {
	Data []db.AdminEquipmentOwnersRow `json:"data"`
}
type AdminUsersReport struct {
	Data []db.AdminReadOnlyUsersRow `json:"data"`
}
type AdminEquipmentReport struct {
	Data []db.AdminReadOnlyEquipmentRow `json:"data"`
}
type AdminBookingsReport struct {
	Data []db.AdminReadOnlyBookingsRow `json:"data"`
}

type AdminPaymentRecord struct {
	ID               string     `json:"id"`
	BookingReference string     `json:"booking_reference"`
	AmountPaise      int64      `json:"amount_paise"`
	Status           string     `json:"status"`
	Purpose          string     `json:"purpose"`
	Gateway          string     `json:"gateway"`
	GatewayMode      string     `json:"gateway_mode"`
	CapturedAt       *time.Time `json:"captured_at"`
	CreatedAt        time.Time  `json:"created_at"`
}

type BookingCategoryStat struct {
	Category string `json:"category"`
	Bookings int32  `json:"bookings"`
}

type BookingTrendPoint struct {
	Month    time.Time `json:"month"`
	Bookings int32     `json:"bookings"`
}

type AnalyticsReport struct {
	Summary    ReadOnlyDashboard     `json:"summary"`
	Categories []BookingCategoryStat `json:"booking_categories"`
	Trends     []BookingTrendPoint   `json:"booking_trends"`
	From       time.Time             `json:"from"`
	To         time.Time             `json:"to"`
}

type User struct {
	ID          uuid.UUID  `json:"id"`
	Phone       string     `json:"phone"`
	FullName    string     `json:"full_name"`
	Email       string     `json:"email,omitempty"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

type AuditEntry struct {
	ID          int64         `json:"id"`
	OccurredAt  time.Time     `json:"occurred_at"`
	ActorUserID uuid.NullUUID `json:"actor_user_id"`
	ActorRole   *string       `json:"actor_role,omitempty"`
	Action      string        `json:"action"`
	EntityType  string        `json:"entity_type"`
	EntityID    uuid.NullUUID `json:"entity_id"`
	Reason      *string       `json:"reason,omitempty"`
	RequestID   *string       `json:"request_id,omitempty"`
}

type UsersQuery struct {
	Status string `form:"status" binding:"omitempty,oneof=active suspended deleted"`
	Q      string `form:"q" binding:"omitempty,max=60" doc:"Phone or name contains"`
	Limit  int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Cursor string `form:"cursor"`
}

type UserPage struct {
	Data       []User `json:"data"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// ModerationRequest carries the reviewer's note (required to reject,
// suspend or remove; the owner sees it).
type ModerationRequest struct {
	Reason string `json:"reason" binding:"max=1000"`
}

type ReasonRequest struct {
	Reason string `json:"reason" binding:"required,min=5,max=500"`
}

type GrantRoleRequest struct {
	Role   string `json:"role" binding:"required,oneof=admin_super admin_ops admin_kyc admin_finance admin_support"`
	Reason string `json:"reason" binding:"required,min=5,max=500"`
}

type StatusResult struct {
	ID     uuid.UUID `json:"id"`
	Status string    `json:"status"`
}

type ListingsQuery struct {
	Status string `form:"status" binding:"omitempty,oneof=pending_review active paused suspended rejected removed"`
	Limit  int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Cursor string `form:"cursor"`
}

type BookingTransitionRequest struct {
	Action string `json:"action" binding:"required,oneof=reject cancel complete close confirm_payment"`
	Reason string `json:"reason" binding:"required,min=5,max=500"`
}

type CreateModelRequest struct {
	CategorySlug string          `json:"category_slug" binding:"required,max=60"`
	Make         string          `json:"make" binding:"required,min=2,max=60"`
	Name         string          `json:"name" binding:"required,min=1,max=100"`
	DefaultSpecs json.RawMessage `json:"default_specs" doc:"JSON object of spec defaults"`
}

type ModelResult struct {
	ID     uuid.UUID `json:"id"`
	MakeID uuid.UUID `json:"make_id"`
}

type AuditQuery struct {
	EntityType string `form:"entity_type" binding:"omitempty,max=60"`
	EntityID   string `form:"entity_id" binding:"omitempty,uuid"`
	ActorID    string `form:"actor_user_id" binding:"omitempty,uuid"`
	Limit      int    `form:"limit" binding:"omitempty,min=1,max=200"`
	Cursor     string `form:"cursor" doc:"Opaque; from next_cursor"`
}

type AuditPage struct {
	Data       []AuditEntry `json:"data"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

// ---- repository --------------------------------------------------------------------

type Repository interface {
	Stats(ctx context.Context) (Stats, error)
	ReadOnlyDashboard(ctx context.Context, from, to time.Time) (ReadOnlyDashboard, error)
	BookingCategoryStats(ctx context.Context, from, to time.Time) ([]BookingCategoryStat, error)
	BookingTrend(ctx context.Context, from, to time.Time) ([]BookingTrendPoint, error)
	ReadOnlyPayments(ctx context.Context, from, to time.Time, after uuid.NullUUID, limit int32) ([]db.AdminReadOnlyPaymentsRow, error)
	ReadOnlyUsers(ctx context.Context, after uuid.NullUUID, limit int32) ([]db.AdminReadOnlyUsersRow, error)
	EquipmentOwners(ctx context.Context, search string, limit int32) ([]db.AdminEquipmentOwnersRow, error)
	ReadOnlyBookings(ctx context.Context, from, to time.Time, after uuid.NullUUID, limit int32) ([]db.AdminReadOnlyBookingsRow, error)
	ReadOnlyEquipment(ctx context.Context, after uuid.NullUUID, limit int32) ([]db.AdminReadOnlyEquipmentRow, error)
	Users(ctx context.Context, p db.AdminListUsersParams) ([]User, error)
	SetUserStatus(ctx context.Context, id uuid.UUID, status string) (StatusResult, error)
	Roles(ctx context.Context, userID uuid.UUID) ([]httpx.Role, error)
	RevokeSessions(ctx context.Context, userID uuid.UUID) error
	GrantPlatformRole(ctx context.Context, userID uuid.UUID, role string, by uuid.UUID) error
	Audit(ctx context.Context, e db.InsertAuditLogParams) error
	AuditLog(ctx context.Context, p db.ListAuditLogsParams) ([]AuditEntry, error)
	UpsertModel(ctx context.Context, categorySlug, make, name string, specs json.RawMessage) (ModelResult, error)
}

type PGRepository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *PGRepository { return &PGRepository{pool: pool} }

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

func (r *PGRepository) Stats(ctx context.Context) (Stats, error) {
	s, err := r.q(ctx).PlatformStats(ctx)
	return Stats{Users: s.Users, Organizations: s.Organizations, ActiveListings: s.ActiveListings,
		ListingsPendingReview: s.ListingsPendingReview, BookingsRequested: s.BookingsRequested,
		BookingsInProgress: s.BookingsInProgress, OpenDisputes: s.OpenDisputes, GMV30dPaise: s.Gmv30dPaise}, database.MapError(err)
}

func (r *PGRepository) ReadOnlyDashboard(ctx context.Context, from, to time.Time) (ReadOnlyDashboard, error) {
	s, err := r.q(ctx).AdminReadOnlyDashboard(ctx, db.AdminReadOnlyDashboardParams{FromAt: from, ToAt: to})
	return ReadOnlyDashboard{
		Owners: s.Owners, Customers: s.Customers, Drivers: s.Drivers, Equipment: s.Equipment, Bookings: s.Bookings,
		PendingBookings: s.PendingBookings, CompletedBookings: s.CompletedBookings, SuccessfulCustomerPaymentsPaise: s.SuccessfulCustomerPaymentsPaise,
		Transactions: s.Transactions, PlatformRevenuePaise: s.PlatformRevenuePaise, DriverPayoutsPaise: s.DriverPayoutsPaise,
		RefundsPaise: s.RefundsPaise, OwnerSettlementsPaise: s.OwnerSettlementsPaise,
		PlatformExpensesPaise: s.PlatformExpensesPaise, NetPlatformProfitPaise: s.NetPlatformProfitPaise,
		PendingSettlements: s.PendingSettlements,
	}, database.MapError(err)
}

func (r *PGRepository) BookingCategoryStats(ctx context.Context, from, to time.Time) ([]BookingCategoryStat, error) {
	rows, err := r.q(ctx).AdminBookingCategoryStats(ctx, db.AdminBookingCategoryStatsParams{FromAt: from, ToAt: to})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]BookingCategoryStat, 0, len(rows))
	for _, row := range rows {
		out = append(out, BookingCategoryStat{Category: row.Category, Bookings: row.Bookings})
	}
	return out, nil
}

func (r *PGRepository) BookingTrend(ctx context.Context, from, to time.Time) ([]BookingTrendPoint, error) {
	rows, err := r.q(ctx).AdminBookingTrend(ctx, db.AdminBookingTrendParams{FromAt: from, ToAt: to})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]BookingTrendPoint, 0, len(rows))
	for _, row := range rows {
		out = append(out, BookingTrendPoint{Month: row.Month, Bookings: row.Bookings})
	}
	return out, nil
}

func (r *PGRepository) ReadOnlyPayments(ctx context.Context, from, to time.Time, after uuid.NullUUID, limit int32) ([]db.AdminReadOnlyPaymentsRow, error) {
	rows, err := r.q(ctx).AdminReadOnlyPayments(ctx, db.AdminReadOnlyPaymentsParams{
		FromAt: &from, ToAt: &to, After: after, ResultLimit: limit,
	})
	return rows, database.MapError(err)
}

func (r *PGRepository) EquipmentOwners(ctx context.Context, search string, limit int32) ([]db.AdminEquipmentOwnersRow, error) {
	rows, err := r.q(ctx).AdminEquipmentOwners(ctx, db.AdminEquipmentOwnersParams{Search: search, ResultLimit: limit})
	return rows, database.MapError(err)
}

func (r *PGRepository) ReadOnlyUsers(ctx context.Context, after uuid.NullUUID, limit int32) ([]db.AdminReadOnlyUsersRow, error) {
	rows, err := r.q(ctx).AdminReadOnlyUsers(ctx, db.AdminReadOnlyUsersParams{After: after, ResultLimit: limit})
	return rows, database.MapError(err)
}

func (r *PGRepository) ReadOnlyBookings(ctx context.Context, from, to time.Time, after uuid.NullUUID, limit int32) ([]db.AdminReadOnlyBookingsRow, error) {
	rows, err := r.q(ctx).AdminReadOnlyBookings(ctx, db.AdminReadOnlyBookingsParams{FromAt: from, ToAt: to, After: after, ResultLimit: limit})
	return rows, database.MapError(err)
}

func (r *PGRepository) ReadOnlyEquipment(ctx context.Context, after uuid.NullUUID, limit int32) ([]db.AdminReadOnlyEquipmentRow, error) {
	rows, err := r.q(ctx).AdminReadOnlyEquipment(ctx, db.AdminReadOnlyEquipmentParams{After: after, ResultLimit: limit})
	return rows, database.MapError(err)
}

func (r *PGRepository) Users(ctx context.Context, p db.AdminListUsersParams) ([]User, error) {
	rows, err := r.q(ctx).AdminListUsers(ctx, p)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]User, 0, len(rows))
	for _, u := range rows {
		out = append(out, User{ID: u.ID, Phone: u.Phone, FullName: u.FullName, Email: u.Email, Status: string(u.Status),
			CreatedAt: u.CreatedAt, LastLoginAt: u.LastLoginAt})
	}
	return out, nil
}

func (r *PGRepository) SetUserStatus(ctx context.Context, id uuid.UUID, status string) (StatusResult, error) {
	row, err := r.q(ctx).SetUserStatus(ctx, db.SetUserStatusParams{ID: id, Status: db.AccountStatus(status)})
	return StatusResult{ID: row.ID, Status: string(row.Status)}, database.MapError(err)
}

func (r *PGRepository) Roles(ctx context.Context, userID uuid.UUID) ([]httpx.Role, error) {
	return auth.LoadRoles(ctx, r.q(ctx), userID)
}

func (r *PGRepository) RevokeSessions(ctx context.Context, userID uuid.UUID) error {
	_, err := r.q(ctx).RevokeAllUserSessions(ctx, userID)
	return database.MapError(err)
}

func (r *PGRepository) GrantPlatformRole(ctx context.Context, userID uuid.UUID, role string, by uuid.UUID) error {
	_, err := r.q(ctx).GrantRole(ctx, db.GrantRoleParams{UserID: userID, RoleCode: role, GrantedBy: uuid.NullUUID{UUID: by, Valid: true}})
	return database.MapError(err)
}

func (r *PGRepository) Audit(ctx context.Context, e db.InsertAuditLogParams) error {
	return database.MapError(r.q(ctx).InsertAuditLog(ctx, e))
}

func (r *PGRepository) AuditLog(ctx context.Context, p db.ListAuditLogsParams) ([]AuditEntry, error) {
	rows, err := r.q(ctx).ListAuditLogs(ctx, p)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]AuditEntry, 0, len(rows))
	for _, a := range rows {
		out = append(out, AuditEntry(a))
	}
	return out, nil
}

func (r *PGRepository) UpsertModel(ctx context.Context, categorySlug, make, name string, specs json.RawMessage) (ModelResult, error) {
	q := r.q(ctx)
	cat, err := q.GetCategoryBySlug(ctx, categorySlug)
	if err != nil {
		if errors.Is(database.MapError(err), apperror.ErrNotFound) {
			return ModelResult{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "category_slug", Code: "not_found", Message: "unknown category"})
		}
		return ModelResult{}, database.MapError(err)
	}
	makeID, err := q.UpsertMake(ctx, make)
	if err != nil {
		return ModelResult{}, database.MapError(err)
	}
	id, err := q.UpsertModel(ctx, db.UpsertModelParams{CategoryID: cat.ID, MakeID: makeID, Name: name, DefaultSpecs: specs})
	return ModelResult{ID: id, MakeID: makeID}, database.MapError(err)
}

// ---- service -----------------------------------------------------------------------

type Service struct {
	repo      Repository
	tx        database.Transactor
	equipment *equipment.Service
	bookings  *bookings.Service
}

func NewService(repo Repository, tx database.Transactor, eq *equipment.Service, bk *bookings.Service) *Service {
	return &Service{repo: repo, tx: tx, equipment: eq, bookings: bk}
}

// audited runs fn and writes an audit entry in the same transaction.
func (s *Service) audited(ctx context.Context, p *httpx.Principal, action, entity string, id uuid.UUID, reason string, after any, fn func(ctx context.Context) error) error {
	reqID := httpx.RequestIDFromContext(ctx)
	return s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "admin", Reason: reason, RequestID: reqID}, func(ctx context.Context) error {
		if err := fn(ctx); err != nil {
			return err
		}
		var body json.RawMessage
		if after != nil {
			body, _ = json.Marshal(after)
		}
		role := "admin"
		return s.repo.Audit(ctx, db.InsertAuditLogParams{
			ActorUserID: uuid.NullUUID{UUID: p.UserID, Valid: true}, ActorRole: &role, Action: action, EntityType: entity,
			EntityID: uuid.NullUUID{UUID: id, Valid: id != uuid.Nil}, Reason: &reason, RequestID: &reqID, After: body,
		})
	})
}

func (s *Service) Stats(ctx context.Context) (Stats, error) { return s.repo.Stats(ctx) }

func (s *Service) Analytics(ctx context.Context, q ReportRangeQuery) (AnalyticsReport, error) {
	from, to, err := reportRange(q)
	if err != nil {
		return AnalyticsReport{}, err
	}
	summary, err := s.repo.ReadOnlyDashboard(ctx, from, to)
	if err != nil {
		return AnalyticsReport{}, err
	}
	categories, err := s.repo.BookingCategoryStats(ctx, from, to)
	if err != nil {
		return AnalyticsReport{}, err
	}
	trends, err := s.repo.BookingTrend(ctx, from, to)
	if err != nil {
		return AnalyticsReport{}, err
	}
	return AnalyticsReport{Summary: summary, Categories: categories, Trends: trends, From: from, To: to}, nil
}

func (s *Service) ReadOnlyPayments(ctx context.Context, q ReportRangeQuery) (PaymentReportPage, error) {
	from, to, err := reportRange(q)
	if err != nil {
		return PaymentReportPage{}, err
	}
	limit := q.Limit
	if limit == 0 {
		limit = 50
	}
	var after uuid.NullUUID
	if q.Cursor != "" {
		id, parseErr := uuid.Parse(q.Cursor)
		if parseErr != nil {
			return PaymentReportPage{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "cursor", Code: "invalid", Message: "must be a UUID"})
		}
		after = uuid.NullUUID{UUID: id, Valid: true}
	}
	rows, err := s.repo.ReadOnlyPayments(ctx, from, to, after, int32(limit+1))
	if err != nil {
		return PaymentReportPage{}, err
	}
	page := PaymentReportPage{Data: make([]AdminPaymentRecord, 0, len(rows))}
	for _, row := range rows {
		page.Data = append(page.Data, AdminPaymentRecord{ID: row.ID.String(), BookingReference: row.BookingReference,
			AmountPaise: row.AmountPaise, Status: row.Status, Purpose: row.Purpose, Gateway: row.Gateway,
			GatewayMode: row.GatewayMode, CapturedAt: row.CapturedAt, CreatedAt: row.CreatedAt})
	}
	if len(rows) > limit {
		page.Data = page.Data[:limit]
		page.NextCursor = page.Data[len(page.Data)-1].ID
	}
	return page, nil
}

func reportPageCursor(cursor string) (uuid.NullUUID, error) {
	if cursor == "" {
		return uuid.NullUUID{}, nil
	}
	id, err := uuid.Parse(cursor)
	if err != nil {
		return uuid.NullUUID{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "cursor", Code: "invalid", Message: "must be a UUID"})
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

func (s *Service) EquipmentOwners(ctx context.Context, q OwnersQuery) (OwnersReport, error) {
	limit := q.Limit
	if limit == 0 {
		limit = 100
	}
	rows, err := s.repo.EquipmentOwners(ctx, strings.TrimSpace(q.Search), int32(limit))
	return OwnersReport{Data: rows}, err
}

func (s *Service) ReadOnlyUsers(ctx context.Context, q ReportRangeQuery) (AdminUsersReport, error) {
	limit := q.Limit
	if limit == 0 {
		limit = 100
	}
	after, err := reportPageCursor(q.Cursor)
	if err != nil {
		return AdminUsersReport{}, err
	}
	rows, err := s.repo.ReadOnlyUsers(ctx, after, int32(limit))
	return AdminUsersReport{Data: rows}, err
}

func (s *Service) ReadOnlyEquipment(ctx context.Context, q ReportRangeQuery) (AdminEquipmentReport, error) {
	limit := q.Limit
	if limit == 0 {
		limit = 100
	}
	after, err := reportPageCursor(q.Cursor)
	if err != nil {
		return AdminEquipmentReport{}, err
	}
	rows, err := s.repo.ReadOnlyEquipment(ctx, after, int32(limit))
	return AdminEquipmentReport{Data: rows}, err
}

func (s *Service) ReadOnlyBookings(ctx context.Context, q ReportRangeQuery) (AdminBookingsReport, error) {
	from, to, err := reportRange(q)
	if err != nil {
		return AdminBookingsReport{}, err
	}
	limit := q.Limit
	if limit == 0 {
		limit = 100
	}
	after, err := reportPageCursor(q.Cursor)
	if err != nil {
		return AdminBookingsReport{}, err
	}
	rows, err := s.repo.ReadOnlyBookings(ctx, from, to, after, int32(limit))
	return AdminBookingsReport{Data: rows}, err
}

func reportRange(q ReportRangeQuery) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	if q.From == "" && q.To == "" {
		return now.AddDate(0, 0, -30), now, nil
	}
	if q.From == "" || q.To == "" {
		return time.Time{}, time.Time{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "from,to", Code: "required_together", Message: "provide both UTC RFC3339 range boundaries"})
	}
	from, errFrom := time.Parse(time.RFC3339, q.From)
	to, errTo := time.Parse(time.RFC3339, q.To)
	if errFrom != nil || errTo != nil || !from.Before(to) || to.Sub(from) > 366*24*time.Hour {
		return time.Time{}, time.Time{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "from,to", Code: "invalid_range", Message: "must be valid UTC RFC3339 instants in increasing order, no more than 366 days apart"})
	}
	return from.UTC(), to.UTC(), nil
}

func (s *Service) Users(ctx context.Context, q UsersQuery) (UserPage, error) {
	k, err := httpx.PageQuery{Limit: q.Limit, Cursor: q.Cursor}.Keyset()
	if err != nil {
		return UserPage{}, err
	}
	p := db.AdminListUsersParams{After: k.After, ResultLimit: k.Fetch()}
	if q.Status != "" {
		p.Status = &q.Status
	}
	if q.Q != "" {
		p.Query = &q.Q
	}
	rows, err := s.repo.Users(ctx, p)
	if err != nil {
		return UserPage{}, err
	}
	pg := httpx.Paginate(rows, k, func(u User) uuid.UUID { return u.ID })
	return UserPage{Data: pg.Data, NextCursor: pg.NextCursor}, nil
}

// SetUserStatus is retained for a stable 403 response to legacy API callers.
func (s *Service) SetUserStatus(ctx context.Context, p *httpx.Principal, id uuid.UUID, status, reason string) (StatusResult, error) {
	return StatusResult{}, apperror.ErrForbidden.WithDetail("platform administrator access is read-only")
}

// GrantRole is retained for a stable 403 response to legacy API callers.
func (s *Service) GrantRole(ctx context.Context, p *httpx.Principal, id uuid.UUID, req GrantRoleRequest) (StatusResult, error) {
	return StatusResult{}, apperror.ErrForbidden.WithDetail("platform administrator access is read-only")
}

func (s *Service) Listings(ctx context.Context, q ListingsQuery) (equipment.SummaryPage, error) {
	return s.equipment.ModerationQueue(ctx, q.Status, httpx.PageQuery{Limit: q.Limit, Cursor: q.Cursor})
}

// Moderate is retained for a stable 403 response to legacy API callers.
func (s *Service) Moderate(ctx context.Context, p *httpx.Principal, id uuid.UUID, action equipment.Action, reason string) (equipment.StatusChange, error) {
	return equipment.StatusChange{}, apperror.ErrForbidden.WithDetail("platform administrator access is read-only")
}

// TransitionBooking is retained for a stable 403 response to legacy API callers.
func (s *Service) TransitionBooking(ctx context.Context, p *httpx.Principal, id uuid.UUID, req BookingTransitionRequest) (bookings.Transition, error) {
	return bookings.Transition{}, apperror.ErrForbidden.WithDetail("platform administrator access is read-only")
}

// CreateModel is retained for a stable 403 response to legacy API callers.
func (s *Service) CreateModel(ctx context.Context, p *httpx.Principal, req CreateModelRequest) (ModelResult, error) {
	return ModelResult{}, apperror.ErrForbidden.WithDetail("platform administrator access is read-only")
}

func (s *Service) AuditLog(ctx context.Context, q AuditQuery) (AuditPage, error) {
	limit := q.Limit
	if limit == 0 {
		limit = 50
	}
	p := db.ListAuditLogsParams{ResultLimit: int32(limit + 1)}
	if q.EntityType != "" {
		p.EntityType = &q.EntityType
	}
	if q.EntityID != "" {
		p.EntityID = uuid.NullUUID{UUID: uuid.MustParse(q.EntityID), Valid: true}
	}
	if q.ActorID != "" {
		p.ActorUserID = uuid.NullUUID{UUID: uuid.MustParse(q.ActorID), Valid: true}
	}
	if q.Cursor != "" {
		before, err := strconv.ParseInt(q.Cursor, 10, 64)
		if err != nil || before <= 0 {
			return AuditPage{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "cursor", Code: "invalid", Message: "is not a valid cursor"})
		}
		p.BeforeID = &before
	}
	rows, err := s.repo.AuditLog(ctx, p)
	if err != nil {
		return AuditPage{}, err
	}
	out := AuditPage{Data: rows}
	if len(rows) > limit {
		out.Data = rows[:limit]
		out.NextCursor = strconv.FormatInt(out.Data[limit-1].ID, 10)
	}
	return out, nil
}

// ---- handler -----------------------------------------------------------------------

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Name() string { return "admin" }

func (h *Handler) Routes(r *httpx.Router) {
	r = r.Tag("admin")
	monitor := r.Admin(httpx.RoleAdminOps, httpx.RoleAdminFinance, httpx.RoleAdminSupport, httpx.RoleAdminKYC)
	monitor.GET("/admin/read-only-dashboard", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q ReportRangeQuery) (any, error) {
		return h.svc.Analytics(c.Request.Context(), q)
	}), httpx.Doc{Summary: "Read-only application and financial analytics for a UTC date range", Query: ReportRangeQuery{}, Response: AnalyticsReport{}})
	monitor.GET("/admin/read-only-payments", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q ReportRangeQuery) (any, error) {
		return h.svc.ReadOnlyPayments(c.Request.Context(), q)
	}), httpx.Doc{Summary: "Read-only customer payment history for a UTC date range", Query: ReportRangeQuery{}, Response: PaymentReportPage{}})
	monitor.GET("/admin/read-only-users", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q ReportRangeQuery) (any, error) {
		return h.svc.ReadOnlyUsers(c.Request.Context(), q)
	}), httpx.Doc{Summary: "Read-only users and their assigned roles", Query: ReportRangeQuery{}, Response: AdminUsersReport{}})
	monitor.GET("/admin/read-only-equipment", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q ReportRangeQuery) (any, error) {
		return h.svc.ReadOnlyEquipment(c.Request.Context(), q)
	}), httpx.Doc{Summary: "Read-only equipment listings", Query: ReportRangeQuery{}, Response: AdminEquipmentReport{}})
	monitor.GET("/admin/read-only-bookings", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q ReportRangeQuery) (any, error) {
		return h.svc.ReadOnlyBookings(c.Request.Context(), q)
	}), httpx.Doc{Summary: "Read-only booking history for a UTC date range", Query: ReportRangeQuery{}, Response: AdminBookingsReport{}})
	monitor.GET("/admin/equipment-owners", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q OwnersQuery) (any, error) {
		return h.svc.EquipmentOwners(c.Request.Context(), q)
	}), httpx.Doc{Summary: "Read-only directory of equipment owners: one row per owner organisation with equipment count", Query: OwnersQuery{}, Response: OwnersReport{}})
	anyAdmin := r.Admin(httpx.RoleAdminOps, httpx.RoleAdminSupport, httpx.RoleAdminFinance, httpx.RoleAdminKYC)
	anyAdmin.GET("/admin/stats", httpx.Handle(200, func(c *gin.Context, _ *httpx.Principal) (any, error) {
		return h.svc.Stats(c.Request.Context())
	}), httpx.Doc{Summary: "Platform statistics", Response: Stats{}})

	// KYC reviewers may suspend accounts that submitted fraudulent documents.
	support := r.Admin(httpx.RoleAdminSupport, httpx.RoleAdminOps, httpx.RoleAdminKYC)
	support.GET("/admin/users", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q UsersQuery) (any, error) {
		return h.svc.Users(c.Request.Context(), q)
	}), httpx.Doc{Summary: "Search users", Query: UsersQuery{}, Response: UserPage{}})
	for path, status := range map[string]string{"suspend": "suspended", "reactivate": "active"} {
		support.POST("/admin/users/:id/"+path, httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req ReasonRequest) (any, error) {
			id, err := httpx.Param(c, "id")
			if err != nil {
				return nil, err
			}
			return h.svc.SetUserStatus(c.Request.Context(), p, id, status, req.Reason)
		}), httpx.Doc{Summary: map[string]string{"suspend": "Suspend a user (signs them out)", "reactivate": "Reactivate a user"}[path],
			Request: ReasonRequest{}, Response: StatusResult{}})
	}
	r.Admin().POST("/admin/users/:id/roles", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req GrantRoleRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.GrantRole(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Grant a platform role (super admin)", Request: GrantRoleRequest{}, Response: StatusResult{}})

	ops := r.Admin(httpx.RoleAdminOps)
	ops.GET("/admin/listings", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q ListingsQuery) (any, error) {
		return h.svc.Listings(c.Request.Context(), q)
	}), httpx.Doc{Summary: "Listing moderation queue", Query: ListingsQuery{}, Response: equipment.SummaryPage{}})
	listingReview := ops.AdminWrites(
		"POST /admin/listings/:id/approve",
		"POST /admin/listings/:id/reject",
	)
	for _, act := range []equipment.Action{equipment.ActionApprove, equipment.ActionReject} {
		listingReview.POST("/admin/listings/:id/"+string(act), httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req ModerationRequest) (any, error) {
			id, err := httpx.Param(c, "id")
			if err != nil {
				return nil, err
			}
			return h.svc.Moderate(c.Request.Context(), p, id, act, req.Reason)
		}), httpx.Doc{Summary: "Listing: " + string(act), Request: ModerationRequest{}, Response: equipment.StatusChange{},
			Description: map[equipment.Action]string{equipment.ActionReject: "Reason required; the owner sees it and can fix and resubmit."}[act]})
	}
	for _, act := range []equipment.Action{equipment.ActionSuspend, equipment.ActionReinstate, equipment.ActionRemove} {
		ops.POST("/admin/listings/:id/"+string(act), httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req ModerationRequest) (any, error) {
			id, err := httpx.Param(c, "id")
			if err != nil {
				return nil, err
			}
			return h.svc.Moderate(c.Request.Context(), p, id, act, req.Reason)
		}), httpx.Doc{Summary: "Listing: " + string(act), Request: ModerationRequest{}, Response: equipment.StatusChange{}})
	}
	ops.POST("/admin/bookings/:id/transition", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req BookingTransitionRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.TransitionBooking(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Force a booking transition (audited)", Request: BookingTransitionRequest{}, Response: bookings.Transition{}})

	ops.POST("/admin/catalogue/models", httpx.HandleJSON(201, func(c *gin.Context, p *httpx.Principal, req CreateModelRequest) (any, error) {
		return h.svc.CreateModel(c.Request.Context(), p, req)
	}), httpx.Doc{Summary: "Add a make/model to the catalogue", Request: CreateModelRequest{}, Response: ModelResult{}, Status: 201})

	r.Admin().GET("/admin/audit-logs", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q AuditQuery) (any, error) {
		return h.svc.AuditLog(c.Request.Context(), q)
	}), httpx.Doc{Summary: "Audit log (super admin)", Query: AuditQuery{}, Response: AuditPage{}})
}
