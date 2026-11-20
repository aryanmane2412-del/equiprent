// Package fleet gives owners and fleet managers an operational view of
// their machines: named fleets (groups of machines with their managers),
// KPIs, an equipment status board, a schedule, operator roster, live
// positions and utilisation with earnings.
//
// Every figure is limited to the machines the caller may manage: owners see
// the whole organisation, a fleet manager only the machines of their fleets
// and direct grants (internal/access, view fleet_manager_scope).
package fleet

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
	"github.com/equiprent/backend/internal/httpx"
)

// ---- domain --------------------------------------------------------------------------------

type Overview struct {
	EquipmentTotal  int32   `json:"equipment_total"`
	Listed          int32   `json:"listed"`
	OnJob           int32   `json:"on_job"`
	Idle            int32   `json:"idle"`
	UtilisationPct  float64 `json:"utilisation_pct" doc:"Machines on a job / listed machines"`
	ActiveBookings  int32   `json:"active_bookings"`
	Operators       int32   `json:"operators"`
	OperatorsOnDuty int32   `json:"operators_on_duty"`
	OpenMaintenance int32   `json:"open_maintenance"`
	Revenue30dPaise int64   `json:"revenue_30d_paise" doc:"Rental money captured in the last 30 days (deposits excluded)"`
	Scope           string  `json:"scope" doc:"organisation (owner / all machines) or assigned (a fleet manager's machines)"`
}

type Operator struct {
	UserID             uuid.UUID  `json:"user_id"`
	FullName           string     `json:"full_name"`
	Phone              string     `json:"phone"`
	OnDuty             bool       `json:"on_duty" doc:"The driver's own switch; never changed by assignments"`
	Availability       string     `json:"availability" doc:"available, assigned, on_job, off_duty or unavailable"`
	Verified           bool       `json:"verified"`
	AssignmentsBlocked bool       `json:"assignments_blocked" doc:"Restricted by EQUIPRENT: cannot take new jobs"`
	Active             bool       `json:"active"`
	RatingAvg          *float64   `json:"rating_avg,omitempty"`
	LicenceExpiresOn   *time.Time `json:"licence_expires_on,omitempty"`
	CurrentBooking     string     `json:"current_booking_reference,omitempty"`
}

type LivePosition struct {
	EquipmentID uuid.UUID `json:"equipment_id"`
	Title       string    `json:"title"`
	Lat         float64   `json:"lat"`
	Lng         float64   `json:"lng"`
	RecordedAt  time.Time `json:"recorded_at"`
}

type UtilisationRow struct {
	EquipmentID    uuid.UUID `json:"equipment_id"`
	Title          string    `json:"title"`
	BookedHours    float64   `json:"booked_hours"`
	Bookings       int32     `json:"bookings"`
	UtilisationPct float64   `json:"utilisation_pct"`
	EarningsPaise  int64     `json:"earnings_paise" doc:"Rental money captured in the window, net of refunds (deposits excluded)"`
}

type ReportQuery struct {
	From time.Time `form:"from" binding:"required" time_format:"2006-01-02T15:04:05Z07:00"`
	To   time.Time `form:"to" binding:"required,gtfield=From" time_format:"2006-01-02T15:04:05Z07:00"`
}

type OperatorList struct {
	Data []Operator `json:"data"`
}
type LiveList struct {
	Data []LivePosition `json:"data"`
}
type UtilisationReport struct {
	From          time.Time        `json:"from"`
	To            time.Time        `json:"to"`
	EarningsPaise int64            `json:"earnings_paise"`
	BookedHours   float64          `json:"booked_hours"`
	Data          []UtilisationRow `json:"data"`
}

// MachineStatus is one row of the equipment status board.
type MachineStatus struct {
	EquipmentID     uuid.UUID  `json:"equipment_id"`
	Title           string     `json:"title"`
	RegistrationNo  string     `json:"registration_no,omitempty"`
	ListingStatus   string     `json:"listing_status"`
	Condition       string     `json:"condition"`
	HourMeter       float64    `json:"hour_meter"`
	FleetID         *uuid.UUID `json:"fleet_id,omitempty"`
	FleetName       string     `json:"fleet_name,omitempty"`
	State           string     `json:"state" doc:"on_job, reserved, blocked, maintenance, idle or unlisted"`
	CurrentBooking  *JobRef    `json:"current_booking,omitempty"`
	NextBooking     *JobRef    `json:"next_booking,omitempty"`
	OpenMaintenance int32      `json:"open_maintenance"`
	LastPosition    *Position  `json:"last_position,omitempty"`
}

type JobRef struct {
	ID        string     `json:"id"`
	Reference string     `json:"reference"`
	Status    string     `json:"status,omitempty"`
	Operator  string     `json:"operator,omitempty"`
	Start     *time.Time `json:"start,omitempty"`
}

