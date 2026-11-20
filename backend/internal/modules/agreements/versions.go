package agreements

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/httpx"
)

// ---- agreement versions -----------------------------------------------------------------

// VersionInfo is one issued version of a booking's agreement.
type VersionInfo struct {
	Version            int32      `json:"version"`
	Status             string     `json:"status" doc:"current, executed (current), superseded"`
	TemplateVersion    string     `json:"template_version"`
	LegalTermsVersion  string     `json:"legal_terms_version,omitempty"`
	TermsSHA256        string     `json:"terms_sha256"`
	CreatedAt          time.Time  `json:"created_at"`
	SupersededAt       *time.Time `json:"superseded_at,omitempty"`
	SupersedeReason    *string    `json:"supersede_reason,omitempty"`
	CustomerAcceptedAt *time.Time `json:"customer_accepted_at,omitempty"`
	OwnerAcceptedAt    *time.Time `json:"owner_accepted_at,omitempty"`
}

type VersionList struct {
	Data []VersionInfo `json:"data"`
}

// Versions lists every version of the booking's agreement, newest first.
func (s *Service) Versions(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID) (VersionList, error) {
	if _, _, err := s.bookings.Access(ctx, p, bookingID); err != nil {
		return VersionList{}, err
	}
	rows, err := s.repo.Versions(ctx, bookingID)
	if err != nil {
		return VersionList{}, err
	}
	out := VersionList{Data: make([]VersionInfo, 0, len(rows))}
	for _, r := range rows {
		st := "current"
		switch {
		case r.SupersededAt != nil:
			st = "superseded"
		case r.CustomerAcceptedAt != nil && r.OwnerAcceptedAt != nil:
			st = "executed"
		}
		out.Data = append(out.Data, VersionInfo{
			Version: r.Version, Status: st, TemplateVersion: r.TemplateVersion, LegalTermsVersion: r.LegalTemplateVersion,
			TermsSHA256: hex.EncodeToString(r.TermsSha256), CreatedAt: r.CreatedAt, SupersededAt: r.SupersededAt,
			SupersedeReason: r.SupersedeReason, CustomerAcceptedAt: r.CustomerAcceptedAt, OwnerAcceptedAt: r.OwnerAcceptedAt,
		})
	}
	return out, nil
}

// GetVersion returns one version with its exact text and acceptance records.
func (s *Service) GetVersion(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID, version int32) (Agreement, error) {
	b, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return Agreement{}, err
	}
	l, err := s.loadVersion(ctx, b, version)
	if err != nil {
		return Agreement{}, err
	}
	return s.view(ctx, p, b, side, l)
}

// ---- document audit history -----------------------------------------------------------------

// HistoryEntry is one audited event on a booking's documents.
type HistoryEntry struct {
	At          time.Time `json:"at"`
	Document    string    `json:"document" doc:"agreement, acceptance or invoice"`
	DocumentID  uuid.UUID `json:"document_id"`
	Action      string    `json:"action" doc:"created, updated, accepted, downloaded, ..."`
	Description string    `json:"description"`
	ActorName   string    `json:"actor_name,omitempty"`
	ActorRole   string    `json:"actor_role,omitempty"`
}

type HistoryList struct {
	Data []HistoryEntry `json:"data"`
}

