// Package reviews lets the parties of a finished booking rate each other:
// customers rate the equipment, the owner's service and the operator;
// owners rate the customer. Each review has a 1-5 rating and optional
// written feedback.
//
// Moderation:
//   - automatic screening holds reviews with contact details, links or
//     abusive words as pending (not public) until a moderator decides;
//   - anyone signed in may report a review; three reports hide it (flagged)
//     until a moderator decides;
//   - moderators publish or hide reviews with a reason, audited.
//
// Only published reviews are listed and counted in ratings. The reviewed
// party may reply once, publicly.
package reviews

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
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
	"github.com/equiprent/backend/internal/modules/bookings"
)

// HideThreshold is the number of reports that hides a review pending moderation.
const HideThreshold = 3

// Review is a review as shown to the public or to the parties.
type Review struct {
	ID             uuid.UUID  `json:"id"`
	BookingID      uuid.UUID  `json:"booking_id"`
	SubjectType    string     `json:"subject_type" doc:"equipment | owner | operator | customer"`
	SubjectID      uuid.UUID  `json:"subject_id"`
	Rating         int16      `json:"rating"`
	Comment        *string    `json:"comment,omitempty"`
	Status         string     `json:"status,omitempty" doc:"pending | published | hidden | flagged (shown to the parties)"`
	ReviewerName   string     `json:"reviewer_name,omitempty"`
	EquipmentTitle *string    `json:"equipment_title,omitempty"`
	Reply          *string    `json:"reply,omitempty"`
	RepliedAt      *time.Time `json:"replied_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type CreateRequest struct {
	Subject string  `json:"subject" binding:"required,oneof=equipment owner operator customer" doc:"equipment and owner (service) are rated by the customer; customer by the owner"`
	Rating  int16   `json:"rating" binding:"required,min=1,max=5"`
	Comment *string `json:"comment" binding:"omitempty,max=2000"`
}

type ReplyRequest struct {
	Reply string `json:"reply" binding:"required,min=2,max=1000"`
}

type ReportRequest struct {
	Reason string  `json:"reason" binding:"required,oneof=spam abusive false_information personal_information irrelevant other"`
	Note   *string `json:"note" binding:"omitempty,max=500"`
}

type ReportResult struct {
	Reported bool `json:"reported" doc:"false if you had already reported it"`
}

type ModerateRequest struct {
	Action string `json:"action" binding:"required,oneof=publish hide"`
	Reason string `json:"reason" binding:"required,min=3,max=500"`
}

// Summary is a subject's rating summary over published reviews.
type Summary struct {
	Average   float64          `json:"average"`
	Count     int32            `json:"count"`
	Histogram map[string]int32 `json:"histogram" doc:"count per star, \"1\"..\"5\""`
}

type ReviewPage struct {
	Data       []Review `json:"data"`
	NextCursor string   `json:"next_cursor,omitempty"`
	Summary    *Summary `json:"summary,omitempty" doc:"First page only"`
}

// BookingReviews lists a booking's reviews and what the caller may still rate.
type BookingReviews struct {
	Data       []Review `json:"data"`
	CanReview  []string `json:"can_review" doc:"Subjects the caller may still review"`
	ReviewOpen bool     `json:"review_open" doc:"The booking is finished, so reviews are open"`
}

// AdminReview is a review in the moderation queue.
type AdminReview struct {
	ID             uuid.UUID  `json:"id"`
	BookingID      uuid.UUID  `json:"booking_id"`
	SubjectType    string     `json:"subject_type"`
	SubjectID      uuid.UUID  `json:"subject_id"`
	SubjectName    string     `json:"subject_name"`
	Rating         int16      `json:"rating"`
	Comment        *string    `json:"comment,omitempty"`
	Status         string     `json:"status"`
	ScreeningFlags []string   `json:"screening_flags"`
	ReportCount    int32      `json:"report_count"`
	ReportReasons  []string   `json:"report_reasons"`
	ReviewerName   string     `json:"reviewer_name"`
	ReviewerUser   string     `json:"reviewer_user_name"`
	ModeratedAt    *time.Time `json:"moderated_at,omitempty"`
	ModerationNote *string    `json:"moderation_note,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type AdminQuery struct {
	Status   string `form:"status" binding:"omitempty,oneof=pending published hidden flagged"`
	Reported bool   `form:"reported" doc:"Only reviews with reports"`
	Limit    int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Cursor   string `form:"cursor"`
}