type Position struct {
	Lat        float64    `json:"lat"`
	Lng        float64    `json:"lng"`
	RecordedAt *time.Time `json:"recorded_at,omitempty"`
}

type EquipmentBoard struct {
	Data []MachineStatus `json:"data"`
}

type BoardQuery struct {
	FleetID string `form:"fleet_id" binding:"omitempty,uuid"`
}

// ScheduleItem is a booking or an availability block on a machine.
type ScheduleItem struct {
	Kind           string    `json:"kind" doc:"booking or block"`
	EquipmentID    uuid.UUID `json:"equipment_id"`
	EquipmentTitle string    `json:"equipment_title"`
	ItemID         uuid.UUID `json:"item_id"`
	Label          string    `json:"label"`
	Status         string    `json:"status"`
	Start          time.Time `json:"start"`
	End            time.Time `json:"end"`
	Operator       string    `json:"operator,omitempty"`
	NeedsOperator  bool      `json:"needs_operator" doc:"Wet hire with no operator assigned yet"`
}

type Schedule struct {
	From time.Time      `json:"from"`
	To   time.Time      `json:"to"`
	Data []ScheduleItem `json:"data"`
}

// Fleet is a named group of an owner's machines with its managers.
type Fleet struct {
	ID             uuid.UUID `json:"id"`
	OrgID          uuid.UUID `json:"organization_id"`
	OrgName        string    `json:"organization_name,omitempty"`
	Name           string    `json:"name"`
	Description    *string   `json:"description,omitempty"`
	Status         string    `json:"status" doc:"active or archived"`
	ArchivedReason *string   `json:"archived_reason,omitempty"`
	EquipmentCount int32     `json:"equipment_count"`
	Managers       []string  `json:"managers"`
	CreatedAt      time.Time `json:"created_at"`
	Members        []Member  `json:"members,omitempty"`
}

type Member struct {
	Kind   string    `json:"kind" doc:"equipment or manager"`
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Detail string    `json:"detail,omitempty"`
}

type FleetList struct {
	Data []Fleet `json:"data"`
}

type FleetRequest struct {
	Name        string  `json:"name" binding:"required,min=2,max=80"`
	Description *string `json:"description" binding:"omitempty,max=500"`
}

type EquipmentIDsRequest struct {
	EquipmentIDs []uuid.UUID `json:"equipment_ids" binding:"required,min=1,max=200" doc:"Machines move here from any other fleet"`
}

type ManagerRequest struct {
	UserID uuid.UUID `json:"user_id" binding:"required"`
}

type ArchiveRequest struct {
	Reason string `json:"reason" binding:"required,min=3,max=300"`
}

type AdminFleetQuery struct {
	OrgID  string `form:"org_id" binding:"omitempty,uuid"`
	Status string `form:"status" binding:"omitempty,oneof=active archived"`
}

// ---- repository ---------------------------------------------------------------------------------

type PGRepository struct {
	pool *pgxpool.Pool
	tx   database.Transactor
}

func NewRepository(pool *pgxpool.Pool, tx database.Transactor) *PGRepository {
	return &PGRepository{pool: pool, tx: tx}
}

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

// ---- service ----------------------------------------------------------------------------------

// Scope answers which machines a caller may see (internal/access).
type Scope interface {
	Scope(ctx context.Context, p *httpx.Principal, org uuid.UUID) (all bool, ids map[uuid.UUID]bool, err error)
}

type Service struct {
	repo  *PGRepository
	scope Scope
}

func NewService(repo *PGRepository, scope Scope) *Service { return &Service{repo: repo, scope: scope} }

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }

