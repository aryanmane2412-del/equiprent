// Package chat provides conversations between customers and owners:
//   - booking chats (customer, owner side and the operator; admins join
//     dispute threads), opened from a booking;
//   - listing enquiries, a direct conversation between a customer
//     organisation and a machine's owner before any booking exists.
//
// Access is re-checked on every request against the booking or the
// organisations, so people who leave an organisation lose access. Messages
// carry text and/or one photo (an upload with purpose chat_image, re-encoded
// to drop EXIF data and returned through a short-lived signed link). New
// messages are announced by push (outbox event chat.message); clients poll
// with ?after= while a thread is open.
package chat

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

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

// DeleteWindow is how long a sender may remove their own message.
const DeleteWindow = 15 * time.Minute

type Conversation struct {
	ID               uuid.UUID     `json:"id"`
	Kind             string        `json:"kind" doc:"booking | dispute | direct (listing enquiry) | support"`
	BookingID        uuid.NullUUID `json:"booking_id"`
	EquipmentID      uuid.NullUUID `json:"equipment_id"`
	Subject          *string       `json:"subject,omitempty"`
	UpdatedAt        time.Time     `json:"updated_at"`
	LastMessage      string        `json:"last_message,omitempty"`
	Unread           int32         `json:"unread"`
	BookingReference string        `json:"booking_reference,omitempty"`
	BookingStatus    string        `json:"booking_status,omitempty"`
}

// Attachment is a photo in a message, readable through URL until ExpiresAt.
type Attachment struct {
	ID          uuid.UUID `json:"id"`
	ContentType string    `json:"content_type"`
	URL         string    `json:"url"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type Message struct {
	ID             uuid.UUID     `json:"id"`
	ConversationID uuid.UUID     `json:"conversation_id"`
	SenderUserID   uuid.NullUUID `json:"sender_user_id" doc:"null for system messages"`
	SenderName     *string       `json:"sender_name,omitempty"`
	Mine           bool          `json:"mine"`
	Body           *string       `json:"body,omitempty"`
	Attachment     *Attachment   `json:"attachment,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	Deleted        bool          `json:"deleted"`
}

type SendRequest struct {
	Body               *string       `json:"body" binding:"omitempty,max=4000"`
	AttachmentUploadID uuid.NullUUID `json:"attachment_upload_id" doc:"An upload with purpose chat_image"`
	ClientID           uuid.NullUUID `json:"client_id" doc:"Client-generated UUID; resends return the same message"`
}

type EnquiryRequest struct {
	Message *string `json:"message" binding:"omitempty,max=4000" doc:"Optional first message"`
}

type MessagesQuery struct {
	Before string `form:"before" binding:"omitempty,uuid" doc:"Older than this message id (history), newest first"`
	After  string `form:"after" binding:"omitempty,uuid" doc:"Newer than this message id (polling), oldest first"`
	Limit  int    `form:"limit" binding:"omitempty,min=1,max=100"`
}

type ConversationList struct {
	Data []Conversation `json:"data"`
}

type MessageList struct {
	Data []Message `json:"data"`
}

type UnreadCount struct {
	Unread int32 `json:"unread"`
}

// ---- repository --------------------------------------------------------------------