type AdminPage struct {
	Data       []AdminReview `json:"data"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// ---- screening ---------------------------------------------------------------------

var (
	phoneRe = regexp.MustCompile(`(?:\+?91[\s-]?)?\b[6-9](?:\d{9}|\d{4}[\s-]\d{5}|\d{2}[\s-]\d{3}[\s-]\d{4})\b`)
	emailRe = regexp.MustCompile(`(?i)\b[\w.+-]+@[\w-]+\.[a-z]{2,}\b`)
	linkRe  = regexp.MustCompile(`(?i)\b(?:https?://|www\.)\S+|\b[\w-]+\.(?:com|in|net|org|co|io|xyz)\b`)
	// Abusive words in English and common Hindi transliterations.
	abuseRe = regexp.MustCompile(`(?i)\b(?:f+u+c+k\w*|shit\w*|bastard\w*|bitch\w*|asshole\w*|chutiy\w*|madarchod\w*|ma+dar ?chod|bhen?chod\w*|behen ?chod\w*|gandu\w*|harami\w*|kutt[ae] ?kamin\w*)\b`)
)

// Screen returns the reasons a comment should wait for a moderator.
func Screen(comment string) []string {
	flags := []string{}
	if phoneRe.MatchString(comment) || emailRe.MatchString(comment) {
		flags = append(flags, "contact_details")
	}
	if linkRe.MatchString(emailRe.ReplaceAllString(comment, " ")) {
		flags = append(flags, "link")
	}
	if abuseRe.MatchString(comment) {
		flags = append(flags, "abusive_language")
	}
	return flags
}

// ---- repository --------------------------------------------------------------------

type Repository interface {
	Create(ctx context.Context, p db.CreateReviewV2Params) (db.Review, error)
	List(ctx context.Context, subjectType string, subjectID uuid.UUID, k httpx.Keyset) ([]Review, error)
	Summary(ctx context.Context, subjectType string, subjectID uuid.UUID) (Summary, error)
	ForBooking(ctx context.Context, bookingID uuid.UUID) ([]db.ListBookingReviewsRow, error)
	OwnerReceived(ctx context.Context, org uuid.UUID, k httpx.Keyset) ([]Review, error)
	Get(ctx context.Context, id uuid.UUID) (db.GetReviewRow, error)
	Reply(ctx context.Context, p db.SetReviewReplyParams) (db.Review, error)
	Report(ctx context.Context, p db.CreateReviewReportParams) (bool, error)
	BumpReports(ctx context.Context, id uuid.UUID) (string, error)
	Moderate(ctx context.Context, p db.ModerateReviewParams) (db.Review, error)
	AdminList(ctx context.Context, p db.AdminListReviewsParams) ([]AdminReview, error)
	RefreshRating(ctx context.Context, subjectType string, subjectID uuid.UUID) error
	Emit(ctx context.Context, reviewID uuid.UUID, p events.Payload) error
	Audit(ctx context.Context, p db.InsertAuditLogParams) error
}

type PGRepository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *PGRepository { return &PGRepository{pool: pool} }

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

func (r *PGRepository) Create(ctx context.Context, p db.CreateReviewV2Params) (db.Review, error) {
	row, err := r.q(ctx).CreateReviewV2(ctx, p)
	return row, database.MapError(err)
}

func (r *PGRepository) List(ctx context.Context, subjectType string, subjectID uuid.UUID, k httpx.Keyset) ([]Review, error) {
	rows, err := r.q(ctx).ListPublishedReviews(ctx, db.ListPublishedReviewsParams{
		SubjectType: db.ReviewSubject(subjectType), SubjectID: subjectID, After: k.After, ResultLimit: k.Fetch(),
	})
	out := make([]Review, 0, len(rows))
	for _, x := range rows {
		out = append(out, Review{ID: x.ID, BookingID: x.BookingID, SubjectType: string(x.SubjectType), SubjectID: x.SubjectID,
			Rating: x.Rating, Comment: x.Comment, ReviewerName: x.ReviewerName, EquipmentTitle: x.EquipmentTitle,
			Reply: x.Reply, RepliedAt: x.RepliedAt, CreatedAt: x.CreatedAt})
	}
	return out, database.MapError(err)
}

func (r *PGRepository) Summary(ctx context.Context, subjectType string, subjectID uuid.UUID) (Summary, error) {
	rows, err := r.q(ctx).ReviewSummary(ctx, db.ReviewSummaryParams{SubjectType: db.ReviewSubject(subjectType), SubjectID: subjectID})
	if err != nil {
		return Summary{}, database.MapError(err)
	}
	return summarise(rows), nil
}

func summarise(rows []db.ReviewSummaryRow) Summary {
	s := Summary{Histogram: map[string]int32{"1": 0, "2": 0, "3": 0, "4": 0, "5": 0}}
	var total int64
	for _, x := range rows {
		s.Histogram[fmt.Sprint(x.Rating)] = x.Cnt
		s.Count += x.Cnt
		total += int64(x.Rating) * int64(x.Cnt)
	}
	if s.Count > 0 {
		s.Average = float64(int64(float64(total)/float64(s.Count)*100+0.5)) / 100
	}
	return s
}

func (r *PGRepository) ForBooking(ctx context.Context, bookingID uuid.UUID) ([]db.ListBookingReviewsRow, error) {
	rows, err := r.q(ctx).ListBookingReviews(ctx, bookingID)
	return rows, database.MapError(err)
}

func (r *PGRepository) OwnerReceived(ctx context.Context, org uuid.UUID, k httpx.Keyset) ([]Review, error) {
	rows, err := r.q(ctx).ListOwnerReceivedReviews(ctx, db.ListOwnerReceivedReviewsParams{OrgID: org, After: k.After, ResultLimit: k.Fetch()})
	out := make([]Review, 0, len(rows))
	for _, x := range rows {
		out = append(out, Review{ID: x.ID, BookingID: x.BookingID, SubjectType: string(x.SubjectType), SubjectID: x.SubjectID,
			Rating: x.Rating, Comment: x.Comment, Status: x.Status, ReviewerName: x.ReviewerName, EquipmentTitle: x.EquipmentTitle,
			Reply: x.Reply, RepliedAt: x.RepliedAt, CreatedAt: x.CreatedAt})
	}
	return out, database.MapError(err)
}

func (r *PGRepository) Get(ctx context.Context, id uuid.UUID) (db.GetReviewRow, error) {
	row, err := r.q(ctx).GetReview(ctx, id)
	return row, database.MapError(err)
}

func (r *PGRepository) Reply(ctx context.Context, p db.SetReviewReplyParams) (db.Review, error) {
	row, err := r.q(ctx).SetReviewReply(ctx, p)
	return row, database.MapError(err)
}

func (r *PGRepository) Report(ctx context.Context, p db.CreateReviewReportParams) (bool, error) {
	n, err := r.q(ctx).CreateReviewReport(ctx, p)
	return n == 1, database.MapError(err)
}

func (r *PGRepository) BumpReports(ctx context.Context, id uuid.UUID) (string, error) {
	row, err := r.q(ctx).BumpReviewReports(ctx, db.BumpReviewReportsParams{ID: id, HideThreshold: HideThreshold})
	return row.Status, database.MapError(err)
}

func (r *PGRepository) Moderate(ctx context.Context, p db.ModerateReviewParams) (db.Review, error) {
	row, err := r.q(ctx).ModerateReview(ctx, p)
	return row, database.MapError(err)
}

func (r *PGRepository) AdminList(ctx context.Context, p db.AdminListReviewsParams) ([]AdminReview, error) {
	rows, err := r.q(ctx).AdminListReviews(ctx, p)
	out := make([]AdminReview, 0, len(rows))
	for _, x := range rows {
		out = append(out, AdminReview{ID: x.ID, BookingID: x.BookingID, SubjectType: string(x.SubjectType), SubjectID: x.SubjectID,
			SubjectName: x.SubjectName, Rating: x.Rating, Comment: x.Comment, Status: x.Status, ScreeningFlags: x.ScreeningFlags,
			ReportCount: x.ReportCount, ReportReasons: x.ReportReasons, ReviewerName: x.ReviewerName, ReviewerUser: x.ReviewerUserName,
			ModeratedAt: x.ModeratedAt, ModerationNote: x.ModerationNote, CreatedAt: x.CreatedAt})
	}
	return out, database.MapError(err)
}

func (r *PGRepository) RefreshRating(ctx context.Context, subjectType string, id uuid.UUID) error {
	q := r.q(ctx)
	switch subjectType {
	case "equipment":
		return database.MapError(q.RefreshEquipmentRating(ctx, id))
	case "operator":
		return database.MapError(q.RefreshOperatorRating(ctx, id))
	default:
		return database.MapError(q.RefreshOrgRating(ctx, id))
	}
}

func (r *PGRepository) Emit(ctx context.Context, id uuid.UUID, p events.Payload) error {
	return database.MapError(events.Emit(ctx, r.q(ctx), "review", id, events.ReviewReceived, p))
}

func (r *PGRepository) Audit(ctx context.Context, p db.InsertAuditLogParams) error {
	return database.MapError(r.q(ctx).InsertAuditLog(ctx, p))
}

// ---- service -----------------------------------------------------------------------

type Service struct {
	repo     Repository
	bookings *bookings.Service
	tx       database.Transactor
}

func NewService(repo Repository, bk *bookings.Service, tx database.Transactor) *Service {
	return &Service{repo: repo, bookings: bk, tx: tx}
}

var finished = map[string]bool{bookings.StatusCompleted: true, bookings.StatusClosed: true}

// Subject resolves who/what a review is about from the booking and side.
func Subject(b bookings.Booking, side bookings.Side, subject string) (reviewerOrg, subjectID uuid.UUID, err error) {
	if subject == "customer" {
		if !side.Has(bookings.SideOwner) {
			return uuid.Nil, uuid.Nil, apperror.ErrForbidden.WithDetail("only the owner reviews the customer")
		}
		return b.OwnerOrgID, b.CustomerOrgID, nil
	}
	if !side.Has(bookings.SideCustomer) {
		return uuid.Nil, uuid.Nil, apperror.ErrForbidden.WithDetail("only the customer reviews the " + subject)
	}
	switch subject {
	case "equipment":
		return b.CustomerOrgID, b.EquipmentID, nil
	case "owner":
		return b.CustomerOrgID, b.OwnerOrgID, nil
	case "operator":
		if b.Operator == nil {
			return uuid.Nil, uuid.Nil, apperror.ErrBusinessRule.WithDetail("no operator worked on this booking")
		}
		return b.CustomerOrgID, b.Operator.UserID, nil
	}
	return uuid.Nil, uuid.Nil, apperror.ErrValidation
}

// stars renders a rating as ★★★★☆.
func stars(n int16) string {
	return strings.Repeat("★", int(n)) + strings.Repeat("☆", 5-int(n))
}

// announce tells the reviewed party about a published review.
func (s *Service) announce(ctx context.Context, r db.Review, ref, reviewer, equipment string) error {
	pl := events.Payload{Reference: ref, Extra: map[string]any{"booking_id": r.BookingID, "review_id": r.ID, "subject": string(r.SubjectType)}}
	what := map[db.ReviewSubject]string{"equipment": equipment, "owner": "your service", "operator": "your work", "customer": "your organisation"}[r.SubjectType]
	pl.Title = fmt.Sprintf("%s %d/5 from %s for %s.", stars(r.Rating), r.Rating, reviewer, what)
	switch r.SubjectType {
	case "equipment", "owner":
		pl.OwnerOrgID = r.SubjectID
		if r.SubjectType == "equipment" {
			if b, err := s.bookings.Load(ctx, r.BookingID); err == nil {
				pl.OwnerOrgID = b.OwnerOrgID
			}
		}
	case "customer":
		pl.CustomerOrgID = r.SubjectID
	case "operator":
		pl.UserIDs = []uuid.UUID{r.SubjectID}
	}
	pl.ActorUserID = r.ReviewerUserID
	return s.repo.Emit(ctx, r.ID, pl)
}

func toReview(r db.Review, reviewer string) Review {
	return Review{ID: r.ID, BookingID: r.BookingID, SubjectType: string(r.SubjectType), SubjectID: r.SubjectID, Rating: r.Rating,
		Comment: r.Comment, Status: r.Status, ReviewerName: reviewer, Reply: r.Reply, RepliedAt: r.RepliedAt, CreatedAt: r.CreatedAt}
}

// Create posts a review for a completed booking. Screened reviews wait for
// a moderator (status pending).
func (s *Service) Create(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID, req CreateRequest) (Review, error) {
	b, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return Review{}, err
	}
	if !finished[b.Status] {
		return Review{}, apperror.ErrBusinessRule.WithDetail("reviews open once the booking is completed")
	}
	reviewerOrg, subjectID, err := Subject(b, side, req.Subject)
	if err != nil {
		return Review{}, err
	}
	flags := []string{}
	if req.Comment != nil {
		c := strings.TrimSpace(*req.Comment)
		if c == "" {
			req.Comment = nil
		} else {
			req.Comment = &c
			flags = Screen(c)
		}
	}
	status := "published"
	if len(flags) > 0 {
		status = "pending"
	}
	reviewer := b.CustomerName
	if reviewerOrg == b.OwnerOrgID {
		reviewer = b.OwnerName
	}
	var out db.Review
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Reason: "review"}, func(ctx context.Context) error {
		out, err = s.repo.Create(ctx, db.CreateReviewV2Params{
			BookingID: b.ID, ReviewerOrgID: reviewerOrg, ReviewerUserID: p.UserID, SubjectType: db.ReviewSubject(req.Subject),
			SubjectID: subjectID, Rating: req.Rating, Comment: req.Comment, Status: status, ScreeningFlags: flags,
		})
		if err != nil {
			return err
		}
		if status != "published" {
			return nil
		}
		if err := s.repo.RefreshRating(ctx, req.Subject, subjectID); err != nil {
			return err
		}
		return s.announce(ctx, out, b.Reference, reviewer, b.EquipmentTitle)
	})
	if err != nil {
		return Review{}, err
	}
	return toReview(out, reviewer), nil
}