// machines is nil for "every machine of org", else the caller's machines
// (an empty, non-nil slice when they manage none).
func (s *Service) machines(ctx context.Context, p *httpx.Principal, org uuid.UUID) ([]uuid.UUID, error) {
	if p.HasOrgRole(org, httpx.RoleOwner) || p.HasPlatformRole(httpx.RoleAdminOps) || s.scope == nil {
		return nil, nil
	}
	all, ids, err := s.scope.Scope(ctx, p, org)
	if err != nil || all {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	return out, nil
}

func (s *Service) Overview(ctx context.Context, p *httpx.Principal, org uuid.UUID) (Overview, error) {
	ids, err := s.machines(ctx, p, org)
	if err != nil {
		return Overview{}, err
	}
	o, err := s.repo.q(ctx).FleetOverview(ctx, db.FleetOverviewParams{OrgID: org, EquipmentIds: ids})
	if err != nil {
		return Overview{}, database.MapError(err)
	}
	out := Overview{
		EquipmentTotal: o.EquipmentTotal, Listed: o.Listed, OnJob: o.OnJob, Idle: max(o.Listed-o.OnJob, 0),
		ActiveBookings: o.ActiveBookings, Operators: o.Operators, OperatorsOnDuty: o.OperatorsOnDuty,
		OpenMaintenance: o.OpenMaintenance, Revenue30dPaise: o.Revenue30dPaise, Scope: "organisation",
	}
	if ids != nil {
		out.Scope = "assigned"
	}
	if o.Listed > 0 {
		out.UtilisationPct = round1(100 * float64(o.OnJob) / float64(o.Listed))
	}
	return out, nil
}

func (s *Service) Operators(ctx context.Context, org uuid.UUID) (OperatorList, error) {
	rows, err := s.repo.q(ctx).FleetOperators(ctx, uuid.NullUUID{UUID: org, Valid: true})
	if err != nil {
		return OperatorList{}, database.MapError(err)
	}
	out := OperatorList{Data: make([]Operator, 0, len(rows))}
	for _, o := range rows {
		out.Data = append(out.Data, Operator{UserID: o.UserID, FullName: o.FullName, Phone: o.Phone, OnDuty: o.OnDuty, Availability: o.Availability, Verified: o.Verified,
			AssignmentsBlocked: o.AssignmentsBlocked, Active: o.Active, RatingAvg: o.RatingAvg, LicenceExpiresOn: o.LicenceExpiresOn,
			CurrentBooking: o.CurrentBookingReference})
	}
	return out, nil
}

func (s *Service) Live(ctx context.Context, p *httpx.Principal, org uuid.UUID) (LiveList, error) {
	ids, err := s.machines(ctx, p, org)
	if err != nil {
		return LiveList{}, err
	}
	rows, err := s.repo.q(ctx).FleetLivePositions(ctx, db.FleetLivePositionsParams{OrgID: org, EquipmentIds: ids})
	if err != nil {
		return LiveList{}, database.MapError(err)
	}
	out := LiveList{Data: make([]LivePosition, 0, len(rows))}
	for _, r := range rows {
		out.Data = append(out.Data, LivePosition(r))
	}
	return out, nil
}

func (s *Service) Utilisation(ctx context.Context, p *httpx.Principal, org uuid.UUID, q ReportQuery) (UtilisationReport, error) {
	if q.To.Sub(q.From) > 366*24*time.Hour {
		return UtilisationReport{}, apperror.ErrValidation.WithDetail("reports cover at most one year")
	}
	ids, err := s.machines(ctx, p, org)
	if err != nil {
		return UtilisationReport{}, err
	}
	from, to := q.From.UTC(), q.To.UTC()
	rows, err := s.repo.q(ctx).FleetUtilisation(ctx, db.FleetUtilisationParams{OrgID: org, FromAt: from, ToAt: to, EquipmentIds: ids})
	if err != nil {
		return UtilisationReport{}, database.MapError(err)
	}
	window := to.Sub(from).Hours()
	out := UtilisationReport{From: from, To: to, Data: make([]UtilisationRow, 0, len(rows))}
	for _, u := range rows {
		out.Data = append(out.Data, UtilisationRow{EquipmentID: u.EquipmentID, Title: u.Title, BookedHours: round1(u.BookedHours),
			Bookings: u.Bookings, UtilisationPct: round1(100 * u.BookedHours / window), EarningsPaise: u.EarningsPaise})
		out.EarningsPaise += u.EarningsPaise
		out.BookedHours += u.BookedHours
	}
	out.BookedHours = round1(out.BookedHours)
	return out, nil
}

func (s *Service) Board(ctx context.Context, p *httpx.Principal, org uuid.UUID, q BoardQuery) (EquipmentBoard, error) {
	ids, err := s.machines(ctx, p, org)
	if err != nil {
		return EquipmentBoard{}, err
	}
	params := db.FleetEquipmentBoardParams{OrgID: org, EquipmentIds: ids}
	if q.FleetID != "" {
		params.FleetID = uuid.NullUUID{UUID: uuid.MustParse(q.FleetID), Valid: true}
	}
	rows, err := s.repo.q(ctx).FleetEquipmentBoard(ctx, params)
	if err != nil {
		return EquipmentBoard{}, database.MapError(err)
	}
	out := EquipmentBoard{Data: make([]MachineStatus, 0, len(rows))}
	for _, r := range rows {
		m := MachineStatus{EquipmentID: r.ID, Title: r.Title, ListingStatus: r.ListingStatus, Condition: r.Condition,
			HourMeter: r.CurrentHmr, FleetName: r.FleetName, OpenMaintenance: r.OpenMaintenance}
		if r.RegistrationNo != nil {
			m.RegistrationNo = *r.RegistrationNo
		}
		if r.FleetID.Valid {
			id := r.FleetID.UUID
			m.FleetID = &id
		}
		if r.CurrentBookingID != "" {
			m.CurrentBooking = &JobRef{ID: r.CurrentBookingID, Reference: r.CurrentReference, Status: r.CurrentStatus, Operator: r.CurrentOperator}
		}
		if r.NextBookingID != "" {
			j := &JobRef{ID: r.NextBookingID, Reference: r.NextReference}
			if t, err := time.Parse(time.RFC3339, r.NextStart); err == nil {
				j.Start = &t
			}
			m.NextBooking = j
		}
		if r.HasLocation {
			m.LastPosition = &Position{Lat: r.LastLat, Lng: r.LastLng, RecordedAt: r.LastSeenAt}
		}
		m.State = machineState(r)
		out.Data = append(out.Data, m)
	}
	return out, nil
}

// machineState summarises what a machine is doing now.
func machineState(r db.FleetEquipmentBoardRow) string {
	switch {
	case r.CurrentStatus != "" && r.CurrentStatus != "operator_assigned":
		return "on_job"
	case r.CurrentStatus == "operator_assigned":
		return "reserved"
	case r.OpenMaintenance > 0:
		return "maintenance"
	case r.BlockedNow:
		return "blocked"
	case r.ListingStatus != "active":
		return "unlisted"
	}
	return "idle"
}

func (s *Service) Schedule(ctx context.Context, p *httpx.Principal, org uuid.UUID, q ReportQuery) (Schedule, error) {
	if q.To.Sub(q.From) > 93*24*time.Hour {
		return Schedule{}, apperror.ErrValidation.WithDetail("a schedule covers at most three months")
	}
	ids, err := s.machines(ctx, p, org)
	if err != nil {
		return Schedule{}, err
	}
	from, to := q.From.UTC(), q.To.UTC()
	rows, err := s.repo.q(ctx).FleetSchedule(ctx, db.FleetScheduleParams{OrgID: org, FromAt: from, ToAt: to, EquipmentIds: ids})
	if err != nil {
		return Schedule{}, database.MapError(err)
	}
	out := Schedule{From: from, To: to, Data: make([]ScheduleItem, 0, len(rows))}
	for _, r := range rows {
		out.Data = append(out.Data, ScheduleItem{Kind: r.Kind, EquipmentID: r.EquipmentID, EquipmentTitle: r.EquipmentTitle, ItemID: r.ItemID,
			Label: r.Label, Status: r.Status, Start: r.StartsAt, End: r.EndsAt, Operator: r.OperatorName,
			NeedsOperator: r.Kind == "booking" && r.WithOperator && r.OperatorName == "" && r.Status == "confirmed"})
	}
	return out, nil
}

// ---- fleets --------------------------------------------------------------------------------------

func toFleet(r db.ListFleetsRow) Fleet {
	names := r.ManagerNames
	if names == nil {
		names = []string{}
	}
	return Fleet{ID: r.ID, OrgID: r.OwnerOrgID, OrgName: r.OrganizationName, Name: r.Name, Description: r.Description, Status: r.Status,
		ArchivedReason: r.ArchivedReason, EquipmentCount: r.EquipmentCount, Managers: names, CreatedAt: r.CreatedAt}
}

// Fleets lists an organisation's fleets: all of them for owners, the ones
// they manage for fleet managers; admins pass org (or uuid.Nil for all).
func (s *Service) Fleets(ctx context.Context, p *httpx.Principal, org uuid.UUID, status string) (FleetList, error) {
	params := db.ListFleetsParams{Lim: 500}
	if org != uuid.Nil {
		params.OrgID = uuid.NullUUID{UUID: org, Valid: true}
	}
	if status != "" {
		params.Status = &status
	}
	if org != uuid.Nil && !p.HasOrgRole(org, httpx.RoleOwner) && !p.IsAdmin() {
		params.ManagerID = uuid.NullUUID{UUID: p.UserID, Valid: true}
	}
	rows, err := s.repo.q(ctx).ListFleets(ctx, params)
	if err != nil {
		return FleetList{}, database.MapError(err)
	}
	out := FleetList{Data: make([]Fleet, 0, len(rows))}
	for _, r := range rows {
		out.Data = append(out.Data, toFleet(r))
	}
	return out, nil
}

// Fleet returns one fleet with its machines and managers.
func (s *Service) Fleet(ctx context.Context, p *httpx.Principal, org, id uuid.UUID) (Fleet, error) {
	list, err := s.Fleets(ctx, p, org, "")
	if err != nil {
		return Fleet{}, err
	}
	for _, f := range list.Data {
		if f.ID == id {
			rows, err := s.repo.q(ctx).ListFleetMembers(ctx, id)
			if err != nil {
				return Fleet{}, database.MapError(err)
			}
			f.Members = make([]Member, 0, len(rows))
			for _, m := range rows {
				f.Members = append(f.Members, Member{Kind: m.Kind, ID: m.MemberID, Name: m.Name, Detail: m.Detail})
			}
			return f, nil
		}
	}
	return Fleet{}, apperror.ErrNotFound
}

// ownFleet loads an active fleet of org that p may change: owners any,
// fleet managers only the fleets they manage.
func (s *Service) ownFleet(ctx context.Context, p *httpx.Principal, org, id uuid.UUID) (db.Fleet, error) {
	f, err := s.repo.q(ctx).GetFleet(ctx, id)
	if err != nil {
		return db.Fleet{}, database.MapError(err)
	}
	if f.OwnerOrgID != org {
		return db.Fleet{}, apperror.ErrNotFound
	}
	if !p.HasOrgRole(org, httpx.RoleOwner) && !p.IsAdmin() {
		ok, err := s.repo.q(ctx).IsFleetManagerOf(ctx, db.IsFleetManagerOfParams{FleetID: id, UserID: p.UserID})
		if err != nil {
			return db.Fleet{}, database.MapError(err)
		}
		if !ok {
			return db.Fleet{}, apperror.ErrNotFound
		}
	}
	if f.Status != "active" {
		return db.Fleet{}, apperror.ErrConflict.WithDetail("the fleet is archived")
	}
	return f, nil
}

func (s *Service) CreateFleet(ctx context.Context, sc httpx.Scope, req FleetRequest) (Fleet, error) {
	var id uuid.UUID
	err := s.repo.tx.InTxAs(ctx, database.Actor{UserID: sc.P.UserID, Role: "owner", Reason: "fleet created"}, func(ctx context.Context) error {
		q := s.repo.q(ctx)
		f, err := q.CreateFleet(ctx, db.CreateFleetParams{OwnerOrgID: sc.Org, Name: strings.TrimSpace(req.Name),
			Description: req.Description, CreatedBy: uuid.NullUUID{UUID: sc.P.UserID, Valid: true}})
		if err != nil {
			return database.MapError(err)
		}
		id = f.ID
		if sc.P.HasOrgRole(sc.Org, httpx.RoleOwner) {
			return nil
		}
		if err := q.EnsureFleetManagerProfile(ctx, db.EnsureFleetManagerProfileParams{UserID: sc.P.UserID, OrganizationID: sc.Org}); err != nil {
			return database.MapError(err)
		}
		_, err = q.AddFleetManager(ctx, db.AddFleetManagerParams{FleetID: id, UserID: sc.P.UserID, AddedBy: uuid.NullUUID{UUID: sc.P.UserID, Valid: true}})
		return database.MapError(err)
	})
	if errors.Is(err, apperror.ErrConflict) {
		return Fleet{}, apperror.ErrConflict.WithDetail("you already have a fleet with this name")
	}
	if err != nil {
		return Fleet{}, err
	}
	return s.Fleet(ctx, sc.P, sc.Org, id)
}

func (s *Service) UpdateFleet(ctx context.Context, sc httpx.Scope, id uuid.UUID, req FleetRequest) (Fleet, error) {
	err := s.repo.tx.InTxAs(ctx, database.Actor{UserID: sc.P.UserID, Role: "owner", Reason: "fleet renamed"}, func(ctx context.Context) error {
		_, err := s.repo.q(ctx).UpdateFleet(ctx, db.UpdateFleetParams{ID: id, OwnerOrgID: sc.Org, Name: strings.TrimSpace(req.Name), Description: req.Description})
		return database.MapError(err)
	})
	if err != nil {
		return Fleet{}, err
	}
	return s.Fleet(ctx, sc.P, sc.Org, id)
}

// ArchiveFleet ends a fleet: its machines and managers are released (the
// managers lose access to those machines unless granted another way).
func (s *Service) ArchiveFleet(ctx context.Context, p *httpx.Principal, org, id uuid.UUID, reason string) (Fleet, error) {
	err := s.repo.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: actorRole(p), Reason: "fleet archived: " + reason}, func(ctx context.Context) error {
		if org != uuid.Nil {
			if _, err := s.ownFleet(ctx, p, org, id); err != nil {
				return err
			}
		}
		_, err := s.repo.q(ctx).ArchiveFleet(ctx, db.ArchiveFleetParams{ID: id, Reason: &reason})
		return database.MapError(err)
	})
	if err != nil {
		return Fleet{}, err
	}
	f, err := s.repo.q(ctx).GetFleet(ctx, id)
	if err != nil {
		return Fleet{}, database.MapError(err)
	}
	list, err := s.Fleets(ctx, p, f.OwnerOrgID, "")
	if err != nil {
		return Fleet{}, err
	}
	for _, x := range list.Data {
		if x.ID == id {
			return x, nil
		}
	}
	return Fleet{}, apperror.ErrNotFound
}

