// Package maintenance keeps each machine's service history: preventive
// maintenance plans (every N hours of the hour meter and/or every N days),
// scheduled and completed jobs (service, repair, inspection, breakdown) with
// their costs (parts and labour) and invoices, and the cost reports.
//
// A job with a downtime window blocks the machine's calendar (an
// availability block with reason maintenance), so it cannot be booked
// meanwhile; finishing early frees the rest of the window and cancelling
// frees all of it. Completing a planned job resets its plan. Reminders are
// sent by the worker (Reminders).
package maintenance

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/access"
	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/filecheck"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/storage"
)

type Plan struct {
	ID          uuid.UUID  `json:"id"`
	EquipmentID uuid.UUID  `json:"equipment_id"`
	Task        string     `json:"task"`
	EveryHours  *int32     `json:"every_hours,omitempty"`
	EveryDays   *int32     `json:"every_days,omitempty"`
	LastDoneHMR *float64   `json:"last_done_hmr,omitempty"`
	LastDoneAt  *time.Time `json:"last_done_at,omitempty"`
	Active      bool       `json:"active"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Window is a time range [start, end).
type Window struct {
	Start time.Time `json:"start" binding:"required"`
	End   time.Time `json:"end" binding:"required,gtfield=Start"`
}

type Record struct {
	ID              uuid.UUID     `json:"id"`
	EquipmentID     uuid.UUID     `json:"equipment_id"`
	EquipmentTitle  string        `json:"equipment_title,omitempty"`
	PlanID          uuid.NullUUID `json:"plan_id"`
	Kind            string        `json:"kind" doc:"service | repair | inspection | breakdown"`
	Task            string        `json:"task"`
	Status          string        `json:"status" doc:"scheduled | in_progress | done | cancelled"`
	ScheduledFor    *time.Time    `json:"scheduled_for,omitempty"`
	PerformedAt     *time.Time    `json:"performed_at,omitempty"`
	HMRAt           *float64      `json:"hmr_at,omitempty"`
	CostPaise       *int64        `json:"cost_paise,omitempty"`
	PartsCostPaise  *int64        `json:"parts_cost_paise,omitempty"`
	LabourCostPaise *int64        `json:"labour_cost_paise,omitempty"`
	Vendor          *string       `json:"vendor,omitempty"`
	Notes           *string       `json:"notes,omitempty"`
	Downtime        *Window       `json:"downtime,omitempty" doc:"While the machine is out of service"`
	BlocksCalendar  bool          `json:"blocks_calendar" doc:"The downtime is blocked on the booking calendar"`
	HasInvoice      bool          `json:"has_invoice"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

type Due struct {
	PlanID      uuid.UUID  `json:"plan_id"`
	Task        string     `json:"task"`
	EquipmentID uuid.UUID  `json:"equipment_id"`
	Title       string     `json:"equipment_title"`
	CurrentHMR  float64    `json:"current_hmr"`
	EveryHours  *int32     `json:"every_hours,omitempty"`
	EveryDays   *int32     `json:"every_days,omitempty"`
	LastDoneAt  *time.Time `json:"last_done_at,omitempty"`
	LastDoneHMR *float64   `json:"last_done_hmr,omitempty"`
}

type CreatePlanRequest struct {
	Task        string   `json:"task" binding:"required,min=3,max=200"`
	EveryHours  *int32   `json:"every_hours" binding:"omitempty,min=1,max=100000"`
	EveryDays   *int32   `json:"every_days" binding:"omitempty,min=1,max=3650"`
	LastDoneHMR *float64 `json:"last_done_hmr" binding:"omitempty,min=0"`
}

type UpdatePlanRequest struct {
	Task       *string `json:"task" binding:"omitempty,min=3,max=200"`
	EveryHours *int32  `json:"every_hours" binding:"omitempty,min=0,max=100000" doc:"0 removes the hour-meter interval"`
	EveryDays  *int32  `json:"every_days" binding:"omitempty,min=0,max=3650" doc:"0 removes the calendar interval"`
	Active     *bool   `json:"active"`
}

type CreateRecordRequest struct {
	PlanID          uuid.NullUUID `json:"plan_id"`
	Kind            string        `json:"kind" binding:"omitempty,oneof=service repair inspection breakdown" doc:"Default service"`
	Task            string        `json:"task" binding:"required,min=3,max=200"`
	Status          string        `json:"status" binding:"required,oneof=scheduled in_progress done"`
	ScheduledFor    *string       `json:"scheduled_for" binding:"omitempty,datetime=2006-01-02"`
	PerformedAt     *time.Time    `json:"performed_at" doc:"For work logged after the fact (status done); default now"`
	HMRAt           *float64      `json:"hmr_at" binding:"omitempty,min=0"`
	CostPaise       *int64        `json:"cost_paise" binding:"omitempty,min=0" doc:"Total; defaults to parts + labour"`
	PartsCostPaise  *int64        `json:"parts_cost_paise" binding:"omitempty,min=0"`
	LabourCostPaise *int64        `json:"labour_cost_paise" binding:"omitempty,min=0"`
	Vendor          *string       `json:"vendor" binding:"omitempty,max=200"`
	Notes           *string       `json:"notes" binding:"omitempty,max=2000"`
	Downtime        *Window       `json:"downtime" doc:"Out-of-service window"`
	BlockCalendar   *bool         `json:"block_calendar" doc:"Block the downtime on the booking calendar (default true for scheduled and in-progress jobs)"`
	InvoiceUploadID uuid.NullUUID `json:"invoice_upload_id" doc:"An upload with purpose maintenance_document"`
}

type UpdateRecordRequest struct {
	Status          string        `json:"status" binding:"required,oneof=scheduled in_progress done cancelled"`
	ScheduledFor    *string       `json:"scheduled_for" binding:"omitempty,datetime=2006-01-02"`
	HMRAt           *float64      `json:"hmr_at" binding:"omitempty,min=0"`
	CostPaise       *int64        `json:"cost_paise" binding:"omitempty,min=0"`
	PartsCostPaise  *int64        `json:"parts_cost_paise" binding:"omitempty,min=0"`
	LabourCostPaise *int64        `json:"labour_cost_paise" binding:"omitempty,min=0"`
	Vendor          *string       `json:"vendor" binding:"omitempty,max=200"`
	Notes           *string       `json:"notes" binding:"omitempty,max=2000"`
	Downtime        *Window       `json:"downtime" doc:"Move the out-of-service window"`
	ClearDowntime   bool          `json:"clear_downtime" doc:"Remove the window (and free the calendar)"`
	InvoiceUploadID uuid.NullUUID `json:"invoice_upload_id"`
}

type HistoryQuery struct {
	EquipmentID string `form:"equipment_id" binding:"omitempty,uuid"`
	Kind        string `form:"kind" binding:"omitempty,oneof=service repair inspection breakdown"`
	Status      string `form:"status" binding:"omitempty,oneof=scheduled in_progress done cancelled"`
	Limit       int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Cursor      string `form:"cursor"`
}

type CostQuery struct {
	From string `form:"from" binding:"omitempty,datetime=2006-01-02" doc:"Default: 12 months ago"`
	To   string `form:"to" binding:"omitempty,datetime=2006-01-02" doc:"Inclusive; default today"`
}

// CostRow is spend for one machine, kind and month.
type CostRow struct {
	EquipmentID     uuid.UUID `json:"equipment_id"`
	EquipmentTitle  string    `json:"equipment_title"`
	Kind            string    `json:"kind"`
	Month           string    `json:"month" doc:"YYYY-MM (IST)"`
	Jobs            int32     `json:"jobs"`
	CostPaise       int64     `json:"cost_paise"`
	PartsCostPaise  int64     `json:"parts_cost_paise"`
	LabourCostPaise int64     `json:"labour_cost_paise"`
	DowntimeHours   float64   `json:"downtime_hours"`
}

// Total is a subtotal by one dimension.
type Total struct {
	Key           string  `json:"key" doc:"equipment id, kind or month"`
	Label         string  `json:"label"`
	Jobs          int32   `json:"jobs"`
	CostPaise     int64   `json:"cost_paise"`
	DowntimeHours float64 `json:"downtime_hours"`
}

type CostReport struct {
	From          string    `json:"from"`
	To            string    `json:"to"`
	TotalPaise    int64     `json:"total_paise"`
	Jobs          int32     `json:"jobs"`
	DowntimeHours float64   `json:"downtime_hours"`
	ByEquipment   []Total   `json:"by_equipment"`
	ByKind        []Total   `json:"by_kind"`
	ByMonth       []Total   `json:"by_month"`
	Rows          []CostRow `json:"rows"`
}

type PlanList struct {
	Data []Plan `json:"data"`
}
type RecordList struct {
	Data []Record `json:"data"`
}
type RecordPage struct {
	Data       []Record `json:"data"`
	NextCursor string   `json:"next_cursor,omitempty"`
}
type DueList struct {
	Data []Due `json:"data"`
}

func toPlan(p db.MaintenancePlan) Plan {
	return Plan{ID: p.ID, EquipmentID: p.EquipmentID, Task: p.Task, EveryHours: p.EveryHours, EveryDays: p.EveryDays,
		LastDoneHMR: p.LastDoneHmr, LastDoneAt: p.LastDoneAt, Active: p.IsActive, CreatedAt: p.CreatedAt}
}

func window(r pgtype.Range[pgtype.Timestamptz]) *Window {
	if !r.Valid || !r.Lower.Valid || !r.Upper.Valid {
		return nil
	}
	return &Window{Start: r.Lower.Time.UTC(), End: r.Upper.Time.UTC()}
}

func toRecord(r db.ListMaintenanceHistoryRow) Record {
	return Record{ID: r.ID, EquipmentID: r.EquipmentID, EquipmentTitle: r.EquipmentTitle, PlanID: r.PlanID, Kind: string(r.Kind),
		Task: r.Task, Status: string(r.Status), ScheduledFor: r.ScheduledFor, PerformedAt: r.PerformedAt, HMRAt: r.HmrAt,
		CostPaise: r.CostPaise, PartsCostPaise: r.PartsCostPaise, LabourCostPaise: r.LabourCostPaise, Vendor: r.Vendor,
		Notes: r.Notes, Downtime: window(r.Downtime), BlocksCalendar: r.AvailabilityBlockID.Valid,
		HasInvoice: r.InvoiceUploadID.Valid, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

// ---- repository --------------------------------------------------------------------

type Repository interface {
	Owner(ctx context.Context, equipmentID uuid.UUID) (uuid.UUID, error)
	Plans(ctx context.Context, equipmentID uuid.UUID) ([]Plan, error)
	CreatePlan(ctx context.Context, p db.CreateMaintenancePlanParams) (Plan, error)
	UpdatePlan(ctx context.Context, p db.UpdateMaintenancePlanParams) (Plan, error)
	Plan(ctx context.Context, equipmentID, planID uuid.UUID) (db.MaintenancePlan, error)
	History(ctx context.Context, p db.ListMaintenanceHistoryParams) ([]Record, error)
	Record(ctx context.Context, equipmentID, id uuid.UUID) (db.GetMaintenanceRecordViewRow, error)
	CreateRecord(ctx context.Context, p db.CreateMaintenanceRecordV2Params) (uuid.UUID, error)
	UpdateRecord(ctx context.Context, p db.UpdateMaintenanceRecordV2Params) (uuid.UUID, error)
	Conflicts(ctx context.Context, equipmentID uuid.UUID, w Window) ([]string, error)
	CreateBlock(ctx context.Context, equipmentID uuid.UUID, w Window, note string, by uuid.UUID) (uuid.UUID, error)
	DeleteBlock(ctx context.Context, id uuid.UUID) error
	TrimBlock(ctx context.Context, id uuid.UUID, end time.Time) error
	SetBlock(ctx context.Context, recordID uuid.UUID, block uuid.NullUUID) error
	PlanDone(ctx context.Context, p db.MarkMaintenancePlanDoneParams) error
	Due(ctx context.Context, org uuid.NullUUID) ([]Due, error)
	Costs(ctx context.Context, p db.MaintenanceCostSummaryParams) ([]CostRow, error)
}

type PGRepository struct {
	pool *pgxpool.Pool
	tx   database.Transactor
}

func NewRepository(pool *pgxpool.Pool, tx database.Transactor) *PGRepository {
	return &PGRepository{pool: pool, tx: tx}
}

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

func (r *PGRepository) Owner(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	row, err := r.q(ctx).EquipmentOwner(ctx, id)
	return row.OwnerOrgID, database.MapError(err)
}

func (r *PGRepository) Plans(ctx context.Context, id uuid.UUID) ([]Plan, error) {
	rows, err := r.q(ctx).ListMaintenancePlans(ctx, id)
	out := make([]Plan, 0, len(rows))
	for _, p := range rows {
		out = append(out, toPlan(p))
	}
	return out, database.MapError(err)
}

func (r *PGRepository) CreatePlan(ctx context.Context, p db.CreateMaintenancePlanParams) (Plan, error) {
	row, err := r.q(ctx).CreateMaintenancePlan(ctx, p)
	return toPlan(row), database.MapError(err)
}

func (r *PGRepository) UpdatePlan(ctx context.Context, p db.UpdateMaintenancePlanParams) (Plan, error) {
	row, err := r.q(ctx).UpdateMaintenancePlan(ctx, p)
	return toPlan(row), database.MapError(err)
}

func (r *PGRepository) Plan(ctx context.Context, equipmentID, planID uuid.UUID) (db.MaintenancePlan, error) {
	p, err := r.q(ctx).GetMaintenancePlan(ctx, db.GetMaintenancePlanParams{ID: planID, EquipmentID: equipmentID})
	return p, database.MapError(err)
}

func (r *PGRepository) History(ctx context.Context, p db.ListMaintenanceHistoryParams) ([]Record, error) {
	rows, err := r.q(ctx).ListMaintenanceHistory(ctx, p)
	out := make([]Record, 0, len(rows))
	for _, x := range rows {
		out = append(out, toRecord(x))
	}
	return out, database.MapError(err)
}

func (r *PGRepository) Record(ctx context.Context, equipmentID, id uuid.UUID) (db.GetMaintenanceRecordViewRow, error) {
	row, err := r.q(ctx).GetMaintenanceRecordView(ctx, db.GetMaintenanceRecordViewParams{ID: id, EquipmentID: equipmentID})
	return row, database.MapError(err)
}

func (r *PGRepository) CreateRecord(ctx context.Context, p db.CreateMaintenanceRecordV2Params) (uuid.UUID, error) {
	id, err := r.q(ctx).CreateMaintenanceRecordV2(ctx, p)
	return id, database.MapError(err)
}

func (r *PGRepository) UpdateRecord(ctx context.Context, p db.UpdateMaintenanceRecordV2Params) (uuid.UUID, error) {
	id, err := r.q(ctx).UpdateMaintenanceRecordV2(ctx, p)
	return id, database.MapError(err)
}

func (r *PGRepository) Conflicts(ctx context.Context, equipmentID uuid.UUID, w Window) ([]string, error) {
	rows, err := r.q(ctx).MaintenanceConflicts(ctx, db.MaintenanceConflictsParams{EquipmentID: equipmentID, StartsAt: w.Start, EndsAt: w.End})
	out := make([]string, 0, len(rows))
	for _, b := range rows {
		out = append(out, b.Reference)
	}
	return out, database.MapError(err)
}

func (r *PGRepository) CreateBlock(ctx context.Context, equipmentID uuid.UUID, w Window, note string, by uuid.UUID) (uuid.UUID, error) {
	id, err := r.q(ctx).CreateMaintenanceBlock(ctx, db.CreateMaintenanceBlockParams{
		EquipmentID: equipmentID, StartsAt: w.Start, EndsAt: w.End, Note: &note, CreatedBy: uuid.NullUUID{UUID: by, Valid: true},
	})
	return id, database.MapError(err)
}

func (r *PGRepository) DeleteBlock(ctx context.Context, id uuid.UUID) error {
	return database.MapError(r.q(ctx).DeleteMaintenanceBlock(ctx, id))
}

func (r *PGRepository) TrimBlock(ctx context.Context, id uuid.UUID, end time.Time) error {
	if err := r.q(ctx).DeleteFutureMaintenanceBlock(ctx, db.DeleteFutureMaintenanceBlockParams{ID: id, FromAt: end}); err != nil {
		return database.MapError(err)
	}
	return database.MapError(r.q(ctx).TrimMaintenanceBlock(ctx, db.TrimMaintenanceBlockParams{ID: id, EndsAt: end}))
}

func (r *PGRepository) SetBlock(ctx context.Context, recordID uuid.UUID, block uuid.NullUUID) error {
	return database.MapError(r.q(ctx).SetMaintenanceBlock(ctx, db.SetMaintenanceBlockParams{ID: recordID, BlockID: block}))
}

func (r *PGRepository) PlanDone(ctx context.Context, p db.MarkMaintenancePlanDoneParams) error {
	return database.MapError(r.q(ctx).MarkMaintenancePlanDone(ctx, p))
}

func (r *PGRepository) Due(ctx context.Context, org uuid.NullUUID) ([]Due, error) {
	rows, err := r.q(ctx).ListDueMaintenance(ctx, org)
	out := make([]Due, 0, len(rows))
	for _, d := range rows {
		out = append(out, Due{PlanID: d.PlanID, Task: d.Task, EquipmentID: d.EquipmentID, Title: d.Title, CurrentHMR: d.CurrentHmr,
			EveryHours: d.EveryHours, EveryDays: d.EveryDays, LastDoneAt: d.LastDoneAt, LastDoneHMR: d.LastDoneHmr})
	}
	return out, database.MapError(err)
}

func (r *PGRepository) Costs(ctx context.Context, p db.MaintenanceCostSummaryParams) ([]CostRow, error) {
	rows, err := r.q(ctx).MaintenanceCostSummary(ctx, p)
	out := make([]CostRow, 0, len(rows))
	for _, x := range rows {
		out = append(out, CostRow{EquipmentID: x.EquipmentID, EquipmentTitle: x.EquipmentTitle, Kind: x.Kind, Month: x.Month,
			Jobs: x.Jobs, CostPaise: x.CostPaise, PartsCostPaise: x.PartsCostPaise, LabourCostPaise: x.LabourCostPaise,
			DowntimeHours: x.DowntimeHours})
	}
	return out, database.MapError(err)
}

// ---- service -----------------------------------------------------------------------

// Uploads attaches invoices and signs their links (the uploads module).
type Uploads interface {
	Claim(ctx context.Context, userID, uploadID uuid.UUID, purposes ...filecheck.Purpose) (db.Upload, error)
	Get(ctx context.Context, id uuid.UUID) (db.Upload, error)
	SignedURL(key string, ttl time.Duration) storage.SignedURL
}

type Service struct {
	repo    Repository
	tx      database.Transactor
	uploads Uploads
	urlTTL  time.Duration
	now     func() time.Time
}

func NewService(repo Repository, tx database.Transactor, up Uploads, urlTTL time.Duration) *Service {
	return &Service{repo: repo, tx: tx, uploads: up, urlTTL: urlTTL, now: time.Now}
}

func field(name, code, msg string) error {
	return apperror.ErrValidation.WithFields(apperror.FieldError{Field: name, Code: code, Message: msg})
}

func (s *Service) check(ctx context.Context, org, equipmentID uuid.UUID) error {
	owner, err := s.repo.Owner(ctx, equipmentID)
	if err != nil {
		return err
	}
	if owner != org {
		return apperror.ErrNotFound
	}
	return nil
}

func (s *Service) Plans(ctx context.Context, org, id uuid.UUID) (PlanList, error) {
	if err := s.check(ctx, org, id); err != nil {
		return PlanList{}, err
	}
	list, err := s.repo.Plans(ctx, id)
	return PlanList{Data: list}, err
}

func (s *Service) CreatePlan(ctx context.Context, org, id uuid.UUID, req CreatePlanRequest) (Plan, error) {
	if err := s.check(ctx, org, id); err != nil {
		return Plan{}, err
	}
	if req.EveryHours == nil && req.EveryDays == nil {
		return Plan{}, apperror.ErrValidation.WithDetail("set every_hours, every_days or both")
	}
	var lastAt *time.Time
	if req.LastDoneHMR != nil {
		now := s.now()
		lastAt = &now
	}
	return s.repo.CreatePlan(ctx, db.CreateMaintenancePlanParams{
		EquipmentID: id, Task: req.Task, EveryHours: req.EveryHours, EveryDays: req.EveryDays, LastDoneHmr: req.LastDoneHMR, LastDoneAt: lastAt,
	})
}

// UpdatePlan changes a plan's task or intervals, or (de)activates it.
func (s *Service) UpdatePlan(ctx context.Context, org, id, planID uuid.UUID, req UpdatePlanRequest) (Plan, error) {
	if err := s.check(ctx, org, id); err != nil {
		return Plan{}, err
	}
	cur, err := s.repo.Plan(ctx, id, planID)
	if err != nil {
		return Plan{}, err
	}
	p := db.UpdateMaintenancePlanParams{ID: planID, EquipmentID: id, Task: req.Task, IsActive: req.Active}
	hours, days := cur.EveryHours, cur.EveryDays
	if req.EveryHours != nil {
		if *req.EveryHours == 0 {
			p.ClearHours, hours = true, nil
		} else {
			p.EveryHours, hours = req.EveryHours, req.EveryHours
		}
	}
	if req.EveryDays != nil {
		if *req.EveryDays == 0 {
			p.ClearDays, days = true, nil
		} else {
			p.EveryDays, days = req.EveryDays, req.EveryDays
		}
	}
	if hours == nil && days == nil {
		return Plan{}, apperror.ErrValidation.WithDetail("a plan needs every_hours, every_days or both")
	}
	return s.repo.UpdatePlan(ctx, p)
}

// costs fills the total from parts + labour and checks the split.
func costs(total, parts, labour *int64) (*int64, error) {
	if total == nil && (parts != nil || labour != nil) {
		sum := int64(0)
		if parts != nil {
			sum += *parts
		}
		if labour != nil {
			sum += *labour
		}
		return &sum, nil
	}
	if total != nil && parts != nil && labour != nil && *parts+*labour > *total {
		return nil, field("cost_paise", "gte", "the total cannot be less than parts plus labour")
	}
	return total, nil
}

func (s *Service) validWindow(w *Window, status string) error {
	if w == nil {
		return nil
	}
	if !w.End.After(w.Start) {
		return field("downtime.end", "gtfield", "must be after the start")
	}
	if w.End.Sub(w.Start) > 180*24*time.Hour {
		return field("downtime", "max", "downtime can be at most 180 days")
	}
	if status != "done" && w.End.Before(s.now()) {
		return field("downtime.end", "future", "an open job's downtime must end in the future")
	}
	return nil
}

// block reserves the calendar for the downtime, reporting clashing bookings.
func (s *Service) block(ctx context.Context, equipmentID, recordID uuid.UUID, w Window, task string, by uuid.UUID) error {
	clash, err := s.repo.Conflicts(ctx, equipmentID, w)
	if err != nil {
		return err
	}
	if len(clash) > 0 {
		return apperror.ErrBookingOverlap.WithDetail("the machine is booked during this downtime (" + strings.Join(clash, ", ") +
			"); reschedule the job or the bookings first")
	}
	id, err := s.repo.CreateBlock(ctx, equipmentID, w, "Maintenance: "+task, by)
	if err != nil {
		if errors.Is(err, apperror.ErrConflict) {
			return apperror.ErrBookingOverlap.WithDetail("the downtime overlaps another block on the calendar")
		}
		return err
	}
	return s.repo.SetBlock(ctx, recordID, uuid.NullUUID{UUID: id, Valid: true})
}

func (s *Service) attachInvoice(ctx context.Context, userID uuid.UUID, id uuid.NullUUID) (uuid.NullUUID, error) {
	if !id.Valid {
		return id, nil
	}
	up, err := s.uploads.Claim(ctx, userID, id.UUID, filecheck.MaintenanceDocument)
	if err != nil {
		return uuid.NullUUID{}, err
	}
	return uuid.NullUUID{UUID: up.ID, Valid: true}, nil
}

func (s *Service) load(ctx context.Context, equipmentID, id uuid.UUID) (Record, error) {
	r, err := s.repo.Record(ctx, equipmentID, id)
	if err != nil {
		return Record{}, err
	}
	return toRecord(db.ListMaintenanceHistoryRow(r)), nil
}

func (s *Service) CreateRecord(ctx context.Context, sc httpx.Scope, id uuid.UUID, req CreateRecordRequest) (Record, error) {
	if err := s.check(ctx, sc.Org, id); err != nil {
		return Record{}, err
	}
	if req.Kind == "" {
		req.Kind = "service"
	}
	if req.PlanID.Valid {
		if _, err := s.repo.Plan(ctx, id, req.PlanID.UUID); errors.Is(err, apperror.ErrNotFound) {
			return Record{}, field("plan_id", "not_found", "no such plan for this machine")
		} else if err != nil {
			return Record{}, err
		}
	}
	total, err := costs(req.CostPaise, req.PartsCostPaise, req.LabourCostPaise)
	if err != nil {
		return Record{}, err
	}
	if err := s.validWindow(req.Downtime, req.Status); err != nil {
		return Record{}, err
	}
	p := db.CreateMaintenanceRecordV2Params{
		EquipmentID: id, PlanID: req.PlanID, Kind: db.MaintenanceKind(req.Kind), Task: strings.TrimSpace(req.Task),
		Status: db.MaintenanceStatus(req.Status), HmrAt: req.HMRAt, CostPaise: total, PartsCostPaise: req.PartsCostPaise,
		LabourCostPaise: req.LabourCostPaise, Vendor: req.Vendor, Notes: req.Notes, RecordedBy: uuid.NullUUID{UUID: sc.P.UserID, Valid: true},
	}
	if req.ScheduledFor != nil {
		d, _ := time.Parse(time.DateOnly, *req.ScheduledFor)
		p.ScheduledFor = &d
	}
	if req.Downtime != nil {
		p.DowntimeStart, p.DowntimeEnd = &req.Downtime.Start, &req.Downtime.End
	}
	switch req.Status {
	case "scheduled":
		if p.ScheduledFor == nil && req.Downtime != nil { // the day the downtime starts (IST)
			day, _ := time.Parse(time.DateOnly, req.Downtime.Start.In(ist).Format(time.DateOnly))
			p.ScheduledFor = &day
		}
		if p.ScheduledFor == nil {
			return Record{}, field("scheduled_for", "required", "scheduled jobs need a date")
		}
	case "done":
		at := s.now()
		if req.PerformedAt != nil {
			if req.PerformedAt.After(at.Add(10 * time.Minute)) {
				return Record{}, field("performed_at", "future", "cannot be in the future")
			}
			at = *req.PerformedAt
		}
		p.PerformedAt = &at
	}
	block := req.Downtime != nil && req.Status != "done" && (req.BlockCalendar == nil || *req.BlockCalendar)
	var recID uuid.UUID
	err = s.tx.InTxAs(ctx, database.Actor{UserID: sc.P.UserID, Role: "owner", Reason: "maintenance " + req.Kind}, func(ctx context.Context) error {
		if p.InvoiceUploadID, err = s.attachInvoice(ctx, sc.P.UserID, req.InvoiceUploadID); err != nil {
			return err
		}
		if recID, err = s.repo.CreateRecord(ctx, p); err != nil {
			return err
		}
		if block {
			if err := s.block(ctx, id, recID, *req.Downtime, p.Task, sc.P.UserID); err != nil {
				return err
			}
		}
		if req.Status == "done" && req.PlanID.Valid {
			return s.repo.PlanDone(ctx, db.MarkMaintenancePlanDoneParams{ID: req.PlanID.UUID, DoneAt: p.PerformedAt, Hmr: req.HMRAt})
		}
		return nil
	})
	if err != nil {
		return Record{}, err
	}
	return s.load(ctx, id, recID)
}

var ist = time.FixedZone("IST", 5*3600+1800)

func (s *Service) UpdateRecord(ctx context.Context, sc httpx.Scope, id, recordID uuid.UUID, req UpdateRecordRequest) (Record, error) {
	if err := s.check(ctx, sc.Org, id); err != nil {
		return Record{}, err
	}
	cur, err := s.repo.Record(ctx, id, recordID)
	if err != nil {
		return Record{}, err
	}
	if cur.Status == db.MaintenanceStatusDone || cur.Status == db.MaintenanceStatusCancelled {
		return Record{}, apperror.ErrInvalidTransition.WithDetail("the job is " + string(cur.Status) + " and can no longer change")
	}
	if req.Status == "scheduled" && cur.Status == db.MaintenanceStatusInProgress {
		return Record{}, apperror.ErrInvalidTransition.WithDetail("a job in progress cannot go back to scheduled")
	}
	parts, labour := req.PartsCostPaise, req.LabourCostPaise
	if parts == nil {
		parts = cur.PartsCostPaise
	}
	if labour == nil {
		labour = cur.LabourCostPaise
	}
	total := req.CostPaise
	if total == nil && (req.PartsCostPaise != nil || req.LabourCostPaise != nil) {
		total, _ = costs(nil, parts, labour)
	}
	effective := total
	if effective == nil {
		effective = cur.CostPaise
	}
	if _, err := costs(effective, parts, labour); err != nil {
		return Record{}, err
	}
	if err := s.validWindow(req.Downtime, req.Status); err != nil {
		return Record{}, err
	}
	now := s.now()
	p := db.UpdateMaintenanceRecordV2Params{
		ID: recordID, EquipmentID: id, Status: db.MaintenanceStatus(req.Status), HmrAt: req.HMRAt, CostPaise: total,
		PartsCostPaise: req.PartsCostPaise, LabourCostPaise: req.LabourCostPaise, Vendor: req.Vendor, Notes: req.Notes,
		ClearDowntime: req.ClearDowntime,
	}
	if req.ScheduledFor != nil {
		d, _ := time.Parse(time.DateOnly, *req.ScheduledFor)
		p.ScheduledFor = &d
	}
	if req.Downtime != nil && !req.ClearDowntime {
		p.DowntimeStart, p.DowntimeEnd = &req.Downtime.Start, &req.Downtime.End
	}
	oldWindow := window(cur.Downtime)
	// Finishing early: the actual downtime ends now.
	if req.Status == "done" && req.Downtime == nil && !req.ClearDowntime && oldWindow != nil && now.Before(oldWindow.End) && now.After(oldWindow.Start) {
		end := now
		p.DowntimeStart, p.DowntimeEnd = &oldWindow.Start, &end
	}
	// Finished before the window even started: the machine was never down.
	if req.Status == "done" && req.Downtime == nil && oldWindow != nil && !now.After(oldWindow.Start) {
		p.ClearDowntime = true
	}
	moved := req.ClearDowntime || req.Downtime != nil
	err = s.tx.InTxAs(ctx, database.Actor{UserID: sc.P.UserID, Role: "owner", Reason: "maintenance " + req.Status}, func(ctx context.Context) error {
		if p.InvoiceUploadID, err = s.attachInvoice(ctx, sc.P.UserID, req.InvoiceUploadID); err != nil {
			return err
		}
		if _, err := s.repo.UpdateRecord(ctx, p); err != nil {
			if errors.Is(err, apperror.ErrNotFound) {
				return apperror.ErrInvalidTransition.WithDetail("the job is closed or does not exist")
			}
			return err
		}
		blockID := cur.AvailabilityBlockID
		switch {
		case req.Status == "cancelled" || moved:
			if blockID.Valid {
				if err := s.repo.DeleteBlock(ctx, blockID.UUID); err != nil {
					return err
				}
				if err := s.repo.SetBlock(ctx, recordID, uuid.NullUUID{}); err != nil {
					return err
				}
			}
			if req.Status != "cancelled" && req.Status != "done" && req.Downtime != nil && !req.ClearDowntime {
				if err := s.block(ctx, id, recordID, *req.Downtime, cur.Task, sc.P.UserID); err != nil {
					return err
				}
			}
		case req.Status == "done" && blockID.Valid:
			// Free whatever part of the blocked window lies ahead.
			if err := s.repo.TrimBlock(ctx, blockID.UUID, now); err != nil {
				return err
			}
		}
		if req.Status == "done" && cur.PlanID.Valid {
			hmr := req.HMRAt
			if hmr == nil {
				hmr = cur.HmrAt
			}
			return s.repo.PlanDone(ctx, db.MarkMaintenancePlanDoneParams{ID: cur.PlanID.UUID, DoneAt: &now, Hmr: hmr})
		}
		return nil
	})
	if err != nil {
		return Record{}, err
	}
	return s.load(ctx, id, recordID)
}

// Records is one machine's history.
func (s *Service) Records(ctx context.Context, org, id uuid.UUID) (RecordList, error) {
	if err := s.check(ctx, org, id); err != nil {
		return RecordList{}, err
	}
	list, err := s.repo.History(ctx, db.ListMaintenanceHistoryParams{OwnerOrgID: org, EquipmentID: uuid.NullUUID{UUID: id, Valid: true}, ResultLimit: 200})
	return RecordList{Data: list}, err
}

// History pages the fleet's maintenance records; only is the machines a
// fleet manager may see (nil = all).
func (s *Service) History(ctx context.Context, org uuid.UUID, only []uuid.UUID, q HistoryQuery) (RecordPage, error) {
	k, err := httpx.PageQuery{Limit: q.Limit, Cursor: q.Cursor}.Keyset()
	if err != nil {
		return RecordPage{}, err
	}
	p := db.ListMaintenanceHistoryParams{OwnerOrgID: org, EquipmentIds: only, After: k.After, ResultLimit: k.Fetch()}
	if q.EquipmentID != "" {
		p.EquipmentID = uuid.NullUUID{UUID: uuid.MustParse(q.EquipmentID), Valid: true}
	}
	if q.Kind != "" {
		p.Kind = &q.Kind
	}
	if q.Status != "" {
		p.Status = &q.Status
	}
	rows, err := s.repo.History(ctx, p)
	if err != nil {
		return RecordPage{}, err
	}
	pg := httpx.Paginate(rows, k, func(r Record) uuid.UUID { return r.ID })
	return RecordPage{Data: pg.Data, NextCursor: pg.NextCursor}, nil
}

// Costs reports completed-work spend and downtime by machine, kind and month.
func (s *Service) Costs(ctx context.Context, org uuid.UUID, only []uuid.UUID, q CostQuery) (CostReport, error) {
	now := s.now().In(ist)
	to, _ := time.ParseInLocation(time.DateOnly, now.Format(time.DateOnly), ist)
	from := to.AddDate(-1, 0, 1)
	if q.To != "" {
		to, _ = time.ParseInLocation(time.DateOnly, q.To, ist)
	}
	if q.From != "" {
		from, _ = time.ParseInLocation(time.DateOnly, q.From, ist)
	}
	if from.After(to) {
		return CostReport{}, field("from", "lte", "must be on or before to")
	}
	if to.Sub(from) > 3*366*24*time.Hour {
		return CostReport{}, field("from", "range", "reports cover at most 3 years")
	}
	rows, err := s.repo.Costs(ctx, db.MaintenanceCostSummaryParams{OwnerOrgID: org, FromAt: from, ToAt: to.AddDate(0, 0, 1), EquipmentIds: only})
	if err != nil {
		return CostReport{}, err
	}
	return summarise(from.Format(time.DateOnly), to.Format(time.DateOnly), rows), nil
}

func summarise(from, to string, rows []CostRow) CostReport {
	out := CostReport{From: from, To: to, Rows: rows, ByEquipment: []Total{}, ByKind: []Total{}, ByMonth: []Total{}}
	add := func(list *[]Total, key, label string, r CostRow) {
		for i := range *list {
			if (*list)[i].Key == key {
				(*list)[i].Jobs += r.Jobs
				(*list)[i].CostPaise += r.CostPaise
				(*list)[i].DowntimeHours += r.DowntimeHours
				return
			}
		}
		*list = append(*list, Total{Key: key, Label: label, Jobs: r.Jobs, CostPaise: r.CostPaise, DowntimeHours: r.DowntimeHours})
	}
	for _, r := range rows {
		out.TotalPaise += r.CostPaise
		out.Jobs += r.Jobs
		out.DowntimeHours += r.DowntimeHours
		add(&out.ByEquipment, r.EquipmentID.String(), r.EquipmentTitle, r)
		add(&out.ByKind, r.Kind, strings.ToUpper(r.Kind[:1])+r.Kind[1:], r)
		add(&out.ByMonth, r.Month, r.Month, r)
	}
	slices.SortFunc(out.ByEquipment, func(a, b Total) int { return int(b.CostPaise - a.CostPaise) })
	return out
}

func (s *Service) Due(ctx context.Context, org uuid.UUID) (DueList, error) {
	list, err := s.repo.Due(ctx, uuid.NullUUID{UUID: org, Valid: true})
	return DueList{Data: list}, err
}

// InvoiceLink issues a short-lived link to a job's invoice.
func (s *Service) InvoiceLink(ctx context.Context, org, id, recordID uuid.UUID) (storage.SignedURL, error) {
	if err := s.check(ctx, org, id); err != nil {
		return storage.SignedURL{}, err
	}
	r, err := s.repo.Record(ctx, id, recordID)
	if err != nil {
		return storage.SignedURL{}, err
	}
	if !r.InvoiceUploadID.Valid {
		return storage.SignedURL{}, apperror.ErrNotFound.WithDetail("no invoice is attached to this job")
	}
	up, err := s.uploads.Get(ctx, r.InvoiceUploadID.UUID)
	if err != nil {
		return storage.SignedURL{}, err
	}
	return s.uploads.SignedURL(up.ObjectKey, s.urlTTL), nil
}

// ReminderText words a reminder for a scheduled job.
func ReminderText(title, task string, day time.Time, today time.Time) string {
	days := int(day.Sub(today).Hours() / 24)
	when := fmt.Sprintf("on %s", day.Format("Mon 02 Jan"))
	switch days {
	case 0:
		when = "today"
	case 1:
		when = "tomorrow"
	}
	return fmt.Sprintf("%s: %s is scheduled %s.", title, task, when)
}

// ---- handler -----------------------------------------------------------------------

type Handler struct {
	svc    *Service
	access access.Equipment
}

// NewHandler builds the handler; per-machine routes are guarded by the
// router, acc scopes the fleet-wide lists.
func NewHandler(svc *Service, acc access.Equipment) *Handler { return &Handler{svc: svc, access: acc} }

func (h *Handler) Name() string { return "maintenance" }

var roles = []string{httpx.RoleOwner, httpx.RoleFleetManager}

func (h *Handler) Routes(r *httpx.Router) {
	a := r.Tag("maintenance").Authed()
	doc := func(d httpx.Doc) httpx.Doc { d.OrgScoped = true; return d }
	a.GET("/owner/equipment/:id/maintenance/plans", httpx.OrgHandle(200, roles, func(c *gin.Context, s httpx.Scope) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Plans(c.Request.Context(), s.Org, id)
	}), doc(httpx.Doc{Summary: "Maintenance plans of a machine", Response: PlanList{}}))
	a.POST("/owner/equipment/:id/maintenance/plans", httpx.OrgHandle(201, roles, func(c *gin.Context, s httpx.Scope) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		var req CreatePlanRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.CreatePlan(c.Request.Context(), s.Org, id, req)
	}), doc(httpx.Doc{Summary: "Add a maintenance plan", Request: CreatePlanRequest{}, Response: Plan{}, Status: 201}))
	a.PATCH("/owner/equipment/:id/maintenance/plans/:plan_id", httpx.OrgHandle(200, roles, func(c *gin.Context, s httpx.Scope) (any, error) {
		ids, err := httpx.Params(c, "id", "plan_id")
		if err != nil {
			return nil, err
		}
		var req UpdatePlanRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.UpdatePlan(c.Request.Context(), s.Org, ids[0], ids[1], req)
	}), doc(httpx.Doc{Summary: "Change or pause a maintenance plan", Request: UpdatePlanRequest{}, Response: Plan{}}))
	a.GET("/owner/equipment/:id/maintenance/records", httpx.OrgHandle(200, roles, func(c *gin.Context, s httpx.Scope) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Records(c.Request.Context(), s.Org, id)
	}), doc(httpx.Doc{Summary: "Service history of a machine", Response: RecordList{}}))
	a.POST("/owner/equipment/:id/maintenance/records", httpx.OrgHandle(201, roles, func(c *gin.Context, s httpx.Scope) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		var req CreateRecordRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.CreateRecord(c.Request.Context(), s, id, req)
	}), doc(httpx.Doc{Summary: "Schedule maintenance or log a service, repair, inspection or breakdown", Request: CreateRecordRequest{}, Response: Record{}, Status: 201,
		Description: "A downtime window on a scheduled or in-progress job blocks the booking calendar; clashing bookings give 409 BOOKING_OVERLAP."}))
	a.PATCH("/owner/equipment/:id/maintenance/records/:record_id", httpx.OrgHandle(200, roles, func(c *gin.Context, s httpx.Scope) (any, error) {
		ids, err := httpx.Params(c, "id", "record_id")
		if err != nil {
			return nil, err
		}
		var req UpdateRecordRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.UpdateRecord(c.Request.Context(), s, ids[0], ids[1], req)
	}), doc(httpx.Doc{Summary: "Reschedule, start, complete or cancel a maintenance job", Request: UpdateRecordRequest{}, Response: Record{},
		Description: "Completing early frees the rest of the blocked window; cancelling frees all of it."}))
	a.GET("/owner/equipment/:id/maintenance/records/:record_id/invoice", httpx.OrgHandle(200, roles, func(c *gin.Context, s httpx.Scope) (any, error) {
		ids, err := httpx.Params(c, "id", "record_id")
		if err != nil {
			return nil, err
		}
		return h.svc.InvoiceLink(c.Request.Context(), s.Org, ids[0], ids[1])
	}), doc(httpx.Doc{Summary: "Download link for a job's invoice", Response: storage.SignedURL{}}))
	a.GET("/owner/maintenance/records", httpx.OrgHandle(200, roles, func(c *gin.Context, s httpx.Scope) (any, error) {
		var q HistoryQuery
		if err := c.ShouldBindQuery(&q); err != nil {
			return nil, httpx.BindError(err)
		}
		only, err := access.Filter(c.Request.Context(), h.access, s.P, s.Org)
		if err != nil {
			return nil, err
		}
		return h.svc.History(c.Request.Context(), s.Org, only, q)
	}), doc(httpx.Doc{Summary: "Maintenance history across the fleet", Query: HistoryQuery{}, Response: RecordPage{}}))
	a.GET("/owner/maintenance/costs", httpx.OrgHandle(200, roles, func(c *gin.Context, s httpx.Scope) (any, error) {
		var q CostQuery
		if err := c.ShouldBindQuery(&q); err != nil {
			return nil, httpx.BindError(err)
		}
		only, err := access.Filter(c.Request.Context(), h.access, s.P, s.Org)
		if err != nil {
			return nil, err
		}
		return h.svc.Costs(c.Request.Context(), s.Org, only, q)
	}), doc(httpx.Doc{Summary: "Maintenance spend and downtime by machine, kind and month", Query: CostQuery{}, Response: CostReport{}}))
	a.GET("/owner/maintenance/due", httpx.OrgHandle(200, roles, func(c *gin.Context, s httpx.Scope) (any, error) {
		due, err := h.svc.Due(c.Request.Context(), s.Org)
		if err != nil {
			return nil, err
		}
		only, err := access.Filter(c.Request.Context(), h.access, s.P, s.Org)
		if err != nil || only == nil {
			return due, err
		}
		kept := make([]Due, 0, len(due.Data))
		for _, d := range due.Data {
			if slices.Contains(only, d.EquipmentID) {
				kept = append(kept, d)
			}
		}
		return DueList{Data: kept}, nil
	}), doc(httpx.Doc{Summary: "Maintenance plans due across the fleet", Response: DueList{}}))
}