type Repository interface {
	Get(ctx context.Context, id uuid.UUID) (db.Conversation, error)
	BookingConversation(ctx context.Context, bookingID uuid.UUID) (db.Conversation, error)
	CreateBookingConversation(ctx context.Context, b bookings.Booking) (db.Conversation, error)
	SupportConversationForUser(ctx context.Context, userID uuid.UUID) (db.Conversation, error)
	SupportAgentIDs(ctx context.Context) ([]uuid.UUID, error)
	CreateSupportConversation(ctx context.Context, requester uuid.UUID, agents []uuid.UUID) (db.Conversation, error)
	Enquiry(ctx context.Context, equipmentID, customerOrg uuid.UUID) (db.Conversation, error)
	CreateEnquiry(ctx context.Context, p db.CreateEnquiryConversationParams) (db.Conversation, error)
	Equipment(ctx context.Context, id uuid.UUID) (db.EquipmentForEnquiryRow, error)
	HasOrgRole(ctx context.Context, userID, org uuid.UUID, roles []string) (bool, error)
	AddParticipant(ctx context.Context, convID, userID uuid.UUID, org uuid.NullUUID) error
	Participants(ctx context.Context, convID uuid.UUID) ([]uuid.UUID, error)
	ForUser(ctx context.Context, userID uuid.UUID) ([]Conversation, error)
	UnreadTotal(ctx context.Context, userID uuid.UUID) (int32, error)
	Messages(ctx context.Context, p db.ListChatMessagesParams) ([]db.ListChatMessagesRow, error)
	Message(ctx context.Context, convID, id uuid.UUID) (db.GetChatMessageRow, error)
	Send(ctx context.Context, p db.CreateChatMessageParams) (db.CreateChatMessageRow, error)
	Delete(ctx context.Context, convID, id, sender uuid.UUID) (bool, error)
	MarkRead(ctx context.Context, convID, userID uuid.UUID) error
	UserName(ctx context.Context, userID uuid.UUID) string
	Emit(ctx context.Context, convID uuid.UUID, p events.Payload) error
}

type PGRepository struct {
	pool *pgxpool.Pool
	tx   database.Transactor
}

func NewRepository(pool *pgxpool.Pool, tx database.Transactor) *PGRepository {
	return &PGRepository{pool: pool, tx: tx}
}

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

func (r *PGRepository) Get(ctx context.Context, id uuid.UUID) (db.Conversation, error) {
	c, err := r.q(ctx).GetConversation(ctx, id)
	return c, database.MapError(err)
}

func (r *PGRepository) BookingConversation(ctx context.Context, bookingID uuid.UUID) (db.Conversation, error) {
	c, err := r.q(ctx).GetBookingConversation(ctx, uuid.NullUUID{UUID: bookingID, Valid: true})
	return c, database.MapError(err)
}

func (r *PGRepository) SupportConversationForUser(ctx context.Context, userID uuid.UUID) (db.Conversation, error) {
	c, err := r.q(ctx).GetSupportConversationForUser(ctx, userID)
	return c, database.MapError(err)
}

func (r *PGRepository) SupportAgentIDs(ctx context.Context) ([]uuid.UUID, error) {
	ids, err := r.q(ctx).ListSupportAgentIDs(ctx)
	return ids, database.MapError(err)
}

func (r *PGRepository) CreateSupportConversation(ctx context.Context, requester uuid.UUID, agents []uuid.UUID) (out db.Conversation, err error) {
	err = r.tx.InTx(ctx, func(ctx context.Context) error {
		c, err := r.q(ctx).CreateSupportConversation(ctx)
		if err != nil {
			return err
		}
		out = c
		if err := r.AddParticipant(ctx, c.ID, requester, uuid.NullUUID{}); err != nil {
			return err
		}
		for _, agent := range agents {
			if err := r.AddParticipant(ctx, c.ID, agent, uuid.NullUUID{}); err != nil {
				return err
			}
		}
		return nil
	})
	return out, database.MapError(err)
}

var (
	customerRoles = []string{httpx.RoleCustomer, httpx.RoleOrgAdmin}
	ownerRoles    = []string{httpx.RoleOwner, httpx.RoleFleetManager, httpx.RoleOrgAdmin}
)

func (r *PGRepository) addOrg(ctx context.Context, convID, org uuid.UUID, roles []string) error {
	ids, err := r.q(ctx).ListOrgUserIDsWithRoles(ctx, db.ListOrgUserIDsWithRolesParams{OrganizationID: uuid.NullUUID{UUID: org, Valid: true}, Roles: roles})
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := r.AddParticipant(ctx, convID, id, uuid.NullUUID{UUID: org, Valid: true}); err != nil {
			return err
		}
	}
	return nil
}

