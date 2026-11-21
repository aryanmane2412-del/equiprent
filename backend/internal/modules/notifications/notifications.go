// Package notifications stores in-app notifications, registers push
// devices, keeps per-category push preferences, and turns outbox domain
// events into notifications for the right people (the Dispatcher, run by the
// background worker). Push goes through a Pusher: Firebase Cloud Messaging
// when FCM credentials are configured, otherwise a logger.
package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	"github.com/equiprent/backend/internal/httpx"
)

// Notification is an in-app notification.
type Notification struct {
	ID        uuid.UUID       `json:"id"`
	Type      string          `json:"type"`
	Category  string          `json:"category"`
	Title     string          `json:"title"`
	Body      string          `json:"body"`
	Data      json.RawMessage `json:"data"`
	ReadAt    *time.Time      `json:"read_at,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type ListQuery struct {
	UnreadOnly bool   `form:"unread_only"`
	Limit      int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Cursor     string `form:"cursor"`
}

type Page struct {
	Data       []Notification `json:"data"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type UnreadCount struct {
	Unread int32 `json:"unread"`
}

type ReadAllResult struct {
	Marked int64 `json:"marked"`
}

type DeviceRequest struct {
	Platform   string  `json:"platform" binding:"required,oneof=android ios web"`
	PushToken  string  `json:"push_token" binding:"required,min=10,max=4096"`
	AppVersion *string `json:"app_version" binding:"omitempty,max=32"`
}

type UnregisterRequest struct {
	PushToken string `json:"push_token" binding:"required,min=10,max=4096"`
}

type DeviceResponse struct {
	ID uuid.UUID `json:"id"`
}

// Device is a registered push device (the token itself is never returned).
type Device struct {
	ID         uuid.UUID  `json:"id"`
	Platform   string     `json:"platform"`
	AppVersion *string    `json:"app_version,omitempty"`
	LastSeenAt time.Time  `json:"last_seen_at"`
	LastPushAt *time.Time `json:"last_push_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type DeviceList struct {
	Data []Device `json:"data"`
	// Push is "fcm" when the server can send push notifications, else "none".
	Push string `json:"push"`
}

// Categories group notification types for preferences.
const (
	CatBookings    = "bookings"
	CatPayments    = "payments"
	CatAgreements  = "agreements"
	CatChat        = "chat"
	CatMaintenance = "maintenance"
	CatReviews     = "reviews"
	CatAccount     = "account"
)

// AllCategories in display order.
var AllCategories = []string{CatBookings, CatPayments, CatAgreements, CatChat, CatMaintenance, CatReviews, CatAccount}

// Category maps a notification type to its preference category.
func Category(eventType string) string {
	switch {
	case eventType == events.BookingFinalCharges, strings.HasPrefix(eventType, "payment."),
		strings.HasPrefix(eventType, "payout."), strings.HasPrefix(eventType, "refund."), strings.HasPrefix(eventType, "deposit."):
		return CatPayments
	case strings.HasPrefix(eventType, "booking."), strings.HasPrefix(eventType, "dispute."), strings.HasPrefix(eventType, "issue."):
		return CatBookings
	case strings.HasPrefix(eventType, "agreement."):
		return CatAgreements
	case strings.HasPrefix(eventType, "chat."):
		return CatChat
	case strings.HasPrefix(eventType, "maintenance."), strings.HasPrefix(eventType, "document."):
		return CatMaintenance
	case strings.HasPrefix(eventType, "review."):
		return CatReviews
	}
	return CatAccount
}

// Preference is one category's push setting (in-app is always on).
type Preference struct {
	Category string `json:"category" binding:"required,oneof=bookings payments agreements chat maintenance reviews account"`
	Push     bool   `json:"push"`
}

type PreferencesRequest struct {
	Preferences []Preference `json:"preferences" binding:"required,min=1,max=7,dive"`
}

type PreferenceList struct {
	Data []Preference `json:"data"`
}

// Message is a notification to deliver.
type Message struct {
	Type  string
	Title string
	Body  string
	Data  map[string]any
	// PushOnly skips the in-app copy (chat messages: the chat itself is the record).
	PushOnly bool
}

// Pusher delivers push notifications (FCM in production).
type Pusher interface {
	Push(ctx context.Context, userID uuid.UUID, m Message) error
}

// LogPusher logs pushes instead of sending them (development).
type LogPusher struct{ Log *slog.Logger }

func (p LogPusher) Push(ctx context.Context, userID uuid.UUID, m Message) error {
	p.Log.DebugContext(ctx, "push notification (not sent: no push provider configured)", "user_id", userID, "type", m.Type)
	return nil
}

// ---- repository --------------------------------------------------------------------

type Repository interface {
	CreateMany(ctx context.Context, users []uuid.UUID, m Message) error
	// CreateForEvent stores one notification per user for an outbox event and returns
	// only the users newly notified (a replayed event inserts nothing).
	CreateForEvent(ctx context.Context, users []uuid.UUID, m Message, event uuid.UUID) ([]uuid.UUID, error)
	BookingContext(ctx context.Context, booking uuid.UUID) (BookingFacts, error)
	BookingOperators(ctx context.Context, booking uuid.UUID) ([]uuid.UUID, error)
	PlatformUsers(ctx context.Context, roles []string) ([]uuid.UUID, error)
	List(ctx context.Context, userID uuid.UUID, unreadOnly bool, k httpx.Keyset) ([]Notification, error)
	Unread(ctx context.Context, userID uuid.UUID) (int32, error)
	MarkRead(ctx context.Context, userID, id uuid.UUID) (bool, error)
	MarkAllRead(ctx context.Context, userID uuid.UUID) (int64, error)
	UpsertDevice(ctx context.Context, p db.UpsertDeviceParams) (uuid.UUID, error)
	DeleteDevice(ctx context.Context, userID uuid.UUID, token string) (bool, error)
	Devices(ctx context.Context, userID uuid.UUID) ([]Device, error)
	OrgUsers(ctx context.Context, org uuid.UUID, roles []string) ([]uuid.UUID, error)
	// OwnerRecipients is the owner side for one booking or machine: owners,
	// org admins and the fleet managers whose scope includes the machine.
	OwnerRecipients(ctx context.Context, org uuid.UUID, bookingID, equipmentID uuid.NullUUID) ([]uuid.UUID, error)
	Muted(ctx context.Context, userID uuid.UUID) ([]string, error)
	Preferences(ctx context.Context, userID uuid.UUID) (map[string]bool, error)
	SetPreferences(ctx context.Context, userID uuid.UUID, prefs []Preference) error
}

type PGRepository struct {
	pool *pgxpool.Pool
	tx   database.Transactor
}

func NewRepository(pool *pgxpool.Pool, tx database.Transactor) *PGRepository {
	return &PGRepository{pool: pool, tx: tx}
}

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

func (r *PGRepository) CreateMany(ctx context.Context, users []uuid.UUID, m Message) error {
	data := []byte("{}")
	if m.Data != nil {
		data, _ = json.Marshal(m.Data)
	}
	return database.MapError(r.q(ctx).CreateNotificationForUsers(ctx, db.CreateNotificationForUsersParams{
		UserIds: users, Type: m.Type, Title: m.Title, Body: m.Body, Data: data, Channels: []string{"in_app", "push"},
	}))
}

func (r *PGRepository) CreateForEvent(ctx context.Context, users []uuid.UUID, m Message, event uuid.UUID) ([]uuid.UUID, error) {
	data := []byte("{}")
	if m.Data != nil {
		data, _ = json.Marshal(m.Data)
	}
	rows, err := r.q(ctx).CreateEventNotifications(ctx, db.CreateEventNotificationsParams{
		UserIds: users, Type: m.Type, Title: m.Title, Body: m.Body, Data: data, Channels: []string{"in_app", "push"},
		EventID: uuid.NullUUID{UUID: event, Valid: true},
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.UserID)
	}
	return out, nil
}

// BookingFacts is the wording context for booking notifications.
type BookingFacts struct{ Equipment, Customer, Site, Start string }

func (r *PGRepository) BookingContext(ctx context.Context, booking uuid.UUID) (BookingFacts, error) {
	b, err := r.q(ctx).NotificationBookingContext(ctx, booking)
	if err != nil {
		return BookingFacts{}, database.MapError(err)
	}
	return BookingFacts{Equipment: b.EquipmentTitle, Customer: b.CustomerName, Site: b.SiteAddress, Start: b.StartsAt.Format("2 Jan 2006")}, nil
}

func (r *PGRepository) BookingOperators(ctx context.Context, booking uuid.UUID) ([]uuid.UUID, error) {
	ids, err := r.q(ctx).BookingActiveOperators(ctx, booking)
	return ids, database.MapError(err)
}

func (r *PGRepository) PlatformUsers(ctx context.Context, roles []string) ([]uuid.UUID, error) {
	ids, err := r.q(ctx).PlatformUsersWithRoles(ctx, roles)
	return ids, database.MapError(err)
}

func (r *PGRepository) List(ctx context.Context, userID uuid.UUID, unreadOnly bool, k httpx.Keyset) ([]Notification, error) {
	rows, err := r.q(ctx).ListNotifications(ctx, db.ListNotificationsParams{UserID: userID, UnreadOnly: unreadOnly, After: k.After, ResultLimit: k.Fetch()})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Notification, 0, len(rows))
	for _, n := range rows {
		out = append(out, Notification{ID: n.ID, Type: n.Type, Category: Category(n.Type), Title: n.Title, Body: n.Body,
			Data: n.Data, ReadAt: n.ReadAt, CreatedAt: n.CreatedAt})
	}
	return out, nil
}

func (r *PGRepository) Unread(ctx context.Context, userID uuid.UUID) (int32, error) {
	n, err := r.q(ctx).CountUnreadNotifications(ctx, userID)
	return n, database.MapError(err)
}

func (r *PGRepository) MarkRead(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	n, err := r.q(ctx).MarkNotificationRead(ctx, db.MarkNotificationReadParams{ID: id, UserID: userID})
	return n == 1, database.MapError(err)
}

func (r *PGRepository) MarkAllRead(ctx context.Context, userID uuid.UUID) (int64, error) {
	n, err := r.q(ctx).MarkAllNotificationsRead(ctx, userID)
	return n, database.MapError(err)
}

func (r *PGRepository) UpsertDevice(ctx context.Context, p db.UpsertDeviceParams) (uuid.UUID, error) {
	id, err := r.q(ctx).UpsertDevice(ctx, p)
	return id, database.MapError(err)
}

func (r *PGRepository) DeleteDevice(ctx context.Context, userID uuid.UUID, token string) (bool, error) {
	n, err := r.q(ctx).DeleteDevice(ctx, db.DeleteDeviceParams{UserID: userID, PushToken: token})
	return n > 0, database.MapError(err)
}

func (r *PGRepository) Devices(ctx context.Context, userID uuid.UUID) ([]Device, error) {
	rows, err := r.q(ctx).ListUserDevices(ctx, userID)
	out := make([]Device, 0, len(rows))
	for _, d := range rows {
		out = append(out, Device{ID: d.ID, Platform: d.Platform, AppVersion: d.AppVersion, LastSeenAt: d.LastSeenAt,
			LastPushAt: d.LastPushAt, CreatedAt: d.CreatedAt})
	}
	return out, database.MapError(err)
}

func (r *PGRepository) OrgUsers(ctx context.Context, org uuid.UUID, roles []string) ([]uuid.UUID, error) {
	ids, err := r.q(ctx).ListOrgUserIDsWithRoles(ctx, db.ListOrgUserIDsWithRolesParams{
		OrganizationID: uuid.NullUUID{UUID: org, Valid: true}, Roles: roles,
	})
	return ids, database.MapError(err)
}

func (r *PGRepository) OwnerRecipients(ctx context.Context, org uuid.UUID, bookingID, equipmentID uuid.NullUUID) ([]uuid.UUID, error) {
	ids, err := r.q(ctx).OwnerSideRecipients(ctx, db.OwnerSideRecipientsParams{OrgID: uuid.NullUUID{UUID: org, Valid: true}, BookingID: bookingID, EquipmentID: equipmentID})
	return ids, database.MapError(err)
}

func (r *PGRepository) Muted(ctx context.Context, userID uuid.UUID) ([]string, error) {
	cats, err := r.q(ctx).PushMutedCategories(ctx, userID)
	return cats, database.MapError(err)
}

func (r *PGRepository) Preferences(ctx context.Context, userID uuid.UUID) (map[string]bool, error) {
	rows, err := r.q(ctx).ListNotificationPreferences(ctx, userID)
	out := map[string]bool{}
	for _, p := range rows {
		out[p.Category] = p.Push
	}
	return out, database.MapError(err)
}

func (r *PGRepository) SetPreferences(ctx context.Context, userID uuid.UUID, prefs []Preference) error {
	return database.MapError(r.tx.InTx(ctx, func(ctx context.Context) error {
		for _, p := range prefs {
			if err := r.q(ctx).UpsertNotificationPreference(ctx, db.UpsertNotificationPreferenceParams{
				UserID: userID, Category: p.Category, Push: p.Push,
			}); err != nil {
				return err
			}
		}
		return nil
	}))
}

// PushTokens, MarkPushed, MarkFailed and DropToken make the repository the
// FCM sender's DeviceStore.
func (r *PGRepository) PushTokens(ctx context.Context, userID uuid.UUID) ([]string, error) {
	rows, err := r.q(ctx).ListUserPushTokens(ctx, userID)
	out := make([]string, 0, len(rows))
	for _, d := range rows {
		out = append(out, d.PushToken)
	}
	return out, database.MapError(err)
}

func (r *PGRepository) MarkPushed(ctx context.Context, token string) error {
	return database.MapError(r.q(ctx).MarkDevicePushed(ctx, token))
}

func (r *PGRepository) MarkFailed(ctx context.Context, token, reason string) error {
	return database.MapError(r.q(ctx).MarkDevicePushFailed(ctx, db.MarkDevicePushFailedParams{PushToken: token, LastError: &reason}))
}

func (r *PGRepository) DropToken(ctx context.Context, token string) error {
	_, err := r.q(ctx).DeletePushToken(ctx, token)
	return database.MapError(err)
}

// ---- service -----------------------------------------------------------------------

type Service struct {
	repo     Repository
	pusher   Pusher
	log      *slog.Logger
	pushName string
}

func NewService(repo Repository, pusher Pusher, log *slog.Logger) *Service {
	name := "none"
	if _, ok := pusher.(*FCMPusher); ok {
		name = "fcm"
	}
	return &Service{repo: repo, pusher: pusher, log: log, pushName: name}
}

// Notify stores the in-app copy for every user (unless push-only) and pushes
// to those who have not muted the category. Push failures are logged, never
// fatal: the in-app copy is the record.
func (s *Service) Notify(ctx context.Context, users []uuid.UUID, m Message) error {
	if len(users) == 0 {
		return nil
	}
	if !m.PushOnly {
		if ev, ok := events.EventIDFrom(ctx); ok {
			// A replayed outbox event notifies (and pushes to) nobody twice.
			fresh, err := s.repo.CreateForEvent(ctx, users, m, ev)
			if err != nil {
				return err
			}
			users = fresh
		} else if err := s.repo.CreateMany(ctx, users, m); err != nil {
			return err
		}
	}
	cat := Category(m.Type)
	for _, u := range users {
		muted, err := s.repo.Muted(ctx, u)
		if err != nil {
			return err
		}
		if slices.Contains(muted, cat) {
			continue
		}
		if err := s.pusher.Push(ctx, u, m); err != nil {
			s.log.WarnContext(ctx, "push failed", "user_id", u, "type", m.Type, "error", err)
		}
	}
	return nil
}

func (s *Service) List(ctx context.Context, p *httpx.Principal, q ListQuery) (Page, error) {
	k, err := httpx.PageQuery{Limit: q.Limit, Cursor: q.Cursor}.Keyset()
	if err != nil {
		return Page{}, err
	}
	rows, err := s.repo.List(ctx, p.UserID, q.UnreadOnly, k)
	if err != nil {
		return Page{}, err
	}
	pg := httpx.Paginate(rows, k, func(n Notification) uuid.UUID { return n.ID })
	return Page{Data: pg.Data, NextCursor: pg.NextCursor}, nil
}

func (s *Service) Unread(ctx context.Context, p *httpx.Principal) (UnreadCount, error) {
	n, err := s.repo.Unread(ctx, p.UserID)
	return UnreadCount{Unread: n}, err
}

func (s *Service) MarkRead(ctx context.Context, p *httpx.Principal, id uuid.UUID) error {
	ok, err := s.repo.MarkRead(ctx, p.UserID, id)
	if err == nil && !ok {
		return apperror.ErrNotFound.WithDetail("no unread notification with this id")
	}
	return err
}

func (s *Service) MarkAllRead(ctx context.Context, p *httpx.Principal) (ReadAllResult, error) {
	n, err := s.repo.MarkAllRead(ctx, p.UserID)
	return ReadAllResult{Marked: n}, err
}

func (s *Service) RegisterDevice(ctx context.Context, p *httpx.Principal, req DeviceRequest) (DeviceResponse, error) {
	id, err := s.repo.UpsertDevice(ctx, db.UpsertDeviceParams{UserID: p.UserID, Platform: req.Platform, PushToken: req.PushToken, AppVersion: req.AppVersion})
	return DeviceResponse{ID: id}, err
}

func (s *Service) UnregisterDevice(ctx context.Context, p *httpx.Principal, token string) error {
	_, err := s.repo.DeleteDevice(ctx, p.UserID, token)
	return err
}

func (s *Service) Devices(ctx context.Context, p *httpx.Principal) (DeviceList, error) {
	list, err := s.repo.Devices(ctx, p.UserID)
	return DeviceList{Data: list, Push: s.pushName}, err
}

func (s *Service) Preferences(ctx context.Context, p *httpx.Principal) (PreferenceList, error) {
	stored, err := s.repo.Preferences(ctx, p.UserID)
	if err != nil {
		return PreferenceList{}, err
	}
	out := make([]Preference, 0, len(AllCategories))
	for _, c := range AllCategories {
		push, ok := stored[c]
		out = append(out, Preference{Category: c, Push: push || !ok})
	}
	return PreferenceList{Data: out}, nil
}

func (s *Service) SetPreferences(ctx context.Context, p *httpx.Principal, req PreferencesRequest) (PreferenceList, error) {
	if err := s.repo.SetPreferences(ctx, p.UserID, req.Preferences); err != nil {
		return PreferenceList{}, err
	}
	return s.Preferences(ctx, p)
}

// ---- outbox dispatcher -------------------------------------------------------------

var (
	customerSide = []string{httpx.RoleCustomer, httpx.RoleOrgAdmin}
)

// subjectOf finds the booking or machine an event is about (booking events
// are keyed by the booking; others carry booking_id / equipment_id).
func subjectOf(eventType string, aggregateID uuid.UUID, p events.Payload) (booking, equipment uuid.NullUUID) {
	if strings.HasPrefix(eventType, "booking.") && aggregateID != uuid.Nil {
		return uuid.NullUUID{UUID: aggregateID, Valid: true}, uuid.NullUUID{}
	}
	get := func(k string) uuid.NullUUID {
		if v, ok := p.Extra[k]; ok {
			if id, err := uuid.Parse(fmt.Sprint(v)); err == nil {
				return uuid.NullUUID{UUID: id, Valid: true}
			}
		}
		return uuid.NullUUID{}
	}
	return get("booking_id"), get("equipment_id")
}

// text is the wording for one audience. {ref} is the booking reference (or
// the payload title), {title} the payload title, {amount} the amount in the
// payload (amount_paise or final_total_paise), {name} the actor name.
type text struct{ title, body string }

// route says who hears about an event and what they read. owner, when set,
// is the wording for the owner side.
type route struct {
	toOwner, toCustomer, toUsers bool
	text
	owner    *text
	pushOnly bool
	// notifyActor also tells the person who caused the event (e.g. "booking submitted").
	notifyActor bool
	// operator tells the driver(s) currently assigned to the booking.
	operator *text
	// admin tells platform admins holding one of adminRoles.
	admin      *text
	adminRoles []string
}

var (
	adminOps     = []string{httpx.RoleAdminSuper, httpx.RoleAdminOps}
	adminKYC     = []string{httpx.RoleAdminSuper, httpx.RoleAdminKYC}
	adminFinance = []string{httpx.RoleAdminSuper, httpx.RoleAdminFinance}
)

var routes = map[string]route{
	events.BookingRequested: {toOwner: true, toCustomer: true, notifyActor: true, text: text{"Booking submitted", "Your request for {equipment} ({ref}) was sent to the owner."},
		owner: &text{"New booking request", "{customer} requested {equipment} ({ref}) at {site}. Respond before it expires."}},
	events.BookingAccepted:         {toCustomer: true, text: text{"Booking accepted", "{ref} was accepted by the owner."}},
	events.BookingRejected:         {toCustomer: true, text: text{"Booking declined", "{ref} was declined by the owner."}},
	events.BookingConfirmed:        {toOwner: true, toCustomer: true, text: text{"Booking confirmed", "{ref} is confirmed."}},
	events.BookingCancelled:        {toOwner: true, toCustomer: true, text: text{"Booking cancelled", "{ref} was cancelled."}, operator: &text{"Job cancelled", "The job for {equipment} ({ref}) was cancelled."}},
	events.BookingExpired:          {toOwner: true, toCustomer: true, text: text{"Request expired", "{ref} expired without a response."}},
	events.BookingOperatorAssigned: {toCustomer: true, text: text{"Operator assigned", "An operator is assigned to {ref}."}},
	events.BookingJobAssigned:      {toUsers: true, text: text{"New job assignment", "{equipment} ({ref}) at {site}, starting {start}. Open it to accept or reject."}},
	events.DeliveryPhotoSubmitted:  {toOwner: true, text: text{"Delivery photos added", "The customer added delivery photos for {equipment} ({ref}). Review and approve them."}},
	events.DeliveryPhotoApproved:   {toCustomer: true, text: text{"Delivery photos approved", "The owner approved your delivery photos for {equipment} ({ref})."}},
	events.BookingJobUnassigned:    {toUsers: true, text: text{"Job changed", "You are no longer assigned to {equipment} ({ref})."}},
	events.BookingOperatorAccepted: {toOwner: true, text: text{"Driver accepted", "The assigned driver accepted the job for {ref}."}},
	events.BookingOperatorDeclined: {toOwner: true, text: text{"Driver rejected the job", "The assigned driver rejected the job for {ref}. Assign another driver."}},
	events.BookingDispatched:       {toCustomer: true, text: text{"Machine on the way", "The machine for {ref} has left the yard."}},
	events.BookingOnSite:           {toCustomer: true, toOwner: true, text: text{"Machine arrived", "The machine for {ref} is on site."}},
	events.BookingActive: {toOwner: true, toCustomer: true, text: text{"Rental started", "{ref} is now active. The rental clock is running."},
		owner: &text{"Rental started", "Handover done for {ref}; the rental is active."}},
	events.BookingReturnInitiated: {toOwner: true, text: text{"Return requested", "The customer released the machine for {ref}."}},
	events.BookingReturned:        {toCustomer: true, text: text{"Machine returned", "{ref}: the machine is back with the owner."}},
	events.BookingCompleted: {toCustomer: true, toOwner: true, text: text{"Rental completed", "{ref} is complete. Rate the machine and the owner."},
		owner: &text{"Rental completed", "{ref} is complete. Rate the customer."}},
	events.BookingFinalCharges:          {toCustomer: true, text: text{"Final bill ready", "{ref}: the final amount is {amount}. Any balance is due now."}},
	events.BookingCancellationRequested: {toOwner: true, text: text{"Cancellation requested", "The customer asked to cancel {ref}. Approve or decline it."}},
	events.BookingCancellationDeclined:  {toCustomer: true, text: text{"Cancellation declined", "The owner declined cancelling {ref}; the booking continues."}},
	events.BookingExtensionRequested: {toOwner: true, toCustomer: true, text: text{"Extension requested", "The owner proposed extending {ref} (extra {amount}). Approve or decline it."},
		owner: &text{"Extension requested", "The customer asked to extend {ref} (extra {amount}). Approve or decline it."}},
	events.BookingExtensionApproved: {toOwner: true, toCustomer: true, text: text{"Extension approved", "{ref} is extended. Extra charge: {amount}."}, operator: &text{"Job changed", "The job for {equipment} ({ref}) was extended."}},
	events.BookingExtensionDeclined: {toOwner: true, toCustomer: true, text: text{"Extension declined", "The extension of {ref} was declined; the end date stays the same."}},
	events.BookingExtensionExpired:  {toOwner: true, toCustomer: true, text: text{"Extension request lapsed", "Nobody answered the extension of {ref} before it ended."}},
	events.BookingPaymentExpired: {toOwner: true, toCustomer: true, text: text{"Booking expired", "{ref} was not paid in time, so the dates were released."},
		owner: &text{"Booking expired", "The customer did not pay for {ref} in time; the dates are free again."}},
	events.BookingRefundPending: {toCustomer: true, text: text{"Refund on its way", "{amount} will be refunded for {ref}."}},
	events.BookingRefunded:      {toCustomer: true, toOwner: true, text: text{"Refund complete", "Every refund for {ref} has been processed."}},
	events.PaymentFailed: {toCustomer: true, text: text{"Payment failed", "Your payment for {ref} did not go through. Please try again before the deadline."},
		admin: &text{"Payment failed", "A customer payment for {ref} failed."}, adminRoles: adminFinance},
	events.PaymentCaptured: {toCustomer: true, toOwner: true, text: text{"Payment confirmed", "We received {amount} for {ref}. Thank you!"},
		owner: &text{"Payment received", "The customer paid {amount} for {ref}."}},
	events.PayoutPaid:            {toOwner: true, text: text{"Payment settled", "{amount} was settled to your bank account for {ref}."}},
	events.RefundProcessed:       {toCustomer: true, text: text{"Refund processed", "{amount} was refunded for {ref}."}},
	events.DepositReleased:       {toCustomer: true, text: text{"Security deposit refund started", "{amount} of your security deposit for {ref} is being refunded. Any deduction was for an approved damage claim."}},
	events.AgreementSigned:       {toOwner: true, toCustomer: true, text: text{"Agreement signed", "{name} signed the rental agreement for {ref}."}},
	events.AgreementExecuted:     {toOwner: true, toCustomer: true, text: text{"Agreement complete", "Both parties signed the agreement for {ref}. The PDF is ready."}},
	events.ChatMessage:           {toUsers: true, pushOnly: true, text: text{"{name}", "{title}"}},
	events.DisputeOpened:         {toOwner: true, toCustomer: true, text: text{"Dispute opened", "A dispute was raised on {ref}."}, admin: &text{"Dispute opened", "A dispute was raised on {ref}."}, adminRoles: adminOps},
	events.OwnerRegistered:       {admin: &text{"New equipment owner", "{title} registered as an equipment owner."}, adminRoles: adminOps},
	events.VerificationSubmitted: {admin: &text{"Verification submitted", "{title}"}, adminRoles: adminKYC},
	events.DisputeResolved:       {toOwner: true, toCustomer: true, text: text{"Dispute resolved", "The dispute on {ref} was resolved."}},
	events.MaintenanceDue:        {toOwner: true, text: text{"Maintenance due", "{title}"}},
	events.MaintenanceReminder:   {toOwner: true, text: text{"Maintenance coming up", "{title}"}},
	events.MaintenanceOverdue:    {toOwner: true, text: text{"Maintenance overdue", "{title}"}},
	events.DocumentExpiring:      {toOwner: true, text: text{"Document expiring", "{title}"}},
	events.ReviewReceived:        {toOwner: true, toCustomer: true, toUsers: true, text: text{"New review", "{title}"}},
	events.IssueReported:         {toOwner: true, text: text{"Issue reported", "{title} on {ref}. Review it in the booking."}},
	events.IssueReviewed:         {toOwner: true, toUsers: true, text: text{"Issue reviewed", "EQUIPRENT reviewed the issue on {ref}: {title}."}},
	events.VerificationApproved:  {toOwner: true, toUsers: true, text: text{"Verification approved", "{title}"}},
	events.VerificationRejected:  {toOwner: true, toUsers: true, text: text{"Verification needs attention", "{title}"}},
}

// INR formats paise as rupees with Indian digit grouping: ₹1,23,456.50.
func INR(paise int64) string {
	neg := paise < 0
	if neg {
		paise = -paise
	}
	whole, frac := paise/100, paise%100
	digits := fmt.Sprint(whole)
	var groups []string
	if len(digits) > 3 {
		head, tail := digits[:len(digits)-3], digits[len(digits)-3:]
		for len(head) > 2 {
			groups = append([]string{head[len(head)-2:]}, groups...)
			head = head[:len(head)-2]
		}
		groups = append([]string{head}, groups...)
		digits = strings.Join(groups, ",") + "," + tail
	}
	out := fmt.Sprintf("₹%s.%02d", digits, frac)
	if neg {
		out = "-" + out
	}
	return out
}

func fill(t text, p events.Payload, f BookingFacts) text {
	ref := p.Reference
	if ref == "" {
		ref = p.Title
	}
	amount := ""
	for _, k := range []string{"amount_paise", "final_total_paise"} {
		if v, ok := p.Extra[k]; ok {
			if f, ok := v.(float64); ok {
				amount = INR(int64(f))
			}
		}
	}
	if amount == "" {
		amount = "the amount due"
	}
	name, _ := p.Extra["actor_name"].(string)
	if name == "" {
		name = "The other party"
	}
	dash := func(s string) string {
		if s == "" {
			return "the booked site"
		}
		return s
	}
	r := strings.NewReplacer("{ref}", ref, "{title}", p.Title, "{amount}", amount, "{name}", name,
		"{equipment}", dash2(f.Equipment, p.Title), "{customer}", dash2(f.Customer, "A customer"), "{site}", dash(f.Site), "{start}", dash2(f.Start, "the booked date"))
	return text{title: r.Replace(t.title), body: r.Replace(t.body)}
}

func dash2(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// Dispatch turns one outbox event into notifications. Unknown event types
// are acknowledged without notifying anyone.
func (s *Service) Dispatch(ctx context.Context, eventType string, aggregateID uuid.UUID, raw json.RawMessage) (int, error) {
	rt, ok := routes[eventType]
	if !ok {
		return 0, nil
	}
	var p events.Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return 0, err
	}
	seen := map[uuid.UUID]bool{p.ActorUserID: !rt.notifyActor, uuid.Nil: true}
	var facts BookingFacts
	if strings.HasPrefix(eventType, "booking.") && aggregateID != uuid.Nil {
		if f, err := s.repo.BookingContext(ctx, aggregateID); err == nil {
			facts = f
		}
	}
	pick := func(ids []uuid.UUID) []uuid.UUID {
		var out []uuid.UUID
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
		return out
	}
	var owners, others []uuid.UUID
	if rt.toOwner && p.OwnerOrgID != uuid.Nil {
		// Fleet managers hear only about machines they manage.
		bookingID, equipmentID := subjectOf(eventType, aggregateID, p)
		var ids []uuid.UUID
		var err error
		if bookingID.Valid || equipmentID.Valid {
			ids, err = s.repo.OwnerRecipients(ctx, p.OwnerOrgID, bookingID, equipmentID)
		} else {
			ids, err = s.repo.OrgUsers(ctx, p.OwnerOrgID, []string{httpx.RoleOwner, httpx.RoleOrgAdmin})
		}
		if err != nil {
			return 0, err
		}
		owners = pick(ids)
	}
	if rt.toCustomer && p.CustomerOrgID != uuid.Nil {
		ids, err := s.repo.OrgUsers(ctx, p.CustomerOrgID, customerSide)
		if err != nil {
			return 0, err
		}
		others = pick(ids)
	}
	if rt.toUsers || len(p.UserIDs) > 0 {
		others = append(others, pick(p.UserIDs)...)
	}
	var drivers, admins []uuid.UUID
	if rt.operator != nil && strings.HasPrefix(eventType, "booking.") {
		ids, err := s.repo.BookingOperators(ctx, aggregateID)
		if err != nil {
			return 0, err
		}
		drivers = pick(ids)
	}
	if rt.admin != nil {
		ids, err := s.repo.PlatformUsers(ctx, rt.adminRoles)
		if err != nil {
			return 0, err
		}
		admins = pick(ids)
	}

	data := map[string]any{"event": eventType, "id": aggregateID}
	if strings.HasPrefix(eventType, "booking.") {
		data["booking_id"] = aggregateID
	}
	for k, v := range p.Extra {
		if k != "actor_name" {
			data[k] = v
		}
	}
	send := func(users []uuid.UUID, t text) error {
		t = fill(t, p, facts)
		return s.Notify(ctx, users, Message{Type: eventType, Title: t.title, Body: t.body, Data: data, PushOnly: rt.pushOnly})
	}
	ownerText := rt.text
	if rt.owner != nil {
		ownerText = *rt.owner
	}
	if err := send(owners, ownerText); err != nil {
		return 0, err
	}
	if len(others) > 0 {
		if err := send(others, rt.text); err != nil {
			return 0, err
		}
	}
	if rt.operator != nil {
		if err := send(drivers, *rt.operator); err != nil {
			return 0, err
		}
	}
	if rt.admin != nil {
		if err := send(admins, *rt.admin); err != nil {
			return 0, err
		}
	}
	return len(owners) + len(others) + len(drivers) + len(admins), nil
}

// ---- handler -----------------------------------------------------------------------

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Name() string { return "notifications" }

func (h *Handler) Routes(r *httpx.Router) {
	a := r.Tag("notifications").Authed()
	a.GET("/notifications", httpx.HandleQuery(200, func(c *gin.Context, p *httpx.Principal, q ListQuery) (any, error) {
		return h.svc.List(c.Request.Context(), p, q)
	}), httpx.Doc{Summary: "My notifications", Query: ListQuery{}, Response: Page{}})
	a.GET("/notifications/unread-count", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		return h.svc.Unread(c.Request.Context(), p)
	}), httpx.Doc{Summary: "Unread badge count", Response: UnreadCount{}})
	a.POST("/notifications/:id/read", httpx.Handle(204, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return nil, h.svc.MarkRead(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Mark one as read"})
	a.POST("/notifications/read-all", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		return h.svc.MarkAllRead(c.Request.Context(), p)
	}), httpx.Doc{Summary: "Mark all as read", Response: ReadAllResult{}})
	a.GET("/me/notification-preferences", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		return h.svc.Preferences(c.Request.Context(), p)
	}), httpx.Doc{Summary: "Push preferences per category", Response: PreferenceList{}})
	a.PUT("/me/notification-preferences", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req PreferencesRequest) (any, error) {
		return h.svc.SetPreferences(c.Request.Context(), p, req)
	}), httpx.Doc{Summary: "Switch push on or off per category", Request: PreferencesRequest{}, Response: PreferenceList{}})
	a.GET("/me/devices", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		return h.svc.Devices(c.Request.Context(), p)
	}), httpx.Doc{Summary: "My push devices", Response: DeviceList{}})
	a.POST("/me/devices", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req DeviceRequest) (any, error) {
		return h.svc.RegisterDevice(c.Request.Context(), p, req)
	}), httpx.Doc{Summary: "Register a push token (FCM registration token)", Request: DeviceRequest{}, Response: DeviceResponse{}})
	a.POST("/me/devices/unregister", httpx.HandleJSON(204, func(c *gin.Context, p *httpx.Principal, req UnregisterRequest) (any, error) {
		return nil, h.svc.UnregisterDevice(c.Request.Context(), p, req.PushToken)
	}), httpx.Doc{Summary: "Remove a push token", Request: UnregisterRequest{}})
}
