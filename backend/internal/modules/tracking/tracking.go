// Package tracking records machine movement for a booking: trips
// (mobilisation, on-site, demobilisation), batched GPS pings from the
// operator app (offline catch-up safe), the last known position (PostgreSQL
// snapshot + cache for live reads) and site geofencing, which moves a
// dispatched booking to on_site automatically when the machine arrives.
package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/cache"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/geo"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/live"
	"github.com/equiprent/backend/internal/modules/bookings"
)

// Point is one GPS fix from a device.
type Point struct {
	Lat        float64   `json:"lat" binding:"latitude"`
	Lng        float64   `json:"lng" binding:"longitude"`
	RecordedAt time.Time `json:"recorded_at" binding:"required"`
	AccuracyM  *float32  `json:"accuracy_m" binding:"omitempty,min=0"`
	SpeedMps   *float32  `json:"speed_mps" binding:"omitempty,min=0"`
	Heading    *int16    `json:"heading" binding:"omitempty,min=0,max=359"`
	BatteryPct *int16    `json:"battery_pct" binding:"omitempty,min=0,max=100"`
}

type BatchRequest struct {
	Points []Point `json:"points" binding:"required,min=1,max=500,dive"`
	// Simulated marks points from the app's GPS simulator (development
	// builds without real location); refused where simulation is off.
	Simulated bool `json:"simulated"`
}

type BatchResult struct {
	Accepted   int    `json:"accepted"`
	Duplicates int    `json:"duplicates"`
	Arrived    bool   `json:"arrived" doc:"The batch moved the booking to on_site (geofence)"`
	TripID     string `json:"trip_id"`
}