func actorRole(p *httpx.Principal) string {
	if p.IsAdmin() {
		return "admin"
	}
	return "owner"
}

func (s *Service) AddEquipment(ctx context.Context, sc httpx.Scope, id uuid.UUID, req EquipmentIDsRequest) (Fleet, error) {
	err := s.repo.tx.InTxAs(ctx, database.Actor{UserID: sc.P.UserID, Role: "owner", Reason: "machines added to fleet"}, func(ctx context.Context) error {
		if _, err := s.ownFleet(ctx, sc.P, sc.Org, id); err != nil {
			return err
		}
		q := s.repo.q(ctx)
		if !sc.P.HasOrgRole(sc.Org, httpx.RoleOwner) {
			if err := s.checkManagerMachines(ctx, sc, req.EquipmentIDs); err != nil {
				return err
			}
		}
		if err := q.RemoveFromAnyFleet(ctx, req.EquipmentIDs); err != nil {
			return database.MapError(err)
		}
		n, err := q.AddFleetEquipment(ctx, db.AddFleetEquipmentParams{FleetID: id, EquipmentIds: req.EquipmentIDs, OwnerOrgID: sc.Org,
			AddedBy: uuid.NullUUID{UUID: sc.P.UserID, Valid: true}})
		if err != nil {
			return database.MapError(err)
		}
		if int(n) != len(dedupe(req.EquipmentIDs)) {
			return apperror.ErrValidation.WithFields(apperror.FieldError{Field: "equipment_ids", Code: "not_owned", Message: "every machine must be one of your organisation's machines"})
		}
		return nil
	})
	if err != nil {
		return Fleet{}, err
	}
	return s.Fleet(ctx, sc.P, sc.Org, id)
}

