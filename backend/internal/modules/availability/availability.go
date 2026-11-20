// Package availability serves equipment calendars: public busy ranges, the
// owner's calendar (bookings + blocks) and owner-managed availability
// blocks. Overlap rules live in the database (exclusion constraints and
// triggers), so a block can never cover a held booking and vice versa.
package availability

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/httpx"
)

// ---- domain ------------------------------------------------------------------------

// Range is a busy interval [start, end).
type Range struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Kind  string    `json:"kind" doc:"booked or blocked"`
}

// Block is an owner-defined unavailable period.
type Block struct {
	ID        uuid.UUID `json:"id"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Reason    string    `json:"reason"`
	Note      *string   `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// CalendarBooking is a booking as shown on the owner's calendar.
type CalendarBooking struct {
	ID        uuid.UUID `json:"id"`
	Reference string    `json:"reference"`
	Status    string    `json:"status"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
}

type RangeQuery struct {
	From time.Time `form:"from" binding:"required" time_format:"2006-01-02T15:04:05Z07:00"`
	To   time.Time `form:"to" binding:"required,gtfield=From" time_format:"2006-01-02T15:04:05Z07:00"`
}

type BusyResponse struct {
	EquipmentID uuid.UUID `json:"equipment_id"`
	Busy        []Range   `json:"busy"`
}

type Calendar struct {
	EquipmentID uuid.UUID         `json:"equipment_id"`
	Bookings    []CalendarBooking `json:"bookings"`
	Blocks      []Block           `json:"blocks"`
}

type CreateBlockRequest struct {
	Start  time.Time `json:"start" binding:"required"`
	End    time.Time `json:"end" binding:"required,gtfield=Start"`
	Reason string    `json:"reason" binding:"required,oneof=owner_block maintenance offline_booking"`
	Note   *string   `json:"note" binding:"omitempty,max=500"`
}

// ---- repository --------------------------------------------------------------------

type Repository interface {
	Owner(ctx context.Context, equipmentID uuid.UUID) (uuid.UUID, string, error)
	Busy(ctx context.Context, equipmentID uuid.UUID, from, to time.Time) ([]Range, error)
	Blocks(ctx context.Context, equipmentID uuid.UUID, from, to time.Time) ([]Block, error)
	Bookings(ctx context.Context, equipmentID uuid.UUID, from, to time.Time) ([]CalendarBooking, error)
	CreateBlock(ctx context.Context, equipmentID, by uuid.UUID, req CreateBlockRequest) (uuid.UUID, error)
	DeleteBlock(ctx context.Context, equipmentID, blockID uuid.UUID) (bool, error)
}

type PGRepository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *PGRepository { return &PGRepository{pool: pool} }

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

func (r *PGRepository) Owner(ctx context.Context, id uuid.UUID) (uuid.UUID, string, error) {
	row, err := r.q(ctx).EquipmentOwner(ctx, id)
	return row.OwnerOrgID, string(row.Status), database.MapError(err)
}

func (r *PGRepository) Busy(ctx context.Context, id uuid.UUID, from, to time.Time) ([]Range, error) {
	rows, err := r.q(ctx).ListBusyRanges(ctx, db.ListBusyRangesParams{EquipmentID: id, FromAt: from, ToAt: to})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Range, 0, len(rows))
	for _, b := range rows {
		out = append(out, Range{Start: b.StartsAt, End: b.EndsAt, Kind: b.Kind})
	}
	return out, nil
}

func (r *PGRepository) Blocks(ctx context.Context, id uuid.UUID, from, to time.Time) ([]Block, error) {
	rows, err := r.q(ctx).ListAvailabilityBlocks(ctx, db.ListAvailabilityBlocksParams{EquipmentID: id, FromAt: from, ToAt: to})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Block, 0, len(rows))
	for _, b := range rows {
		out = append(out, Block{ID: b.ID, Start: b.StartsAt, End: b.EndsAt, Reason: string(b.Reason), Note: b.Note, CreatedAt: b.CreatedAt})
	}
	return out, nil
}

func (r *PGRepository) Bookings(ctx context.Context, id uuid.UUID, from, to time.Time) ([]CalendarBooking, error) {
	rows, err := r.q(ctx).ListEquipmentBookingsInRange(ctx, db.ListEquipmentBookingsInRangeParams{EquipmentID: id, FromAt: from, ToAt: to})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]CalendarBooking, 0, len(rows))
	for _, b := range rows {
		out = append(out, CalendarBooking{ID: b.ID, Reference: b.Reference, Status: string(b.Status), Start: b.StartsAt, End: b.EndsAt})
	}
	return out, nil
}

func (r *PGRepository) CreateBlock(ctx context.Context, id, by uuid.UUID, req CreateBlockRequest) (uuid.UUID, error) {
	blockID, err := r.q(ctx).CreateAvailabilityBlock(ctx, db.CreateAvailabilityBlockParams{
		EquipmentID: id, StartsAt: req.Start.UTC(), EndsAt: req.End.UTC(),
		Reason: db.AvailabilityBlockReason(req.Reason), Note: req.Note, CreatedBy: uuid.NullUUID{UUID: by, Valid: true},
	})
	return blockID, database.MapError(err)
}

func (r *PGRepository) DeleteBlock(ctx context.Context, id, blockID uuid.UUID) (bool, error) {
	n, err := r.q(ctx).DeleteAvailabilityBlock(ctx, db.DeleteAvailabilityBlockParams{ID: blockID, EquipmentID: id})
	return n == 1, database.MapError(err)
}

// ---- service -----------------------------------------------------------------------

const maxWindow = 366 * 24 * time.Hour

type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func checkWindow(from, to time.Time) error {
	if !to.After(from) || to.Sub(from) > maxWindow {
		return apperror.ErrValidation.WithDetail("the window must be positive and at most one year")
	}
	return nil
}

// Busy returns busy ranges of an active listing (public: no booking details).
func (s *Service) Busy(ctx context.Context, id uuid.UUID, q RangeQuery) (BusyResponse, error) {
	if err := checkWindow(q.From, q.To); err != nil {
		return BusyResponse{}, err
	}
	if _, status, err := s.repo.Owner(ctx, id); err != nil {
		return BusyResponse{}, err
	} else if status != "active" {
		return BusyResponse{}, apperror.ErrNotFound
	}
	busy, err := s.repo.Busy(ctx, id, q.From, q.To)
	return BusyResponse{EquipmentID: id, Busy: busy}, err
}

func (s *Service) ownerCheck(ctx context.Context, org, id uuid.UUID) error {
	owner, _, err := s.repo.Owner(ctx, id)
	if err != nil {
		return err
	}
	if owner != org {
		return apperror.ErrNotFound
	}
	return nil
}

// Calendar returns bookings and blocks for the owner.
func (s *Service) Calendar(ctx context.Context, org, id uuid.UUID, q RangeQuery) (Calendar, error) {
	if err := checkWindow(q.From, q.To); err != nil {
		return Calendar{}, err
	}
	if err := s.ownerCheck(ctx, org, id); err != nil {
		return Calendar{}, err
	}
	bookings, err := s.repo.Bookings(ctx, id, q.From, q.To)
	if err != nil {
		return Calendar{}, err
	}
	blocks, err := s.repo.Blocks(ctx, id, q.From, q.To)
	return Calendar{EquipmentID: id, Bookings: bookings, Blocks: blocks}, err
}

// CreateBlock marks the machine unavailable (fails with BOOKING_OVERLAP if a
// held booking or another block overlaps).
func (s *Service) CreateBlock(ctx context.Context, sc httpx.Scope, id uuid.UUID, req CreateBlockRequest) (Block, error) {
	if err := s.ownerCheck(ctx, sc.Org, id); err != nil {
		return Block{}, err
	}
	if err := checkWindow(req.Start, req.End); err != nil {
		return Block{}, err
	}
	blockID, err := s.repo.CreateBlock(ctx, id, sc.P.UserID, req)
	if err != nil {
		return Block{}, err
	}
	return Block{ID: blockID, Start: req.Start.UTC(), End: req.End.UTC(), Reason: req.Reason, Note: req.Note, CreatedAt: time.Now().UTC()}, nil
}

func (s *Service) DeleteBlock(ctx context.Context, org, id, blockID uuid.UUID) error {
	if err := s.ownerCheck(ctx, org, id); err != nil {
		return err
	}
	ok, err := s.repo.DeleteBlock(ctx, id, blockID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.ErrNotFound
	}
	return nil
}

// ---- handler -----------------------------------------------------------------------

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Name() string { return "availability" }

var ownerRoles = []string{httpx.RoleOwner, httpx.RoleFleetManager}

func (h *Handler) Routes(r *httpx.Router) {
	r = r.Tag("availability")
	r.GET("/equipment/:id/availability", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q RangeQuery) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Busy(c.Request.Context(), id, q)
	}), httpx.Doc{Summary: "Busy ranges of a listing", Query: RangeQuery{}, Response: BusyResponse{}})

	a := r.Authed()
	a.GET("/owner/equipment/:id/calendar", httpx.OrgHandle(200, ownerRoles, func(c *gin.Context, s httpx.Scope) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		var q RangeQuery
		if err := httpx.BindQ(c, &q); err != nil {
			return nil, err
		}
		return h.svc.Calendar(c.Request.Context(), s.Org, id, q)
	}), httpx.Doc{Summary: "Owner calendar: bookings and blocks", Query: RangeQuery{}, Response: Calendar{}, OrgScoped: true})
	a.POST("/owner/equipment/:id/blocks", httpx.OrgHandle(201, ownerRoles, func(c *gin.Context, s httpx.Scope) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		var req CreateBlockRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.CreateBlock(c.Request.Context(), s, id, req)
	}), httpx.Doc{Summary: "Block dates", Request: CreateBlockRequest{}, Response: Block{}, Status: 201, OrgScoped: true})
	a.DELETE("/owner/equipment/:id/blocks/:block_id", httpx.OrgHandle(204, ownerRoles, func(c *gin.Context, s httpx.Scope) (any, error) {
		ids, err := httpx.Params(c, "id", "block_id")
		if err != nil {
			return nil, err
		}
		return nil, h.svc.DeleteBlock(c.Request.Context(), s.Org, ids[0], ids[1])
	}), httpx.Doc{Summary: "Remove a block", OrgScoped: true})
}
