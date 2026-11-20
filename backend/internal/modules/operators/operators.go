// Package operators manages machine operators: their profile (licence,
// experience, skills), duty status and assigned jobs. Job actions (accept,
// decline, arrived, start, returned) live with the booking lifecycle.
package operators

import (
	"context"
	"regexp"
	"strings"
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

// Profile is an operator's professional profile.
type Profile struct {
	UserID           uuid.UUID     `json:"user_id"`
	EmployerOrgID    uuid.NullUUID `json:"employer_organization_id"`
	LicenceLast4     *string       `json:"licence_last4,omitempty"`
	LicenceClass     *string       `json:"licence_class,omitempty"`
	LicenceExpiresOn *string       `json:"licence_expires_on,omitempty" doc:"YYYY-MM-DD"`
	ExperienceYears  *int16        `json:"experience_years,omitempty"`
	OnDuty           bool          `json:"on_duty"`
	Verified         bool          `json:"verified"`
	RatingAvg        *float64      `json:"rating_avg,omitempty"`
	RatingCount      int32         `json:"rating_count"`
	Skills           []string      `json:"skills" doc:"Category slugs the operator can run"`
}

// Job is an assignment in the operator's queue.
type Job struct {
	AssignmentID        uuid.UUID `json:"assignment_id"`
	AssignmentStatus    string    `json:"assignment_status"`
	BookingID           uuid.UUID `json:"booking_id"`
	Reference           string    `json:"reference"`
	BookingStatus       string    `json:"booking_status"`
	Start               time.Time `json:"start"`
	End                 time.Time `json:"end"`
	EquipmentTitle      string    `json:"equipment_title"`
	SiteLine1           string    `json:"site_line1"`
	SiteCity            string    `json:"site_city"`
	SiteLat             float64   `json:"site_lat"`
	SiteLng             float64   `json:"site_lng"`
	RegistrationNo      string    `json:"registration_no,omitempty"`
	CustomerName        string    `json:"customer_name"`
	DriverPaymentPaise  int64     `json:"driver_payment_paise"`
	DriverPaymentStatus string    `json:"driver_payment_status"`
}

type UpsertProfileRequest struct {
	LicenceNumber    string   `json:"licence_number" binding:"omitempty,min=6,max=20" doc:"Only the last 4 characters are stored here"`
	LicenceClass     *string  `json:"licence_class" binding:"omitempty,max=40"`
	LicenceExpiresOn *string  `json:"licence_expires_on" binding:"omitempty,datetime=2006-01-02"`
	ExperienceYears  *int16   `json:"experience_years" binding:"omitempty,min=0,max=60"`
	Skills           []string `json:"skills" binding:"omitempty,max=20,dive,max=60"`
}

type DutyRequest struct {
	OnDuty bool `json:"on_duty"`
}

type DutyResponse struct {
	OnDuty bool `json:"on_duty"`
}

type JobsQuery struct {
	Status string `form:"status" binding:"omitempty,oneof=assigned accepted declined replaced completed"`
	Scope  string `form:"scope" binding:"omitempty,oneof=current history" doc:"current: open jobs, soonest first; history: past, declined or reassigned jobs, newest first"`
	Limit  int    `form:"limit" binding:"omitempty,min=1,max=100"`
}

type JobList struct {
	Data []Job `json:"data"`
}

// ---- repository --------------------------------------------------------------------

type Repository interface {
	Profile(ctx context.Context, userID uuid.UUID) (Profile, error)
	Upsert(ctx context.Context, p db.UpsertOperatorProfileParams, skills []string) error
	// VerificationLocked reports whether the operator's verification is
	// submitted or approved (licence details are then frozen).
	VerificationLocked(ctx context.Context, userID uuid.UUID) (bool, error)
	SetDuty(ctx context.Context, userID uuid.UUID, onDuty bool) (bool, error)
	Jobs(ctx context.Context, userID uuid.UUID, scope string, status *string, limit int) ([]Job, error)
}

type PGRepository struct {
	pool *pgxpool.Pool
	tx   database.Transactor
}

func NewRepository(pool *pgxpool.Pool, tx database.Transactor) *PGRepository {
	return &PGRepository{pool: pool, tx: tx}
}

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

func (r *PGRepository) Profile(ctx context.Context, userID uuid.UUID) (Profile, error) {
	q := r.q(ctx)
	p, err := q.GetOperatorProfile(ctx, userID)
	if err != nil {
		return Profile{}, database.MapError(err)
	}
	skills, err := q.ListOperatorSkills(ctx, userID)
	if err != nil {
		return Profile{}, database.MapError(err)
	}
	out := Profile{
		UserID: p.UserID, EmployerOrgID: p.EmployerOrganizationID, LicenceLast4: p.LicenceLast4, LicenceClass: p.LicenceClass,
		ExperienceYears: p.ExperienceYears, OnDuty: p.OnDuty, Verified: p.VerifiedAt != nil,
		RatingAvg: p.RatingAvg, RatingCount: p.RatingCount, Skills: skills,
	}
	if p.LicenceExpiresOn != nil {
		d := p.LicenceExpiresOn.Format(time.DateOnly)
		out.LicenceExpiresOn = &d
	}
	return out, nil
}

func (r *PGRepository) VerificationLocked(ctx context.Context, userID uuid.UUID) (bool, error) {
	locked, err := r.q(ctx).OperatorVerificationLocked(ctx, uuid.NullUUID{UUID: userID, Valid: true})
	return locked, database.MapError(err)
}

func (r *PGRepository) Upsert(ctx context.Context, p db.UpsertOperatorProfileParams, skills []string) error {
	return database.MapError(r.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: httpx.RoleOperator}, func(ctx context.Context) error {
		q := r.q(ctx)
		if _, err := q.UpsertOperatorProfile(ctx, p); err != nil {
			return err
		}
		if skills == nil {
			return nil
		}
		if err := q.DeleteOperatorSkills(ctx, p.UserID); err != nil {
			return err
		}
		return q.AddOperatorSkills(ctx, db.AddOperatorSkillsParams{OperatorUserID: p.UserID, CategorySlugs: skills})
	}))
}