// CreateBookingConversation opens the booking thread with both
// organisations' relevant members and the operator as participants.
func (r *PGRepository) CreateBookingConversation(ctx context.Context, b bookings.Booking) (db.Conversation, error) {
	var out db.Conversation
	err := r.tx.InTx(ctx, func(ctx context.Context) error {
		subject := b.Reference + " · " + b.EquipmentTitle
		c, err := r.q(ctx).CreateConversation(ctx, db.CreateConversationParams{
			Kind: db.ConversationKindBooking, BookingID: uuid.NullUUID{UUID: b.ID, Valid: true}, Subject: &subject,
		})
		if err != nil {
			return err
		}
		out = c
		if err := r.addOrg(ctx, c.ID, b.CustomerOrgID, customerRoles); err != nil {
			return err
		}
		if err := r.addOrg(ctx, c.ID, b.OwnerOrgID, ownerRoles); err != nil {
			return err
		}
		if b.Operator != nil {
			return r.AddParticipant(ctx, c.ID, b.Operator.UserID, uuid.NullUUID{UUID: b.OwnerOrgID, Valid: true})
		}
		return nil
	})
	return out, database.MapError(err)
}

func (r *PGRepository) Enquiry(ctx context.Context, equipmentID, customerOrg uuid.UUID) (db.Conversation, error) {
	c, err := r.q(ctx).GetEnquiryConversation(ctx, db.GetEnquiryConversationParams{
		EquipmentID: uuid.NullUUID{UUID: equipmentID, Valid: true}, CustomerOrgID: uuid.NullUUID{UUID: customerOrg, Valid: true},
	})
	return c, database.MapError(err)
}

// CreateEnquiry opens a listing enquiry with the customer organisation's
// buyers and the owner's owners and admins.
func (r *PGRepository) CreateEnquiry(ctx context.Context, p db.CreateEnquiryConversationParams) (db.Conversation, error) {
	var out db.Conversation
	err := r.tx.InTx(ctx, func(ctx context.Context) error {
		c, err := r.q(ctx).CreateEnquiryConversation(ctx, p)
		if err != nil {
			return err
		}
		out = c
		if err := r.addOrg(ctx, c.ID, p.CustomerOrgID.UUID, customerRoles); err != nil {
			return err
		}
		return r.addOrg(ctx, c.ID, p.OwnerOrgID.UUID, []string{httpx.RoleOwner, httpx.RoleOrgAdmin})
	})
	return out, database.MapError(err)
}

func (r *PGRepository) Equipment(ctx context.Context, id uuid.UUID) (db.EquipmentForEnquiryRow, error) {
	e, err := r.q(ctx).EquipmentForEnquiry(ctx, id)
	return e, database.MapError(err)
}

func (r *PGRepository) HasOrgRole(ctx context.Context, userID, org uuid.UUID, roles []string) (bool, error) {
	ok, err := r.q(ctx).UserHasOrgRole(ctx, db.UserHasOrgRoleParams{UserID: userID, OrganizationID: uuid.NullUUID{UUID: org, Valid: true}, Roles: roles})
	return ok, database.MapError(err)
}

func (r *PGRepository) AddParticipant(ctx context.Context, convID, userID uuid.UUID, org uuid.NullUUID) error {
	return database.MapError(r.q(ctx).AddConversationParticipant(ctx, db.AddConversationParticipantParams{
		ConversationID: convID, UserID: userID, OrganizationID: org,
	}))
}

func (r *PGRepository) Participants(ctx context.Context, convID uuid.UUID) ([]uuid.UUID, error) {
	ids, err := r.q(ctx).ListConversationParticipantIDs(ctx, convID)
	return ids, database.MapError(err)
}

