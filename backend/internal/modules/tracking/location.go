package tracking

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/geo"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/live"
	"github.com/equiprent/backend/internal/modules/bookings"
)

// ---- privacy notice ---------------------------------------------------------------------------

// Policy is the tracking notice shown before anyone shares their location.
type Policy struct {
	Version string   `json:"version"`
	Title   string   `json:"title"`
	Points  []string `json:"points"`
}

func policy(version string) Policy {
	return Policy{Version: version, Title: "Sharing your location for this job", Points: []string{
		"EQUIPRENT uses your phone's location only while you deliver, work with or return this machine: from dispatch until the machine is handed back.",
		"Your phone's GPS shows where your phone is. It is used as the machine's position while you travel with it; a tracker fitted to the machine is used instead when there is one.",
		"We record position, accuracy, speed, heading and time. We do not collect anything else from your phone.",
		"Only the people on this booking (the owner's team for this machine and the customer during the delivery and rental) and EQUIPRENT support can see it, and every view is logged.",
		"Sharing stops automatically when the booking ends, when you stop it, or after a long time without updates. You can stop at any time.",
		"Location history is kept for 90 days for disputes and then deleted.",
	}}
}

// ---- sessions -----------------------------------------------------------------------------------

// SessionRequest starts sharing location for a booking.
type SessionRequest struct {
	Consent       bool    `json:"consent" binding:"required" doc:"Must be true: the person read the tracking notice"`
	PolicyVersion string  `json:"policy_version" binding:"required,max=40"`
	Background    bool    `json:"background" doc:"The app may keep sharing while in the background (where the OS permits)"`
	Platform      *string `json:"platform" binding:"omitempty,oneof=android ios web other"`
}

