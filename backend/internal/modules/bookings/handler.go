package bookings

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/httpx"
)

// Handler exposes the booking endpoints for customers, owners and operators.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Name() string { return "bookings" }

var customerRoles = []string{httpx.RoleCustomer}
var ownerRoles = []string{httpx.RoleOwner, httpx.RoleFleetManager}

func (h *Handler) Routes(r *httpx.Router) {
	pub := r.Tag("bookings")
	pub.GET("/booking-policy", httpx.Handle(200, func(c *gin.Context, _ *httpx.Principal) (any, error) {
		return h.svc.Policy(c.Request.Context())
	}), httpx.Doc{Summary: "Booking, payment and cancellation policy", Response: Policy{}})
	pub.POST("/equipment/:id/estimate", httpx.HandleJSON(200, func(c *gin.Context, _ *httpx.Principal, req EstimateRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Estimate(c.Request.Context(), id, req)
	}), httpx.Doc{Summary: "Estimated charges and availability for a listing", Request: EstimateRequest{}, Response: EstimateResponse{},
		Description: "An estimate (kind estimate), recomputed from the listing's stored terms: rental lines (auto = cheapest mix of " +
			"month/week/day/hour plans), extra hours, delivery, pickup, operator, fuel, platform fee, each configured tax, " +
			"refundable deposit and advance, all in paise. Give an exact period or local dates in the listing's time zone. " +
			"Final charges are settled after the rental."})
	pub.GET("/equipment/:id/availability/check", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q AvailabilityQuery) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.CheckAvailability(c.Request.Context(), id, q)
	}), httpx.Doc{Summary: "Is the machine free for a period?", Query: AvailabilityQuery{}, Response: AvailabilityResult{},
		Description: "Checks held bookings, owner blocks and the owner's minimum/maximum duration, and suggests the next free start."})

	a := r.Tag("bookings").Authed()
	a.POST("/bookings", httpx.OrgHandle(201, customerRoles, func(c *gin.Context, s httpx.Scope) (any, error) {
		var req CreateRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.Create(c.Request.Context(), s.P, s.Org, req)
	}), httpx.Doc{Summary: "Request a booking", Request: CreateRequest{}, Response: Booking{}, Status: 201, Idempotent: true, OrgScoped: true,
		Description: "Instant-book listings are confirmed (or await payment) at once; others wait for the owner until owner_response_due_at."})
	a.GET("/bookings", httpx.OrgHandle(200, customerRoles, func(c *gin.Context, s httpx.Scope) (any, error) {
		var q ListQuery
		if err := httpx.BindQ(c, &q); err != nil {
			return nil, err
		}
		return h.svc.List(c.Request.Context(), s.P, "customer", s.Org, q)
	}), httpx.Doc{Summary: "My organisation's bookings (as customer)", Query: ListQuery{}, Response: SummaryPage{}, OrgScoped: true})
	a.GET("/bookings/:id", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Get(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Booking detail with timeline", Response: Booking{}})
	a.POST("/bookings/:id/cancel", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		var q CancelQuery
		if err := httpx.BindQ(c, &q); err != nil {
			return nil, err
		}
		var req ActionRequest
		if !q.DryRun {
			if err := httpx.Bind(c, &req); err != nil {
				return nil, err
			}
		}
		return h.svc.Cancel(c.Request.Context(), p, id, req, q.DryRun)
	}), httpx.Doc{Summary: "Cancel (or preview the fee and refund with ?dry_run=true)", Query: CancelQuery{}, Request: ActionRequest{}, Response: CancelResult{},
		Description: "Before dispatch the booking is cancelled at once (refund_pending when money is returned). Once the machine " +
			"has left the yard a customer's cancellation becomes cancellation_requested and the owner approves or declines it. " +
			"Customers pay the policy fee after the owner accepted; owner cancellations refund in full."})
	h.action(a, "/bookings/:id/handover/confirm", ActionStart, "Confirm handover (machine starts working)")
	h.action(a, "/bookings/:id/return-request", ActionRequestReturn, "Release the machine")

	// drafts
	a.PATCH("/bookings/:id", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req DraftRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.UpdateDraft(c.Request.Context(), p, id, req.create(), req.Version)
	}), httpx.Doc{Summary: "Edit a draft (re-priced)", Request: DraftRequest{}, Response: Booking{}})
	a.POST("/bookings/:id/submit", h.withBody(func(c *gin.Context, p *httpx.Principal, id uuid.UUID, req ActionRequest) (any, error) {
		return h.svc.Submit(c.Request.Context(), p, id, req.Version)
	}), httpx.Doc{Summary: "Submit a draft", Request: ActionRequest{}, Response: Booking{}, Idempotent: true,
		Description: "Re-prices the draft, checks the dates and sends it to the owner (or, for instant-book listings, awaits payment)."})

	// cancellation requests
	a.POST("/bookings/:id/cancellation/withdraw", h.cancellation("withdraw"), httpx.Doc{Summary: "Withdraw a cancellation request", Request: ActionRequest{}, Response: Transition{}})

	// extensions
	a.POST("/bookings/:id/extensions/quote", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req ExtensionRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		req.DryRun = true
		return h.svc.RequestExtension(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Price an extension and check the dates", Request: ExtensionRequest{}, Response: ExtensionQuote{}})
	a.POST("/bookings/:id/extensions", httpx.HandleJSON(201, func(c *gin.Context, p *httpx.Principal, req ExtensionRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.RequestExtension(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Request an extension (customer or owner)", Request: ExtensionRequest{}, Response: ExtensionQuote{}, Status: 201, Idempotent: true,
		Description: "Only active rentals. The other party approves or declines; unanswered requests lapse when the rental reaches its current end."})
	a.GET("/bookings/:id/extensions", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Extensions(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Extension requests of a booking", Response: ExtensionList{}})
	for _, d := range []string{"approve", "decline", "withdraw"} {
		a.POST("/bookings/:id/extensions/:ext_id/"+d, h.extension(d), httpx.Doc{Summary: strings.ToUpper(d[:1]) + d[1:] + " an extension request",
			Request: ActionRequest{}, Response: Extension{}})
	}

	o := r.Tag("owner-bookings").Authed()
	o.GET("/owner/bookings", httpx.OrgHandle(200, ownerRoles, func(c *gin.Context, s httpx.Scope) (any, error) {
		var q ListQuery
		if err := httpx.BindQ(c, &q); err != nil {
			return nil, err
		}
		return h.svc.List(c.Request.Context(), s.P, "owner", s.Org, q)
	}), httpx.Doc{Summary: "Booking inbox (as owner)", Query: ListQuery{}, Response: SummaryPage{}, OrgScoped: true})
	h.action(o, "/owner/bookings/:id/accept", ActionAccept, "Accept a request")
	h.action(o, "/owner/bookings/:id/reject", ActionReject, "Reject a request (reason required)")
	h.action(o, "/owner/bookings/:id/dispatch", ActionDispatch, "Dispatch the machine (starts the mobilisation trip)")
	h.action(o, "/owner/bookings/:id/mark-returned", ActionMarkReturned, "Confirm the machine is back")
	h.action(o, "/owner/bookings/:id/complete", ActionComplete, "Complete the booking")
	o.POST("/owner/bookings/:id/cancellation/approve", h.cancellation("approve"), httpx.Doc{Summary: "Approve a customer's cancellation request",
		Request: ActionRequest{}, Response: Transition{}, Description: "Cancels the booking; paid money minus the policy fee is refunded."})
	o.POST("/owner/bookings/:id/cancellation/decline", h.cancellation("decline"), httpx.Doc{Summary: "Decline a cancellation request (reason required)",
		Request: ActionRequest{}, Response: Transition{}, Description: "The booking returns to where it was (dispatched or on site)."})
	o.POST("/owner/bookings/:id/assign-operator", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req AssignOperatorRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.AssignOperator(c.Request.Context(), p, id, req.OperatorUserID)
	}), httpx.Doc{Summary: "Assign an operator", Request: AssignOperatorRequest{}, Response: Transition{}})
	o.POST("/owner/bookings/:id/driver-compensation", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req DriverCompensationRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.RecordDriverCompensation(c.Request.Context(), p, id, req.AgreedAmountPaise)
	}), httpx.Doc{Summary: "Record agreed payment for the assigned driver", Request: DriverCompensationRequest{}, Response: DriverCompensation{}})
	o.GET("/owner/driver-compensations", httpx.OrgHandle(200, []string{httpx.RoleOwner}, func(c *gin.Context, sc httpx.Scope) (any, error) {
		items, err := h.svc.DriverCompensations(c.Request.Context(), sc.P, sc.Org)
		return DriverCompensationList{Data: items}, err
	}), httpx.Doc{Summary: "Driver payment history", Response: DriverCompensationList{}, OrgScoped: true})
	o.POST("/owner/driver-compensations/:id/pay", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req DriverCompensationPaymentRequest) (any, error) {
		_, org, ok := httpx.ActingOrg(c, httpx.RoleOwner)
		if !ok {
			return nil, apperror.ErrForbidden
		}
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.PayDriverCompensation(c.Request.Context(), p, org, id, req.TransactionReference)
	}), httpx.Doc{Summary: "Record completed driver payment", Request: DriverCompensationPaymentRequest{}, Response: DriverCompensation{}})

	o.POST("/owner/bookings/:id/final-charges", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		var q CancelQuery
		if err := httpx.BindQ(c, &q); err != nil {
			return nil, err
		}
		var req FinalChargesRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.SettleFinal(c.Request.Context(), p, id, req, q.DryRun)
	}), httpx.Doc{Summary: "Settle final charges (dry_run=true to preview)", Request: FinalChargesRequest{}, Response: FinalChargesResult{},
		Description: "Uses the rates frozen in the booking's estimate: the booked period is the minimum; late return per started hour " +
			"after the grace period; hour-meter extra hours; owner-supplied fuel on worked hours; damage recovery. Saved once."})

	fin := r.Tag("bookings").Admin(httpx.RoleAdminFinance)
	fin.GET("/admin/tax-rates", httpx.Handle(200, func(c *gin.Context, _ *httpx.Principal) (any, error) {
		return h.svc.TaxRates(c.Request.Context())
	}), httpx.Doc{Summary: "Configured taxes", Response: TaxRateList{}})
	fin.PUT("/admin/tax-rates", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req TaxRateRequest) (any, error) {
		return h.svc.SaveTaxRate(c.Request.Context(), p, req)
	}), httpx.Doc{Summary: "Create or replace a tax from a date", Request: TaxRateRequest{}, Response: TaxRateList{},
		Description: "A new valid_from starts a new rate; bookings keep the taxes frozen in their estimate. Saving clears the rule's review."})
	fin.POST("/admin/tax-rates/:id/review", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req TaxReviewRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.ReviewTaxRate(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Record the compliance review of a tax rule", Request: TaxReviewRequest{}, Response: TaxRateList{},
		Description: "Staging and production refuse to issue invoices while an active rule has no recorded review."})

	op := r.Tag("operator-jobs").Authed()
	op.GET("/operator/driver-compensations", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		items, err := h.svc.OperatorDriverCompensations(c.Request.Context(), p)
		if err != nil {
			return nil, err
		}
		history, err := h.svc.OperatorDriverCompensationHistory(c.Request.Context(), p)
		return DriverCompensationList{Data: items, History: history}, err
	}), httpx.Doc{Summary: "My agreed driver payment and payment history", Response: DriverCompensationList{}})
	op.POST("/operator/jobs/:booking_id/accept", h.respond(true), httpx.Doc{Summary: "Accept an assigned job", Response: Transition{}})
	op.POST("/operator/jobs/:booking_id/decline", h.respond(false), httpx.Doc{Summary: "Decline an assigned job", Response: Transition{}})
	h.actionAt(op, "/operator/jobs/:booking_id/dispatch", "booking_id", ActionDispatch, "Leave for the site")
	h.actionAt(op, "/operator/jobs/:booking_id/arrived", "booking_id", ActionArrive, "Arrived on site (manual geofence fallback)")
	h.actionAt(op, "/operator/jobs/:booking_id/start", "booking_id", ActionStart, "Start work after handover")
	h.actionAt(op, "/operator/jobs/:booking_id/returned", "booking_id", ActionMarkReturned, "Machine handed back")
}