func (r *PGRepository) ForUser(ctx context.Context, userID uuid.UUID) ([]Conversation, error) {
	rows, err := r.q(ctx).ListUserConversationsV2(ctx, db.ListUserConversationsV2Params{UserID: uuid.NullUUID{UUID: userID, Valid: true}, ResultLimit: 100})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Conversation, 0, len(rows))
	for _, c := range rows {
		out = append(out, Conversation{ID: c.ID, Kind: string(c.Kind), BookingID: c.BookingID, EquipmentID: c.EquipmentID,
			Subject: c.Subject, UpdatedAt: c.UpdatedAt, LastMessage: c.LastMessage, Unread: c.Unread,
			BookingReference: c.BookingReference, BookingStatus: c.BookingStatus})
	}
	return out, nil
}

func (r *PGRepository) UnreadTotal(ctx context.Context, userID uuid.UUID) (int32, error) {
	n, err := r.q(ctx).CountUnreadMessages(ctx, uuid.NullUUID{UUID: userID, Valid: true})
	return n, database.MapError(err)
}

func (r *PGRepository) Messages(ctx context.Context, p db.ListChatMessagesParams) ([]db.ListChatMessagesRow, error) {
	rows, err := r.q(ctx).ListChatMessages(ctx, p)
	return rows, database.MapError(err)
}

func (r *PGRepository) Message(ctx context.Context, convID, id uuid.UUID) (db.GetChatMessageRow, error) {
	m, err := r.q(ctx).GetChatMessage(ctx, db.GetChatMessageParams{ID: id, ConversationID: convID})
	return m, database.MapError(err)
}

func (r *PGRepository) Send(ctx context.Context, p db.CreateChatMessageParams) (db.CreateChatMessageRow, error) {
	m, err := r.q(ctx).CreateChatMessage(ctx, p)
	return m, database.MapError(err)
}

func (r *PGRepository) Delete(ctx context.Context, convID, id, sender uuid.UUID) (bool, error) {
	n, err := r.q(ctx).SoftDeleteChatMessage(ctx, db.SoftDeleteChatMessageParams{
		ID: id, ConversationID: convID, SenderUserID: uuid.NullUUID{UUID: sender, Valid: true}, WindowMinutes: int32(DeleteWindow / time.Minute),
	})
	return n == 1, database.MapError(err)
}

func (r *PGRepository) MarkRead(ctx context.Context, convID, userID uuid.UUID) error {
	return database.MapError(r.q(ctx).MarkConversationRead(ctx, db.MarkConversationReadParams{ConversationID: convID, UserID: userID}))
}

func (r *PGRepository) UserName(ctx context.Context, userID uuid.UUID) string {
	u, err := r.q(ctx).GetUserByID(ctx, userID)
	if err != nil || strings.TrimSpace(u.FullName) == "" {
		return "New message"
	}
	return u.FullName
}

func (r *PGRepository) Emit(ctx context.Context, convID uuid.UUID, p events.Payload) error {
	return database.MapError(events.Emit(ctx, r.q(ctx), "conversation", convID, events.ChatMessage, p))
}

// ---- service -----------------------------------------------------------------------

// Uploads claims chat photos and signs their links (the uploads module).
type Uploads interface {
	Claim(ctx context.Context, userID, uploadID uuid.UUID, purposes ...filecheck.Purpose) (db.Upload, error)
	SignedURL(key string, ttl time.Duration) storage.SignedURL
}

type Service struct {
	repo     Repository
	bookings *bookings.Service
	uploads  Uploads
	tx       database.Transactor
	imageTTL time.Duration
	now      func() time.Time
}

func NewService(repo Repository, bk *bookings.Service, up Uploads, tx database.Transactor, imageTTL time.Duration) *Service {
	if imageTTL <= 0 {
		imageTTL = time.Hour
	}
	return &Service{repo: repo, bookings: bk, uploads: up, tx: tx, imageTTL: imageTTL, now: time.Now}
}

