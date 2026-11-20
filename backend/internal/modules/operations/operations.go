// Package operations is the operator's job workspace and its evidence:
// job detail (customer, machine, site, navigation), the operator's
// schedule, handover and return checklists with before/after photographs,
// issue (damage, breakdown, safety) reports, the admin issue queue and
// account restrictions.
//
// Access always goes through the booking: the assigned operator, the
// owner side (fleet managers only for their machines) and the customer see
// a booking's evidence; nobody else learns it exists.
package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/events"
	"github.com/equiprent/backend/internal/filecheck"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/modules/bookings"
	"github.com/equiprent/backend/internal/storage"
)

// ---- domain -------------------------------------------------------------------------------------

// JobDetail is everything an operator needs for one job.
type JobDetail struct {
	BookingID                uuid.UUID   `json:"booking_id"`
	Reference                string      `json:"reference"`
	BookingStatus            string      `json:"booking_status"`
	AssignmentStatus         string      `json:"assignment_status" doc:"assigned, accepted, declined, replaced or completed"`
	Current                  bool        `json:"current" doc:"false once the job was declined, reassigned or finished"`
	Start                    time.Time   `json:"start"`
	End                      time.Time   `json:"end"`
	TimeZone                 string      `json:"time_zone"`
	WorkDescription          string      `json:"work_description"`
	Customer                 Contact     `json:"customer"`
	Owner                    Contact     `json:"owner"`
	Equipment                Machine     `json:"equipment"`
	Site                     Site        `json:"site"`
	Navigation               Navigation  `json:"navigation"`
	Checklists               []Checklist `json:"checklists"`
	Issues                   []Issue     `json:"issues"`
	NextSteps                []string    `json:"next_steps" doc:"accept, decline, dispatch, arrived, handover_checklist, await_customer, start, return_checklist, returned"`
	Delivery                 bool        `json:"delivery"`
	Pickup                   bool        `json:"pickup"`
	RespondedAt              *time.Time  `json:"responded_at,omitempty"`
	DriverPaymentPaise       int64       `json:"driver_payment_paise"`
	DriverPaymentStatus      string      `json:"driver_payment_status"`
	HandoverInspectionStatus string      `json:"handover_inspection_status"`
	ReturnInspectionStatus   string      `json:"return_inspection_status"`
}

type Contact struct {
	Name    string `json:"name"`
	Contact string `json:"contact,omitempty"`
	Phone   string `json:"phone,omitempty" doc:"Shown once the operator has accepted the job"`
}

type Machine struct {
	ID             uuid.UUID `json:"id"`
	Title          string    `json:"title"`
	Make           string    `json:"make"`
	Model          string    `json:"model"`
	Category       string    `json:"category"`
	Year           *int16    `json:"year,omitempty"`
	RegistrationNo string    `json:"registration_no,omitempty"`
	SerialNo       string    `json:"serial_no,omitempty"`
	HourMeter      float64   `json:"hour_meter"`
	Condition      string    `json:"condition"`
	FuelPolicy     string    `json:"fuel_policy"`
}

type Site struct {
	Label     string  `json:"label,omitempty"`
	Address   string  `json:"address"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	GeofenceM int32   `json:"geofence_m"`
}

type Navigation struct {
	URL     string  `json:"url" doc:"Opens turn-by-turn directions to the site"`
	YardLat float64 `json:"yard_lat"`
	YardLng float64 `json:"yard_lng"`
}

// Photo is a stored job photograph behind a short-lived link.
type Photo struct {
	ID  uuid.UUID `json:"id"`
	URL string    `json:"url"`
}

// Checklist is handover or return evidence.
type Checklist struct {
	ID          uuid.UUID       `json:"id"`
	Kind        string          `json:"kind" doc:"handover (before) or return (after)"`
	Status      string          `json:"status" doc:"Inspection Pending, Inspection In Progress, Submitted, Acknowledged, Damage Reported or Disputed"`
	HourMeter   *float64        `json:"hour_meter,omitempty" doc:"Optional reading"`
	FuelPct     *int16          `json:"fuel_level_pct,omitempty"`
	Items       json.RawMessage `json:"items" swaggertype:"object" doc:"Item → ok (true) / not ok (false)"`
	DamageNotes *string         `json:"damage_notes,omitempty"`
	Notes       *string         `json:"notes,omitempty"`
	HourPhoto   *Photo          `json:"hour_meter_photo,omitempty"`
	Photos      []Photo         `json:"photos"`
	CapturedAt  time.Time       `json:"captured_at"`
	SubmittedBy string          `json:"submitted_by"`
}

type ChecklistList struct {
	Data []Checklist `json:"data"`
}

// CustomerDeliveryPhoto is delivery evidence shared with the booking parties.
type CustomerDeliveryPhoto struct {
	ID              uuid.UUID  `json:"id"`
	BookingID       uuid.UUID  `json:"booking_id"`
	UploadID        uuid.UUID  `json:"upload_id"`
	URL             string     `json:"url"`
	CapturedAt      time.Time  `json:"captured_at" doc:"Device capture time, validated against future timestamps"`
	SubmittedAt     time.Time  `json:"submitted_at" doc:"Server receipt time"`
	SubmittedBy     uuid.UUID  `json:"submitted_by"`
	SubmittedByName string     `json:"submitted_by_name"`
	ApprovedAt      *time.Time `json:"approved_at" doc:"Set when the equipment owner approved the photo"`
	ApprovedByName  string     `json:"approved_by_name,omitempty"`
}

type CustomerDeliveryPhotoList struct {
	Data []CustomerDeliveryPhoto `json:"data"`
}

type CustomerDeliveryPhotoRequest struct {
	UploadID   uuid.UUID `json:"upload_id" binding:"required"`
	CapturedAt time.Time `json:"captured_at" binding:"required"`
	ClientID   uuid.UUID `json:"client_id" binding:"required" doc:"Idempotency key for retries"`
}

type InspectionAcknowledgementRequest struct {
	Stage        string `json:"stage" binding:"required,oneof=handover return"`
	Disagreement string `json:"disagreement" binding:"max=2000"`
}

type InspectionAcknowledgement struct {
	ID             uuid.UUID `json:"id"`
	BookingID      uuid.UUID `json:"booking_id"`
	Stage          string    `json:"stage"`
	UserID         uuid.UUID `json:"user_id"`
	Role           string    `json:"role"`
	AcknowledgedAt time.Time `json:"acknowledged_at"`
	Disagreement   string    `json:"disagreement,omitempty"`
}

// ChecklistRequest submits handover (before) or return (after) evidence.
type ChecklistRequest struct {
	Kind        string          `json:"kind" binding:"required,oneof=handover return"`
	HourMeter   *float64        `json:"hour_meter" binding:"omitempty,min=0,max=1000000" doc:"Optional hour-meter reading"`
	HourPhotoID *uuid.UUID      `json:"hour_meter_photo_upload_id" doc:"Optional photo of the hour meter"`
	PhotoIDs    []uuid.UUID     `json:"photo_upload_ids" binding:"required,min=2,max=12" doc:"job_photo uploads: at least two views of the machine"`
	FuelPct     *int16          `json:"fuel_level_pct" binding:"omitempty,min=0,max=100"`
	Items       map[string]bool `json:"items" binding:"max=40"`
	DamageNotes *string         `json:"damage_notes" binding:"omitempty,max=1000" doc:"At return, also files a damage issue"`
	Notes       *string         `json:"notes" binding:"omitempty,max=1000"`
	Lat         *float64        `json:"lat" binding:"omitempty,latitude"`
	Lng         *float64        `json:"lng" binding:"omitempty,longitude"`
	CapturedAt  time.Time       `json:"captured_at" binding:"required"`
	ClientID    uuid.UUID       `json:"client_id" binding:"required" doc:"Idempotency for offline retries"`
}

// Issue is a reported problem on a job.
type Issue struct {
	ID             uuid.UUID  `json:"id"`
	BookingID      uuid.UUID  `json:"booking_id"`
	Kind           string     `json:"kind"`
	Stage          string     `json:"stage"`
	Severity       string     `json:"severity"`
	Description    string     `json:"description"`
	Photos         []Photo    `json:"photos"`
	ReporterName   string     `json:"reporter_name,omitempty"`
	ReporterRole   string     `json:"reporter_role"`
	Status         string     `json:"status" doc:"open, under_review, resolved or dismissed"`
	ResolutionNote *string    `json:"resolution_note,omitempty"`
	ReviewedAt     *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	// Admin queue only.
	BookingReference   string                   `json:"booking_reference,omitempty"`
	EquipmentTitle     string                   `json:"equipment_title,omitempty"`
	OwnerName          string                   `json:"owner_name,omitempty"`
	CustomerName       string                   `json:"customer_name,omitempty"`
	InspectionEvidence []Checklist              `json:"inspection_evidence,omitempty" doc:"Original handover and return evidence for admin comparison"`
	DecisionHistory    []InspectionHistoryEntry `json:"decision_history,omitempty"`
}

type InspectionHistoryEntry struct {
	Action    string    `json:"action"`
	PartyRole *string   `json:"party_role,omitempty"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

type IssueList struct {
	Data []Issue `json:"data"`
}

type IssueRequest struct {
	Kind        string      `json:"kind" binding:"required,oneof=damage breakdown safety site_access other"`
	Stage       string      `json:"stage" binding:"required,oneof=before_handover during_rental at_return inspection_handover inspection_return"`
	Severity    string      `json:"severity" binding:"required,oneof=minor major critical"`
	Description string      `json:"description" binding:"required,min=5,max=2000"`
	PhotoIDs    []uuid.UUID `json:"photo_upload_ids" binding:"max=10"`
}

type IssueQuery struct {
	Status   string `form:"status" binding:"omitempty,oneof=open under_review resolved dismissed"`
	Severity string `form:"severity" binding:"omitempty,oneof=minor major critical"`
}

type ReviewIssueRequest struct {
	Status    string `json:"status" binding:"required,oneof=under_review resolved dismissed"`
	Note      string `json:"note" binding:"max=1000" doc:"Required to resolve or dismiss"`
	Action    string `json:"action" binding:"omitempty,oneof=review request_explanation decision"`
	PartyRole string `json:"party_role" binding:"omitempty,oneof=customer owner operator"`
}

// Restriction limits what an account may do.
type Restriction struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"user_id"`
	UserName   string     `json:"user_name,omitempty"`
	UserPhone  string     `json:"user_phone,omitempty"`
	Kind       string     `json:"kind" doc:"operator_assignments, fleet_management or new_bookings"`
	Reason     string     `json:"reason"`
	Active     bool       `json:"active"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LiftedAt   *time.Time `json:"lifted_at,omitempty"`
	LiftReason *string    `json:"lift_reason,omitempty"`
}