func (h *Handler) action(r *httpx.Router, path string, act Action, summary string) {
	h.actionAt(r, path, "id", act, summary)
}

func (h *Handler) actionAt(r *httpx.Router, path, param string, act Action, summary string) {
	r.POST(path, httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, param)
		if err != nil {
			return nil, err
		}
		var req ActionRequest
		if c.Request.ContentLength > 0 {
			if err := httpx.Bind(c, &req); err != nil {
				return nil, err
			}
		}
		return h.svc.Act(c.Request.Context(), p, id, act, req)
	}), httpx.Doc{Summary: summary, Request: ActionRequest{}, Response: Transition{}})
}

// withBody parses :id and an optional ActionRequest body.
func (h *Handler) withBody(fn func(c *gin.Context, p *httpx.Principal, id uuid.UUID, req ActionRequest) (any, error)) gin.HandlerFunc {
	return httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		var req ActionRequest
		if c.Request.ContentLength > 0 {
			if err := httpx.Bind(c, &req); err != nil {
				return nil, err
			}
		}
		return fn(c, p, id, req)
	})
}

func (h *Handler) cancellation(decision string) gin.HandlerFunc {
	return h.withBody(func(c *gin.Context, p *httpx.Principal, id uuid.UUID, req ActionRequest) (any, error) {
		return h.svc.RespondToCancellation(c.Request.Context(), p, id, decision, req)
	})
}

func (h *Handler) extension(decision string) gin.HandlerFunc {
	return h.withBody(func(c *gin.Context, p *httpx.Principal, id uuid.UUID, req ActionRequest) (any, error) {
		ext, err := httpx.Param(c, "ext_id")
		if err != nil {
			return nil, err
		}
		return h.svc.RespondToExtension(c.Request.Context(), p, id, ext, decision, req)
	})
}

func (h *Handler) respond(accept bool) gin.HandlerFunc {
	return httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "booking_id")
		if err != nil {
			return nil, err
		}
		return h.svc.RespondToJob(c.Request.Context(), p, id, accept)
	})
}