// List pages published reviews of a subject, with the summary on page one.
func (s *Service) List(ctx context.Context, subjectType string, id uuid.UUID, q httpx.PageQuery) (ReviewPage, error) {
	k, err := q.Keyset()
	if err != nil {
		return ReviewPage{}, err
	}
	rows, err := s.repo.List(ctx, subjectType, id, k)
	if err != nil {
		return ReviewPage{}, err
	}
	page := httpx.Paginate(rows, k, func(r Review) uuid.UUID { return r.ID })
	out := ReviewPage{Data: page.Data, NextCursor: page.NextCursor}
	if q.Cursor == "" {
		sum, err := s.repo.Summary(ctx, subjectType, id)
		if err != nil {
			return ReviewPage{}, err
		}
		out.Summary = &sum
	}
	return out, nil
}

// ForBooking lists the booking's reviews visible to the caller: published
// ones, plus everything their own organisation wrote or received.
func (s *Service) ForBooking(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID) (BookingReviews, error) {
	b, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return BookingReviews{}, err
	}
	rows, err := s.repo.ForBooking(ctx, bookingID)
	if err != nil {
		return BookingReviews{}, err
	}
	out := BookingReviews{Data: []Review{}, CanReview: []string{}, ReviewOpen: finished[b.Status]}
	written := map[string]bool{}
	mine := uuid.Nil
	switch {
	case side.Has(bookings.SideCustomer):
		mine = b.CustomerOrgID
	case side.Has(bookings.SideOwner):
		mine = b.OwnerOrgID
	}
	for _, r := range rows {
		if r.ReviewerOrgID == mine {
			written[string(r.SubjectType)] = true
		}
		if r.Status != "published" && r.ReviewerOrgID != mine {
			continue
		}
		out.Data = append(out.Data, Review{ID: r.ID, BookingID: r.BookingID, SubjectType: string(r.SubjectType), SubjectID: r.SubjectID,
			Rating: r.Rating, Comment: r.Comment, Status: r.Status, ReviewerName: r.ReviewerName, Reply: r.Reply,
			RepliedAt: r.RepliedAt, CreatedAt: r.CreatedAt})
	}
	if out.ReviewOpen {
		var subjects []string
		switch {
		case side.Has(bookings.SideCustomer):
			subjects = []string{"equipment", "owner"}
			if b.Operator != nil {
				subjects = append(subjects, "operator")
			}
		case side.Has(bookings.SideOwner):
			subjects = []string{"customer"}
		}
		for _, sub := range subjects {
			if !written[sub] {
				out.CanReview = append(out.CanReview, sub)
			}
		}
	}
	return out, nil
}

