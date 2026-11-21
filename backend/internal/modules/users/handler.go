package users

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/equiprent/backend/internal/httpx"
)

// Handler exposes /me and /orgs.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Name() string { return "users" }

func (h *Handler) Routes(r *httpx.Router) {
	a := r.Tag("users").Authed()
	a.GET("/me", h.me, httpx.Doc{Summary: "Current user with roles", Response: Profile{}})
	a.PATCH("/me", h.updateMe, httpx.Doc{Summary: "Update profile", Request: UpdateProfileRequest{}, Response: Profile{}})
	a.GET("/me/orgs", h.myOrgs, httpx.Doc{Summary: "Organisations I belong to", Response: OrgList{}})
	a.POST("/orgs", h.createOrg, httpx.Doc{Summary: "Create a business organisation", Request: CreateOrgRequest{}, Response: Organization{}, Status: 201})
	a.GET("/orgs/:org_id", h.org, httpx.Doc{Summary: "Organisation details", Response: Organization{}})
	a.PATCH("/orgs/:org_id", h.updateOrg, httpx.Doc{Summary: "Update organisation (org admin)", Request: UpdateOrgRequest{}, Response: Organization{}})
	a.GET("/orgs/:org_id/members", h.members, httpx.Doc{Summary: "List members", Response: MemberList{}})
	a.POST("/orgs/:org_id/members", h.addMember, httpx.Doc{Summary: "Add a member by phone", Request: AddMemberRequest{}, Response: Member{}, Status: 201})
	a.DELETE("/orgs/:org_id/members/:user_id", h.removeMember, httpx.Doc{Summary: "Revoke a member's role", Query: RemoveMemberQuery{}})
	a.GET("/me/invitations", h.myInvitations, httpx.Doc{Summary: "Invitations to join an organisation", Response: InvitationList{}})
	a.POST("/invitations/:id/accept", h.respondInvitation(true), httpx.Doc{Summary: "Accept an invitation (the role is granted only now)"})
	a.POST("/invitations/:id/decline", h.respondInvitation(false), httpx.Doc{Summary: "Decline an invitation"})
	a.GET("/orgs/:org_id/invitations", h.orgInvitations, httpx.Doc{Summary: "Pending invitations of the organisation", Response: OrgInvitationList{}})
	a.DELETE("/orgs/:org_id/invitations/:id", h.revokeInvitation, httpx.Doc{Summary: "Withdraw a pending invitation"})
	a.GET("/orgs/:org_id/addresses", h.addresses, httpx.Doc{Summary: "Saved sites and yards", Response: AddressList{}})
	a.POST("/orgs/:org_id/addresses", h.createAddress, httpx.Doc{Summary: "Save a site or yard", Request: CreateAddressRequest{}, Response: Address{}, Status: 201})
	a.DELETE("/orgs/:org_id/addresses/:id", h.deleteAddress, httpx.Doc{Summary: "Delete a saved address"})
}

func (h *Handler) me(c *gin.Context) {
	if p, ok := httpx.MustPrincipal(c); ok {
		httpx.Send(c, http.StatusOK)(h.svc.Me(c.Request.Context(), p))
	}
}

func (h *Handler) updateMe(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	var req UpdateProfileRequest
	if ok && httpx.BindJSON(c, &req) {
		httpx.Send(c, http.StatusOK)(h.svc.UpdateMe(c.Request.Context(), p, req))
	}
}

func (h *Handler) myOrgs(c *gin.Context) {
	if p, ok := httpx.MustPrincipal(c); ok {
		httpx.Send(c, http.StatusOK)(h.svc.MyOrgs(c.Request.Context(), p))
	}
}

func (h *Handler) createOrg(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	var req CreateOrgRequest
	if ok && httpx.BindJSON(c, &req) {
		httpx.Send(c, http.StatusCreated)(h.svc.CreateOrg(c.Request.Context(), p, req))
	}
}

func (h *Handler) org(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	if orgID, ok := httpx.UUIDParam(c, "org_id"); ok {
		httpx.Send(c, http.StatusOK)(h.svc.Org(c.Request.Context(), p, orgID))
	}
}

func (h *Handler) updateOrg(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	orgID, ok := httpx.UUIDParam(c, "org_id")
	var req UpdateOrgRequest
	if ok && httpx.BindJSON(c, &req) {
		httpx.Send(c, http.StatusOK)(h.svc.UpdateOrg(c.Request.Context(), p, orgID, req))
	}
}

func (h *Handler) members(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	if orgID, ok := httpx.UUIDParam(c, "org_id"); ok {
		httpx.Send(c, http.StatusOK)(h.svc.Members(c.Request.Context(), p, orgID))
	}
}

func (h *Handler) addMember(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	orgID, ok := httpx.UUIDParam(c, "org_id")
	var req AddMemberRequest
	if ok && httpx.BindJSON(c, &req) {
		httpx.Send(c, http.StatusCreated)(h.svc.AddMember(c.Request.Context(), p, orgID, req))
	}
}

func (h *Handler) myInvitations(c *gin.Context) {
	if p, ok := httpx.MustPrincipal(c); ok {
		httpx.Send(c, http.StatusOK)(h.svc.MyInvitations(c.Request.Context(), p))
	}
}

func (h *Handler) respondInvitation(accept bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := httpx.MustPrincipal(c)
		if !ok {
			return
		}
		if id, ok := httpx.UUIDParam(c, "id"); ok {
			httpx.Done(c, h.svc.RespondInvitation(c.Request.Context(), p, id, accept))
		}
	}
}

func (h *Handler) orgInvitations(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	if orgID, ok := httpx.UUIDParam(c, "org_id"); ok {
		httpx.Send(c, http.StatusOK)(h.svc.OrgInvitations(c.Request.Context(), p, orgID))
	}
}

func (h *Handler) revokeInvitation(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	orgID, ok := httpx.UUIDParam(c, "org_id")
	if !ok {
		return
	}
	if id, ok := httpx.UUIDParam(c, "id"); ok {
		httpx.Done(c, h.svc.RevokeInvitation(c.Request.Context(), p, orgID, id))
	}
}

func (h *Handler) removeMember(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	orgID, ok := httpx.UUIDParam(c, "org_id")
	if !ok {
		return
	}
	userID, ok := httpx.UUIDParam(c, "user_id")
	var q RemoveMemberQuery
	if ok && httpx.BindQuery(c, &q) {
		httpx.Done(c, h.svc.RemoveMember(c.Request.Context(), p, orgID, userID, q.Role))
	}
}

func (h *Handler) addresses(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	if orgID, ok := httpx.UUIDParam(c, "org_id"); ok {
		httpx.Send(c, http.StatusOK)(h.svc.Addresses(c.Request.Context(), p, orgID))
	}
}

func (h *Handler) createAddress(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	orgID, ok := httpx.UUIDParam(c, "org_id")
	var req CreateAddressRequest
	if ok && httpx.BindJSON(c, &req) {
		httpx.Send(c, http.StatusCreated)(h.svc.CreateAddress(c.Request.Context(), p, orgID, req))
	}
}

func (h *Handler) deleteAddress(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	orgID, ok := httpx.UUIDParam(c, "org_id")
	if !ok {
		return
	}
	if id, ok := httpx.UUIDParam(c, "id"); ok {
		httpx.Done(c, h.svc.DeleteAddress(c.Request.Context(), p, orgID, id))
	}
}
