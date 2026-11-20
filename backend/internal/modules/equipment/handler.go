package equipment

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/access"
	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/httpx"
)

// Handler exposes catalogue and listing endpoints.
type Handler struct {
	svc      *Service
	optional gin.HandlerFunc // optional auth for public detail (owner preview)
	access   access.Equipment
}

// NewHandler creates the handler. optionalAuth sets the principal when a
// valid token is present and never rejects (may be nil).
// NewHandler builds the handler. acc scopes fleet managers to the machines
// they are authorised for (per-machine routes are guarded by the router).
func NewHandler(svc *Service, optionalAuth gin.HandlerFunc, acc access.Equipment) *Handler {
	return &Handler{svc: svc, optional: optionalAuth, access: acc}
}

func (h *Handler) Name() string { return "equipment" }

func (h *Handler) Routes(r *httpx.Router) {
	cat := r.Tag("catalogue")
	cat.GET("/categories", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q CategoriesQuery) (any, error) {
		lang := q.Lang
		if lang == "" {
			lang = preferredLanguage(c.GetHeader("Accept-Language"))
		}
		return h.svc.Categories(c.Request.Context(), lang)
	}), httpx.Doc{Summary: "Equipment categories (localised)", Query: CategoriesQuery{}, Response: CategoryList{}})
	cat.GET("/makes", httpx.Handle(200, func(c *gin.Context, _ *httpx.Principal) (any, error) {
		return h.svc.Makes(c.Request.Context())
	}), httpx.Doc{Summary: "Manufacturers", Response: MakeList{}})
	cat.GET("/models", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q ModelsQuery) (any, error) {
		return h.svc.Models(c.Request.Context(), q)
	}), httpx.Doc{Summary: "Machine models", Query: ModelsQuery{}, Response: ModelList{}})
	cat.GET("/cancellation-policies", httpx.Handle(200, func(c *gin.Context, _ *httpx.Principal) (any, error) {
		return h.svc.CancellationPresets(c.Request.Context())
	}), httpx.Doc{Summary: "Cancellation policy presets", Response: CancellationPresetList{}})
	cat.GET("/cities", httpx.Handle(200, func(c *gin.Context, _ *httpx.Principal) (any, error) {
		return h.svc.Cities(c.Request.Context())
	}), httpx.Doc{Summary: "Cities served", Response: CityList{}})

	pub := r.Tag("equipment")
	if h.optional != nil {
		pub = pub.Use(h.optional)
	}
	pub.GET("/equipment/:id", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Public(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Listing detail", Response: Listing{}})

	o := r.Tag("owner-equipment").Authed()
	org := httpx.Doc{OrgScoped: true}
	o.GET("/owner/equipment", h.owned(func(c *gin.Context, owner orgCtx) (any, error) {
		var q ListQuery
		if err := c.ShouldBindQuery(&q); err != nil {
			return nil, httpx.BindError(err)
		}
		only, err := access.Filter(c.Request.Context(), h.access, owner.p, owner.org)
		if err != nil {
			return nil, err
		}
		return h.svc.ListMine(c.Request.Context(), owner.org, q, only)
	}), with(org, httpx.Doc{Summary: "My listings", Query: ListQuery{}, Response: SummaryPage{}}))
	o.POST("/owner/equipment", h.owned(func(c *gin.Context, owner orgCtx) (any, error) {
		var req CreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			return nil, httpx.BindError(err)
		}
		if !owner.p.HasOrgRole(owner.org, httpx.RoleOwner) {
			if blocked, err := h.access.ManagementBlocked(c.Request.Context(), owner.p.UserID); err != nil {
				return nil, err
			} else if blocked {
				return nil, apperror.ErrForbidden.WithDetail("your fleet management access is restricted by EQUIPRENT")
			}
		}
		l, err := h.svc.Create(c.Request.Context(), owner.org, req)
		if err == nil && !owner.p.HasOrgRole(owner.org, httpx.RoleOwner) {
			// A fleet manager manages the machines they add.
			err = h.access.Grant(c.Request.Context(), owner.p.UserID, owner.org, l.ID)
		}
		return l, err
	}, 201), with(org, httpx.Doc{Summary: "Create a draft listing", Request: CreateRequest{}, Response: Listing{}, Status: 201}))
	o.GET("/owner/equipment/:id", h.ownedID(func(c *gin.Context, owner orgCtx) (any, error) {
		return h.svc.OwnerView(c.Request.Context(), owner.org, owner.id)
	}), with(org, httpx.Doc{Summary: "One of my listings (with documents and moderation notes)", Response: Listing{}}))
	o.PATCH("/owner/equipment/:id", h.ownedID(func(c *gin.Context, owner orgCtx) (any, error) {
		var req UpdateRequest
		if v := ifMatchVersion(c); v > 0 {
			req.Version = int32(v)
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			return nil, httpx.BindError(err)
		}
		return h.svc.Update(c.Request.Context(), owner.org, owner.id, req)
	}), with(org, httpx.Doc{Summary: "Edit a listing (optimistic locking)", Request: UpdateRequest{}, Response: Listing{},
		Description: "Send the version you read in the body or as If-Match: \"<version>\". 412 VERSION_MISMATCH if it changed."}))
	o.DELETE("/owner/equipment/:id", h.ownedID(func(c *gin.Context, owner orgCtx) (any, error) {
		return nil, h.svc.Retire(c.Request.Context(), owner.org, owner.id)
	}, 204), with(org, httpx.Doc{Summary: "Retire a listing"}))
	for _, a := range []Action{ActionSubmit, ActionPause, ActionResume} {
		o.POST("/owner/equipment/:id/"+string(a), h.ownedID(func(c *gin.Context, owner orgCtx) (any, error) {
			return h.svc.Act(c.Request.Context(), owner.org, owner.id, a)
		}), with(org, httpx.Doc{Summary: actionSummary[a], Response: StatusChange{}}))
	}
	o.PUT("/owner/equipment/:id/pricing", h.ownedID(func(c *gin.Context, owner orgCtx) (any, error) {
		var req PricingRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			return nil, httpx.BindError(err)
		}
		return h.svc.SetPricing(c.Request.Context(), owner.org, owner.id, req)
	}), with(org, httpx.Doc{Summary: "Replace prices and mobilisation rates", Request: PricingRequest{}, Response: Listing{}}))
	o.GET("/owner/equipment/:id/booking-rules", h.ownedID(func(c *gin.Context, owner orgCtx) (any, error) {
		return h.svc.BookingRules(c.Request.Context(), owner.org, owner.id)
	}), with(org, httpx.Doc{Summary: "Rental rules: duration limits, deposit, fuel, late return, pickup, time zone, cancellation", Response: BookingRules{}}))
	o.PUT("/owner/equipment/:id/booking-rules", h.ownedID(func(c *gin.Context, owner orgCtx) (any, error) {
		var req BookingRulesRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			return nil, httpx.BindError(err)
		}
		return h.svc.SetBookingRules(c.Request.Context(), owner.org, owner.id, req)
	}), with(org, httpx.Doc{Summary: "Replace the rental rules", Request: BookingRulesRequest{}, Response: BookingRules{},
		Description: "Takes effect for new estimates and bookings; existing bookings keep the terms frozen in their price."}))
	o.PUT("/owner/equipment/:id/specs", h.ownedID(func(c *gin.Context, owner orgCtx) (any, error) {
		var req SpecsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			return nil, httpx.BindError(err)
		}
		return h.svc.SetSpecs(c.Request.Context(), owner.org, owner.id, req)
	}), with(org, httpx.Doc{Summary: "Set specifications", Request: SpecsRequest{}, Response: Listing{}}))
	h.listingRoutes(r)
}