func (r *PGRepository) SetDuty(ctx context.Context, userID uuid.UUID, onDuty bool) (bool, error) {
	v, err := r.q(ctx).SetOperatorDuty(ctx, db.SetOperatorDutyParams{UserID: userID, OnDuty: onDuty})
	return v, database.MapError(err)
}

func (r *PGRepository) Jobs(ctx context.Context, userID uuid.UUID, scope string, status *string, limit int) ([]Job, error) {
	if scope == "" {
		scope = "all"
	}
	rows, err := r.q(ctx).ListOperatorJobsScoped(ctx, db.ListOperatorJobsScopedParams{OperatorUserID: userID, Scope: scope, Lim: int32(limit)})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Job, 0, len(rows))
	for _, j := range rows {
		if status != nil && j.AssignmentStatus != *status {
			continue
		}
		out = append(out, Job{RegistrationNo: j.RegistrationNo, CustomerName: j.CustomerName,
			DriverPaymentPaise: j.DriverPaymentPaise, DriverPaymentStatus: j.DriverPaymentStatus,
			AssignmentID: j.AssignmentID, AssignmentStatus: string(j.AssignmentStatus), BookingID: j.BookingID,
			Reference: j.Reference, BookingStatus: string(j.BookingStatus), Start: j.StartsAt, End: j.EndsAt,
			EquipmentTitle: j.EquipmentTitle, SiteLine1: j.SiteLine1, SiteCity: j.SiteCity, SiteLat: j.SiteLat, SiteLng: j.SiteLng,
		})
	}
	return out, nil
}

// ---- service -----------------------------------------------------------------------

var last4Pattern = regexp.MustCompile(`^[A-Z0-9]{4}$`)

type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func requireOperator(p *httpx.Principal) error {
	for _, r := range p.Roles {
		if r.Code == httpx.RoleOperator {
			return nil
		}
	}
	return apperror.ErrForbidden.WithDetail("ask the equipment owner to add you as an operator first")
}