// Session is an active (or just ended) tracking session.
type Session struct {
	Active          bool       `json:"active"`
	TripID          *uuid.UUID `json:"trip_id,omitempty"`
	Kind            string     `json:"kind,omitempty"`
	Source          string     `json:"source,omitempty"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	EndReason       *string    `json:"end_reason,omitempty"`
	IntervalSeconds int        `json:"interval_seconds" doc:"How often the app should record a position"`
	UploadSeconds   int        `json:"upload_seconds" doc:"How often the app should upload its buffer"`
	Background      bool       `json:"background"`
}

var trackableStatuses = []string{bookings.StatusDispatched, bookings.StatusOnSite, bookings.StatusActive, "extension_requested",
	"cancellation_requested", bookings.StatusReturnInitiated}

func tripKindFor(status string) string {
	switch status {
	case bookings.StatusDispatched:
		return "mobilisation"
	case bookings.StatusReturnInitiated:
		return "demobilisation"
	}
	return "on_site"
}

// sharer: the operator, or the owner side on dry hire / without an operator.
func sharer(side bookings.Side, b bookings.Booking) bool {
	return side.Has(bookings.SideOperator) || (side.Has(bookings.SideOwner) && b.Operator == nil)
}

// StartSession records consent and opens (or joins) the booking's session.
func (s *Service) StartSession(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID, req SessionRequest) (Session, error) {
	b, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return Session{}, err
	}
	if !sharer(side, b) {
		return Session{}, apperror.ErrForbidden.WithDetail("the assigned operator (or the owner's team without an operator) shares location")
	}
	if !req.Consent {
		return Session{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "consent", Code: "required", Message: "read and accept the tracking notice"})
	}
	if req.PolicyVersion != s.opt.PolicyVersion {
		return Session{}, apperror.ErrConflict.WithDetail("the tracking notice changed; read it again")
	}
	if !slices.Contains(trackableStatuses, b.Status) {
		return Session{}, apperror.ErrInvalidTransition.WithDetail("location is shared only from dispatch until the machine is handed back")
	}
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "operator", Reason: "tracking consent"}, func(ctx context.Context) error {
		q := s.q(ctx)
		consent, err := q.GetActiveConsent(ctx, db.GetActiveConsentParams{BookingID: b.ID, UserID: p.UserID})
		if errors.Is(database.MapError(err), apperror.ErrNotFound) {
			consent, err = q.InsertConsent(ctx, db.InsertConsentParams{BookingID: b.ID, UserID: p.UserID, PolicyVersion: req.PolicyVersion,
				Background: req.Background, Platform: req.Platform})
		}
		if err != nil {
			return database.MapError(err)
		}
		cid := uuid.NullUUID{UUID: consent.ID, Valid: true}
		open, err := q.GetOpenSession(ctx, b.ID)
		switch {
		case err == nil:
			return database.MapError(q.AttachConsentToTrip(ctx, db.AttachConsentToTripParams{ID: open.ID, ConsentID: cid}))
		case errors.Is(database.MapError(err), apperror.ErrNotFound):
			op := uuid.NullUUID{}
			if b.Operator != nil {
				op = uuid.NullUUID{UUID: b.Operator.UserID, Valid: true}
			}
			_, err = q.StartSessionTrip(ctx, db.StartSessionTripParams{BookingID: b.ID, EquipmentID: b.EquipmentID, OperatorUserID: op,
				Kind: db.TripKind(tripKindFor(b.Status)), ConsentID: cid, Source: "app"})
			return database.MapError(err)
		default:
			return database.MapError(err)
		}
	})
	if err != nil {
		return Session{}, err
	}
	return s.session(ctx, b.ID, req.Background)
}

func (s *Service) session(ctx context.Context, bookingID uuid.UUID, background bool) (Session, error) {
	out := Session{IntervalSeconds: 10, UploadSeconds: 30, Background: background}
	open, err := s.q(ctx).GetOpenSession(ctx, bookingID)
	if errors.Is(database.MapError(err), apperror.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return Session{}, database.MapError(err)
	}
	id, started := open.ID, open.StartedAt
	out.Active, out.TripID, out.Kind, out.Source, out.StartedAt = true, &id, string(open.Kind), open.Source, &started
	return out, nil
}

// StopSession withdraws the caller's consent and ends the open session.
func (s *Service) StopSession(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID) (Session, error) {
	b, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return Session{}, err
	}
	if !sharer(side, b) {
		return Session{}, apperror.ErrForbidden
	}
	var ended db.EndSessionRow
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "operator", Reason: "tracking stopped"}, func(ctx context.Context) error {
		q := s.q(ctx)
		if _, err := q.WithdrawConsent(ctx, db.WithdrawConsentParams{BookingID: b.ID, UserID: p.UserID}); err != nil {
			return database.MapError(err)
		}
		var err error
		ended, err = q.EndSession(ctx, db.EndSessionParams{BookingID: b.ID, EndReason: ptr("stopped_by_user")})
		if errors.Is(database.MapError(err), apperror.ErrNotFound) {
			return nil
		}
		return database.MapError(err)
	})
	if err != nil {
		return Session{}, err
	}
	s.hub.Publish(live.Message{Type: "session_ended", EquipmentID: b.EquipmentID, BookingID: b.ID})
	out := Session{IntervalSeconds: 10, UploadSeconds: 30}
	if ended.ID != uuid.Nil {
		id := ended.ID
		out.TripID, out.Kind, out.StartedAt, out.EndedAt, out.EndReason = &id, string(ended.Kind), &ended.StartedAt, ended.EndedAt, ended.EndReason
	}
	return out, nil
}

func ptr[T any](v T) *T { return &v }

// CloseIdle ends sessions without positions for the idle timeout (worker).
func (s *Service) CloseIdle(ctx context.Context) (int, error) {
	rows, err := s.q(ctx).CloseIdleSessions(ctx, s.opt.IdleTimeout.Seconds())
	return len(rows), database.MapError(err)
}

// ---- track view -----------------------------------------------------------------------------------

// Fix is a position with where it came from.
type Fix struct {
	Lat        float64   `json:"lat"`
	Lng        float64   `json:"lng"`
	RecordedAt time.Time `json:"recorded_at"`
	AccuracyM  *float32  `json:"accuracy_m,omitempty"`
	Source     string    `json:"source" doc:"app (a phone with the machine), telematics (a tracker on the machine) or simulated"`
}

// TrackView is everything the tracking screen shows.
type TrackView struct {
	BookingID   uuid.UUID     `json:"booking_id"`
	Status      string        `json:"status"`
	Destination Position      `json:"destination" doc:"The delivery site"`
	Equipment   *Fix          `json:"equipment_position,omitempty" doc:"Best known machine position (tracker if fitted, else the operator's phone)"`
	Operator    *Fix          `json:"operator_position,omitempty" doc:"The operator's phone in the current session"`
	SourceNote  string        `json:"source_note"`
	ETA         *geo.Estimate `json:"eta,omitempty" doc:"While the machine is on its way"`
	Route       []geo.Point   `json:"route" doc:"The current session's path (thinned)"`
	Session     Session       `json:"session"`
	Trips       []TripInfo    `json:"trips"`
	Hidden      bool          `json:"hidden" doc:"true when the location is not shared with you at this stage"`
}

// TripInfo is a session in the booking's history.
type TripInfo struct {
	ID        uuid.UUID  `json:"id"`
	Kind      string     `json:"kind"`
	Source    string     `json:"source"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	EndReason *string    `json:"end_reason,omitempty"`
	DistanceM *int32     `json:"distance_m,omitempty"`
	Points    int32      `json:"points"`
	Consented bool       `json:"consented"`
}

