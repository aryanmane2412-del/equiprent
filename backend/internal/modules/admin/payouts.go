package admin

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/httpx"
)

// StuckPayoutAfter is how long a payout may stay "processing" before finance is told.
const StuckPayoutAfter = 2 * time.Hour

// PayoutsQuery filters the payout list.
type PayoutsQuery struct {
	Status string `form:"status" binding:"omitempty,oneof=queued processing paid failed reversed"`
	Limit  int    `form:"limit" binding:"omitempty,min=1,max=200"`
}

// PayoutPage lists payouts, newest first.
type PayoutPage struct {
	Data []db.AdminListPayoutsRow `json:"data"`
}

// PayoutAttention is everything about owner payouts that needs a person.
type PayoutAttention struct {
	// AutomaticPayouts says whether the worker pays owners at all. When false, owners are paid
	// manually and UnpayableOwners is not computed.
	AutomaticPayouts bool `json:"automatic_payouts"`
	// NeedsAttention is true when any list below is not empty.
	NeedsAttention bool `json:"needs_attention"`
	// Stuck: still "processing" after StuckPayoutAfter.
	Stuck []db.AdminStuckPayoutsRow `json:"stuck"`
	// FailedRecently: failed or reversed in the last 7 days.
	FailedRecently []db.AdminRecentFailedPayoutsRow `json:"failed_recently"`
	// Mismatches: our books and the gateway disagree, including a payout the bank
	// reversed after it was marked paid. A person decides what to do.
	Mismatches []db.AdminPayoutMismatchesRow `json:"mismatches"`
	// UnpayableOwners: money is waiting but the owner has no verified payout account for the gateway
	// in use (after switching from test to live keys, owners must verify their account again).
	UnpayableOwners []db.AdminUnpayableOwnersRow `json:"unpayable_owners"`
}

// PayoutsHandler gives finance visibility into owner payouts (read-only).
type PayoutsHandler struct {
	pool           *pgxpool.Pool
	automatic      bool
	provider, mode string
}

// NewPayoutsHandler builds the handler. automatic, provider and mode describe the payout gateway the
// worker uses (settlements.AccountKind); with automatic=false owners are paid manually.
func NewPayoutsHandler(pool *pgxpool.Pool, automatic bool, provider, mode string) *PayoutsHandler {
	return &PayoutsHandler{pool: pool, automatic: automatic, provider: provider, mode: mode}
}

func (h *PayoutsHandler) Name() string { return "admin-payouts" }

func (h *PayoutsHandler) q(ctx context.Context) *db.Queries { return database.Queries(ctx, h.pool) }

// Routes registers the finance payout endpoints.
func (h *PayoutsHandler) Routes(r *httpx.Router) {
	r = r.Tag("admin")
	finance := r.Admin(httpx.RoleAdminFinance, httpx.RoleAdminOps)
	finance.GET("/admin/payouts", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q PayoutsQuery) (any, error) {
		return h.list(c.Request.Context(), q)
	}), httpx.Doc{Summary: "Owner payouts, newest first (read-only)", Query: PayoutsQuery{}, Response: PayoutPage{}})
	finance.GET("/admin/payouts/attention", httpx.Handle(200, func(c *gin.Context, _ *httpx.Principal) (any, error) {
		return h.attention(c.Request.Context())
	}), httpx.Doc{Summary: "Payouts that need a person: stuck, failed, mismatched with the gateway, or unpayable owners", Response: PayoutAttention{}})
}

func (h *PayoutsHandler) list(ctx context.Context, q PayoutsQuery) (PayoutPage, error) {
	limit := q.Limit
	if limit == 0 {
		limit = 50
	}
	var status *string
	if q.Status != "" {
		status = &q.Status
	}
	rows, err := h.q(ctx).AdminListPayouts(ctx, db.AdminListPayoutsParams{Status: status, ResultLimit: int32(limit)})
	if err != nil {
		return PayoutPage{}, database.MapError(err)
	}
	if rows == nil {
		rows = []db.AdminListPayoutsRow{}
	}
	return PayoutPage{Data: rows}, nil
}

func (h *PayoutsHandler) attention(ctx context.Context) (PayoutAttention, error) {
	q := h.q(ctx)
	const limit = 100
	stuck, err := q.AdminStuckPayouts(ctx, db.AdminStuckPayoutsParams{StuckSecs: StuckPayoutAfter.Seconds(), ResultLimit: limit})
	if err != nil {
		return PayoutAttention{}, database.MapError(err)
	}
	failed, err := q.AdminRecentFailedPayouts(ctx, limit)
	if err != nil {
		return PayoutAttention{}, database.MapError(err)
	}
	mismatches, err := q.AdminPayoutMismatches(ctx, limit)
	if err != nil {
		return PayoutAttention{}, database.MapError(err)
	}
	var unpayable []db.AdminUnpayableOwnersRow
	if h.automatic {
		unpayable, err = q.AdminUnpayableOwners(ctx, db.AdminUnpayableOwnersParams{Provider: h.provider, Mode: h.mode, ResultLimit: limit})
		if err != nil {
			return PayoutAttention{}, database.MapError(err)
		}
	}
	out := PayoutAttention{AutomaticPayouts: h.automatic, Stuck: stuck, FailedRecently: failed, Mismatches: mismatches, UnpayableOwners: unpayable}
	if out.Stuck == nil {
		out.Stuck = []db.AdminStuckPayoutsRow{}
	}
	if out.FailedRecently == nil {
		out.FailedRecently = []db.AdminRecentFailedPayoutsRow{}
	}
	if out.Mismatches == nil {
		out.Mismatches = []db.AdminPayoutMismatchesRow{}
	}
	if out.UnpayableOwners == nil {
		out.UnpayableOwners = []db.AdminUnpayableOwnersRow{}
	}
	out.NeedsAttention = len(stuck)+len(failed)+len(mismatches)+len(unpayable) > 0
	return out, nil
}
