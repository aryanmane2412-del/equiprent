// Package disputes handles booking disputes: either party raises one
// (hour-meter, damage, payment, no-show…), a dispute conversation is opened,
// the booking moves to "disputed", and platform admins assign and resolve it
// with an optional financial outcome. All changes are audited by trigger.
package disputes

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/events"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/modules/bookings"
)

// Dispute is the API view of a dispute.
type Dispute struct {
	ID                  uuid.UUID     `json:"id"`
	BookingID           uuid.UUID     `json:"booking_id"`
	RaisedByOrgID       uuid.UUID     `json:"raised_by_org_id"`
	AgainstOrgID        uuid.UUID     `json:"against_org_id"`
	Category            string        `json:"category"`
	Summary             string        `json:"summary"`
	AmountClaimedPaise  int64         `json:"amount_claimed_paise"`
	ResolvedAmountPaise *int64        `json:"resolved_amount_paise,omitempty"`
	Status              string        `json:"status"`
	Resolution          *string       `json:"resolution,omitempty"`
	AssignedAdminID     uuid.NullUUID `json:"assigned_admin_id"`
	ConversationID      uuid.NullUUID `json:"conversation_id"`
	OpenedAt            time.Time     `json:"opened_at"`
	ResolvedAt          *time.Time    `json:"resolved_at,omitempty"`
}

type CreateRequest struct {
	Category           string `json:"category" binding:"required,oneof=hour_meter damage payment no_show late_arrival quality other"`
	Summary            string `json:"summary" binding:"required,min=10,max=2000"`
	AmountClaimedPaise int64  `json:"amount_claimed_paise" binding:"min=0"`
}

type ResolveRequest struct {
	Outcome             string `json:"outcome" binding:"required,oneof=resolved rejected"`
	Resolution          string `json:"resolution" binding:"required,min=10,max=2000"`
	ResolvedAmountPaise *int64 `json:"resolved_amount_paise" binding:"omitempty,min=0"`
}

type QueueQuery struct {
	Status string `form:"status" binding:"omitempty,oneof=open under_review resolved rejected"`
	Limit  int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Cursor string `form:"cursor"`
}