func sourceNote(src string) string {
	switch src {
	case "telematics":
		return "Position from the tracker fitted to the machine."
	case "simulated":
		return "Simulated position (development): not a real location."
	case "app":
		return "Position of the operator's phone travelling with the machine, not a tracker on the machine itself."
	}
	return "No position shared yet."
}

// customerSees: customers see positions from dispatch until hand-back.
func customerSees(status string) bool { return slices.Contains(trackableStatuses, status) }

// View builds the tracking screen for a party and logs the access.
func (s *Service) View(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID) (TrackView, error) {
	b, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return TrackView{}, err
	}
	row, err := s.repo.Snapshot(ctx, bookingID)
	if err != nil {
		return TrackView{}, err
	}
	out := TrackView{BookingID: b.ID, Status: b.Status, Destination: Position{Lat: row.SiteLat, Lng: row.SiteLng}, Route: []geo.Point{}}
	if out.Session, err = s.session(ctx, b.ID, false); err != nil {
		return TrackView{}, err
	}
	trips, err := s.q(ctx).BookingTripsDetailed(ctx, b.ID)
	if err != nil {
		return TrackView{}, database.MapError(err)
	}
	for _, t := range trips {
		out.Trips = append(out.Trips, TripInfo{ID: t.ID, Kind: string(t.Kind), Source: t.Source, StartedAt: t.StartedAt, EndedAt: t.EndedAt,
			EndReason: t.EndReason, DistanceM: t.DistanceM, Points: t.Points, Consented: t.Consented})
	}
	if out.Trips == nil {
		out.Trips = []TripInfo{}
	}
	onlyCustomer := side.Has(bookings.SideCustomer) && !side.Has(bookings.SideOwner|bookings.SideOperator|bookings.SideAdmin)
	if onlyCustomer && !customerSees(b.Status) {
		out.Hidden, out.SourceNote = true, "The machine's location is shared with you only during delivery and the rental."
		return out, nil
	}
	if err := s.audit(ctx, p.UserID, "booking", b.ID, "live"); err != nil {
		return TrackView{}, err
	}
	if last, err := s.q(ctx).GetLastLocation(ctx, b.EquipmentID); err == nil {
		out.Equipment = &Fix{Lat: last.Lat, Lng: last.Lng, RecordedAt: last.RecordedAt, AccuracyM: last.AccuracyM, Source: last.Source}
	}
	if out.Session.Active {
		if f, err := s.q(ctx).LatestPhoneFix(ctx, *out.Session.TripID); err == nil {
			out.Operator = &Fix{Lat: f.Lat, Lng: f.Lng, RecordedAt: f.RecordedAt, AccuracyM: f.AccuracyM, Source: f.Source}
		}
		pts, err := s.q(ctx).TripPoints(ctx, db.TripPointsParams{TripID: *out.Session.TripID, MaxPoints: 200})
		if err != nil {
			return TrackView{}, database.MapError(err)
		}
		for _, pt := range pts {
			out.Route = append(out.Route, geo.Point{Lat: pt.Lat, Lng: pt.Lng})
		}
	}
	src := ""
	if out.Equipment != nil {
		src = out.Equipment.Source
	}
	out.SourceNote = sourceNote(src)
	if out.Equipment != nil && (b.Status == bookings.StatusDispatched || b.Status == bookings.StatusReturnInitiated) && s.router != nil {
		if e, err := s.router.Route(ctx, geo.Point{Lat: out.Equipment.Lat, Lng: out.Equipment.Lng}, geo.Point{Lat: row.SiteLat, Lng: row.SiteLng}); err == nil {
			out.ETA = &e
		}
	}
	return out, nil
}

// HistoryView is one session's path.
type HistoryView struct {
	TripID uuid.UUID      `json:"trip_id"`
	Points []HistoryPoint `json:"points"`
}

type HistoryPoint struct {
	Lat        float64   `json:"lat"`
	Lng        float64   `json:"lng"`
	RecordedAt time.Time `json:"recorded_at"`
	AccuracyM  *float32  `json:"accuracy_m,omitempty"`
	SpeedMps   *float32  `json:"speed_mps,omitempty"`
	Source     string    `json:"source"`
}

type HistoryQuery struct {
	TripID string `form:"trip_id" binding:"required,uuid"`
}