// join checks that the caller may use a conversation right now and makes
// them a participant. Outsiders get 404 so conversations are not disclosed.
func (s *Service) join(ctx context.Context, p *httpx.Principal, convID uuid.UUID) (db.Conversation, error) {
	c, err := s.repo.Get(ctx, convID)
	if err != nil {
		return db.Conversation{}, err
	}
	allowed, org := false, uuid.NullUUID{}
	switch {
	case c.BookingID.Valid:
		b, side, err := s.bookings.Access(ctx, p, c.BookingID.UUID)
		switch {
		case err == nil && side.Has(bookings.SideCustomer):
			allowed, org = true, uuid.NullUUID{UUID: b.CustomerOrgID, Valid: true}
		case err == nil && side.Has(bookings.SideOwner|bookings.SideOperator):
			allowed, org = true, uuid.NullUUID{UUID: b.OwnerOrgID, Valid: true}
		case err == nil && c.Kind == db.ConversationKindDispute:
			allowed = true // platform admins handling the dispute
		}
		if c.Kind == db.ConversationKindDispute && p.HasPlatformRole(httpx.RoleAdminOps, httpx.RoleAdminFinance) {
			allowed = true
		}
	case c.EquipmentID.Valid:
		if ok, err := s.repo.HasOrgRole(ctx, p.UserID, c.CustomerOrgID.UUID, customerRoles); err != nil {
			return db.Conversation{}, err
		} else if ok {
			allowed, org = true, c.CustomerOrgID
		} else if ok, err := s.repo.HasOrgRole(ctx, p.UserID, c.OwnerOrgID.UUID, []string{httpx.RoleOwner, httpx.RoleOrgAdmin}); err != nil {
			return db.Conversation{}, err
		} else if ok {
			allowed, org = true, c.OwnerOrgID
		}
	default:
		ids, err := s.repo.Participants(ctx, convID)
		if err != nil {
			return db.Conversation{}, err
		}
		for _, id := range ids {
			allowed = allowed || id == p.UserID
		}
	}
	if !allowed {
		return db.Conversation{}, apperror.ErrNotFound
	}
	return c, s.repo.AddParticipant(ctx, convID, p.UserID, org)
}

func toConversation(c db.Conversation) Conversation {
	return Conversation{ID: c.ID, Kind: string(c.Kind), BookingID: c.BookingID, EquipmentID: c.EquipmentID, Subject: c.Subject, UpdatedAt: c.UpdatedAt}
}

// ForBooking returns (creating if needed) the booking's conversation.
func (s *Service) ForBooking(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID) (Conversation, error) {
	b, _, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return Conversation{}, err
	}
	c, err := s.repo.BookingConversation(ctx, bookingID)
	if errors.Is(err, apperror.ErrNotFound) {
		c, err = s.repo.CreateBookingConversation(ctx, b)
		if errors.Is(err, apperror.ErrConflict) { // created concurrently
			c, err = s.repo.BookingConversation(ctx, bookingID)
		}
	}
	if err != nil {
		return Conversation{}, err
	}
	c, err = s.join(ctx, p, c.ID)
	if err != nil {
		return Conversation{}, err
	}
	out := toConversation(c)
	out.BookingReference, out.BookingStatus = b.Reference, b.Status
	return out, nil
}

// Support opens the caller's support conversation and ensures platform
// support staff, rather than another marketplace customer, are participants.
func (s *Service) Support(ctx context.Context, p *httpx.Principal) (Conversation, error) {
	agents, err := s.repo.SupportAgentIDs(ctx)
	if err != nil {
		return Conversation{}, err
	}
	if len(agents) == 0 {
		return Conversation{}, apperror.ErrServiceUnavailable.WithDetail("customer support is not available right now")
	}
	c, err := s.repo.SupportConversationForUser(ctx, p.UserID)
	if errors.Is(err, apperror.ErrNotFound) {
		c, err = s.repo.CreateSupportConversation(ctx, p.UserID, agents)
	}
	if err != nil {
		return Conversation{}, err
	}
	c, err = s.join(ctx, p, c.ID)
	if err != nil {
		return Conversation{}, err
	}
	return toConversation(c), nil
}