// Trip is a movement leg of a booking.
type Trip struct {
	ID        uuid.UUID  `json:"id"`
	Kind      string     `json:"kind"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	DistanceM *int32     `json:"distance_m,omitempty"`
}

type StartTripRequest struct {
	Kind string `json:"kind" binding:"required,oneof=mobilisation on_site demobilisation"`
}

// Position is a last known location.
type Position struct {
	Lat        float64   `json:"lat"`
	Lng        float64   `json:"lng"`
	RecordedAt time.Time `json:"recorded_at"`
}

// Snapshot is the tracking view of a booking.
type Snapshot struct {
	BookingID    uuid.UUID `json:"booking_id"`
	Status       string    `json:"status"`
	Site         Position  `json:"site"`
	Position     *Position `json:"position,omitempty"`
	MetersToSite *float64  `json:"meters_to_site,omitempty"`
	Trips        []Trip    `json:"trips"`
}

// ---- repository --------------------------------------------------------------------

type Repository interface {
	OpenTrip(ctx context.Context, bookingID uuid.UUID) (Trip, uuid.UUID, error)
	StartTrip(ctx context.Context, bookingID, equipmentID uuid.UUID, operator uuid.NullUUID, kind string) (Trip, error)
	EndTrip(ctx context.Context, tripID uuid.UUID) (Trip, error)
	Trips(ctx context.Context, bookingID uuid.UUID) ([]Trip, error)
	InsertPing(ctx context.Context, tripID, equipmentID uuid.UUID, p Point) (bool, error)
	UpsertLast(ctx context.Context, equipmentID uuid.UUID, p Point) error
	DistanceToSite(ctx context.Context, bookingID uuid.UUID, lat, lng float64) (float64, int32, error)
	RecordArrival(ctx context.Context, bookingID uuid.UUID, p Point) (bool, error)
	Snapshot(ctx context.Context, bookingID uuid.UUID) (db.GetTrackingSnapshotRow, error)
}

type PGRepository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *PGRepository { return &PGRepository{pool: pool} }

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

func (r *PGRepository) OpenTrip(ctx context.Context, bookingID uuid.UUID) (Trip, uuid.UUID, error) {
	t, err := r.q(ctx).GetOpenTripForBooking(ctx, bookingID)
	if err != nil {
		return Trip{}, uuid.Nil, database.MapError(err)
	}
	return Trip{ID: t.ID, Kind: string(t.Kind), StartedAt: t.StartedAt, EndedAt: t.EndedAt, DistanceM: t.DistanceM}, t.EquipmentID, nil
}

func (r *PGRepository) StartTrip(ctx context.Context, bookingID, equipmentID uuid.UUID, operator uuid.NullUUID, kind string) (Trip, error) {
	t, err := r.q(ctx).StartTrip(ctx, db.StartTripParams{
		BookingID: bookingID, EquipmentID: equipmentID, OperatorUserID: operator, Kind: db.TripKind(kind), StartedAt: time.Now(),
	})
	if err != nil {
		return Trip{}, database.MapError(err)
	}
	return Trip{ID: t.ID, Kind: string(t.Kind), StartedAt: t.StartedAt}, nil
}

func (r *PGRepository) EndTrip(ctx context.Context, tripID uuid.UUID) (Trip, error) {
	now := time.Now()
	t, err := r.q(ctx).EndTrip(ctx, db.EndTripParams{ID: tripID, EndedAt: &now})
	if err != nil {
		return Trip{}, database.MapError(err)
	}
	return Trip{ID: t.ID, Kind: string(t.Kind), StartedAt: t.StartedAt, EndedAt: t.EndedAt, DistanceM: t.DistanceM}, nil
}

func (r *PGRepository) Trips(ctx context.Context, bookingID uuid.UUID) ([]Trip, error) {
	rows, err := r.q(ctx).ListBookingTrips(ctx, bookingID)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Trip, 0, len(rows))
	for _, t := range rows {
		out = append(out, Trip{ID: t.ID, Kind: string(t.Kind), StartedAt: t.StartedAt, EndedAt: t.EndedAt, DistanceM: t.DistanceM})
	}
	return out, nil
}

func (r *PGRepository) InsertPing(ctx context.Context, tripID, equipmentID uuid.UUID, p Point) (bool, error) {
	n, err := r.q(ctx).InsertLocationPing(ctx, db.InsertLocationPingParams{
		TripID: tripID, EquipmentID: equipmentID, RecordedAt: p.RecordedAt.UTC(), Lat: p.Lat, Lng: p.Lng,
		AccuracyM: p.AccuracyM, SpeedMps: p.SpeedMps, Heading: p.Heading, BatteryPct: p.BatteryPct,
	})
	return n == 1, database.MapError(err)
}

func (r *PGRepository) UpsertLast(ctx context.Context, equipmentID uuid.UUID, p Point) error {
	return database.MapError(r.q(ctx).UpsertLastLocation(ctx, db.UpsertLastLocationParams{
		EquipmentID: equipmentID, Lat: p.Lat, Lng: p.Lng, RecordedAt: p.RecordedAt.UTC(),
	}))
}

func (r *PGRepository) DistanceToSite(ctx context.Context, bookingID uuid.UUID, lat, lng float64) (float64, int32, error) {
	row, err := r.q(ctx).DistanceToSite(ctx, db.DistanceToSiteParams{BookingID: bookingID, Lat: lat, Lng: lng})
	return row.Meters, row.SiteGeofenceM, database.MapError(err)
}

// RecordArrival writes the enter_site event once; false if already recorded.
func (r *PGRepository) RecordArrival(ctx context.Context, bookingID uuid.UUID, p Point) (bool, error) {
	q := r.q(ctx)
	seen, err := q.HasGeofenceEvent(ctx, db.HasGeofenceEventParams{BookingID: bookingID, Event: db.GeofenceEventTypeEnterSite})
	if err != nil || seen {
		return false, database.MapError(err)
	}
	err = q.InsertGeofenceEvent(ctx, db.InsertGeofenceEventParams{
		BookingID: bookingID, Event: db.GeofenceEventTypeEnterSite, OccurredAt: p.RecordedAt.UTC(), Lat: p.Lat, Lng: p.Lng,
	})
	return err == nil, database.MapError(err)
}

func (r *PGRepository) Snapshot(ctx context.Context, bookingID uuid.UUID) (db.GetTrackingSnapshotRow, error) {
	row, err := r.q(ctx).GetTrackingSnapshot(ctx, bookingID)
	return row, database.MapError(err)
}

// ---- service -----------------------------------------------------------------------

// Pings far in the future (bad device clocks) are rejected.
const maxClockSkew = 5 * time.Minute

// trackable statuses accept pings.
var trackable = []string{bookings.StatusDispatched, bookings.StatusOnSite, bookings.StatusActive, bookings.StatusReturnInitiated}

type Service struct {
	repo     Repository
	bookings *bookings.Service
	store    cache.Store
	tx       database.Transactor
	now      func() time.Time

	// location features (WithLocation)
	pool       *pgxpool.Pool
	hub        *live.Hub
	tickets    *live.Tickets
	router     geo.Router
	scope      Scope
	principals PrincipalLoader
	opt        Options
	providers  map[string]TelematicsProvider
}

func NewService(repo Repository, bk *bookings.Service, store cache.Store, tx database.Transactor) *Service {
	return &Service{repo: repo, bookings: bk, store: store, tx: tx, now: time.Now}
}

// Ingest stores a batch of points for the booking's open trip.
func (s *Service) Ingest(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID, req BatchRequest) (BatchResult, error) {
	b, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return BatchResult{}, err
	}
	if !sharer(side, b) {
		return BatchResult{}, apperror.ErrForbidden.WithDetail("only the assigned operator (or the owner's team without an operator) shares location")
	}
	if !slices.Contains(trackableStatuses, b.Status) {
		return BatchResult{}, apperror.ErrInvalidTransition.WithDetail("location is shared from dispatch until return")
	}
	source := "app"
	if req.Simulated {
		if !s.opt.AllowSimulated {
			return BatchResult{}, apperror.ErrBusinessRule.WithDetail("simulated positions are not accepted here")
		}
		source = "simulated"
	}
	// Location is shared only under the sharer's recorded consent.
	if _, err := s.q(ctx).GetActiveConsent(ctx, db.GetActiveConsentParams{BookingID: b.ID, UserID: p.UserID}); err != nil {
		if errors.Is(database.MapError(err), apperror.ErrNotFound) {
			return BatchResult{}, apperror.New(403, "TRACKING_CONSENT_REQUIRED", "Start a tracking session first").
				WithDetail("read the tracking notice and start sharing (POST .../tracking/session)")
		}
		return BatchResult{}, database.MapError(err)
	}
	trip, equipmentID, err := s.repo.OpenTrip(ctx, bookingID)
	if errors.Is(err, apperror.ErrNotFound) {
		return BatchResult{}, apperror.ErrConflict.WithDetail("no trip is in progress for this booking")
	}
	if err != nil {
		return BatchResult{}, err
	}

	slices.SortFunc(req.Points, func(a, b Point) int { return a.RecordedAt.Compare(b.RecordedAt) })
	limit := s.now().Add(maxClockSkew)
	res := BatchResult{TripID: trip.ID.String()}
	var latest *Point
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		for i := range req.Points {
			pt := req.Points[i]
			if pt.RecordedAt.After(limit) {
				return apperror.ErrValidation.WithDetail("a point is timestamped in the future; check the device clock")
			}
			// Only what tracking needs is kept (no battery level).
			n, err := q.InsertSourcedPing(ctx, db.InsertSourcedPingParams{TripID: trip.ID, EquipmentID: equipmentID, RecordedAt: pt.RecordedAt.UTC(),
				Lat: pt.Lat, Lng: pt.Lng, AccuracyM: pt.AccuracyM, SpeedMps: pt.SpeedMps, Heading: pt.Heading, Source: source})
			if err != nil {
				return database.MapError(err)
			}
			if n == 1 {
				res.Accepted++
			} else {
				res.Duplicates++
			}
			latest = &req.Points[i]
		}
		// A tracker on the machine outranks a phone: a fresh tracker fix is
		// not overwritten by the operator's phone.
		if last, err := q.GetLastLocation(ctx, equipmentID); err == nil && last.Source == "telematics" && s.now().Sub(last.RecordedAt) < 10*time.Minute {
			return nil
		}
		return database.MapError(q.UpsertSourcedLastLocation(ctx, db.UpsertSourcedLastLocationParams{EquipmentID: equipmentID, Lat: latest.Lat,
			Lng: latest.Lng, RecordedAt: latest.RecordedAt.UTC(), Source: source, AccuracyM: latest.AccuracyM,
			BookingID: uuid.NullUUID{UUID: bookingID, Valid: true}}))
	})
	if err != nil {
		return BatchResult{}, err
	}
	if body, err := json.Marshal(Position{Lat: latest.Lat, Lng: latest.Lng, RecordedAt: latest.RecordedAt.UTC()}); err == nil {
		_ = s.store.Set(ctx, "live:equipment:"+equipmentID.String(), body, 10*time.Minute)
	}
	s.hub.Publish(live.Message{Type: "position", EquipmentID: equipmentID, BookingID: bookingID, Lat: latest.Lat, Lng: latest.Lng,
		AccuracyM: latest.AccuracyM, RecordedAt: latest.RecordedAt.UTC(), Source: source})

	if b.Status == bookings.StatusDispatched {
		meters, fence, err := s.repo.DistanceToSite(ctx, bookingID, latest.Lat, latest.Lng)
		if err != nil {
			return BatchResult{}, err
		}
		if meters <= float64(fence) {
			first, err := s.repo.RecordArrival(ctx, bookingID, *latest)
			if err != nil {
				return BatchResult{}, err
			}
			if first {
				if _, err := s.bookings.Act(ctx, p, bookingID, bookings.ActionArrive, bookings.ActionRequest{Reason: "geofence: arrived on site"}); err == nil {
					res.Arrived = true
				}
			}
		}
	}
	return res, nil
}

// StartTrip opens a trip (closing nothing; one open trip per machine).
func (s *Service) StartTrip(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID, kind string) (Trip, error) {
	b, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return Trip{}, err
	}
	if !side.Has(bookings.SideOperator | bookings.SideOwner) {
		return Trip{}, apperror.ErrForbidden
	}
	operator := uuid.NullUUID{}
	if b.Operator != nil {
		operator = uuid.NullUUID{UUID: b.Operator.UserID, Valid: true}
	}
	return s.repo.StartTrip(ctx, b.ID, b.EquipmentID, operator, kind)
}

// EndTrip closes the open trip and computes its distance from the pings.
func (s *Service) EndTrip(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID) (Trip, error) {
	_, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return Trip{}, err
	}
	if !side.Has(bookings.SideOperator | bookings.SideOwner) {
		return Trip{}, apperror.ErrForbidden
	}
	trip, _, err := s.repo.OpenTrip(ctx, bookingID)
	if err != nil {
		return Trip{}, err
	}
	return s.repo.EndTrip(ctx, trip.ID)
}

// Track returns the current position and trips (parties).
func (s *Service) Track(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID) (Snapshot, error) {
	b, _, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return Snapshot{}, err
	}
	row, err := s.repo.Snapshot(ctx, bookingID)
	if err != nil {
		return Snapshot{}, err
	}
	out := Snapshot{BookingID: b.ID, Status: string(row.Status), Site: Position{Lat: row.SiteLat, Lng: row.SiteLng}}
	if raw, err := s.store.Get(ctx, "live:equipment:"+b.EquipmentID.String()); err == nil {
		var live Position
		if json.Unmarshal(raw, &live) == nil {
			out.Position = &live
			m := DistanceMeters(live.Lat, live.Lng, row.SiteLat, row.SiteLng)
			out.MetersToSite = &m
		}
	}
	if out.Position == nil && row.HasPosition && row.RecordedAt != nil {
		out.Position = &Position{Lat: row.Lat, Lng: row.Lng, RecordedAt: *row.RecordedAt}
		m := row.MetersToSite
		out.MetersToSite = &m
	}
	if out.Trips, err = s.repo.Trips(ctx, bookingID); err != nil {
		return Snapshot{}, err
	}
	return out, nil
}

// ---- handler -----------------------------------------------------------------------

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Name() string { return "tracking" }

func (h *Handler) Routes(r *httpx.Router) {
	a := r.Tag("tracking").Authed()
	a.POST("/operator/jobs/:booking_id/tracking/batch", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req BatchRequest) (any, error) {
		id, err := httpx.Param(c, "booking_id")
		if err != nil {
			return nil, err
		}
		return h.svc.Ingest(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Upload location points (offline catch-up safe)", Request: BatchRequest{}, Response: BatchResult{},
		Description: "Duplicate points (same trip and timestamp) are ignored. Arriving inside the site geofence moves a dispatched booking to on_site."})
	a.POST("/bookings/:id/trips", httpx.HandleJSON(201, func(c *gin.Context, p *httpx.Principal, req StartTripRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.StartTrip(c.Request.Context(), p, id, req.Kind)
	}), httpx.Doc{Summary: "Start a trip", Request: StartTripRequest{}, Response: Trip{}, Status: 201})
	a.POST("/bookings/:id/trips/end", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.EndTrip(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "End the open trip", Response: Trip{}})
	a.GET("/bookings/:id/track", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.View(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Machine and operator positions, destination, route, ETA and sessions", Response: TrackView{},
		Description: "Customers see positions only during delivery and the rental (hidden otherwise). Every view is recorded in the location access log."})
	h.locationRoutes(r)
}

// DistanceMeters is the great-circle distance between two WGS84 points
// (haversine; within 0.5% of PostGIS geography distance at these ranges).
func DistanceMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371008.8 // mean Earth radius, metres
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat, dLng := rad(lat2-lat1), rad(lng2-lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(rad(lat1))*math.Cos(rad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Asin(math.Min(1, math.Sqrt(a)))
}