// History returns a session's path: the owner side, the session's operator
// and admins; the customer only while the booking is in progress.
func (s *Service) History(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID, q HistoryQuery) (HistoryView, error) {
	b, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return HistoryView{}, err
	}
	onlyCustomer := side.Has(bookings.SideCustomer) && !side.Has(bookings.SideOwner|bookings.SideOperator|bookings.SideAdmin)
	if onlyCustomer && !customerSees(b.Status) {
		return HistoryView{}, apperror.ErrForbidden.WithDetail("location history is shared with customers only during delivery and the rental")
	}
	tripID := uuid.MustParse(q.TripID)
	trips, err := s.q(ctx).BookingTripsDetailed(ctx, b.ID)
	if err != nil {
		return HistoryView{}, database.MapError(err)
	}
	if !slices.ContainsFunc(trips, func(t db.BookingTripsDetailedRow) bool { return t.ID == tripID }) {
		return HistoryView{}, apperror.ErrNotFound
	}
	if err := s.audit(ctx, p.UserID, "booking", b.ID, "history"); err != nil {
		return HistoryView{}, err
	}
	pts, err := s.q(ctx).TripPoints(ctx, db.TripPointsParams{TripID: tripID, MaxPoints: 500})
	if err != nil {
		return HistoryView{}, database.MapError(err)
	}
	out := HistoryView{TripID: tripID, Points: make([]HistoryPoint, 0, len(pts))}
	for _, pt := range pts {
		out.Points = append(out.Points, HistoryPoint{Lat: pt.Lat, Lng: pt.Lng, RecordedAt: pt.RecordedAt, AccuracyM: pt.AccuracyM, SpeedMps: pt.SpeedMps, Source: pt.Source})
	}
	return out, nil
}

func (s *Service) audit(ctx context.Context, user uuid.UUID, subjectType string, subject uuid.UUID, kind string) error {
	return database.MapError(s.q(ctx).LogLocationAccess(ctx, db.LogLocationAccessParams{UserID: user, SubjectType: subjectType, SubjectID: subject, AccessKind: kind}))
}

// ---- live stream --------------------------------------------------------------------------------

// TicketView opens a WebSocket within its validity.
type TicketView struct {
	Ticket    string    `json:"ticket"`
	ExpiresAt time.Time `json:"expires_at"`
	Path      string    `json:"path" doc:"Connect to this path (relative to the API base) with ?ticket="`
}

// BookingTicket authorises a live stream of a booking's machine.
func (s *Service) BookingTicket(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID) (TicketView, error) {
	b, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return TicketView{}, err
	}
	onlyCustomer := side.Has(bookings.SideCustomer) && !side.Has(bookings.SideOwner|bookings.SideOperator|bookings.SideAdmin)
	if onlyCustomer && !customerSees(b.Status) {
		return TicketView{}, apperror.ErrForbidden.WithDetail("the machine's location is shared with you only during delivery and the rental")
	}
	t, exp := s.tickets.Issue(p.UserID, "booking", b.ID)
	return TicketView{Ticket: t, ExpiresAt: exp, Path: "/ws/track"}, nil
}

// FleetTicket authorises the live stream of an organisation's machines in
// the caller's scope.
func (s *Service) FleetTicket(ctx context.Context, p *httpx.Principal, org uuid.UUID) (TicketView, error) {
	if !p.HasOrgRole(org, httpx.RoleOwner, httpx.RoleFleetManager) {
		return TicketView{}, apperror.ErrNotFound
	}
	t, exp := s.tickets.Issue(p.UserID, "fleet", org)
	return TicketView{Ticket: t, ExpiresAt: exp, Path: "/ws/track"}, nil
}

// PrincipalLoader rebuilds the principal (roles) for a ticket's user.
type PrincipalLoader func(ctx context.Context, userID uuid.UUID, org uuid.UUID) (*httpx.Principal, error)

// stream resolves which machines a ticket may watch, re-checking access at
// connect time (a role revoked after the ticket was issued is honoured).
func (s *Service) stream(ctx context.Context, tk live.Ticket) (*httpx.Principal, []uuid.UUID, uuid.UUID, error) {
	if s.principals == nil {
		return nil, nil, uuid.Nil, apperror.ErrServiceUnavailable
	}
	switch tk.Kind {
	case "booking":
		p, err := s.principals(ctx, tk.UserID, uuid.Nil)
		if err != nil {
			return nil, nil, uuid.Nil, err
		}
		b, side, err := s.bookings.Access(ctx, p, tk.Subject)
		if err != nil {
			return nil, nil, uuid.Nil, err
		}
		onlyCustomer := side.Has(bookings.SideCustomer) && !side.Has(bookings.SideOwner|bookings.SideOperator|bookings.SideAdmin)
		if onlyCustomer && !customerSees(b.Status) {
			return nil, nil, uuid.Nil, apperror.ErrForbidden
		}
		return p, []uuid.UUID{b.EquipmentID}, b.ID, s.audit(ctx, p.UserID, "booking", b.ID, "stream")
	case "fleet":
		p, err := s.principals(ctx, tk.UserID, tk.Subject)
		if err != nil {
			return nil, nil, uuid.Nil, err
		}
		if !p.HasOrgRole(tk.Subject, httpx.RoleOwner, httpx.RoleFleetManager) {
			return nil, nil, uuid.Nil, apperror.ErrNotFound
		}
		ids, err := s.fleetMachines(ctx, p, tk.Subject)
		if err != nil {
			return nil, nil, uuid.Nil, err
		}
		return p, ids, uuid.Nil, s.audit(ctx, p.UserID, "fleet", tk.Subject, "fleet_live")
	}
	return nil, nil, uuid.Nil, live.ErrBadTicket
}