// Enquire opens (or returns) the acting customer organisation's enquiry
// about a listing, optionally with a first message.
func (s *Service) Enquire(ctx context.Context, sc httpx.Scope, equipmentID uuid.UUID, req EnquiryRequest) (Conversation, error) {
	e, err := s.repo.Equipment(ctx, equipmentID)
	if err != nil {
		return Conversation{}, err
	}
	if e.Status != "active" {
		return Conversation{}, apperror.ErrNotFound
	}
	if e.OwnerOrgID == sc.Org {
		return Conversation{}, apperror.ErrBusinessRule.WithDetail("you cannot enquire about your own machine")
	}
	c, err := s.repo.Enquiry(ctx, equipmentID, sc.Org)
	if errors.Is(err, apperror.ErrNotFound) {
		subject := "Enquiry · " + e.Title
		c, err = s.repo.CreateEnquiry(ctx, db.CreateEnquiryConversationParams{
			Subject: &subject, EquipmentID: uuid.NullUUID{UUID: equipmentID, Valid: true},
			CustomerOrgID: uuid.NullUUID{UUID: sc.Org, Valid: true}, OwnerOrgID: uuid.NullUUID{UUID: e.OwnerOrgID, Valid: true},
		})
		if errors.Is(err, apperror.ErrConflict) {
			c, err = s.repo.Enquiry(ctx, equipmentID, sc.Org)
		}
	}
	if err != nil {
		return Conversation{}, err
	}
	if _, err := s.join(ctx, sc.P, c.ID); err != nil {
		return Conversation{}, err
	}
	if req.Message != nil && strings.TrimSpace(*req.Message) != "" {
		if _, err := s.Send(ctx, sc.P, c.ID, SendRequest{Body: req.Message}); err != nil {
			return Conversation{}, err
		}
	}
	return toConversation(c), nil
}

func (s *Service) List(ctx context.Context, p *httpx.Principal) (ConversationList, error) {
	list, err := s.repo.ForUser(ctx, p.UserID)
	return ConversationList{Data: list}, err
}

func (s *Service) Unread(ctx context.Context, p *httpx.Principal) (UnreadCount, error) {
	n, err := s.repo.UnreadTotal(ctx, p.UserID)
	return UnreadCount{Unread: n}, err
}

func (s *Service) Get(ctx context.Context, p *httpx.Principal, convID uuid.UUID) (Conversation, error) {
	c, err := s.join(ctx, p, convID)
	if err != nil {
		return Conversation{}, err
	}
	out := toConversation(c)
	if c.BookingID.Valid {
		if b, err := s.bookings.Load(ctx, c.BookingID.UUID); err == nil {
			out.BookingReference, out.BookingStatus = b.Reference, b.Status
		}
	}
	return out, nil
}

func (s *Service) attachment(id uuid.NullUUID, key, ct *string) *Attachment {
	if !id.Valid || key == nil {
		return nil
	}
	link := s.uploads.SignedURL(*key, s.imageTTL)
	a := &Attachment{ID: id.UUID, URL: link.URL, ExpiresAt: link.ExpiresAt}
	if ct != nil {
		a.ContentType = *ct
	}
	return a
}