// checkManagerMachines: every machine must be one the fleet manager
// manages, and in no fleet or in one of their own fleets.
func (s *Service) checkManagerMachines(ctx context.Context, sc httpx.Scope, ids []uuid.UUID) error {
	all, scope, err := s.scope.Scope(ctx, sc.P, sc.Org)
	if err != nil {
		return err
	}
	q := s.repo.q(ctx)
	for _, id := range ids {
		if !all && !scope[id] {
			return apperror.ErrValidation.WithFields(apperror.FieldError{Field: "equipment_ids", Code: "not_in_scope", Message: "you can only add machines you manage"})
		}
	}
	rows, err := q.EquipmentFleets(ctx, ids)
	if err != nil {
		return database.MapError(err)
	}
	for _, r := range rows {
		mine, err := q.IsFleetManagerOf(ctx, db.IsFleetManagerOfParams{FleetID: r.FleetID, UserID: sc.P.UserID})
		if err != nil {
			return database.MapError(err)
		}
		if !mine {
			return apperror.ErrConflict.WithDetail("a machine is in another manager's fleet; ask the owner to move it")
		}
	}
	return nil
}

func dedupe(ids []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func (s *Service) RemoveEquipment(ctx context.Context, sc httpx.Scope, id, equipmentID uuid.UUID) (Fleet, error) {
	err := s.repo.tx.InTxAs(ctx, database.Actor{UserID: sc.P.UserID, Role: "owner", Reason: "machine removed from fleet"}, func(ctx context.Context) error {
		if _, err := s.ownFleet(ctx, sc.P, sc.Org, id); err != nil {
			return err
		}
		n, err := s.repo.q(ctx).RemoveFleetEquipment(ctx, db.RemoveFleetEquipmentParams{FleetID: id, EquipmentID: equipmentID})
		if err == nil && n == 0 {
			return apperror.ErrNotFound.WithDetail("the machine is not in this fleet")
		}
		return database.MapError(err)
	})
	if err != nil {
		return Fleet{}, err
	}
	return s.Fleet(ctx, sc.P, sc.Org, id)
}

func (s *Service) AddManager(ctx context.Context, sc httpx.Scope, id uuid.UUID, req ManagerRequest) (Fleet, error) {
	err := s.repo.tx.InTxAs(ctx, database.Actor{UserID: sc.P.UserID, Role: "owner", Reason: "fleet manager added"}, func(ctx context.Context) error {
		if _, err := s.ownFleet(ctx, sc.P, sc.Org, id); err != nil {
			return err
		}
		q := s.repo.q(ctx)
		ok, err := q.IsOrgFleetManager(ctx, db.IsOrgFleetManagerParams{UserID: req.UserID, OrganizationID: uuid.NullUUID{UUID: sc.Org, Valid: true}})
		if err != nil {
			return database.MapError(err)
		}
		if !ok {
			return apperror.ErrValidation.WithFields(apperror.FieldError{Field: "user_id", Code: "not_fleet_manager",
				Message: "add this person as a fleet manager of your organisation first"})
		}
		if err := q.EnsureFleetManagerProfile(ctx, db.EnsureFleetManagerProfileParams{UserID: req.UserID, OrganizationID: sc.Org}); err != nil {
			return database.MapError(err)
		}
		_, err = q.AddFleetManager(ctx, db.AddFleetManagerParams{FleetID: id, UserID: req.UserID, AddedBy: uuid.NullUUID{UUID: sc.P.UserID, Valid: true}})
		return database.MapError(err)
	})
	if err != nil {
		return Fleet{}, err
	}
	return s.Fleet(ctx, sc.P, sc.Org, id)
}

func (s *Service) RemoveManager(ctx context.Context, sc httpx.Scope, id, userID uuid.UUID) (Fleet, error) {
	err := s.repo.tx.InTxAs(ctx, database.Actor{UserID: sc.P.UserID, Role: "owner", Reason: "fleet manager removed"}, func(ctx context.Context) error {
		if _, err := s.ownFleet(ctx, sc.P, sc.Org, id); err != nil {
			return err
		}
		n, err := s.repo.q(ctx).RemoveFleetManager(ctx, db.RemoveFleetManagerParams{FleetID: id, UserID: userID})
		if err == nil && n == 0 {
			return apperror.ErrNotFound.WithDetail("this person does not manage the fleet")
		}
		return database.MapError(err)
	})
	if err != nil {
		return Fleet{}, err
	}
	return s.Fleet(ctx, sc.P, sc.Org, id)
}

// ---- handler -----------------------------------------------------------------------------------

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Name() string { return "fleet" }

// fleetOrg authorises the :org_id path organisation for owners and fleet managers.
func fleetOrg(c *gin.Context, p *httpx.Principal) (uuid.UUID, error) {
	org, err := httpx.Param(c, "org_id")
	if err != nil {
		return uuid.Nil, err
	}
	if !p.HasOrgRole(org, httpx.RoleOwner, httpx.RoleFleetManager) && !p.HasPlatformRole(httpx.RoleAdminOps) {
		if p.IsMember(org) {
			return uuid.Nil, apperror.ErrForbidden.WithDetail("fleet views need the owner or fleet manager role")
		}
		return uuid.Nil, apperror.ErrNotFound
	}
	return org, nil
}

func withOrg(fn func(c *gin.Context, p *httpx.Principal, org uuid.UUID) (any, error)) func(c *gin.Context, p *httpx.Principal) (any, error) {
	return func(c *gin.Context, p *httpx.Principal) (any, error) {
		org, err := fleetOrg(c, p)
		if err != nil {
			return nil, err
		}
		return fn(c, p, org)
	}
}

var (
	ownerOnly    = []string{httpx.RoleOwner}
	fleetEditors = []string{httpx.RoleOwner, httpx.RoleFleetManager}
)

func (h *Handler) Routes(r *httpx.Router) {
	a := r.Tag("fleet").Authed()
	a.GET("/fleet/:org_id/overview", httpx.Handle(200, withOrg(func(c *gin.Context, p *httpx.Principal, org uuid.UUID) (any, error) {
		return h.svc.Overview(c.Request.Context(), p, org)
	})), httpx.Doc{Summary: "Fleet KPIs (limited to the caller's machines)", Response: Overview{}})
	a.GET("/fleet/:org_id/operators", httpx.Handle(200, withOrg(func(c *gin.Context, _ *httpx.Principal, org uuid.UUID) (any, error) {
		return h.svc.Operators(c.Request.Context(), org)
	})), httpx.Doc{Summary: "Operator roster: duty, verification, restrictions and current job", Response: OperatorList{}})
	a.GET("/fleet/:org_id/live", httpx.Handle(200, withOrg(func(c *gin.Context, p *httpx.Principal, org uuid.UUID) (any, error) {
		return h.svc.Live(c.Request.Context(), p, org)
	})), httpx.Doc{Summary: "Last known positions of the caller's machines", Response: LiveList{},
		Description: "Fleet managers see only the machines of their fleets and direct grants."})
	a.GET("/fleet/:org_id/reports/utilisation", httpx.HandleQuery(200, func(c *gin.Context, p *httpx.Principal, q ReportQuery) (any, error) {
		org, err := fleetOrg(c, p)
		if err != nil {
			return nil, err
		}
		return h.svc.Utilisation(c.Request.Context(), p, org, q)
	}), httpx.Doc{Summary: "Booked hours and earnings per machine", Query: ReportQuery{}, Response: UtilisationReport{}})
	a.GET("/fleet/:org_id/equipment", httpx.HandleQuery(200, func(c *gin.Context, p *httpx.Principal, q BoardQuery) (any, error) {
		org, err := fleetOrg(c, p)
		if err != nil {
			return nil, err
		}
		return h.svc.Board(c.Request.Context(), p, org, q)
	}), httpx.Doc{Summary: "Equipment status board", Query: BoardQuery{}, Response: EquipmentBoard{},
		Description: "Each machine's state (on job, reserved, maintenance, blocked, idle, unlisted), current and next booking, open maintenance and last position."})
	a.GET("/fleet/:org_id/schedule", httpx.HandleQuery(200, func(c *gin.Context, p *httpx.Principal, q ReportQuery) (any, error) {
		org, err := fleetOrg(c, p)
		if err != nil {
			return nil, err
		}
		return h.svc.Schedule(c.Request.Context(), p, org, q)
	}), httpx.Doc{Summary: "Bookings and availability blocks on the caller's machines", Query: ReportQuery{}, Response: Schedule{}})
	a.GET("/fleet/:org_id/fleets", httpx.Handle(200, withOrg(func(c *gin.Context, p *httpx.Principal, org uuid.UUID) (any, error) {
		return h.svc.Fleets(c.Request.Context(), p, org, "active")
	})), httpx.Doc{Summary: "Fleets (all for owners, the ones you manage for fleet managers)", Response: FleetList{}})
	a.GET("/fleet/:org_id/fleets/:id", httpx.Handle(200, withOrg(func(c *gin.Context, p *httpx.Principal, org uuid.UUID) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Fleet(c.Request.Context(), p, org, id)
	})), httpx.Doc{Summary: "One fleet with its machines and managers", Response: Fleet{}})

	// Owners shape their fleets; fleet managers create and edit the fleets
	// they manage (with machines they already manage). Only owners decide
	// who manages a fleet.
	a.POST("/owner/fleets", httpx.OrgHandle(201, fleetEditors, func(c *gin.Context, s httpx.Scope) (any, error) {
		var req FleetRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.CreateFleet(c.Request.Context(), s, req)
	}), httpx.Doc{Summary: "Create a fleet (a fleet manager becomes its manager)", Request: FleetRequest{}, Response: Fleet{}, Status: 201, OrgScoped: true})
	a.PATCH("/owner/fleets/:id", httpx.OrgHandle(200, fleetEditors, func(c *gin.Context, s httpx.Scope) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		var req FleetRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.UpdateFleet(c.Request.Context(), s, id, req)
	}), httpx.Doc{Summary: "Rename a fleet", Request: FleetRequest{}, Response: Fleet{}, OrgScoped: true})
	a.POST("/owner/fleets/:id/archive", httpx.OrgHandle(200, fleetEditors, func(c *gin.Context, s httpx.Scope) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		var req ArchiveRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.ArchiveFleet(c.Request.Context(), s.P, s.Org, id, req.Reason)
	}), httpx.Doc{Summary: "Archive a fleet (its managers lose access to its machines)", Request: ArchiveRequest{}, Response: Fleet{}, OrgScoped: true})
	a.POST("/owner/fleets/:id/equipment", httpx.OrgHandle(200, fleetEditors, func(c *gin.Context, s httpx.Scope) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		var req EquipmentIDsRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.AddEquipment(c.Request.Context(), s, id, req)
	}), httpx.Doc{Summary: "Add machines to a fleet (moving them from any other fleet)", Request: EquipmentIDsRequest{}, Response: Fleet{}, OrgScoped: true})
	a.DELETE("/owner/fleets/:id/equipment/:equipment_id", httpx.OrgHandle(200, fleetEditors, func(c *gin.Context, s httpx.Scope) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		eq, err := httpx.Param(c, "equipment_id")
		if err != nil {
			return nil, err
		}
		return h.svc.RemoveEquipment(c.Request.Context(), s, id, eq)
	}), httpx.Doc{Summary: "Take a machine out of a fleet", Response: Fleet{}, OrgScoped: true})
	a.POST("/owner/fleets/:id/managers", httpx.OrgHandle(200, ownerOnly, func(c *gin.Context, s httpx.Scope) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		var req ManagerRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.AddManager(c.Request.Context(), s, id, req)
	}), httpx.Doc{Summary: "Give a fleet manager this fleet", Request: ManagerRequest{}, Response: Fleet{}, OrgScoped: true})
	a.DELETE("/owner/fleets/:id/managers/:user_id", httpx.OrgHandle(200, ownerOnly, func(c *gin.Context, s httpx.Scope) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		u, err := httpx.Param(c, "user_id")
		if err != nil {
			return nil, err
		}
		return h.svc.RemoveManager(c.Request.Context(), s, id, u)
	}), httpx.Doc{Summary: "Take a fleet away from a manager", Response: Fleet{}, OrgScoped: true})

	// Admins oversee fleet accounts.
	ad := r.Tag("fleet").Admin(httpx.RoleAdminOps, httpx.RoleAdminSupport)
	ad.GET("/admin/fleets", httpx.HandleQuery(200, func(c *gin.Context, p *httpx.Principal, q AdminFleetQuery) (any, error) {
		org := uuid.Nil
		if q.OrgID != "" {
			org = uuid.MustParse(q.OrgID)
		}
		return h.svc.Fleets(c.Request.Context(), p, org, q.Status)
	}), httpx.Doc{Summary: "Fleets across organisations", Query: AdminFleetQuery{}, Response: FleetList{}})
	ad.POST("/admin/fleets/:id/archive", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req ArchiveRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.ArchiveFleet(c.Request.Context(), p, uuid.Nil, id, req.Reason)
	}), httpx.Doc{Summary: "Archive a fleet account (e.g. after abuse)", Request: ArchiveRequest{}, Response: Fleet{}})
}