// fleetMachines are the organisation's machines the caller may watch.
func (s *Service) fleetMachines(ctx context.Context, p *httpx.Principal, org uuid.UUID) ([]uuid.UUID, error) {
	all := true
	var ids map[uuid.UUID]bool
	if !p.HasOrgRole(org, httpx.RoleOwner) && s.scope != nil {
		var err error
		if all, ids, err = s.scope.Scope(ctx, p, org); err != nil {
			return nil, err
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT id FROM equipment WHERE owner_org_id = $1 AND deleted_at IS NULL`, org)
	if err != nil {
		return nil, database.MapError(err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if all || ids[id] {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

// ServeStream upgrades GET /ws/track?ticket= and forwards updates until the
// client leaves or the ticket's access ends. Customers' booking streams end
// when the booking leaves the delivery/rental stages.
func (h *Handler) ServeStream(c *gin.Context) {
	tk, err := h.svc.tickets.Verify(c.Query("ticket"))
	if err != nil {
		httpx.Fail(c, apperror.New(http.StatusUnauthorized, "TICKET_INVALID", "The stream ticket is invalid or expired"))
		return
	}
	ctx := c.Request.Context()
	_, machines, bookingID, err := h.svc.stream(ctx, tk)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{OriginPatterns: h.svc.opt.Origins})
	if err != nil {
		return // Accept wrote the error response
	}
	defer conn.CloseNow()
	sub := h.svc.hub.Subscribe(machines...)
	defer sub.Close()
	ctx = conn.CloseRead(ctx) // we only write; reading handles pings and close frames
	// Start with the current positions.
	for _, id := range machines {
		if last, err := h.svc.q(ctx).GetLastLocation(ctx, id); err == nil {
			_ = wsjson.Write(ctx, conn, live.Message{Type: "snapshot", EquipmentID: id, BookingID: bookingID, Lat: last.Lat, Lng: last.Lng,
				AccuracyM: last.AccuracyM, RecordedAt: last.RecordedAt, Source: last.Source})
		}
	}
	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-sub.C:
			if !ok {
				return
			}
			if bookingID != uuid.Nil && m.BookingID != uuid.Nil && m.BookingID != bookingID {
				continue // another booking of the same machine
			}
			wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := wsjson.Write(wctx, conn, m)
			cancel()
			if err != nil {
				return
			}
			if m.Type == "session_ended" && bookingID != uuid.Nil {
				_ = conn.Close(websocket.StatusNormalClosure, "session ended")
				return
			}
		case <-keepalive.C:
			pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// ---- hardware trackers ---------------------------------------------------------------------------

// Device is a registered tracker.
type Device struct {
	ID         uuid.UUID  `json:"id"`
	Provider   string     `json:"provider"`
	ExternalID string     `json:"external_id"`
	Label      *string    `json:"label,omitempty"`
	Status     string     `json:"status"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type DeviceList struct {
	Data []Device `json:"data"`
}

type DeviceRequest struct {
	Provider   string  `json:"provider" binding:"required,max=30"`
	ExternalID string  `json:"external_id" binding:"required,min=1,max=120" doc:"The device id at the vendor (IMEI, serial)"`
	Label      *string `json:"label" binding:"omitempty,max=80"`
}

// RegisteredDevice includes the token, shown only once.
type RegisteredDevice struct {
	Device Device `json:"device"`
	Token  string `json:"token" doc:"Configure the device to send Authorization: Bearer <token>. Shown only once."`
}

type DeviceIngestResult struct {
	Accepted int  `json:"accepted"`
	InTrip   bool `json:"in_trip" doc:"A tracking session was open, so the points joined the history"`
}

func tokenHash(t string) []byte {
	h := sha256.Sum256([]byte(t))
	return h[:]
}

// RegisterDevice fits a tracker to a machine (owner side, machine scope
// enforced by the equipment guard).
func (s *Service) RegisterDevice(ctx context.Context, sc httpx.Scope, equipmentID uuid.UUID, req DeviceRequest) (RegisteredDevice, error) {
	if _, ok := s.providers[req.Provider]; !ok {
		return RegisteredDevice{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "provider", Code: "unknown", Message: "unsupported tracker provider"})
	}
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	token := "trk_" + base64.RawURLEncoding.EncodeToString(raw)
	var row db.InsertTelematicsDeviceRow
	err := s.tx.InTxAs(ctx, database.Actor{UserID: sc.P.UserID, Role: "owner", Reason: "tracker registered"}, func(ctx context.Context) error {
		var err error
		row, err = s.q(ctx).InsertTelematicsDevice(ctx, db.InsertTelematicsDeviceParams{EquipmentID: equipmentID, OwnerOrgID: sc.Org,
			Provider: req.Provider, ExternalID: strings.TrimSpace(req.ExternalID), TokenSha256: tokenHash(token), Label: req.Label,
			CreatedBy: uuid.NullUUID{UUID: sc.P.UserID, Valid: true}})
		return database.MapError(err)
	})
	if errors.Is(err, apperror.ErrNotFound) {
		return RegisteredDevice{}, apperror.ErrNotFound
	}
	if errors.Is(err, apperror.ErrConflict) {
		return RegisteredDevice{}, apperror.ErrConflict.WithDetail("this machine already has an active tracker, or the device is registered elsewhere")
	}
	if err != nil {
		return RegisteredDevice{}, err
	}
	return RegisteredDevice{Token: token, Device: Device{ID: row.ID, Provider: row.Provider, ExternalID: row.ExternalID, Label: row.Label,
		Status: row.Status, LastSeenAt: row.LastSeenAt, CreatedAt: row.CreatedAt}}, nil
}

func (s *Service) Devices(ctx context.Context, org, equipmentID uuid.UUID) (DeviceList, error) {
	rows, err := s.q(ctx).ListTelematicsDevices(ctx, db.ListTelematicsDevicesParams{EquipmentID: equipmentID, OwnerOrgID: org})
	if err != nil {
		return DeviceList{}, database.MapError(err)
	}
	out := DeviceList{Data: make([]Device, 0, len(rows))}
	for _, r := range rows {
		out.Data = append(out.Data, Device{ID: r.ID, Provider: r.Provider, ExternalID: r.ExternalID, Label: r.Label, Status: r.Status,
			LastSeenAt: r.LastSeenAt, CreatedAt: r.CreatedAt})
	}
	return out, nil
}

func (s *Service) RevokeDevice(ctx context.Context, sc httpx.Scope, equipmentID, deviceID uuid.UUID) error {
	return s.tx.InTxAs(ctx, database.Actor{UserID: sc.P.UserID, Role: "owner", Reason: "tracker revoked"}, func(ctx context.Context) error {
		n, err := s.q(ctx).RevokeTelematicsDevice(ctx, db.RevokeTelematicsDeviceParams{ID: deviceID, EquipmentID: equipmentID, OwnerOrgID: sc.Org})
		if err == nil && n == 0 {
			return apperror.ErrNotFound
		}
		return database.MapError(err)
	})
}

// IngestDevice accepts a vendor payload from an authenticated tracker. The
// machine's last position is always updated (it is an asset, not a
// person); points join the history only within an open session.
func (s *Service) IngestDevice(ctx context.Context, provider, bearer string, body []byte) (DeviceIngestResult, error) {
	prov, ok := s.providers[provider]
	if !ok {
		return DeviceIngestResult{}, apperror.ErrNotFound
	}
	q := s.q(ctx)
	dev, err := q.DeviceByToken(ctx, tokenHash(bearer))
	if err != nil || dev.Provider != provider {
		return DeviceIngestResult{}, apperror.New(http.StatusUnauthorized, "DEVICE_UNAUTHORIZED", "Unknown or revoked device token")
	}
	pts, err := prov.Parse(body)
	if err != nil {
		return DeviceIngestResult{}, apperror.ErrBadRequest.WithDetail(err.Error())
	}
	limit := s.now().Add(maxClockSkew)
	source := "telematics"
	if provider == "simulated" {
		source = "simulated"
	}
	res := DeviceIngestResult{}
	var latest *DevicePosition
	var bookingID uuid.NullUUID
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		trip, terr := q.OpenTripForEquipment(ctx, dev.EquipmentID)
		res.InTrip = terr == nil
		if res.InTrip {
			bookingID = uuid.NullUUID{UUID: trip.BookingID, Valid: true}
		}
		slices.SortFunc(pts, func(a, b DevicePosition) int { return a.RecordedAt.Compare(b.RecordedAt) })
		for i := range pts {
			pt := pts[i]
			if pt.ExternalID != dev.ExternalID {
				return apperror.ErrForbidden.WithDetail("the payload is for another device")
			}
			if pt.RecordedAt.After(limit) {
				return apperror.ErrValidation.WithDetail("a point is timestamped in the future; check the device clock")
			}
			if res.InTrip {
				if _, err := q.InsertSourcedPing(ctx, db.InsertSourcedPingParams{TripID: trip.ID, EquipmentID: dev.EquipmentID, RecordedAt: pt.RecordedAt.UTC(),
					Lat: pt.Lat, Lng: pt.Lng, AccuracyM: pt.AccuracyM, SpeedMps: pt.SpeedMps, Heading: pt.Heading, Source: source}); err != nil {
					return database.MapError(err)
				}
			}
			res.Accepted++
			latest = &pts[i]
		}
		if err := q.UpsertSourcedLastLocation(ctx, db.UpsertSourcedLastLocationParams{EquipmentID: dev.EquipmentID, Lat: latest.Lat, Lng: latest.Lng,
			RecordedAt: latest.RecordedAt.UTC(), Source: source, AccuracyM: latest.AccuracyM, BookingID: bookingID}); err != nil {
			return database.MapError(err)
		}
		return database.MapError(q.TouchDevice(ctx, dev.ID))
	})
	if err != nil {
		return DeviceIngestResult{}, err
	}
	s.hub.Publish(live.Message{Type: "position", EquipmentID: dev.EquipmentID, BookingID: bookingID.UUID, Lat: latest.Lat, Lng: latest.Lng,
		AccuracyM: latest.AccuracyM, RecordedAt: latest.RecordedAt.UTC(), Source: source})
	return res, nil
}