var actionSummary = map[Action]string{
	ActionSubmit: "Submit for review", ActionPause: "Pause (hide from search)", ActionResume: "Resume a paused listing",
}

type orgCtx struct {
	p   *httpx.Principal
	org uuid.UUID
	id  uuid.UUID
}

// owned runs fn for a caller acting for an owner organisation.
func (h *Handler) owned(fn func(c *gin.Context, o orgCtx) (any, error), status ...int) gin.HandlerFunc {
	code := 200
	if len(status) > 0 {
		code = status[0]
	}
	return httpx.OrgHandle(code, OwnerRoles, func(c *gin.Context, s httpx.Scope) (any, error) {
		return fn(c, orgCtx{p: s.P, org: s.Org})
	})
}

// ownedID is owned plus the :id path parameter.
func (h *Handler) ownedID(fn func(c *gin.Context, o orgCtx) (any, error), status ...int) gin.HandlerFunc {
	return h.owned(func(c *gin.Context, o orgCtx) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		o.id = id
		return fn(c, o)
	}, status...)
}

func with(base, d httpx.Doc) httpx.Doc {
	d.OrgScoped = base.OrgScoped
	return d
}

// ifMatchVersion parses If-Match: "3" (or W/"3").
func ifMatchVersion(c *gin.Context) int {
	v := strings.Trim(strings.TrimPrefix(c.GetHeader("If-Match"), "W/"), `"`)
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}

func preferredLanguage(header string) string {
	for _, part := range strings.Split(header, ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]))
		tag = strings.SplitN(tag, "-", 2)[0]
		switch tag {
		case "en", "hi", "mr", "ta", "te", "kn", "gu", "bn":
			return tag
		}
	}
	return "en"
}