// OwnerReceived pages the published reviews of the acting owner and its machines.
func (s *Service) OwnerReceived(ctx context.Context, org uuid.UUID, q httpx.PageQuery) (ReviewPage, error) {
	k, err := q.Keyset()
	if err != nil {
		return ReviewPage{}, err
	}
	rows, err := s.repo.OwnerReceived(ctx, org, k)
	if err != nil {
		return ReviewPage{}, err
	}
	page := httpx.Paginate(rows, k, func(r Review) uuid.UUID { return r.ID })
	out := ReviewPage{Data: page.Data, NextCursor: page.NextCursor}
	if q.Cursor == "" {
		sum, err := s.repo.Summary(ctx, "owner", org)
		if err != nil {
			return ReviewPage{}, err
		}
		out.Summary = &sum
	}
	return out, nil
}

// subjectOrg is the organisation that answers for a review.
func subjectOrg(r db.GetReviewRow) uuid.UUID {
	switch r.SubjectType {
	case "equipment":
		return r.EquipmentOwnerOrgID.UUID
	case "owner", "customer":
		return r.SubjectID
	}
	return uuid.Nil
}

// Reply lets the reviewed organisation answer a published review once.
func (s *Service) Reply(ctx context.Context, p *httpx.Principal, id uuid.UUID, req ReplyRequest) (Review, error) {
	r, err := s.repo.Get(ctx, id)
	if err != nil {
		return Review{}, err
	}
	org := subjectOrg(r)
	roles := []string{httpx.RoleOwner, httpx.RoleOrgAdmin}
	if r.SubjectType == "customer" {
		roles = []string{httpx.RoleCustomer}
	}
	if org == uuid.Nil || !p.HasOrgRole(org, roles...) {
		return Review{}, apperror.ErrForbidden.WithDetail("only the reviewed party can reply")
	}
	if r.Status != "published" {
		return Review{}, apperror.ErrBusinessRule.WithDetail("you can reply to published reviews only")
	}
	text := strings.TrimSpace(req.Reply)
	if flags := Screen(text); len(flags) > 0 {
		return Review{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "reply", Code: "screened",
			Message: "remove contact details, links or abusive language (" + strings.Join(flags, ", ") + ")"})
	}
	row, err := s.repo.Reply(ctx, db.SetReviewReplyParams{ID: id, Reply: &text, ReplyBy: uuid.NullUUID{UUID: p.UserID, Valid: true}})
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return Review{}, apperror.ErrConflict.WithDetail("this review already has a reply")
		}
		return Review{}, err
	}
	return toReview(row, ""), nil
}