// ---- access audit (admin) ---------------------------------------------------------------------------

type AccessEntry struct {
	UserID      uuid.UUID `json:"user_id"`
	UserName    string    `json:"user_name"`
	SubjectType string    `json:"subject_type"`
	SubjectID   uuid.UUID `json:"subject_id"`
	AccessKind  string    `json:"access_kind"`
	Hour        time.Time `json:"hour"`
	FirstAt     time.Time `json:"first_at"`
	LastAt      time.Time `json:"last_at"`
	Count       int32     `json:"count"`
}

type AccessList struct {
	Data []AccessEntry `json:"data"`
}

type AccessQuery struct {
	SubjectID string `form:"subject_id" binding:"omitempty,uuid"`
	UserID    string `form:"user_id" binding:"omitempty,uuid"`
}

func (s *Service) AccessLog(ctx context.Context, f AccessQuery) (AccessList, error) {
	params := db.ListLocationAccessParams{Lim: 500}
	if f.SubjectID != "" {
		params.SubjectID = uuid.NullUUID{UUID: uuid.MustParse(f.SubjectID), Valid: true}
	}
	if f.UserID != "" {
		params.UserID = uuid.NullUUID{UUID: uuid.MustParse(f.UserID), Valid: true}
	}
	rows, err := s.q(ctx).ListLocationAccess(ctx, params)
	if err != nil {
		return AccessList{}, database.MapError(err)
	}
	out := AccessList{Data: make([]AccessEntry, 0, len(rows))}
	for _, r := range rows {
		out.Data = append(out.Data, AccessEntry(r))
	}
	return out, nil
}