type DisputePage struct {
	Data       []Dispute `json:"data"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

func toDispute(d db.Dispute) Dispute {
	return Dispute{
		ID: d.ID, BookingID: d.BookingID, RaisedByOrgID: d.RaisedByOrgID, AgainstOrgID: d.AgainstOrgID,
		Category: string(d.Category), Summary: d.Summary, AmountClaimedPaise: d.AmountClaimedPaise,
		ResolvedAmountPaise: d.ResolvedAmountPaise, Status: string(d.Status), Resolution: d.Resolution,
		AssignedAdminID: d.AssignedAdminID, ConversationID: d.ConversationID, OpenedAt: d.OpenedAt, ResolvedAt: d.ResolvedAt,
	}
}

// ---- repository --------------------------------------------------------------------

type Repository interface {
	Open(ctx context.Context, p db.CreateDisputeParams, b bookings.Booking) (Dispute, error)
	Get(ctx context.Context, id uuid.UUID) (Dispute, error)
	ForOrg(ctx context.Context, org uuid.UUID, k httpx.Keyset) ([]Dispute, error)
	Queue(ctx context.Context, status *string, k httpx.Keyset) ([]Dispute, error)
	Assign(ctx context.Context, id, admin uuid.UUID) (Dispute, error)
	Resolve(ctx context.Context, p db.ResolveDisputeParams) (Dispute, error)
	Emit(ctx context.Context, id uuid.UUID, eventType string, p events.Payload) error
}

type PGRepository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *PGRepository { return &PGRepository{pool: pool} }

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

// Open creates the dispute and its conversation (raiser + both orgs' users
// are added as they post). Must run inside the caller's transaction.
func (r *PGRepository) Open(ctx context.Context, p db.CreateDisputeParams, b bookings.Booking) (Dispute, error) {
	q := r.q(ctx)
	subject := "Dispute on " + b.Reference
	conv, err := q.CreateConversation(ctx, db.CreateConversationParams{
		Kind: db.ConversationKindDispute, BookingID: uuid.NullUUID{UUID: b.ID, Valid: true}, Subject: &subject,
	})
	if err != nil {
		return Dispute{}, database.MapError(err)
	}
	if err := q.AddConversationParticipant(ctx, db.AddConversationParticipantParams{
		ConversationID: conv.ID, UserID: p.RaisedByUserID, OrganizationID: uuid.NullUUID{UUID: p.RaisedByOrgID, Valid: true},
	}); err != nil {
		return Dispute{}, database.MapError(err)
	}
	p.ConversationID = uuid.NullUUID{UUID: conv.ID, Valid: true}
	d, err := q.CreateDispute(ctx, p)
	return toDispute(d), database.MapError(err)
}

func (r *PGRepository) Get(ctx context.Context, id uuid.UUID) (Dispute, error) {
	d, err := r.q(ctx).GetDispute(ctx, id)
	return toDispute(d), database.MapError(err)
}

func (r *PGRepository) ForOrg(ctx context.Context, org uuid.UUID, k httpx.Keyset) ([]Dispute, error) {
	rows, err := r.q(ctx).ListOrgDisputes(ctx, db.ListOrgDisputesParams{OrgID: org, After: k.After, ResultLimit: k.Fetch()})
	return mapAll(rows), database.MapError(err)
}

func (r *PGRepository) Queue(ctx context.Context, status *string, k httpx.Keyset) ([]Dispute, error) {
	rows, err := r.q(ctx).ListDisputeQueue(ctx, db.ListDisputeQueueParams{Status: status, After: k.After, ResultLimit: k.Fetch()})
	return mapAll(rows), database.MapError(err)
}

func (r *PGRepository) Assign(ctx context.Context, id, admin uuid.UUID) (Dispute, error) {
	d, err := r.q(ctx).AssignDispute(ctx, db.AssignDisputeParams{ID: id, AdminID: uuid.NullUUID{UUID: admin, Valid: true}})
	return toDispute(d), database.MapError(err)
}

func (r *PGRepository) Resolve(ctx context.Context, p db.ResolveDisputeParams) (Dispute, error) {
	d, err := r.q(ctx).ResolveDispute(ctx, p)
	return toDispute(d), database.MapError(err)
}

func (r *PGRepository) Emit(ctx context.Context, id uuid.UUID, eventType string, p events.Payload) error {
	return database.MapError(events.Emit(ctx, r.q(ctx), "dispute", id, eventType, p))
}

func mapAll(rows []db.Dispute) []Dispute {
	out := make([]Dispute, 0, len(rows))
	for _, d := range rows {
		out = append(out, toDispute(d))
	}
	return out
}

// ---- service -----------------------------------------------------------------------

// Disputes can be raised from dispatch until 7 days after completion.
var disputableStatuses = map[string]bool{
	bookings.StatusDispatched: true, bookings.StatusOnSite: true, bookings.StatusActive: true,
	bookings.StatusReturnInitiated: true, bookings.StatusReturned: true, bookings.StatusCompleted: true,
	bookings.StatusDisputed: true,
}

type Service struct {
	repo     Repository
	bookings *bookings.Service
	tx       database.Transactor
}

func NewService(repo Repository, bk *bookings.Service, tx database.Transactor) *Service {
	return &Service{repo: repo, bookings: bk, tx: tx}
}

// Open raises a dispute against the other party of a booking.
func (s *Service) Open(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID, req CreateRequest) (Dispute, error) {
	b, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return Dispute{}, err
	}
	var raisedBy, against uuid.UUID
	switch {
	case side.Has(bookings.SideCustomer):
		raisedBy, against = b.CustomerOrgID, b.OwnerOrgID
	case side.Has(bookings.SideOwner):
		raisedBy, against = b.OwnerOrgID, b.CustomerOrgID
	default:
		return Dispute{}, apperror.ErrForbidden.WithDetail("only the customer or owner can raise a dispute")
	}
	if !disputableStatuses[b.Status] {
		return Dispute{}, apperror.ErrInvalidTransition.WithDetail("disputes can be raised once the machine is dispatched")
	}
	var out Dispute
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: side.Role(), Reason: "dispute: " + req.Category}, func(ctx context.Context) error {
		d, err := s.repo.Open(ctx, db.CreateDisputeParams{
			BookingID: b.ID, RaisedByOrgID: raisedBy, RaisedByUserID: p.UserID, AgainstOrgID: against,
			Category: db.DisputeCategory(req.Category), Summary: strings.TrimSpace(req.Summary), AmountClaimedPaise: req.AmountClaimedPaise,
		}, b)
		if err != nil {
			return err
		}
		out = d
		if err := s.bookings.MarkDisputed(ctx, b); err != nil {
			return err
		}
		return s.repo.Emit(ctx, d.ID, events.DisputeOpened, events.Payload{
			Reference: b.Reference, CustomerOrgID: b.CustomerOrgID, OwnerOrgID: b.OwnerOrgID, ActorUserID: p.UserID,
			Extra: map[string]any{"category": req.Category, "booking_id": b.ID},
		})
	})
	return out, err
}

// Get returns a dispute to the involved organisations or admins.
func (s *Service) Get(ctx context.Context, p *httpx.Principal, id uuid.UUID) (Dispute, error) {
	d, err := s.repo.Get(ctx, id)
	if err != nil {
		return Dispute{}, err
	}
	if !p.IsMember(d.RaisedByOrgID) && !p.IsMember(d.AgainstOrgID) && !p.HasPlatformRole(httpx.RoleAdminOps, httpx.RoleAdminFinance) {
		return Dispute{}, apperror.ErrNotFound
	}
	return d, nil
}

func page(rows []Dispute, k httpx.Keyset) DisputePage {
	pg := httpx.Paginate(rows, k, func(d Dispute) uuid.UUID { return d.ID })
	return DisputePage{Data: pg.Data, NextCursor: pg.NextCursor}
}

// ForOrg pages disputes involving the acting organisation.
func (s *Service) ForOrg(ctx context.Context, org uuid.UUID, q httpx.PageQuery) (DisputePage, error) {
	k, err := q.Keyset()
	if err != nil {
		return DisputePage{}, err
	}
	rows, err := s.repo.ForOrg(ctx, org, k)
	return page(rows, k), err
}

// Queue is the admin work queue.
func (s *Service) Queue(ctx context.Context, q QueueQuery) (DisputePage, error) {
	k, err := httpx.PageQuery{Limit: q.Limit, Cursor: q.Cursor}.Keyset()
	if err != nil {
		return DisputePage{}, err
	}
	var status *string
	if q.Status != "" {
		status = &q.Status
	}
	rows, err := s.repo.Queue(ctx, status, k)
	return page(rows, k), err
}

// Assign takes a dispute for review.
func (s *Service) Assign(ctx context.Context, p *httpx.Principal, id uuid.UUID) (Dispute, error) {
	var out Dispute
	err := s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "admin"}, func(ctx context.Context) error {
		d, err := s.repo.Assign(ctx, id, p.UserID)
		out = d
		return err
	})
	if err != nil && errors.Is(err, apperror.ErrNotFound) {
		return Dispute{}, apperror.ErrInvalidTransition.WithDetail("the dispute is closed or does not exist")
	}
	return out, err
}

// Resolve closes a dispute; a disputed booking returns to completed.
func (s *Service) Resolve(ctx context.Context, p *httpx.Principal, id uuid.UUID, req ResolveRequest) (Dispute, error) {
	current, err := s.repo.Get(ctx, id)
	if err != nil {
		return Dispute{}, err
	}
	resolution := strings.TrimSpace(req.Resolution)
	var out Dispute
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "admin", Reason: resolution}, func(ctx context.Context) error {
		d, err := s.repo.Resolve(ctx, db.ResolveDisputeParams{
			ID: id, Status: db.DisputeStatus(req.Outcome), Resolution: &resolution,
			ResolvedAmountPaise: req.ResolvedAmountPaise, AdminID: uuid.NullUUID{UUID: p.UserID, Valid: true},
		})
		if err != nil {
			if errors.Is(err, apperror.ErrNotFound) {
				return apperror.ErrInvalidTransition.WithDetail("the dispute is already closed")
			}
			return err
		}
		out = d
		b, _, err := s.bookings.Access(ctx, p, current.BookingID)
		if err == nil && b.Status == bookings.StatusDisputed {
			if _, err := s.bookings.Act(ctx, p, b.ID, bookings.ActionResolveDispute, bookings.ActionRequest{Reason: resolution}); err != nil {
				return err
			}
		}
		return s.repo.Emit(ctx, d.ID, events.DisputeResolved, events.Payload{
			CustomerOrgID: b.CustomerOrgID, OwnerOrgID: b.OwnerOrgID, Reference: b.Reference, ActorUserID: p.UserID,
			Extra: map[string]any{"outcome": req.Outcome},
		})
	})
	return out, err
}

// ---- handler -----------------------------------------------------------------------

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Name() string { return "disputes" }

func (h *Handler) Routes(r *httpx.Router) {
	a := r.Tag("disputes").Authed()
	a.POST("/bookings/:id/disputes", httpx.HandleJSON(201, func(c *gin.Context, p *httpx.Principal, req CreateRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Open(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Raise a dispute", Request: CreateRequest{}, Response: Dispute{}, Status: 201})
	a.GET("/disputes", httpx.OrgHandle(200, nil, func(c *gin.Context, s httpx.Scope) (any, error) {
		var q httpx.PageQuery
		if err := httpx.BindQ(c, &q); err != nil {
			return nil, err
		}
		return h.svc.ForOrg(c.Request.Context(), s.Org, q)
	}), httpx.Doc{Summary: "Disputes involving my organisation", Query: httpx.PageQuery{}, Response: DisputePage{}, OrgScoped: true})
	a.GET("/disputes/:id", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Get(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Dispute detail", Response: Dispute{}})

	adm := r.Tag("admin").Admin(httpx.RoleAdminOps, httpx.RoleAdminFinance)
	adm.GET("/admin/disputes", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q QueueQuery) (any, error) {
		return h.svc.Queue(c.Request.Context(), q)
	}), httpx.Doc{Summary: "Dispute queue", Query: QueueQuery{}, Response: DisputePage{}})
	adm.POST("/admin/disputes/:id/assign", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Assign(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Take a dispute for review", Response: Dispute{}})
	adm.POST("/admin/disputes/:id/resolve", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req ResolveRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Resolve(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Resolve or reject a dispute (audited)", Request: ResolveRequest{}, Response: Dispute{}})
}