// Report flags a review; enough reports hide it until a moderator decides.
func (s *Service) Report(ctx context.Context, p *httpx.Principal, id uuid.UUID, req ReportRequest) (ReportResult, error) {
	r, err := s.repo.Get(ctx, id)
	if err != nil {
		return ReportResult{}, err
	}
	if r.ReviewerUserID == p.UserID {
		return ReportResult{}, apperror.ErrBusinessRule.WithDetail("you cannot report your own review")
	}
	if r.Status != "published" && r.Status != "flagged" {
		return ReportResult{}, apperror.ErrNotFound
	}
	var reported bool
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		reported, err = s.repo.Report(ctx, db.CreateReviewReportParams{ReviewID: id, ReporterUserID: p.UserID, Reason: req.Reason, Note: req.Note})
		if err != nil || !reported {
			return err
		}
		status, err := s.repo.BumpReports(ctx, id)
		if err != nil {
			return err
		}
		if status == "flagged" && r.Status == "published" {
			return s.repo.RefreshRating(ctx, string(r.SubjectType), r.SubjectID)
		}
		return nil
	})
	return ReportResult{Reported: reported}, err
}

// Moderate publishes or hides a review, with a reason that is audited.
func (s *Service) Moderate(ctx context.Context, p *httpx.Principal, id uuid.UUID, req ModerateRequest) (AdminReview, error) {
	r, err := s.repo.Get(ctx, id)
	if err != nil {
		return AdminReview{}, err
	}
	status := map[string]string{"publish": "published", "hide": "hidden"}[req.Action]
	reason := strings.TrimSpace(req.Reason)
	reqID := httpx.RequestIDFromContext(ctx)
	var out db.Review
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "admin", Reason: reason, RequestID: reqID}, func(ctx context.Context) error {
		out, err = s.repo.Moderate(ctx, db.ModerateReviewParams{ID: id, Status: status, ModeratedBy: uuid.NullUUID{UUID: p.UserID, Valid: true}, ModerationNote: &reason})
		if err != nil {
			return err
		}
		if err := s.repo.RefreshRating(ctx, string(r.SubjectType), r.SubjectID); err != nil {
			return err
		}
		after, _ := json.Marshal(map[string]any{"from": r.Status, "to": status})
		role := "admin"
		if err := s.repo.Audit(ctx, db.InsertAuditLogParams{
			ActorUserID: uuid.NullUUID{UUID: p.UserID, Valid: true}, ActorRole: &role, Action: "review." + req.Action, EntityType: "review",
			EntityID: uuid.NullUUID{UUID: id, Valid: true}, Reason: &reason, RequestID: &reqID, After: after,
		}); err != nil {
			return err
		}
		if status == "published" && r.Status == "pending" { // first publication: tell the reviewed party
			b, err := s.bookings.Load(ctx, r.BookingID)
			if err != nil {
				return err
			}
			reviewer := b.CustomerName
			if r.ReviewerOrgID == b.OwnerOrgID {
				reviewer = b.OwnerName
			}
			return s.announce(ctx, out, b.Reference, reviewer, b.EquipmentTitle)
		}
		return nil
	})
	if err != nil {
		return AdminReview{}, err
	}
	return AdminReview{ID: out.ID, BookingID: out.BookingID, SubjectType: string(out.SubjectType), SubjectID: out.SubjectID,
		Rating: out.Rating, Comment: out.Comment, Status: out.Status, ScreeningFlags: out.ScreeningFlags, ReportCount: out.ReportCount,
		ReportReasons: []string{}, ModeratedAt: out.ModeratedAt, ModerationNote: out.ModerationNote, CreatedAt: out.CreatedAt}, nil
}

