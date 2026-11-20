package verification

import (
	"github.com/gin-gonic/gin"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/httpx"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Name() string { return "verification" }

var ownerRoles = []string{httpx.RoleOwner}

// operator returns the caller if they hold the operator role anywhere.
func operator(p *httpx.Principal) error {
	for _, o := range p.Orgs() {
		if p.HasOrgRole(o, httpx.RoleOperator) {
			return nil
		}
	}
	return apperror.ErrForbidden.WithDetail("only operators have an operator verification")
}

// opHandle runs fn for callers holding the operator role.
func opHandle(status int, fn func(c *gin.Context, p *httpx.Principal) (any, error)) gin.HandlerFunc {
	return httpx.Handle(status, func(c *gin.Context, p *httpx.Principal) (any, error) {
		if err := operator(p); err != nil {
			return nil, err
		}
		return fn(c, p)
	})
}

func (h *Handler) Routes(r *httpx.Router) {
	a := r.Tag("verification").Authed()

	// customer
	a.GET("/verification/customer", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		return h.svc.Customer(c.Request.Context(), p)
	}), httpx.Doc{Summary: "My customer profile checklist", Response: CustomerStatus{}})
	a.PUT("/me/avatar", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req AvatarRequest) (any, error) {
		return h.svc.SetAvatar(c.Request.Context(), p, req)
	}), httpx.Doc{Summary: "Set my profile image from an upload (purpose avatar)", Request: AvatarRequest{}, Response: CustomerStatus{}})

	// owner (acting organisation, owner role)
	a.GET("/verification/owner", httpx.OrgHandle(200, ownerRoles, func(c *gin.Context, s httpx.Scope) (any, error) {
		return h.svc.Owner(c.Request.Context(), s.Org)
	}), httpx.Doc{Summary: "Owner verification status, details, documents and payout account", Response: OwnerVerification{}})
	a.PUT("/verification/owner/business", httpx.OrgHandle(200, ownerRoles, func(c *gin.Context, s httpx.Scope) (any, error) {
		var req OwnerBusinessRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.SaveBusiness(c.Request.Context(), s, req)
	}), httpx.Doc{Summary: "Save business, contact and address details", Request: OwnerBusinessRequest{}, Response: OwnerVerification{}})
	a.POST("/verification/owner/documents", httpx.OrgHandle(201, ownerRoles, func(c *gin.Context, s httpx.Scope) (any, error) {
		var req DocumentRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.AddOwnerDocument(c.Request.Context(), s, req)
	}), httpx.Doc{Summary: "Attach an identity or business document", Request: DocumentRequest{}, Response: Document{}, Status: 201})
	a.DELETE("/verification/owner/documents/:id", httpx.OrgHandle(204, ownerRoles, func(c *gin.Context, s httpx.Scope) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return httpx.Nothing, h.svc.DeleteOwnerDocument(c.Request.Context(), s, id)
	}), httpx.Doc{Summary: "Remove a document that is not verified", Status: 204})
	a.PUT("/verification/owner/bank-account", httpx.OrgHandle(200, ownerRoles, func(c *gin.Context, s httpx.Scope) (any, error) {
		var req BankAccountRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.SetBankAccount(c.Request.Context(), s, req)
	}), httpx.Doc{Summary: "Set the payout account (verified by the payment provider)", Request: BankAccountRequest{}, Response: BankAccount{},
		Description: "The account is validated by the approved payment provider before it is saved. Only the last 4 digits are ever returned."})
	a.POST("/verification/owner/submit", httpx.OrgHandle(200, ownerRoles, func(c *gin.Context, s httpx.Scope) (any, error) {
		return h.svc.SubmitOwner(c.Request.Context(), s)
	}), httpx.Doc{Summary: "Submit owner verification for review", Response: Application{}})
	a.POST("/verification/owner/reopen", httpx.OrgHandle(200, ownerRoles, func(c *gin.Context, s httpx.Scope) (any, error) {
		return h.svc.ReopenOwner(c.Request.Context(), s)
	}), httpx.Doc{Summary: "Reopen submitted or verified business details for editing", Response: OwnerVerification{},
		Description: "The application returns to draft and the organisation to pending; it must be submitted and approved again."})
	a.GET("/verification/owner/fleet-managers", httpx.OrgHandle(200, ownerRoles, func(c *gin.Context, s httpx.Scope) (any, error) {
		return h.svc.FleetManagers(c.Request.Context(), s.Org)
	}), httpx.Doc{Summary: "Fleet managers of my organisation and their access", Response: FleetManagerList{}})
	a.PUT("/verification/owner/fleet-managers/:user_id", httpx.OrgHandle(200, ownerRoles, func(c *gin.Context, s httpx.Scope) (any, error) {
		user, err := httpx.Param(c, "user_id")
		if err != nil {
			return nil, err
		}
		var req FleetAccessRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.SetFleetAccess(c.Request.Context(), s, user, req)
	}), httpx.Doc{Summary: "Set which machines a fleet manager may manage", Request: FleetAccessRequest{}, Response: FleetManagerSummary{}})

	// operator
	a.GET("/verification/operator", opHandle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		return h.svc.Operator(c.Request.Context(), p.UserID)
	}), httpx.Doc{Summary: "Operator verification status, licence, qualifications and documents", Response: OperatorVerification{}})
	a.PUT("/verification/operator/profile", opHandle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		var req OperatorProfileRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.SaveOperatorProfile(c.Request.Context(), p, req)
	}), httpx.Doc{Summary: "Save licence, experience, skills and availability", Request: OperatorProfileRequest{}, Response: OperatorVerification{}})
	a.PUT("/verification/operator/address", opHandle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		var req OperatorHomeAddressRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.SaveOperatorHomeAddress(c.Request.Context(), p, req)
	}), httpx.Doc{Summary: "Save the driver's private home address", Request: OperatorHomeAddressRequest{}, Response: OperatorVerification{}})
	a.POST("/verification/operator/qualifications", opHandle(201, func(c *gin.Context, p *httpx.Principal) (any, error) {
		var req QualificationRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.AddQualification(c.Request.Context(), p, req)
	}), httpx.Doc{Summary: "Add a qualification", Request: QualificationRequest{}, Response: Qualification{}, Status: 201})
	a.DELETE("/verification/operator/qualifications/:id", opHandle(204, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return httpx.Nothing, h.svc.DeleteQualification(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Remove a qualification", Status: 204})
	a.POST("/verification/operator/documents", opHandle(201, func(c *gin.Context, p *httpx.Principal) (any, error) {
		var req DocumentRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.AddOperatorDocument(c.Request.Context(), p, req)
	}), httpx.Doc{Summary: "Attach a licence or identity document", Request: DocumentRequest{}, Response: Document{}, Status: 201})
	a.DELETE("/verification/operator/documents/:id", opHandle(204, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return httpx.Nothing, h.svc.DeleteOperatorDocument(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Remove a document that is not verified", Status: 204})
	a.POST("/verification/operator/submit", opHandle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		return h.svc.SubmitOperator(c.Request.Context(), p)
	}), httpx.Doc{Summary: "Submit operator verification for review", Response: Application{}})

	// fleet manager
	a.GET("/verification/fleet-manager", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		return h.svc.FleetManager(c.Request.Context(), p)
	}), httpx.Doc{Summary: "My company and the machines I am authorised to manage", Response: FleetManagerView{}})

	// documents
	a.GET("/verification/documents/:id/url", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.DocumentURL(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Get a short-lived signed link to a verification document", Response: DocumentURLResponse{},
		Description: "Only the document's owner organisation, its operator, or a KYC reviewer may get a link. Every link issued is audited."})

	// admin review
	ad := r.Tag("admin").Admin(ReviewerRoles...)
	adWrite := ad.AdminWrites(
		"POST /admin/verifications/:id/start-review",
		"POST /admin/verifications/:id/documents/:doc_id/review",
		"POST /admin/verifications/:id/approve",
		"POST /admin/verifications/:id/reject",
	)
	ad.GET("/admin/verifications", httpx.HandleQuery(200, func(c *gin.Context, p *httpx.Principal, q QueueQuery) (any, error) {
		return h.svc.Queue(c.Request.Context(), q)
	}), httpx.Doc{Summary: "Verification review queue", Query: QueueQuery{}, Response: ApplicationPage{}})
	ad.GET("/admin/verifications/:id", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Detail(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Application with documents (signed links) and history", Response: ApplicationDetail{}})
	adWrite.POST("/admin/verifications/:id/start-review", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.StartReview(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Start reviewing an application", Response: Application{}})
	adWrite.POST("/admin/verifications/:id/documents/:doc_id/review", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		ids, err := httpx.Params(c, "id", "doc_id")
		if err != nil {
			return nil, err
		}
		var req DocumentReviewRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.ReviewDocument(c.Request.Context(), p, ids[0], ids[1], req)
	}), httpx.Doc{Summary: "Approve or reject one document", Request: DocumentReviewRequest{}, Response: Document{}})
	adWrite.POST("/admin/verifications/:id/approve", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		var req DecisionRequest
		if c.Request.ContentLength > 0 {
			if err := httpx.Bind(c, &req); err != nil {
				return nil, err
			}
		}
		return h.svc.Approve(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Approve an application", Request: DecisionRequest{}, Response: Application{}})
	adWrite.POST("/admin/verifications/:id/reject", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		var req DecisionRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.Reject(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Reject (or revoke) an application with a reason", Request: DecisionRequest{}, Response: Application{}})
}