// ---- wiring ------------------------------------------------------------------------------------

// Options configure location features.
type Options struct {
	PolicyVersion  string
	AllowSimulated bool
	IdleTimeout    time.Duration
	Origins        []string // WebSocket origin patterns (browser clients)
}

// Scope answers which machines a fleet manager may see (internal/access).
type Scope interface {
	Scope(ctx context.Context, p *httpx.Principal, org uuid.UUID) (all bool, ids map[uuid.UUID]bool, err error)
}

// WithLocation enables sessions, streams, routing, trackers and audit.
func (s *Service) WithLocation(pool *pgxpool.Pool, hub *live.Hub, tickets *live.Tickets, router geo.Router, scope Scope, principals PrincipalLoader, opt Options) *Service {
	if opt.PolicyVersion == "" {
		opt.PolicyVersion = "2026-11"
	}
	if opt.IdleTimeout <= 0 {
		opt.IdleTimeout = 3 * time.Hour
	}
	s.pool, s.hub, s.tickets, s.router, s.scope, s.principals, s.opt = pool, hub, tickets, router, scope, principals, opt
	s.providers = Providers(opt.AllowSimulated)
	return s
}

func (s *Service) q(ctx context.Context) *db.Queries { return database.Queries(ctx, s.pool) }

// Hub exposes the live hub (other modules publish to it).
func (s *Service) Hub() *live.Hub { return s.hub }