// History is the audit trail of the booking's agreements, acceptance records
// and invoices. IP addresses are not shown here.
func (s *Service) History(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID) (HistoryList, error) {
	if _, _, err := s.bookings.Access(ctx, p, bookingID); err != nil {
		return HistoryList{}, err
	}
	rows, err := s.repo.History(ctx, bookingID, 200)
	if err != nil {
		return HistoryList{}, err
	}
	out := HistoryList{Data: make([]HistoryEntry, 0, len(rows))}
	for _, r := range rows {
		e := HistoryEntry{At: r.OccurredAt, DocumentID: r.EntityID.UUID, ActorName: r.ActorName}
		if r.ActorRole != nil {
			e.ActorRole = *r.ActorRole
		}
		reason := ""
		if r.Reason != nil {
			reason = *r.Reason
		}
		switch r.EntityType {
		case "rental_agreements":
			e.Document, e.Action = "agreement", map[string]string{"insert": "issued", "update": "updated"}[r.Action]
			e.Description = "Agreement version " + r.Version + " " + e.Action
			if r.Action == "update" {
				e.Action, e.Description = "acceptance_recorded", "Agreement version "+r.Version+": an acceptance was recorded"
				if r.SupersededNow {
					e.Action, e.Description = "superseded", "Agreement version "+r.Version+" superseded: "+r.SupersedeReason
				}
			}
		case "agreement_signatures":
			e.Document, e.Action = "acceptance", "accepted"
			e.Description = "Acceptance recorded for the " + r.Party
		case "invoices":
			e.Document, e.Action = "invoice", map[string]string{"insert": "created", "update": "updated"}[r.Action]
			e.Description = "Invoice " + r.InvoiceNumber + " " + e.Action
			if r.NewStatus == "issued" {
				e.Action, e.Description = "issued", "Invoice "+r.InvoiceNumber+" issued"
			}
		default: // rental_agreement / invoice: application events (downloads)
			e.Document = strings.TrimSuffix(strings.TrimPrefix(r.EntityType, "rental_"), "s")
			e.Action = strings.TrimPrefix(r.Action, "document.")
			e.Description = reason
			if e.Action == "url_issued" {
				e.Action = "downloaded"
			}
		}
		if e.Action == "" {
			e.Action = r.Action
		}
		if e.Description == "" {
			e.Description = reason
		}
		out.Data = append(out.Data, e)
	}
	return out, nil
}

// ---- legal templates (admin) ------------------------------------------------------------------

// TemplateStore manages legal templates.
type TemplateStore interface {
	List(ctx context.Context) ([]db.LegalTemplate, error)
	Get(ctx context.Context, id uuid.UUID) (db.LegalTemplate, error)
	Create(ctx context.Context, p db.CreateLegalTemplateParams) (db.LegalTemplate, error)
	UpdateDraft(ctx context.Context, p db.UpdateLegalTemplateDraftParams) (db.LegalTemplate, error)
	Review(ctx context.Context, p db.ReviewLegalTemplateParams) (db.LegalTemplate, error)
	Publish(ctx context.Context, id, by uuid.UUID) (db.LegalTemplate, error)
	DeleteDraft(ctx context.Context, id uuid.UUID) (bool, error)
}

type pgTemplates struct{ r *PGRepository }

func (t pgTemplates) List(ctx context.Context) ([]db.LegalTemplate, error) {
	rows, err := t.r.q(ctx).ListLegalTemplates(ctx, "rental_agreement")
	return rows, database.MapError(err)
}

func (t pgTemplates) Get(ctx context.Context, id uuid.UUID) (db.LegalTemplate, error) {
	row, err := t.r.q(ctx).GetLegalTemplate(ctx, id)
	return row, database.MapError(err)
}

func (t pgTemplates) Create(ctx context.Context, p db.CreateLegalTemplateParams) (db.LegalTemplate, error) {
	row, err := t.r.q(ctx).CreateLegalTemplate(ctx, p)
	return row, database.MapError(err)
}

func (t pgTemplates) UpdateDraft(ctx context.Context, p db.UpdateLegalTemplateDraftParams) (db.LegalTemplate, error) {
	row, err := t.r.q(ctx).UpdateLegalTemplateDraft(ctx, p)
	return row, database.MapError(err)
}

func (t pgTemplates) Review(ctx context.Context, p db.ReviewLegalTemplateParams) (db.LegalTemplate, error) {
	row, err := t.r.q(ctx).ReviewLegalTemplate(ctx, p)
	return row, database.MapError(err)
}