func (s *Service) Profile(ctx context.Context, p *httpx.Principal) (Profile, error) {
	if err := requireOperator(p); err != nil {
		return Profile{}, err
	}
	return s.repo.Profile(ctx, p.UserID)
}

// Upsert saves the profile. The full licence number is never stored here:
// verification goes through the KYC flow, which keeps it encrypted.
func (s *Service) Upsert(ctx context.Context, p *httpx.Principal, req UpsertProfileRequest) (Profile, error) {
	if err := requireOperator(p); err != nil {
		return Profile{}, err
	}
	// Verified (or in-review) licence details may only change through a new
	// verification, never silently through this endpoint.
	locked, err := s.repo.VerificationLocked(ctx, p.UserID)
	if err != nil {
		return Profile{}, err
	}
	if locked {
		return Profile{}, apperror.ErrInvalidTransition.WithDetail("your verification is submitted or approved; licence details can only change after a rejection (see /verification/operator)")
	}
	params := db.UpsertOperatorProfileParams{UserID: p.UserID, LicenceClass: req.LicenceClass, ExperienceYears: req.ExperienceYears}
	if req.LicenceNumber != "" {
		clean := strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(req.LicenceNumber))
		last4 := clean[max(0, len(clean)-4):]
		if !last4Pattern.MatchString(last4) {
			return Profile{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "licence_number", Code: "invalid", Message: "letters and digits only"})
		}
		params.LicenceLast4 = &last4
	}
	if req.LicenceExpiresOn != nil {
		d, _ := time.Parse(time.DateOnly, *req.LicenceExpiresOn) // format checked by binding
		params.LicenceExpiresOn = &d
	}
	if err := s.repo.Upsert(ctx, params, req.Skills); err != nil {
		return Profile{}, err
	}
	return s.repo.Profile(ctx, p.UserID)
}

func (s *Service) SetDuty(ctx context.Context, p *httpx.Principal, onDuty bool) (DutyResponse, error) {
	if err := requireOperator(p); err != nil {
		return DutyResponse{}, err
	}
	v, err := s.repo.SetDuty(ctx, p.UserID, onDuty)
	return DutyResponse{OnDuty: v}, err
}

func (s *Service) Jobs(ctx context.Context, p *httpx.Principal, q JobsQuery) (JobList, error) {
	if err := requireOperator(p); err != nil {
		return JobList{}, err
	}
	var status *string
	if q.Status != "" {
		status = &q.Status
	}
	limit := q.Limit
	if limit == 0 {
		limit = 50
	}
	jobs, err := s.repo.Jobs(ctx, p.UserID, q.Scope, status, limit)
	return JobList{Data: jobs}, err
}

// ---- handler -----------------------------------------------------------------------

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Name() string { return "operators" }

func (h *Handler) Routes(r *httpx.Router) {
	a := r.Tag("operators").Authed()
	a.GET("/operators/me/profile", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		return h.svc.Profile(c.Request.Context(), p)
	}), httpx.Doc{Summary: "My operator profile", Response: Profile{}})
	a.PUT("/operators/me/profile", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req UpsertProfileRequest) (any, error) {
		return h.svc.Upsert(c.Request.Context(), p, req)
	}), httpx.Doc{Summary: "Update licence, experience and skills", Request: UpsertProfileRequest{}, Response: Profile{}})
	a.POST("/operator/availability", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req DutyRequest) (any, error) {
		return h.svc.SetDuty(c.Request.Context(), p, req.OnDuty)
	}), httpx.Doc{Summary: "Go on / off duty", Request: DutyRequest{}, Response: DutyResponse{}})
	a.GET("/operator/jobs", httpx.HandleQuery(200, func(c *gin.Context, p *httpx.Principal, q JobsQuery) (any, error) {
		return h.svc.Jobs(c.Request.Context(), p, q)
	}), httpx.Doc{Summary: "My assigned jobs", Query: JobsQuery{}, Response: JobList{}})
}