func (h *Handler) locationRoutes(r *httpx.Router) {
	r.Tag("tracking").GET("/tracking/policy", httpx.Handle(200, func(c *gin.Context, _ *httpx.Principal) (any, error) {
		return policy(h.svc.opt.PolicyVersion), nil
	}), httpx.Doc{Summary: "The tracking notice shown before sharing location", Response: Policy{}})

	a := r.Tag("tracking").Authed()
	start := httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req SessionRequest) (any, error) {
		id, err := bookingParam(c)
		if err != nil {
			return nil, err
		}
		return h.svc.StartSession(c.Request.Context(), p, id, req)
	})
	stop := httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := bookingParam(c)
		if err != nil {
			return nil, err
		}
		return h.svc.StopSession(c.Request.Context(), p, id)
	})
	startDoc := httpx.Doc{Summary: "Start sharing location (records consent)", Request: SessionRequest{}, Response: Session{},
		Description: "Needs consent to the current tracking notice (GET /tracking/policy). Only from dispatch until hand-back; it stops by itself when the booking leaves those stages."}
	stopDoc := httpx.Doc{Summary: "Stop sharing location (withdraws consent, ends the session)", Response: Session{}}
	a.POST("/operator/jobs/:booking_id/tracking/session", start, startDoc)
	a.DELETE("/operator/jobs/:booking_id/tracking/session", stop, stopDoc)
	a.POST("/bookings/:id/tracking/session", start, startDoc)
	a.DELETE("/bookings/:id/tracking/session", stop, stopDoc)

	a.GET("/bookings/:id/track/history", httpx.HandleQuery(200, func(c *gin.Context, p *httpx.Principal, q HistoryQuery) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.History(c.Request.Context(), p, id, q)
	}), httpx.Doc{Summary: "One tracking session's path (thinned to 500 points)", Query: HistoryQuery{}, Response: HistoryView{}})
	a.POST("/bookings/:id/track/ticket", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.BookingTicket(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Ticket for the live position stream (WebSocket)", Response: TicketView{},
		Description: "Valid for about a minute; connect to GET /ws/track?ticket=... Messages are JSON live updates (snapshot, position, session_ended)."})
	a.POST("/fleet/:org_id/live/ticket", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		org, err := httpx.Param(c, "org_id")
		if err != nil {
			return nil, err
		}
		return h.svc.FleetTicket(c.Request.Context(), p, org)
	}), httpx.Doc{Summary: "Ticket for the live stream of your machines (WebSocket)", Response: TicketView{},
		Description: "Fleet managers receive only the machines of their fleets."})
	r.Tag("tracking").GET("/ws/track", h.ServeStream, httpx.Doc{Summary: "Live position stream (WebSocket upgrade)",
		Description: "Authenticated by the ticket query parameter. Access is re-checked when the socket opens."})

	a.POST("/owner/equipment/:id/trackers", httpx.OrgHandle(201, ownerRoles, func(c *gin.Context, sc httpx.Scope) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		var req DeviceRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.RegisterDevice(c.Request.Context(), sc, id, req)
	}), httpx.Doc{Summary: "Fit a GPS tracker / telematics unit to a machine", Request: DeviceRequest{}, Response: RegisteredDevice{}, Status: 201, OrgScoped: true})
	a.GET("/owner/equipment/:id/trackers", httpx.OrgHandle(200, ownerRoles, func(c *gin.Context, sc httpx.Scope) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Devices(c.Request.Context(), sc.Org, id)
	}), httpx.Doc{Summary: "A machine's trackers", Response: DeviceList{}, OrgScoped: true})
	a.DELETE("/owner/equipment/:id/trackers/:device_id", httpx.OrgHandle(204, ownerRoles, func(c *gin.Context, sc httpx.Scope) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		dev, err := httpx.Param(c, "device_id")
		if err != nil {
			return nil, err
		}
		return nil, h.svc.RevokeDevice(c.Request.Context(), sc, id, dev)
	}), httpx.Doc{Summary: "Revoke a tracker (its token stops working)", Status: 204, OrgScoped: true})
	r.Tag("tracking").POST("/telematics/:provider/positions", func(c *gin.Context) {
		body, err := c.GetRawData()
		if err != nil {
			httpx.Fail(c, httpx.BindError(err))
			return
		}
		bearer := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer"))
		res, err := h.svc.IngestDevice(c.Request.Context(), c.Param("provider"), bearer, body)
		httpx.Reply(c, 200, res, err)
	}, httpx.Doc{Summary: "Positions from a tracker (device token)", Response: DeviceIngestResult{},
		Description: "Authorization: Bearer <device token>. The provider adapts the vendor's payload (generic: {device_id, positions:[{lat,lng,recorded_at,...}]})."})

	r.Tag("tracking").Admin(httpx.RoleAdminSupport, httpx.RoleAdminOps).GET("/admin/location-access", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q AccessQuery) (any, error) {
		return h.svc.AccessLog(c.Request.Context(), q)
	}), httpx.Doc{Summary: "Who viewed which locations (hourly)", Query: AccessQuery{}, Response: AccessList{}})
}

var ownerRoles = []string{httpx.RoleOwner, httpx.RoleFleetManager}

func bookingParam(c *gin.Context) (uuid.UUID, error) {
	if c.Param("booking_id") != "" {
		return httpx.Param(c, "booking_id")
	}
	return httpx.Param(c, "id")
}