type RestrictionList struct {
	Data []Restriction `json:"data"`
}

type RestrictionRequest struct {
	Kind      string     `json:"kind" binding:"required,oneof=operator_assignments fleet_management new_bookings"`
	Reason    string     `json:"reason" binding:"required,min=3,max=500"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type RestrictionQuery struct {
	UserID string `form:"user_id" binding:"omitempty,uuid"`
	Active *bool  `form:"active"`
}

type LiftRequest struct {
	Reason string `json:"reason" binding:"required,min=3,max=500"`
}

type ScheduleQuery struct {
	From time.Time `form:"from" binding:"required" time_format:"2006-01-02T15:04:05Z07:00"`
	To   time.Time `form:"to" binding:"required,gtfield=From" time_format:"2006-01-02T15:04:05Z07:00"`
}

// ScheduleEntry is one job on the operator's calendar.
type ScheduleEntry struct {
	BookingID        uuid.UUID `json:"booking_id"`
	Reference        string    `json:"reference"`
	AssignmentStatus string    `json:"assignment_status"`
	BookingStatus    string    `json:"booking_status"`
	Start            time.Time `json:"start"`
	End              time.Time `json:"end"`
	EquipmentTitle   string    `json:"equipment_title"`
	Site             string    `json:"site"`
	CustomerName     string    `json:"customer_name"`
}

type OperatorSchedule struct {
	From time.Time       `json:"from"`
	To   time.Time       `json:"to"`
	Data []ScheduleEntry `json:"data"`
}

// ---- service ------------------------------------------------------------------------------------

// Files claims uploads and mints download links (the uploads module).
type Files interface {
	Claim(ctx context.Context, userID, uploadID uuid.UUID, purposes ...filecheck.Purpose) (db.Upload, error)
	SignedURL(key string, ttl time.Duration) storage.SignedURL
}

type Service struct {
	pool     *pgxpool.Pool
	tx       database.Transactor
	bookings *bookings.Service
	files    Files
	urlTTL   time.Duration
	// demoGate (development simulation only) skips the inspection-acknowledgement gate so
	// testers can start work and hand back without the customer's confirmation.
	demoGate bool
}

// WithDemoGate turns the demo shortcut on. Never enable outside development.
func (s *Service) WithDemoGate(on bool) *Service {
	s.demoGate = on
	return s
}

func NewService(pool *pgxpool.Pool, tx database.Transactor, bk *bookings.Service, files Files, urlTTL time.Duration) *Service {
	if urlTTL <= 0 {
		urlTTL = 10 * time.Minute
	}
	return &Service{pool: pool, tx: tx, bookings: bk, files: files, urlTTL: urlTTL}
}

func (s *Service) q(ctx context.Context) *db.Queries { return database.Queries(ctx, s.pool) }

var openBooking = map[string]bool{"confirmed": true, "operator_assigned": true, "dispatched": true, "on_site": true, "active": true,
	"extension_requested": true, "return_initiated": true, "cancellation_requested": true}

// Job returns the operator's view of one of their jobs (current or past).
func (s *Service) Job(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID) (JobDetail, error) {
	r, err := s.q(ctx).OperatorJobDetail(ctx, db.OperatorJobDetailParams{BookingID: bookingID, OperatorUserID: p.UserID})
	if err != nil {
		return JobDetail{}, database.MapError(err)
	}
	current := (r.AssignmentStatus == "assigned" || r.AssignmentStatus == "accepted") && openBooking[r.BookingStatus]
	out := JobDetail{
		BookingID: r.BookingID, Reference: r.Reference, BookingStatus: r.BookingStatus, AssignmentStatus: r.AssignmentStatus,
		Current: current, Start: r.StartsAt, End: r.EndsAt, TimeZone: r.Timezone, WorkDescription: r.WorkDescription,
		Customer: Contact{Name: r.CustomerName, Contact: r.CustomerContact}, Owner: Contact{Name: r.OwnerName},
		DriverPaymentPaise: r.DriverPaymentPaise, DriverPaymentStatus: r.DriverPaymentStatus,
		Equipment: Machine{ID: r.EquipmentID, Title: r.EquipmentTitle, Make: r.MakeName, Model: r.ModelName, Category: r.CategoryName,
			Year: r.ManufactureYear, RegistrationNo: r.RegistrationNo, SerialNo: r.SerialNo, HourMeter: r.CurrentHmr,
			Condition: r.Condition, FuelPolicy: r.FuelPolicy},
		Site:       Site{Label: r.SiteLabel, Address: r.SiteAddress, Lat: r.SiteLat, Lng: r.SiteLng, GeofenceM: r.SiteGeofenceM},
		Navigation: Navigation{URL: directionsURL(r.YardLat, r.YardLng, r.SiteLat, r.SiteLng), YardLat: r.YardLat, YardLng: r.YardLng},
		Delivery:   r.Delivery, Pickup: r.Pickup, RespondedAt: r.RespondedAt,
	}
	// Phone numbers are shared once the operator has taken the job.
	if current && r.AssignmentStatus == "accepted" {
		out.Customer.Phone, out.Owner.Phone = r.CustomerPhone, r.OwnerPhone
	}
	if out.Checklists, err = s.checklists(ctx, bookingID); err != nil {
		return JobDetail{}, err
	}
	if out.Issues, err = s.issues(ctx, bookingID); err != nil {
		return JobDetail{}, err
	}
	if out.HandoverInspectionStatus, err = s.inspectionStatus(ctx, bookingID, "handover", r.BookingStatus, out.Checklists, out.Issues); err != nil {
		return JobDetail{}, err
	}
	if out.ReturnInspectionStatus, err = s.inspectionStatus(ctx, bookingID, "return", r.BookingStatus, out.Checklists, out.Issues); err != nil {
		return JobDetail{}, err
	}
	out.NextSteps = nextSteps(out)
	// Work starts once the customer confirms the handover; until then the driver waits instead of
	// seeing a "start" button that the acknowledgement gate would refuse.
	if i := slices.Index(out.NextSteps, "start"); i >= 0 {
		if n, err := s.InspectionAcknowledgements(ctx, bookingID, "handover"); err == nil && n < 2 {
			out.NextSteps[i] = "await_customer"
		}
	}
	return out, nil
}

func (s *Service) inspectionStatus(ctx context.Context, bookingID uuid.UUID, stage, bookingStatus string, checks []Checklist, issues []Issue) (string, error) {
	issueStage := "inspection_" + stage
	if stage == "return" {
		issueStage = "inspection_return"
	}
	for _, issue := range issues {
		if issue.Stage == issueStage || (stage == "return" && issue.Stage == "at_return") {
			if issue.Status == "under_review" {
				return "Disputed", nil
			}
			if issue.ReporterRole == "customer" && strings.HasPrefix(issue.Stage, "inspection_") {
				return "Disputed", nil
			}
			if issue.Status == "open" {
				return "Damage Reported", nil
			}
		}
	}
	if !hasKind(checks, stage) {
		if stage == "handover" && (bookingStatus == "on_site" || bookingStatus == "active") {
			return "Inspection In Progress", nil
		}
		if stage == "return" && (bookingStatus == "return_initiated" || bookingStatus == "active") {
			return "Inspection In Progress", nil
		}
		return "Inspection Pending", nil
	}
	n, err := s.q(ctx).CountInspectionAcknowledgements(ctx, db.CountInspectionAcknowledgementsParams{BookingID: bookingID, Stage: stage})
	if err != nil {
		return "", database.MapError(err)
	}
	if n >= 2 {
		return "Acknowledged", nil
	}
	return "Submitted", nil
}

// directionsURL opens directions from the yard to the site.
func directionsURL(fromLat, fromLng, toLat, toLng float64) string {
	return fmt.Sprintf("https://www.google.com/maps/dir/?api=1&origin=%.6f,%.6f&destination=%.6f,%.6f&travelmode=driving", fromLat, fromLng, toLat, toLng)
}

func hasKind(cs []Checklist, kind string) bool {
	for _, c := range cs {
		if c.Kind == kind {
			return true
		}
	}
	return false
}

// nextSteps lists what the operator can do now, in order.
func nextSteps(j JobDetail) []string {
	out := []string{}
	if !j.Current {
		return out
	}
	if j.AssignmentStatus == "assigned" {
		return append(out, "accept", "decline")
	}
	switch j.BookingStatus {
	case "operator_assigned", "confirmed":
		if !hasKind(j.Checklists, "handover") {
			out = append(out, "handover_checklist")
		} else {
			out = append(out, "dispatch")
		}
	case "dispatched":
		out = append(out, "arrived")
	case "on_site":
		if !hasKind(j.Checklists, "handover") {
			out = append(out, "handover_checklist")
		} else {
			out = append(out, "start")
		}
	case "active", "extension_requested", "return_initiated":
		if !hasKind(j.Checklists, "return") {
			out = append(out, "return_checklist")
		} else if j.BookingStatus == "return_initiated" {
			out = append(out, "returned")
		}
	}
	return out
}

// Schedule lists the operator's jobs overlapping [from, to).
func (s *Service) Schedule(ctx context.Context, p *httpx.Principal, q ScheduleQuery) (OperatorSchedule, error) {
	if q.To.Sub(q.From) > 93*24*time.Hour {
		return OperatorSchedule{}, apperror.ErrValidation.WithDetail("a schedule covers at most three months")
	}
	from, to := q.From.UTC(), q.To.UTC()
	rows, err := s.q(ctx).ListOperatorJobsScoped(ctx, db.ListOperatorJobsScopedParams{OperatorUserID: p.UserID, Scope: "all",
		FromAt: &from, ToAt: &to, Lim: 500})
	if err != nil {
		return OperatorSchedule{}, database.MapError(err)
	}
	out := OperatorSchedule{From: from, To: to, Data: []ScheduleEntry{}}
	for _, r := range rows {
		if r.AssignmentStatus == "declined" || r.AssignmentStatus == "replaced" {
			continue
		}
		out.Data = append(out.Data, ScheduleEntry{BookingID: r.BookingID, Reference: r.Reference, AssignmentStatus: r.AssignmentStatus,
			BookingStatus: r.BookingStatus, Start: r.StartsAt, End: r.EndsAt, EquipmentTitle: r.EquipmentTitle,
			Site: strings.TrimSpace(r.SiteLine1 + ", " + r.SiteCity), CustomerName: r.CustomerName})
	}
	return out, nil
}

// ---- checklists ---------------------------------------------------------------------------------

func (s *Service) photos(ctx context.Context, ids []uuid.UUID) ([]Photo, error) {
	out := make([]Photo, 0, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.q(ctx).UploadKeys(ctx, ids)
	if err != nil {
		return nil, database.MapError(err)
	}
	keys := map[uuid.UUID]string{}
	for _, r := range rows {
		keys[r.ID] = r.ObjectKey
	}
	for _, id := range ids {
		if k, ok := keys[id]; ok {
			out = append(out, Photo{ID: id, URL: s.files.SignedURL(k, s.urlTTL).URL})
		}
	}
	return out, nil
}

func (s *Service) checklists(ctx context.Context, bookingID uuid.UUID) ([]Checklist, error) {
	rows, err := s.q(ctx).ListBookingChecklists(ctx, bookingID)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Checklist, 0, len(rows))
	for _, r := range rows {
		ph, err := s.photos(ctx, r.PhotoUploadIds)
		if err != nil {
			return nil, err
		}
		items := r.Items
		if len(items) == 0 {
			items = json.RawMessage(`{}`)
		}
		out = append(out, Checklist{ID: r.ID, Kind: r.Kind, Status: "Submitted", HourMeter: r.HmrReading, FuelPct: r.FuelLevelPct, Items: items,
			DamageNotes: r.DamageNotes, Notes: r.Notes, HourPhoto: hourPhoto(s, r.HmrPhotoObjectKey),
			Photos: ph, CapturedAt: r.CapturedAt, SubmittedBy: r.SubmittedByName})
	}
	return out, nil
}

// Checklists returns a booking's handover/return evidence to its parties.
func (s *Service) Checklists(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID) (ChecklistList, error) {
	if _, _, err := s.bookings.Access(ctx, p, bookingID); err != nil {
		return ChecklistList{}, err
	}
	list, err := s.checklists(ctx, bookingID)
	return ChecklistList{Data: list}, err
}

func (s *Service) AcknowledgeInspection(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID, req InspectionAcknowledgementRequest) (InspectionAcknowledgement, error) {
	b, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return InspectionAcknowledgement{}, err
	}
	role := ""
	if side.Has(bookings.SideCustomer) {
		role = "customer"
	}
	if side.Has(bookings.SideOwner) {
		role = "owner"
	}
	if side.Has(bookings.SideOperator) {
		role = "operator"
	}
	if role == "" {
		return InspectionAcknowledgement{}, apperror.ErrForbidden
	}
	checks, err := s.Checklists(ctx, p, bookingID)
	if err != nil {
		return InspectionAcknowledgement{}, err
	}
	if !hasKind(checks.Data, req.Stage) {
		return InspectionAcknowledgement{}, apperror.ErrBusinessRule.WithDetail("submit the " + req.Stage + " inspection first")
	}
	if req.Stage == "handover" && b.Status != bookings.StatusOnSite {
		return InspectionAcknowledgement{}, apperror.ErrInvalidTransition.WithDetail("handover review is available while the equipment is on site")
	}
	if req.Stage == "return" && b.Status != bookings.StatusReturnInitiated {
		return InspectionAcknowledgement{}, apperror.ErrInvalidTransition.WithDetail("return review is available before return confirmation")
	}
	if role != "customer" && strings.TrimSpace(req.Disagreement) != "" {
		return InspectionAcknowledgement{}, apperror.ErrValidation.WithDetail("only customers can report a disagreement; other parties acknowledge the inspection")
	}
	var row db.AcknowledgeBookingInspectionRow
	disagreement := strings.TrimSpace(req.Disagreement)
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: role, Reason: "inspection acknowledgement"}, func(ctx context.Context) error {
		var err error
		row, err = s.q(ctx).AcknowledgeBookingInspection(ctx, db.AcknowledgeBookingInspectionParams{BookingID: bookingID, Stage: req.Stage, UserID: p.UserID, Role: role, Disagreement: optionalText(disagreement)})
		if errors.Is(database.MapError(err), apperror.ErrNotFound) {
			// Already acknowledged (e.g. by submitting the checklist): acknowledging again is a no-op.
			row, err = db.AcknowledgeBookingInspectionRow{AcknowledgedAt: time.Now()}, nil
			return nil
		}
		if err != nil {
			return database.MapError(err)
		}
		if role == "customer" && disagreement != "" {
			stage := "inspection_" + req.Stage
			_, err = s.insertIssue(ctx, s.q(ctx), p, b, role, IssueRequest{Kind: "damage", Stage: stage, Severity: "major", Description: disagreement})
			return err
		}
		return nil
	})
	if err != nil {
		return InspectionAcknowledgement{}, err
	}
	return InspectionAcknowledgement{ID: row.ID, BookingID: bookingID, Stage: req.Stage, UserID: p.UserID, Role: role, AcknowledgedAt: row.AcknowledgedAt, Disagreement: req.Disagreement}, nil
}

func optionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// RecordInspection stores a clean acknowledgement for a stage (idempotent per user).
func (s *Service) RecordInspection(ctx context.Context, bookingID uuid.UUID, stage string, userID uuid.UUID, role string) error {
	_, err := s.q(ctx).AcknowledgeBookingInspection(ctx, db.AcknowledgeBookingInspectionParams{BookingID: bookingID, Stage: stage, UserID: userID, Role: role})
	err = database.MapError(err)
	if errors.Is(err, apperror.ErrNotFound) { // already acknowledged (conflict returns no row)
		return nil
	}
	return err
}

func (s *Service) InspectionAcknowledgements(ctx context.Context, bookingID uuid.UUID, stage string) (int32, error) {
	if s.demoGate {
		return 2, nil
	}
	count, err := s.q(ctx).CountInspectionAcknowledgements(ctx, db.CountInspectionAcknowledgementsParams{BookingID: bookingID, Stage: stage})
	return count, database.MapError(err)
}

// CustomerDeliveryPhotos returns delivery evidence only to the booking parties.
func (s *Service) CustomerDeliveryPhotos(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID) (CustomerDeliveryPhotoList, error) {
	if _, _, err := s.bookings.Access(ctx, p, bookingID); err != nil {
		return CustomerDeliveryPhotoList{}, err
	}
	rows, err := s.q(ctx).ListCustomerDeliveryPhotos(ctx, bookingID)
	if err != nil {
		return CustomerDeliveryPhotoList{}, database.MapError(err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UploadID)
	}
	photos, err := s.photos(ctx, ids)
	if err != nil {
		return CustomerDeliveryPhotoList{}, err
	}
	urls := make(map[uuid.UUID]string, len(photos))
	for _, photo := range photos {
		urls[photo.ID] = photo.URL
	}
	out := CustomerDeliveryPhotoList{Data: make([]CustomerDeliveryPhoto, 0, len(rows))}
	for _, row := range rows {
		out.Data = append(out.Data, CustomerDeliveryPhoto{ID: row.ID, BookingID: row.BookingID, UploadID: row.UploadID,
			URL: urls[row.UploadID], CapturedAt: row.CapturedAt, SubmittedAt: row.SubmittedAt, SubmittedBy: row.SubmittedBy,
			SubmittedByName: row.SubmittedByName, ApprovedAt: row.ApprovedAt, ApprovedByName: row.ApprovedByName})
	}
	return out, nil
}

// AddCustomerDeliveryPhoto lets the customer record timestamped delivery evidence.
func (s *Service) AddCustomerDeliveryPhoto(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID, req CustomerDeliveryPhotoRequest) (CustomerDeliveryPhoto, error) {
	b, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return CustomerDeliveryPhoto{}, err
	}
	if !side.Has(bookings.SideCustomer) {
		return CustomerDeliveryPhoto{}, apperror.ErrForbidden
	}
	if b.Status != bookings.StatusOnSite {
		return CustomerDeliveryPhoto{}, apperror.ErrInvalidTransition.WithDetail("delivery photos can be submitted when the machine arrives, before work starts")
	}
	if req.CapturedAt.After(time.Now().Add(10 * time.Minute)) {
		return CustomerDeliveryPhoto{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "captured_at", Code: "future", Message: "cannot be in the future"})
	}
	if _, err := s.files.Claim(ctx, p.UserID, req.UploadID, filecheck.JobPhoto); err != nil {
		return CustomerDeliveryPhoto{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "upload_id", Code: "invalid", Message: "upload your delivery photo first"})
	}
	var row db.CustomerDeliveryPhoto
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "customer", Reason: "delivery photo"}, func(ctx context.Context) error {
		var err error
		row, err = s.q(ctx).InsertCustomerDeliveryPhoto(ctx, db.InsertCustomerDeliveryPhotoParams{
			BookingID: bookingID, UploadID: req.UploadID, ClientID: req.ClientID, CapturedAt: req.CapturedAt.UTC(), SubmittedBy: p.UserID,
		})
		if err == nil {
			pl := bookings.EventPayload(b, p.UserID)
			pl.Extra = map[string]any{"booking_id": b.ID}
			return database.MapError(events.Emit(ctx, s.q(ctx), "booking", b.ID, events.DeliveryPhotoSubmitted, pl))
		}
		if !errors.Is(database.MapError(err), apperror.ErrNotFound) {
			return database.MapError(err)
		}
		row, err = s.q(ctx).GetCustomerDeliveryPhotoByClient(ctx, req.ClientID)
		if err != nil {
			mapped := database.MapError(err)
			if errors.Is(mapped, apperror.ErrNotFound) {
				return apperror.ErrConflict.WithDetail("this photo upload was already attached to a delivery record")
			}
			return mapped
		}
		if row.BookingID != bookingID || row.UploadID != req.UploadID || row.SubmittedBy != p.UserID {
			return apperror.ErrConflict.WithDetail("client_id was already used for another photo")
		}
		return nil
	})
	if err != nil {
		return CustomerDeliveryPhoto{}, err
	}
	photos, err := s.CustomerDeliveryPhotos(ctx, p, bookingID)
	if err != nil {
		return CustomerDeliveryPhoto{}, err
	}
	for _, photo := range photos.Data {
		if photo.ID == row.ID {
			return photo, nil
		}
	}
	return CustomerDeliveryPhoto{}, apperror.ErrNotFound
}

func hourPhoto(s *Service, key *string) *Photo {
	if key == nil || *key == "" {
		return nil
	}
	return &Photo{URL: s.files.SignedURL(*key, s.urlTTL).URL}
}

// ApproveDeliveryPhoto lets the booking's equipment owner approve a customer's delivery photo (idempotent).
func (s *Service) ApproveDeliveryPhoto(ctx context.Context, p *httpx.Principal, bookingID, photoID uuid.UUID) (CustomerDeliveryPhotoList, error) {
	b, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return CustomerDeliveryPhotoList{}, err
	}
	if !side.Has(bookings.SideOwner) {
		return CustomerDeliveryPhotoList{}, apperror.ErrForbidden.WithDetail("only the equipment owner approves delivery photos")
	}
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "owner", Reason: "approve delivery photo"}, func(ctx context.Context) error {
		_, err := s.q(ctx).ApproveCustomerDeliveryPhoto(ctx, db.ApproveCustomerDeliveryPhotoParams{ID: photoID, BookingID: bookingID, UserID: p.UserID})
		if err != nil {
			return database.MapError(err) // not found: unknown photo or already approved
		}
		pl := bookings.EventPayload(b, p.UserID)
		pl.Extra = map[string]any{"booking_id": b.ID}
		return database.MapError(events.Emit(ctx, s.q(ctx), "booking", b.ID, events.DeliveryPhotoApproved, pl))
	})
	if err != nil && !errors.Is(err, apperror.ErrNotFound) {
		return CustomerDeliveryPhotoList{}, err
	}
	list, err := s.CustomerDeliveryPhotos(ctx, p, bookingID)
	if err != nil {
		return list, err
	}
	for _, ph := range list.Data {
		if ph.ID == photoID {
			return list, nil
		}
	}
	return CustomerDeliveryPhotoList{}, apperror.ErrNotFound
}

// claimPhotos checks that every upload is the caller's own job photo.
func (s *Service) claimPhotos(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) ([]string, error) {
	keys := make([]string, 0, len(ids))
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if seen[id] {
			return nil, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "photo_upload_ids", Code: "duplicate", Message: "each photo once"})
		}
		seen[id] = true
		up, err := s.files.Claim(ctx, userID, id, filecheck.JobPhoto)
		if err != nil {
			return nil, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "photo_upload_ids", Code: "invalid",
				Message: "upload job photos (purpose job_photo) yourself first"})
		}
		keys = append(keys, up.ObjectKey)
	}
	return keys, nil
}

// SubmitChecklist records handover (before) or return (after) evidence. The
// assigned operator submits it; on dry hire the owner side does.
func (s *Service) SubmitChecklist(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID, req ChecklistRequest) (Checklist, error) {
	b, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return Checklist{}, err
	}
	if !side.Has(bookings.SideOperator) && !(side.Has(bookings.SideOwner) && b.Operator == nil) {
		return Checklist{}, apperror.ErrForbidden.WithDetail("the assigned operator (or the owner on dry hire) submits checklists")
	}
	q := s.q(ctx)
	if prev, err := q.GetChecklistByClient(ctx, req.ClientID); err == nil {
		if prev.BookingID != bookingID || prev.Kind != req.Kind {
			return Checklist{}, apperror.ErrConflict.WithDetail("client_id was used for another checklist")
		}
		return s.checklistByID(ctx, bookingID, prev.ID) // offline replay
	}
	switch req.Kind {
	case "handover":
		if b.Status != bookings.StatusConfirmed && b.Status != bookings.StatusOperatorAssigned {
			return Checklist{}, apperror.ErrInvalidTransition.WithDetail("the pre-delivery checklist must be completed before dispatch")
		}
	case "return":
		if b.Status != bookings.StatusActive && b.Status != bookings.StatusReturnInitiated && b.Status != "extension_requested" {
			return Checklist{}, apperror.ErrInvalidTransition.WithDetail("the return checklist is done at the end of the rental")
		}
	}
	existing, err := s.checklists(ctx, bookingID)
	if err != nil {
		return Checklist{}, err
	}
	if hasKind(existing, req.Kind) {
		return Checklist{}, apperror.ErrConflict.WithDetail("the " + req.Kind + " checklist was already submitted")
	}
	floor := 0.0
	for _, c := range existing {
		if c.Kind == "handover" && c.HourMeter != nil {
			floor = *c.HourMeter
		}
	}
	if req.Kind == "return" && req.HourMeter != nil && *req.HourMeter < floor {
		return Checklist{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "hour_meter", Code: "min",
			Message: fmt.Sprintf("the hour meter cannot be below the handover reading (%.1f)", floor)})
	}
	if req.CapturedAt.After(time.Now().Add(10 * time.Minute)) {
		return Checklist{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "captured_at", Code: "future", Message: "cannot be in the future"})
	}
	var hourKey *string // the hour-meter photo is optional
	if req.HourPhotoID != nil {
		hourKeys, err := s.claimPhotos(ctx, p.UserID, []uuid.UUID{*req.HourPhotoID})
		if err != nil {
			return Checklist{}, err
		}
		hourKey = &hourKeys[0]
	}
	keys, err := s.claimPhotos(ctx, p.UserID, req.PhotoIDs)
	if err != nil {
		return Checklist{}, err
	}
	items := req.Items
	if items == nil {
		items = map[string]bool{}
	}
	rawItems, _ := json.Marshal(items)
	role := "operator"
	if !side.Has(bookings.SideOperator) {
		role = "owner"
	}
	var id uuid.UUID
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: role, Reason: req.Kind + " checklist"}, func(ctx context.Context) error {
		q := s.q(ctx)
		var err error
		id, err = q.InsertJobChecklist(ctx, db.InsertJobChecklistParams{
			BookingID: bookingID, Kind: req.Kind, HmrReading: req.HourMeter, HmrPhotoObjectKey: hourKey, FuelLevelPct: req.FuelPct,
			Items: rawItems, PhotoObjectKeys: keys, PhotoUploadIds: req.PhotoIDs, DamageNotes: trimmed(req.DamageNotes), Notes: trimmed(req.Notes),
			CapturedAt: req.CapturedAt.UTC(), SubmittedBy: p.UserID, ClientID: req.ClientID, Lat: req.Lat, Lng: req.Lng,
		})
		if err != nil {
			err = database.MapError(err)
			if errors.Is(err, apperror.ErrNotFound) {
				return apperror.ErrConflict.WithDetail("the " + req.Kind + " checklist was already submitted")
			}
			return err
		}
		if req.HourMeter != nil {
			if err := q.SetEquipmentHMR(ctx, db.SetEquipmentHMRParams{ID: b.EquipmentID, Hmr: *req.HourMeter}); err != nil {
				return database.MapError(err)
			}
		}
		// Submitting the inspection checklist is the equipment side's acknowledgement of that stage.
		if err := s.RecordInspection(ctx, bookingID, req.Kind, p.UserID, role); err != nil {
			return err
		}
		// Damage seen at return becomes an issue for the owner and EQUIPRENT.
		if req.Kind == "return" && trimmed(req.DamageNotes) != nil {
			return s.fileIssue(ctx, q, p, b, role, IssueRequest{Kind: "damage", Stage: "at_return", Severity: "minor",
				Description: *trimmed(req.DamageNotes), PhotoIDs: req.PhotoIDs})
		}
		return nil
	})
	if err != nil {
		return Checklist{}, err
	}
	return s.checklistByID(ctx, bookingID, id)
}

func trimmed(s *string) *string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	v := strings.TrimSpace(*s)
	return &v
}

func (s *Service) checklistByID(ctx context.Context, bookingID, id uuid.UUID) (Checklist, error) {
	list, err := s.checklists(ctx, bookingID)
	if err != nil {
		return Checklist{}, err
	}
	for _, c := range list {
		if c.ID == id {
			return c, nil
		}
	}
	return Checklist{}, apperror.ErrNotFound
}

// ---- issues ----------------------------------------------------------------------------------

func (s *Service) toIssue(ctx context.Context, i db.IssueReport, reporter string) (Issue, error) {
	ph, err := s.photos(ctx, i.PhotoUploadIds)
	if err != nil {
		return Issue{}, err
	}
	return Issue{ID: i.ID, BookingID: i.BookingID, Kind: i.Kind, Stage: i.Stage, Severity: i.Severity, Description: i.Description,
		Photos: ph, ReporterName: reporter, ReporterRole: i.ReporterRole, Status: string(i.Status), ResolutionNote: i.ResolutionNote,
		ReviewedAt: i.ReviewedAt, CreatedAt: i.CreatedAt}, nil
}

func (s *Service) issues(ctx context.Context, bookingID uuid.UUID) ([]Issue, error) {
	rows, err := s.q(ctx).ListBookingIssues(ctx, bookingID)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Issue, 0, len(rows))
	for _, r := range rows {
		x, err := s.toIssue(ctx, db.IssueReport{ID: r.ID, BookingID: r.BookingID, EquipmentID: r.EquipmentID, Kind: r.Kind, Stage: r.Stage,
			Severity: r.Severity, Description: r.Description, PhotoUploadIds: r.PhotoUploadIds, ReportedBy: r.ReportedBy, ReporterRole: r.ReporterRole,
			Status: r.Status, ResolutionNote: r.ResolutionNote, ReviewedBy: r.ReviewedBy, ReviewedAt: r.ReviewedAt, CreatedAt: r.CreatedAt}, r.ReporterName)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, nil
}

// Issues lists a booking's reported issues for its parties.
func (s *Service) Issues(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID) (IssueList, error) {
	if _, _, err := s.bookings.Access(ctx, p, bookingID); err != nil {
		return IssueList{}, err
	}
	list, err := s.issues(ctx, bookingID)
	return IssueList{Data: list}, err
}

// Report files an issue on a booking (operator, owner side or customer).
func (s *Service) Report(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID, req IssueRequest) (Issue, error) {
	b, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return Issue{}, err
	}
	role := ""
	switch {
	case side.Has(bookings.SideOperator):
		role = "operator"
	case side.Has(bookings.SideOwner):
		role = "owner"
	case side.Has(bookings.SideCustomer):
		role = "customer"
	default:
		return Issue{}, apperror.ErrForbidden.WithDetail("only the parties and the operator report issues")
	}
	if b.Status == bookings.StatusDraft || b.Status == bookings.StatusRequested {
		return Issue{}, apperror.ErrInvalidTransition.WithDetail("issues are reported on confirmed jobs")
	}
	var out db.IssueReport
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: role, Reason: "issue reported"}, func(ctx context.Context) error {
		if _, err := s.claimPhotos(ctx, p.UserID, req.PhotoIDs); err != nil {
			return err
		}
		var err error
		out, err = s.insertIssue(ctx, s.q(ctx), p, b, role, req)
		return err
	})
	if err != nil {
		return Issue{}, err
	}
	return s.toIssue(ctx, out, "")
}

// fileIssue records an issue inside an existing transaction.
func (s *Service) fileIssue(ctx context.Context, q *db.Queries, p *httpx.Principal, b bookings.Booking, role string, req IssueRequest) error {
	_, err := s.insertIssue(ctx, q, p, b, role, req)
	return err
}

func (s *Service) insertIssue(ctx context.Context, q *db.Queries, p *httpx.Principal, b bookings.Booking, role string, req IssueRequest) (db.IssueReport, error) {
	ids := req.PhotoIDs
	if ids == nil {
		ids = []uuid.UUID{}
	}
	row, err := q.InsertIssueReport(ctx, db.InsertIssueReportParams{BookingID: b.ID, EquipmentID: b.EquipmentID, Kind: req.Kind, Stage: req.Stage,
		Severity: req.Severity, Description: strings.TrimSpace(req.Description), PhotoUploadIds: ids, ReportedBy: p.UserID, ReporterRole: role})
	if err != nil {
		return db.IssueReport{}, database.MapError(err)
	}
	pl := bookings.EventPayload(b, p.UserID)
	pl.Extra = map[string]any{"booking_id": b.ID, "equipment_id": b.EquipmentID, "issue_id": row.ID,
		"title": fmt.Sprintf("%s %s reported", strings.ToUpper(req.Severity[:1])+req.Severity[1:], strings.ReplaceAll(req.Kind, "_", " "))}
	return row, database.MapError(events.Emit(ctx, q, "issue", row.ID, events.IssueReported, pl))
}

// Queue lists issues for EQUIPRENT's review, most severe first.
func (s *Service) Queue(ctx context.Context, f IssueQuery) (IssueList, error) {
	params := db.ListIssueQueueParams{Lim: 200}
	if f.Status != "" {
		params.Status = &f.Status
	}
	if f.Severity != "" {
		params.Severity = &f.Severity
	}
	rows, err := s.q(ctx).ListIssueQueue(ctx, params)
	if err != nil {
		return IssueList{}, database.MapError(err)
	}
	out := IssueList{Data: make([]Issue, 0, len(rows))}
	for _, r := range rows {
		x, err := s.toIssue(ctx, db.IssueReport{ID: r.ID, BookingID: r.BookingID, EquipmentID: r.EquipmentID, Kind: r.Kind, Stage: r.Stage,
			Severity: r.Severity, Description: r.Description, PhotoUploadIds: r.PhotoUploadIds, ReporterRole: r.ReporterRole, Status: r.Status,
			ResolutionNote: r.ResolutionNote, ReviewedAt: r.ReviewedAt, CreatedAt: r.CreatedAt}, r.ReporterName)
		if err != nil {
			return IssueList{}, err
		}
		x.BookingReference, x.EquipmentTitle, x.OwnerName, x.CustomerName = r.BookingReference, r.EquipmentTitle, r.OwnerName, r.CustomerName
		if x.InspectionEvidence, err = s.checklists(ctx, r.BookingID); err != nil {
			return IssueList{}, err
		}
		if x.DecisionHistory, err = s.inspectionHistory(ctx, r.ID); err != nil {
			return IssueList{}, err
		}
		out.Data = append(out.Data, x)
	}
	return out, nil
}

func (s *Service) inspectionHistory(ctx context.Context, issueID uuid.UUID) ([]InspectionHistoryEntry, error) {
	rows, err := s.q(ctx).ListInspectionDecisionHistory(ctx, issueID)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]InspectionHistoryEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, InspectionHistoryEntry{Action: row.Action, PartyRole: row.PartyRole, Note: row.Note, CreatedAt: row.CreatedAt})
	}
	return out, nil
}

// Review moves an issue along (under review, resolved, dismissed) and tells
// the owner side and the reporter.
func (s *Service) Review(ctx context.Context, p *httpx.Principal, id uuid.UUID, req ReviewIssueRequest) (Issue, error) {
	note := strings.TrimSpace(req.Note)
	if req.Action == "request_explanation" {
		if !slices.Contains([]string{"customer", "owner", "operator"}, req.PartyRole) || len(note) < 3 {
			return Issue{}, apperror.ErrValidation.WithDetail("select a party and provide an explanation request")
		}
		var row db.IssueReport
		err := s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "admin", Reason: "inspection explanation requested"}, func(ctx context.Context) error {
			var err error
			row, err = s.q(ctx).GetIssue(ctx, id)
			if err != nil {
				return database.MapError(err)
			}
			_, err = s.q(ctx).InsertInspectionDecisionHistory(ctx, db.InsertInspectionDecisionHistoryParams{IssueID: id, ActorUserID: p.UserID, Action: "request_explanation", PartyRole: &req.PartyRole, Note: note})
			return database.MapError(err)
		})
		if err != nil {
			return Issue{}, err
		}
		return s.toIssue(ctx, row, "")
	}
	if req.Status != "under_review" && len(note) < 3 {
		return Issue{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "note", Code: "required", Message: "explain the outcome"})
	}
	var row db.IssueReport
	err := s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "admin", Reason: "issue " + req.Status}, func(ctx context.Context) error {
		q := s.q(ctx)
		var notePtr *string
		if note != "" {
			notePtr = &note
		}
		var err error
		row, err = q.ReviewIssue(ctx, db.ReviewIssueParams{ID: id, Status: db.IssueStatus(req.Status), ResolutionNote: notePtr,
			ReviewedBy: uuid.NullUUID{UUID: p.UserID, Valid: true}})
		if err != nil {
			err = database.MapError(err)
			if errors.Is(err, apperror.ErrNotFound) {
				if _, gerr := q.GetIssue(ctx, id); gerr == nil {
					return apperror.ErrConflict.WithDetail("the issue is already closed")
				}
			}
			return err
		}
		if req.Action == "decision" {
			if _, err := q.InsertInspectionDecisionHistory(ctx, db.InsertInspectionDecisionHistoryParams{IssueID: id, ActorUserID: p.UserID, Action: "decision", Note: note}); err != nil {
				return database.MapError(err)
			}
		}
		b, err := s.bookings.Load(ctx, row.BookingID)
		if err != nil {
			return err
		}
		pl := bookings.EventPayload(b, p.UserID)
		pl.UserIDs = []uuid.UUID{row.ReportedBy}
		pl.Extra = map[string]any{"booking_id": b.ID, "equipment_id": b.EquipmentID, "issue_id": row.ID, "title": strings.ReplaceAll(req.Status, "_", " ")}
		return database.MapError(events.Emit(ctx, q, "issue", row.ID, events.IssueReviewed, pl))
	})
	if err != nil {
		return Issue{}, err
	}
	return s.toIssue(ctx, row, "")
}

// ---- restrictions ---------------------------------------------------------------------------------

func toRestriction(r db.ListRestrictionsRow) Restriction {
	return Restriction{ID: r.ID, UserID: r.UserID, UserName: r.UserName, UserPhone: r.UserPhone, Kind: r.Kind, Reason: r.Reason, Active: r.Active,
		CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt, LiftedAt: r.LiftedAt, LiftReason: r.LiftReason}
}

func (s *Service) Restrictions(ctx context.Context, f RestrictionQuery) (RestrictionList, error) {
	params := db.ListRestrictionsParams{Lim: 200, ActiveOnly: f.Active != nil && *f.Active}
	if f.UserID != "" {
		params.UserID = uuid.NullUUID{UUID: uuid.MustParse(f.UserID), Valid: true}
	}
	rows, err := s.q(ctx).ListRestrictions(ctx, params)
	if err != nil {
		return RestrictionList{}, database.MapError(err)
	}
	out := RestrictionList{Data: make([]Restriction, 0, len(rows))}
	for _, r := range rows {
		out.Data = append(out.Data, toRestriction(r))
	}
	return out, nil
}

func (s *Service) Restrict(ctx context.Context, p *httpx.Principal, userID uuid.UUID, req RestrictionRequest) (Restriction, error) {
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		return Restriction{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "expires_at", Code: "future", Message: "must be in the future"})
	}
	var id uuid.UUID
	err := s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "admin", Reason: "restriction: " + req.Reason}, func(ctx context.Context) error {
		r, err := s.q(ctx).InsertRestriction(ctx, db.InsertRestrictionParams{UserID: userID, Kind: req.Kind, Reason: strings.TrimSpace(req.Reason),
			CreatedBy: uuid.NullUUID{UUID: p.UserID, Valid: true}, ExpiresAt: req.ExpiresAt})
		id = r.ID
		return database.MapError(err)
	})
	if errors.Is(err, apperror.ErrConflict) {
		return Restriction{}, apperror.ErrConflict.WithDetail("this restriction is already in force")
	}
	if err != nil {
		return Restriction{}, err
	}
	return s.restriction(ctx, userID, id)
}

func (s *Service) restriction(ctx context.Context, userID, id uuid.UUID) (Restriction, error) {
	list, err := s.Restrictions(ctx, RestrictionQuery{UserID: userID.String()})
	if err != nil {
		return Restriction{}, err
	}
	for _, r := range list.Data {
		if r.ID == id {
			return r, nil
		}
	}
	return Restriction{}, apperror.ErrNotFound
}

func (s *Service) Lift(ctx context.Context, p *httpx.Principal, id uuid.UUID, req LiftRequest) (Restriction, error) {
	var r db.AccountRestriction
	reason := strings.TrimSpace(req.Reason)
	err := s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "admin", Reason: "restriction lifted: " + reason}, func(ctx context.Context) error {
		var err error
		r, err = s.q(ctx).LiftRestriction(ctx, db.LiftRestrictionParams{ID: id, LiftedBy: uuid.NullUUID{UUID: p.UserID, Valid: true}, LiftReason: &reason})
		return database.MapError(err)
	})
	if err != nil {
		return Restriction{}, err
	}
	return s.restriction(ctx, r.UserID, r.ID)
}

// ---- handler -------------------------------------------------------------------------------------

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Name() string { return "operations" }

func (h *Handler) Routes(r *httpx.Router) {
	a := r.Tag("operator-jobs").Authed()
	a.GET("/operator/jobs/:booking_id", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "booking_id")
		if err != nil {
			return nil, err
		}
		return h.svc.Job(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "One of my jobs: customer, machine, site, navigation, evidence and next steps", Response: JobDetail{},
		Description: "Customer and owner phone numbers are shown once you have accepted the job."})
	a.GET("/operator/schedule", httpx.HandleQuery(200, func(c *gin.Context, p *httpx.Principal, q ScheduleQuery) (any, error) {
		return h.svc.Schedule(c.Request.Context(), p, q)
	}), httpx.Doc{Summary: "My jobs in a period", Query: ScheduleQuery{}, Response: OperatorSchedule{}})
	submit := httpx.HandleJSON(201, func(c *gin.Context, p *httpx.Principal, req ChecklistRequest) (any, error) {
		id, err := httpx.Param(c, "booking_id")
		if err != nil {
			id, err = httpx.Param(c, "id")
		}
		if err != nil {
			return nil, err
		}
		return h.svc.SubmitChecklist(c.Request.Context(), p, id, req)
	})
	checklistDoc := httpx.Doc{Summary: "Submit the handover (before) or return (after) checklist", Request: ChecklistRequest{}, Response: Checklist{}, Status: 201,
		Description: "Hour meter with its photo, at least two job_photo uploads of the machine, items, fuel, notes and position. Starting work and handing the machine back require it."}
	a.POST("/operator/jobs/:booking_id/checklists", submit, checklistDoc)
	report := httpx.HandleJSON(201, func(c *gin.Context, p *httpx.Principal, req IssueRequest) (any, error) {
		id, err := httpx.Param(c, "booking_id")
		if err != nil {
			id, err = httpx.Param(c, "id")
		}
		if err != nil {
			return nil, err
		}
		return h.svc.Report(c.Request.Context(), p, id, req)
	})
	issueDoc := httpx.Doc{Summary: "Report damage, a breakdown, a safety or access problem", Request: IssueRequest{}, Response: Issue{}, Status: 201}
	a.POST("/operator/jobs/:booking_id/issues", report, issueDoc)

	b := r.Tag("bookings").Authed()
	b.POST("/bookings/:id/inspection-acknowledgements", httpx.HandleJSON(201, func(c *gin.Context, p *httpx.Principal, req InspectionAcknowledgementRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.AcknowledgeInspection(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Acknowledge the booking inspection or report a disagreement", Request: InspectionAcknowledgementRequest{}, Response: InspectionAcknowledgement{}})
	b.POST("/bookings/:id/delivery-photos", httpx.HandleJSON(201, func(c *gin.Context, p *httpx.Principal, req CustomerDeliveryPhotoRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.AddCustomerDeliveryPhoto(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Upload customer evidence of equipment delivery", Request: CustomerDeliveryPhotoRequest{}, Response: CustomerDeliveryPhoto{}, Status: 201,
		Description: "Customer-only submission while the booking is on site. Images are private and shared only with the booking parties; server receipt time is recorded separately from capture time."})
	b.POST("/bookings/:id/delivery-photos/:photo_id/approve", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		photo, err := httpx.Param(c, "photo_id")
		if err != nil {
			return nil, err
		}
		return h.svc.ApproveDeliveryPhoto(c.Request.Context(), p, id, photo)
	}), httpx.Doc{Summary: "Approve a customer delivery photo (equipment owner only)", Response: CustomerDeliveryPhotoList{}})
	b.GET("/bookings/:id/delivery-photos", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.CustomerDeliveryPhotos(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "List booking delivery photos", Response: CustomerDeliveryPhotoList{}})
	b.POST("/bookings/:id/checklists", submit, checklistDoc)
	b.GET("/bookings/:id/checklists", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Checklists(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Handover and return evidence of a booking", Response: ChecklistList{}})
	b.POST("/bookings/:id/issues", report, issueDoc)
	b.GET("/bookings/:id/issues", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Issues(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Issues reported on a booking", Response: IssueList{}})

	ad := r.Tag("admin").Admin(httpx.RoleAdminOps, httpx.RoleAdminSupport)
	ad.GET("/admin/issues", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q IssueQuery) (any, error) {
		return h.svc.Queue(c.Request.Context(), q)
	}), httpx.Doc{Summary: "Reported issues, most severe first", Query: IssueQuery{}, Response: IssueList{}})
	ad.POST("/admin/issues/:id/review", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req ReviewIssueRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Review(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Move an issue to under review, resolved or dismissed", Request: ReviewIssueRequest{}, Response: Issue{}})
	ad.GET("/admin/restrictions", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q RestrictionQuery) (any, error) {
		return h.svc.Restrictions(c.Request.Context(), q)
	}), httpx.Doc{Summary: "Account restrictions", Query: RestrictionQuery{}, Response: RestrictionList{}})
	ad.POST("/admin/users/:id/restrictions", httpx.HandleJSON(201, func(c *gin.Context, p *httpx.Principal, req RestrictionRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Restrict(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Restrict an account", Request: RestrictionRequest{}, Response: Restriction{}, Status: 201,
		Description: "operator_assignments: cannot be assigned jobs. fleet_management: loses access to fleet machines. new_bookings: cannot book. Use suspend for a full block."})
	ad.POST("/admin/restrictions/:id/lift", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req LiftRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Lift(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Lift a restriction", Request: LiftRequest{}, Response: Restriction{}})
}