// Queue lists reviews for moderators.
func (s *Service) Queue(ctx context.Context, q AdminQuery) (AdminPage, error) {
	k, err := httpx.PageQuery{Limit: q.Limit, Cursor: q.Cursor}.Keyset()
	if err != nil {
		return AdminPage{}, err
	}
	p := db.AdminListReviewsParams{ReportedOnly: q.Reported, After: k.After, ResultLimit: k.Fetch()}
	if q.Status != "" {
		p.Status = &q.Status
	}
	rows, err := s.repo.AdminList(ctx, p)
	if err != nil {
		return AdminPage{}, err
	}
	page := httpx.Paginate(rows, k, func(r AdminReview) uuid.UUID { return r.ID })
	return AdminPage{Data: page.Data, NextCursor: page.NextCursor}, nil
}

// ---- handler -----------------------------------------------------------------------

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Name() string { return "reviews" }

var ownerRoles = []string{httpx.RoleOwner, httpx.RoleFleetManager}

func (h *Handler) Routes(r *httpx.Router) {
	r = r.Tag("reviews")
	list := func(subject, param string) gin.HandlerFunc {
		return httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q httpx.PageQuery) (any, error) {
			id, err := httpx.Param(c, param)
			if err != nil {
				return nil, err
			}
			return h.svc.List(c.Request.Context(), subject, id, q)
		})
	}
	r.GET("/equipment/:id/reviews", list("equipment", "id"), httpx.Doc{Summary: "Equipment ratings and reviews", Query: httpx.PageQuery{}, Response: ReviewPage{}})
	r.GET("/owners/:org_id/reviews", list("owner", "org_id"), httpx.Doc{Summary: "Owner service ratings and reviews", Query: httpx.PageQuery{}, Response: ReviewPage{}})
	a := r.Authed()
	a.POST("/bookings/:id/reviews", httpx.HandleJSON(201, func(c *gin.Context, p *httpx.Principal, req CreateRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Create(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Review a completed booking", Request: CreateRequest{}, Response: Review{}, Status: 201,
		Description: "Comments with contact details, links or abusive language are held for moderation (status pending)."})
	a.GET("/bookings/:id/reviews", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.ForBooking(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Reviews on a booking and what you may still review", Response: BookingReviews{}})
	a.GET("/owner/reviews", httpx.OrgHandle(200, ownerRoles, func(c *gin.Context, s httpx.Scope) (any, error) {
		var q httpx.PageQuery
		if err := c.ShouldBindQuery(&q); err != nil {
			return nil, httpx.BindError(err)
		}
		return h.svc.OwnerReceived(c.Request.Context(), s.Org, q)
	}), httpx.Doc{Summary: "Reviews of your organisation and machines", Query: httpx.PageQuery{}, Response: ReviewPage{}, OrgScoped: true})
	a.POST("/reviews/:id/reply", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req ReplyRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Reply(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Reply publicly to a review of you (once)", Request: ReplyRequest{}, Response: Review{}})
	a.POST("/reviews/:id/report", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req ReportRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Report(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Report a review", Request: ReportRequest{}, Response: ReportResult{},
		Description: fmt.Sprintf("%d reports hide the review until a moderator decides.", HideThreshold)})

	mod := r.Admin(httpx.RoleAdminOps, httpx.RoleAdminSupport)
	mod.GET("/admin/reviews", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q AdminQuery) (any, error) {
		return h.svc.Queue(c.Request.Context(), q)
	}), httpx.Doc{Summary: "Review moderation queue", Query: AdminQuery{}, Response: AdminPage{}})
	mod.POST("/admin/reviews/:id/moderate", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req ModerateRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Moderate(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Publish or hide a review (audited)", Request: ModerateRequest{}, Response: AdminReview{}})
}