func (s *Service) Messages(ctx context.Context, p *httpx.Principal, convID uuid.UUID, q MessagesQuery) (MessageList, error) {
	if _, err := s.join(ctx, p, convID); err != nil {
		return MessageList{}, err
	}
	if q.Before != "" && q.After != "" {
		return MessageList{}, apperror.ErrValidation.WithDetail("use before or after, not both")
	}
	params := db.ListChatMessagesParams{ConversationID: convID, ResultLimit: 50}
	if q.Limit > 0 {
		params.ResultLimit = int32(q.Limit)
	}
	if q.Before != "" {
		params.Before = uuid.NullUUID{UUID: uuid.MustParse(q.Before), Valid: true}
	}
	if q.After != "" {
		params.After = uuid.NullUUID{UUID: uuid.MustParse(q.After), Valid: true}
	}
	rows, err := s.repo.Messages(ctx, params)
	if err != nil {
		return MessageList{}, err
	}
	out := make([]Message, 0, len(rows))
	for _, m := range rows {
		msg := Message{ID: m.ID, ConversationID: m.ConversationID, SenderUserID: m.SenderUserID, SenderName: m.SenderName,
			Mine: m.SenderUserID.Valid && m.SenderUserID.UUID == p.UserID, Body: m.Body, CreatedAt: m.CreatedAt, Deleted: m.DeletedAt != nil}
		if msg.Deleted {
			msg.Body = nil
		} else {
			msg.Attachment = s.attachment(m.AttachmentUploadID, m.AttachmentKey, m.AttachmentContentType)
		}
		out = append(out, msg)
	}
	return MessageList{Data: out}, nil
}

func preview(body *string, photo bool) string {
	if body == nil {
		if photo {
			return "📷 Photo"
		}
		return ""
	}
	text := strings.Join(strings.Fields(*body), " ")
	if utf8.RuneCountInString(text) > 120 {
		text = string([]rune(text)[:117]) + "…"
	}
	if photo {
		text = "📷 " + text
	}
	return text
}

func (s *Service) Send(ctx context.Context, p *httpx.Principal, convID uuid.UUID, req SendRequest) (Message, error) {
	if req.Body != nil {
		trimmed := strings.TrimSpace(*req.Body)
		if trimmed == "" {
			req.Body = nil
		} else {
			req.Body = &trimmed
		}
	}
	if req.Body == nil && !req.AttachmentUploadID.Valid {
		return Message{}, apperror.ErrValidation.WithDetail("send text or a photo")
	}
	c, err := s.join(ctx, p, convID)
	if err != nil {
		return Message{}, err
	}
	var out Message
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Reason: "chat message"}, func(ctx context.Context) error {
		params := db.CreateChatMessageParams{ConversationID: convID, SenderUserID: uuid.NullUUID{UUID: p.UserID, Valid: true},
			Body: req.Body, ClientID: req.ClientID}
		var key, ct *string
		if req.AttachmentUploadID.Valid {
			up, err := s.uploads.Claim(ctx, p.UserID, req.AttachmentUploadID.UUID, filecheck.ChatImage)
			if err != nil {
				return err
			}
			key, ct = &up.ObjectKey, &up.ContentType
			params.AttachmentObjectKey, params.AttachmentUploadID = key, uuid.NullUUID{UUID: up.ID, Valid: true}
		}
		m, err := s.repo.Send(ctx, params)
		if err != nil {
			return err
		}
		if m.ConversationID != convID {
			return apperror.ErrConflict.WithDetail("client_id was already used for another conversation")
		}
		name := s.repo.UserName(ctx, p.UserID)
		out = Message{ID: m.ID, ConversationID: m.ConversationID, SenderUserID: m.SenderUserID, SenderName: &name, Mine: true,
			Body: m.Body, CreatedAt: m.CreatedAt, Deleted: m.DeletedAt != nil}
		if m.AttachmentUploadID.Valid && key == nil {
			full, err := s.repo.Message(ctx, convID, m.ID)
			if err != nil {
				return err
			}
			key, ct = full.AttachmentKey, full.AttachmentContentType
		}
		out.Attachment = s.attachment(m.AttachmentUploadID, key, ct)
		if !m.Inserted {
			return nil // an idempotent resend: announced the first time
		}
		users, err := s.repo.Participants(ctx, convID)
		if err != nil {
			return err
		}
		extra := map[string]any{"conversation_id": convID, "message_id": m.ID, "actor_name": name}
		if c.BookingID.Valid {
			extra["booking_id"] = c.BookingID.UUID
		}
		return s.repo.Emit(ctx, convID, events.Payload{ActorUserID: p.UserID, UserIDs: users,
			Title: preview(m.Body, m.AttachmentUploadID.Valid), Extra: extra})
	})
	return out, err
}