// Publish retires the published template and publishes id (call in a transaction).
func (t pgTemplates) Publish(ctx context.Context, id, by uuid.UUID) (db.LegalTemplate, error) {
	if err := t.r.q(ctx).RetirePublishedLegalTemplate(ctx, "rental_agreement"); err != nil {
		return db.LegalTemplate{}, database.MapError(err)
	}
	row, err := t.r.q(ctx).PublishLegalTemplate(ctx, db.PublishLegalTemplateParams{ID: id, PublishedBy: uuid.NullUUID{UUID: by, Valid: true}})
	return row, database.MapError(err)
}

func (t pgTemplates) DeleteDraft(ctx context.Context, id uuid.UUID) (bool, error) {
	n, err := t.r.q(ctx).DeleteLegalTemplateDraft(ctx, id)
	return n == 1, database.MapError(err)
}

// LegalTemplate is the admin view of a template.
type LegalTemplate struct {
	ID               uuid.UUID  `json:"id"`
	Version          string     `json:"version"`
	Title            string     `json:"title"`
	Jurisdiction     string     `json:"jurisdiction"`
	Status           string     `json:"status" doc:"draft, published or retired"`
	Body             LegalBody  `json:"body"`
	Missing          []string   `json:"missing" doc:"Sections still empty (a template is published only when complete)"`
	ReviewedByName   *string    `json:"reviewed_by_name,omitempty"`
	ReviewerCapacity *string    `json:"reviewer_capacity,omitempty"`
	ReviewReference  *string    `json:"review_reference,omitempty"`
	ReviewedAt       *time.Time `json:"reviewed_at,omitempty"`
	PublishedAt      *time.Time `json:"published_at,omitempty"`
	RetiredAt        *time.Time `json:"retired_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type LegalTemplateList struct {
	Data []LegalTemplate `json:"data"`
}

// LegalTemplateRequest creates or edits a draft.
type LegalTemplateRequest struct {
	Version      string    `json:"version" binding:"omitempty,min=2,max=40" doc:"Required when creating, e.g. rental-terms-2026.2"`
	Title        string    `json:"title" binding:"required,min=3,max=120"`
	Jurisdiction string    `json:"jurisdiction" binding:"omitempty,len=2"`
	Body         LegalBody `json:"body"`
}

// ReviewRequest records the legal review of a draft.
type ReviewRequest struct {
	ReviewedByName   string  `json:"reviewed_by_name" binding:"required,min=2,max=120"`
	ReviewerCapacity string  `json:"reviewer_capacity" binding:"required,min=2,max=120" doc:"e.g. Advocate, in-house counsel"`
	ReviewReference  *string `json:"review_reference" binding:"omitempty,max=200" doc:"Opinion or ticket reference"`
}

func toTemplate(t db.LegalTemplate) LegalTemplate {
	var body LegalBody
	_ = json.Unmarshal(t.Body, &body)
	miss := body.Validate()
	if miss == nil {
		miss = []string{}
	}
	return LegalTemplate{ID: t.ID, Version: t.Version, Title: t.Title, Jurisdiction: t.Jurisdiction, Status: t.Status, Body: body,
		Missing: miss, ReviewedByName: t.ReviewedByName, ReviewerCapacity: t.ReviewerCapacity, ReviewReference: t.ReviewReference,
		ReviewedAt: t.ReviewedAt, PublishedAt: t.PublishedAt, RetiredAt: t.RetiredAt, CreatedAt: t.CreatedAt}
}

func (s *Service) Templates(ctx context.Context) (LegalTemplateList, error) {
	rows, err := s.repo.Templates().List(ctx)
	if err != nil {
		return LegalTemplateList{}, err
	}
	out := LegalTemplateList{Data: make([]LegalTemplate, 0, len(rows))}
	for _, r := range rows {
		out.Data = append(out.Data, toTemplate(r))
	}
	return out, nil
}

func (s *Service) Template(ctx context.Context, id uuid.UUID) (LegalTemplate, error) {
	t, err := s.repo.Templates().Get(ctx, id)
	return toTemplate(t), err
}

func (s *Service) CreateTemplate(ctx context.Context, p *httpx.Principal, req LegalTemplateRequest) (LegalTemplate, error) {
	if req.Version == "" {
		return LegalTemplate{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "version", Code: "required", Message: "name the version"})
	}
	body, _ := json.Marshal(req.Body)
	var t db.LegalTemplate
	err := s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "admin", Reason: "legal template drafted"}, func(ctx context.Context) error {
		var err error
		t, err = s.repo.Templates().Create(ctx, db.CreateLegalTemplateParams{Kind: "rental_agreement", Version: strings.ToLower(req.Version),
			Title: req.Title, Jurisdiction: jurisdiction(req.Jurisdiction), Body: body, CreatedBy: uuid.NullUUID{UUID: p.UserID, Valid: true}})
		return err
	})
	if errors.Is(err, apperror.ErrConflict) {
		return LegalTemplate{}, apperror.ErrConflict.WithDetail("a template with this version already exists")
	}
	return toTemplate(t), err
}

func jurisdiction(j string) string {
	if j == "" {
		return "IN"
	}
	return strings.ToUpper(j)
}

func (s *Service) UpdateTemplate(ctx context.Context, p *httpx.Principal, id uuid.UUID, req LegalTemplateRequest) (LegalTemplate, error) {
	body, _ := json.Marshal(req.Body)
	var t db.LegalTemplate
	err := s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "admin", Reason: "legal template edited"}, func(ctx context.Context) error {
		var err error
		t, err = s.repo.Templates().UpdateDraft(ctx, db.UpdateLegalTemplateDraftParams{ID: id, Title: req.Title, Body: body, Jurisdiction: jurisdiction(req.Jurisdiction)})
		return err
	})
	if errors.Is(err, apperror.ErrNotFound) {
		return LegalTemplate{}, apperror.ErrConflict.WithDetail("only drafts can be edited; create a new version instead")
	}
	return toTemplate(t), err
}

// ReviewTemplate records who reviewed a draft. Editing the draft afterwards
// keeps the record, so review last, then publish.
func (s *Service) ReviewTemplate(ctx context.Context, p *httpx.Principal, id uuid.UUID, req ReviewRequest) (LegalTemplate, error) {
	var t db.LegalTemplate
	err := s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "admin", Reason: "legal review recorded"}, func(ctx context.Context) error {
		var err error
		t, err = s.repo.Templates().Review(ctx, db.ReviewLegalTemplateParams{ID: id, ReviewedByName: &req.ReviewedByName,
			ReviewerCapacity: &req.ReviewerCapacity, ReviewReference: req.ReviewReference})
		return err
	})
	if errors.Is(err, apperror.ErrNotFound) {
		return LegalTemplate{}, apperror.ErrConflict.WithDetail("only drafts can be reviewed")
	}
	return toTemplate(t), err
}

// PublishTemplate makes a complete draft the terms for new agreements
// (agreements already issued keep their own copy).
func (s *Service) PublishTemplate(ctx context.Context, p *httpx.Principal, id uuid.UUID) (LegalTemplate, error) {
	cur, err := s.repo.Templates().Get(ctx, id)
	if err != nil {
		return LegalTemplate{}, err
	}
	v := toTemplate(cur)
	switch {
	case cur.Status != "draft":
		return LegalTemplate{}, apperror.ErrConflict.WithDetail("only drafts can be published")
	case len(v.Missing) > 0:
		return LegalTemplate{}, apperror.ErrBusinessRule.WithDetail("complete these sections first: " + strings.Join(v.Missing, ", "))
	case s.requireReviewed && cur.ReviewedAt == nil:
		return LegalTemplate{}, apperror.ErrBusinessRule.WithDetail("record the legal review before publishing")
	}
	var t db.LegalTemplate
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "admin", Reason: "legal template published"}, func(ctx context.Context) error {
		var err error
		t, err = s.repo.Templates().Publish(ctx, id, p.UserID)
		return err
	})
	return toTemplate(t), err
}

func (s *Service) DeleteTemplate(ctx context.Context, p *httpx.Principal, id uuid.UUID) error {
	return s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "admin", Reason: "legal template draft deleted"}, func(ctx context.Context) error {
		ok, err := s.repo.Templates().DeleteDraft(ctx, id)
		if err == nil && !ok {
			return apperror.ErrConflict.WithDetail("only drafts can be deleted")
		}
		return err
	})
}

// ---- routes -----------------------------------------------------------------------------------

func versionParam(c *gin.Context) (int32, error) {
	v, err := strconv.Atoi(c.Param("version"))
	if err != nil || v < 1 || v > 10000 {
		return 0, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "version", Code: "min", Message: "must be a version number"})
	}
	return int32(v), nil
}

func (h *Handler) versionRoutes(a *httpx.Router) {
	a.GET("/bookings/:id/agreement/versions", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Versions(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Every issued version of the agreement", Response: VersionList{}})
	a.GET("/bookings/:id/agreement/versions/:version", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		v, err := versionParam(c)
		if err != nil {
			return nil, err
		}
		return h.svc.GetVersion(c.Request.Context(), p, id, v)
	}), httpx.Doc{Summary: "One agreement version with its exact text and acceptance records", Response: Agreement{}})
	a.GET("/bookings/:id/agreement/versions/:version/document", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		v, err := versionParam(c)
		if err != nil {
			return nil, err
		}
		return h.svc.DocumentLink(c.Request.Context(), p, id, v)
	}), httpx.Doc{Summary: "Download link for one agreement version's PDF", Response: DocumentLink{}})
	a.GET("/bookings/:id/documents/history", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.History(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Audit history of the booking's agreements, acceptance records and invoices", Response: HistoryList{},
		Description: "Issues, acceptances, supersessions, invoice issues and every download, newest first. Parties and admins only."})

	read := a.Admin(httpx.RoleAdminOps, httpx.RoleAdminSupport)
	read.GET("/admin/legal-templates", httpx.Handle(200, func(c *gin.Context, _ *httpx.Principal) (any, error) {
		return h.svc.Templates(c.Request.Context())
	}), httpx.Doc{Summary: "Rental agreement templates (all versions)", Response: LegalTemplateList{}})
	read.GET("/admin/legal-templates/:id", httpx.Handle(200, func(c *gin.Context, _ *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Template(c.Request.Context(), id)
	}), httpx.Doc{Summary: "One agreement template", Response: LegalTemplate{}})
	super := a.Admin()
	super.POST("/admin/legal-templates", httpx.HandleJSON(201, func(c *gin.Context, p *httpx.Principal, req LegalTemplateRequest) (any, error) {
		return h.svc.CreateTemplate(c.Request.Context(), p, req)
	}), httpx.Doc{Summary: "Draft a new agreement template version", Request: LegalTemplateRequest{}, Response: LegalTemplate{}, Status: 201})
	super.PUT("/admin/legal-templates/:id", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req LegalTemplateRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.UpdateTemplate(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Edit a draft template", Request: LegalTemplateRequest{}, Response: LegalTemplate{}})
	super.POST("/admin/legal-templates/:id/review", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req ReviewRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.ReviewTemplate(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Record the legal review of a draft", Request: ReviewRequest{}, Response: LegalTemplate{}})
	super.POST("/admin/legal-templates/:id/publish", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.PublishTemplate(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Publish a draft (retires the current terms for new agreements)", Response: LegalTemplate{},
		Description: "Requires every section; in staging and production also a recorded legal review."})
	super.DELETE("/admin/legal-templates/:id", httpx.Handle(204, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return nil, h.svc.DeleteTemplate(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Delete a draft template", Status: 204})
}