// Delete removes the caller's own message within DeleteWindow.
func (s *Service) Delete(ctx context.Context, p *httpx.Principal, convID, msgID uuid.UUID) error {
	if _, err := s.join(ctx, p, convID); err != nil {
		return err
	}
	ok, err := s.repo.Delete(ctx, convID, msgID, p.UserID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.ErrBusinessRule.WithDetail("you can remove only your own messages, within 15 minutes of sending")
	}
	return nil
}

func (s *Service) MarkRead(ctx context.Context, p *httpx.Principal, convID uuid.UUID) error {
	if _, err := s.join(ctx, p, convID); err != nil {
		return err
	}
	return s.repo.MarkRead(ctx, convID, p.UserID)
}

// ---- handler -----------------------------------------------------------------------

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Name() string { return "chat" }

var enquiryRoles = []string{httpx.RoleCustomer}

func (h *Handler) Routes(r *httpx.Router) {
	a := r.Tag("chat").Authed()
	a.POST("/support/conversation", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		return h.svc.Support(c.Request.Context(), p)
	}), httpx.Doc{Summary: "Open the caller's conversation with EQUIPRENT customer support", Response: Conversation{}})
	a.GET("/conversations", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		return h.svc.List(c.Request.Context(), p)
	}), httpx.Doc{Summary: "My conversations with unread counts", Response: ConversationList{}})
	a.GET("/conversations/unread-count", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		return h.svc.Unread(c.Request.Context(), p)
	}), httpx.Doc{Summary: "Unread chat messages across conversations", Response: UnreadCount{}})
	a.POST("/bookings/:id/conversation", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.ForBooking(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Open (or get) the booking chat", Response: Conversation{}})
	a.POST("/equipment/:id/enquiry", httpx.OrgHandle(200, enquiryRoles, func(c *gin.Context, s httpx.Scope) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		var req EnquiryRequest
		if c.Request.ContentLength != 0 { // the first message is optional
			if err := httpx.Bind(c, &req); err != nil {
				return nil, err
			}
		}
		return h.svc.Enquire(c.Request.Context(), s, id, req)
	}), httpx.Doc{Summary: "Message the owner about a listing", Request: EnquiryRequest{}, Response: Conversation{}, OrgScoped: true})
	a.GET("/conversations/:id", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Get(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "A conversation", Response: Conversation{}})
	a.GET("/conversations/:id/messages", httpx.HandleQuery(200, func(c *gin.Context, p *httpx.Principal, q MessagesQuery) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Messages(c.Request.Context(), p, id, q)
	}), httpx.Doc{Summary: "Message history (before=, newest first) or new messages (after=, oldest first)", Query: MessagesQuery{}, Response: MessageList{}})
	a.POST("/conversations/:id/messages", httpx.HandleJSON(201, func(c *gin.Context, p *httpx.Principal, req SendRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Send(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Send a message (text and/or a chat_image upload)", Request: SendRequest{}, Response: Message{}, Status: 201})
	a.DELETE("/conversations/:id/messages/:message_id", httpx.Handle(204, func(c *gin.Context, p *httpx.Principal) (any, error) {
		ids, err := httpx.Params(c, "id", "message_id")
		if err != nil {
			return nil, err
		}
		return nil, h.svc.Delete(c.Request.Context(), p, ids[0], ids[1])
	}), httpx.Doc{Summary: "Remove your own message (within 15 minutes)"})
	a.POST("/conversations/:id/read", httpx.Handle(204, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return nil, h.svc.MarkRead(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Mark the conversation read"})
}
