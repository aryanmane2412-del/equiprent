package equipment

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/filecheck"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/storage"
)

// MaxPhotos is the number of photographs a listing may have.
const MaxPhotos = 20

// ModeratorRoles review listings and manage the catalogue.
var ModeratorRoles = []string{httpx.RoleAdminOps}

// Media is the part of the uploads service listings use.
type Media interface {
	Claim(ctx context.Context, userID, uploadID uuid.UUID, purposes ...filecheck.Purpose) (db.Upload, error)
	SignedURL(key string, ttl time.Duration) storage.SignedURL
	CacheableURL(key string, ttl time.Duration) storage.SignedURL
}

// MediaOptions sets link lifetimes.
type MediaOptions struct {
	PhotoTTL    time.Duration // photos: cacheable links
	DocumentTTL time.Duration // documents: short-lived links
}

// WithMedia enables photo/document handling.
func (s *Service) WithMedia(m Media, opt MediaOptions) *Service {
	if opt.PhotoTTL <= 0 {
		opt.PhotoTTL = time.Hour
	}
	if opt.DocumentTTL <= 0 {
		opt.DocumentTTL = 5 * time.Minute
	}
	s.media, s.mediaOpt = m, opt
	return s
}

// ListingStore is persistence for the listing system beyond Repository.
// PGRepository implements it; unit-test fakes may not.
type ListingStore interface {
	ResolveModel(ctx context.Context, categorySlug, manufacturer, model string) (uuid.UUID, error)
	ModelCategory(ctx context.Context, modelID uuid.UUID) (slug string, active bool, err error)
	Browse(ctx context.Context, p db.BrowseListingsParams) ([]Summary, error)
	AddListingPhoto(ctx context.Context, p db.AddListingPhotoParams) (uuid.UUID, error)
	DeletePhoto(ctx context.Context, equipmentID, photoID uuid.UUID) (bool, error)
	SetCover(ctx context.Context, equipmentID, photoID uuid.UUID) (bool, error)
	PhotoCount(ctx context.Context, equipmentID uuid.UUID) (int, error)
	Documents(ctx context.Context, equipmentID uuid.UUID) ([]Document, error)
	Document(ctx context.Context, equipmentID, docID uuid.UUID) (Document, error)
	AddDocument(ctx context.Context, p db.AddEquipmentDocumentParams) (uuid.UUID, error)
	DeleteDocument(ctx context.Context, equipmentID, docID uuid.UUID) (bool, error)
	RequireReview(ctx context.Context, id uuid.UUID) (bool, error)
	Moderate(ctx context.Context, id uuid.UUID, from []string, to string, note *string, by uuid.UUID) (StatusChange, error)
	Audit(ctx context.Context, p db.InsertListingAuditParams) error
	AdminCategories(ctx context.Context) ([]AdminCategory, error)
	Category(ctx context.Context, id uuid.UUID) (db.EquipmentCategory, error)
	CreateCategory(ctx context.Context, p db.CreateCategoryParams) (uuid.UUID, error)
	UpdateCategory(ctx context.Context, p db.UpdateCategoryParams) error
}

// ---- PostgreSQL ------------------------------------------------------------------------

func (r *PGRepository) ResolveModel(ctx context.Context, categorySlug, manufacturer, model string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.tx.InTx(ctx, func(ctx context.Context) error {
		q := r.q(ctx)
		cat, err := q.CategoryBySlug(ctx, categorySlug)
		if err != nil {
			if errors.Is(database.MapError(err), apperror.ErrNotFound) {
				return fieldErr("category", "unknown", "is not a known category")
			}
			return err
		}
		if !cat.IsActive {
			return fieldErr("category", "inactive", "is not available for new listings")
		}
		makeID, err := q.ResolveMake(ctx, manufacturer)
		if err != nil {
			return err
		}
		m, err := q.FindModel(ctx, db.FindModelParams{MakeID: makeID, Name: model})
		switch {
		case err == nil:
			if m.CategoryID != cat.ID {
				return fieldErr("model", "category", "this model belongs to another category")
			}
			id = m.ID
			return nil
		case !errors.Is(database.MapError(err), apperror.ErrNotFound):
			return err
		}
		id, err = q.InsertModel(ctx, db.InsertModelParams{CategoryID: cat.ID, MakeID: makeID, Name: model})
		return err
	})
	return id, database.MapError(err)
}

func (r *PGRepository) ModelCategory(ctx context.Context, modelID uuid.UUID) (string, bool, error) {
	row, err := r.q(ctx).ModelCategory(ctx, modelID)
	if err != nil {
		if errors.Is(database.MapError(err), apperror.ErrNotFound) {
			return "", false, fieldErr("model_id", "unknown", "is not a catalogue model")
		}
		return "", false, database.MapError(err)
	}
	return row.Slug, row.IsActive, nil
}

func (r *PGRepository) Browse(ctx context.Context, p db.BrowseListingsParams) ([]Summary, error) {
	rows, err := r.q(ctx).BrowseListings(ctx, p)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Summary, 0, len(rows))
	for _, e := range rows {
		out = append(out, Summary{
			ID: e.ID, Title: e.Title, Status: string(e.Status), Version: e.Version, InstantBook: e.InstantBook,
			RatingAvg: e.RatingAvg, RatingCount: e.RatingCount, ModelName: e.ModelName, MakeName: e.MakeName,
			CategorySlug: e.CategorySlug, CategoryName: e.CategoryName, ManufactureYear: e.ManufactureYear,
			Condition: string(e.Condition), LocationLabel: e.LocationLabel, CityName: e.CityName,
			DeliveryAvailable: e.DeliveryAvailable, WetHire: e.WetHire, DryHire: e.DryHire, OwnerOrgID: e.OwnerOrgID,
			OwnerName: e.OwnerName, OwnerVerified: e.OwnerVerified, PhotoCount: e.PhotoCount, PricePaise: e.PricePaise,
			PriceUnit: e.PriceUnit, ModerationNote: e.ModerationNote, CreatedAt: e.CreatedAt, CoverObjectKey: e.CoverObjectKey,
		})
	}
	return out, nil
}

func (r *PGRepository) AddListingPhoto(ctx context.Context, p db.AddListingPhotoParams) (uuid.UUID, error) {
	id, err := r.q(ctx).AddListingPhoto(ctx, p)
	return id, database.MapError(err)
}

func (r *PGRepository) DeletePhoto(ctx context.Context, equipmentID, photoID uuid.UUID) (bool, error) {
	var ok bool
	err := r.tx.InTx(ctx, func(ctx context.Context) error {
		q := r.q(ctx)
		n, err := q.DeleteListingPhoto(ctx, db.DeleteListingPhotoParams{ID: photoID, EquipmentID: equipmentID})
		if err != nil || n == 0 {
			return err
		}
		ok = true
		return q.EnsureListingCover(ctx, equipmentID)
	})
	return ok, database.MapError(err)
}

func (r *PGRepository) SetCover(ctx context.Context, equipmentID, photoID uuid.UUID) (bool, error) {
	var ok bool
	err := r.tx.InTx(ctx, func(ctx context.Context) error {
		q := r.q(ctx)
		if err := q.ClearListingCover(ctx, equipmentID); err != nil {
			return err
		}
		n, err := q.SetListingCover(ctx, db.SetListingCoverParams{ID: photoID, EquipmentID: equipmentID})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperror.ErrNotFound // rolls back the cleared cover
		}
		ok = true
		return nil
	})
	if errors.Is(err, apperror.ErrNotFound) {
		return false, nil
	}
	return ok, database.MapError(err)
}

func (r *PGRepository) PhotoCount(ctx context.Context, equipmentID uuid.UUID) (int, error) {
	n, err := r.q(ctx).CountListingPhotos(ctx, equipmentID)
	return int(n), database.MapError(err)
}

func toDoc(id uuid.UUID, docType db.EquipmentDocumentType, number *string, ct string, from, until *time.Time,
	status db.VerificationStatus, key string, created time.Time) Document {
	return Document{ID: id, DocType: string(docType), DocumentNumber: number, ContentType: ct, ValidFrom: dateStr(from),
		ValidUntil: dateStr(until), Status: string(status), CreatedAt: created, ObjectKey: key}
}

func (r *PGRepository) Documents(ctx context.Context, equipmentID uuid.UUID) ([]Document, error) {
	rows, err := r.q(ctx).ListListingDocuments(ctx, equipmentID)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Document, 0, len(rows))
	for _, d := range rows {
		out = append(out, toDoc(d.ID, d.DocType, d.DocumentNumber, d.ContentType, d.ValidFrom, d.ValidUntil, d.Status, d.ObjectKey, d.CreatedAt))
	}
	return out, nil
}

func (r *PGRepository) Document(ctx context.Context, equipmentID, docID uuid.UUID) (Document, error) {
	d, err := r.q(ctx).GetListingDocument(ctx, db.GetListingDocumentParams{ID: docID, EquipmentID: equipmentID})
	if err != nil {
		return Document{}, database.MapError(err)
	}
	return toDoc(d.ID, d.DocType, d.DocumentNumber, d.ContentType, d.ValidFrom, d.ValidUntil, d.Status, d.ObjectKey, d.CreatedAt), nil
}

func (r *PGRepository) AddDocument(ctx context.Context, p db.AddEquipmentDocumentParams) (uuid.UUID, error) {
	id, err := r.q(ctx).AddEquipmentDocument(ctx, p)
	return id, database.MapError(err)
}

func (r *PGRepository) DeleteDocument(ctx context.Context, equipmentID, docID uuid.UUID) (bool, error) {
	n, err := r.q(ctx).DeleteListingDocument(ctx, db.DeleteListingDocumentParams{ID: docID, EquipmentID: equipmentID})
	return n == 1, database.MapError(err)
}

func (r *PGRepository) RequireReview(ctx context.Context, id uuid.UUID) (bool, error) {
	n, err := r.q(ctx).RequireListingReview(ctx, id)
	return n == 1, database.MapError(err)
}

func (r *PGRepository) Moderate(ctx context.Context, id uuid.UUID, from []string, to string, note *string, by uuid.UUID) (StatusChange, error) {
	row, err := r.q(ctx).ModerateListing(ctx, db.ModerateListingParams{
		ID: id, FromStatuses: from, ToStatus: db.ListingStatus(to), Note: note, ModeratedBy: uuid.NullUUID{UUID: by, Valid: by != uuid.Nil},
	})
	if err != nil {
		return StatusChange{}, database.MapError(err)
	}
	return StatusChange{ID: row.ID, Status: string(row.Status), Version: row.Version}, nil
}

func (r *PGRepository) Audit(ctx context.Context, p db.InsertListingAuditParams) error {
	return database.MapError(r.q(ctx).InsertListingAudit(ctx, p))
}

func (r *PGRepository) AdminCategories(ctx context.Context) ([]AdminCategory, error) {
	rows, err := r.q(ctx).ListCategoriesWithCounts(ctx, db.ListCategoriesWithCountsParams{Lang: "hi", IncludeInactive: true})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]AdminCategory, 0, len(rows))
	for _, c := range rows {
		hi := c.NameLocal
		if hi == c.NameEn {
			hi = ""
		}
		out = append(out, AdminCategory{
			Category: Category{ID: c.ID, ParentID: c.ParentID, Slug: c.Slug, Name: c.NameEn, NameEn: c.NameEn, IconKey: c.IconKey,
				SortOrder: c.SortOrder, ListingCount: c.ListingCount, SpecSchema: c.SpecSchema},
			NameHi: hi, IsActive: c.IsActive, ModelCount: c.ModelCount,
		})
	}
	return out, nil
}

func (r *PGRepository) Category(ctx context.Context, id uuid.UUID) (db.EquipmentCategory, error) {
	c, err := r.q(ctx).GetCategory(ctx, id)
	return c, database.MapError(err)
}

func (r *PGRepository) CreateCategory(ctx context.Context, p db.CreateCategoryParams) (uuid.UUID, error) {
	id, err := r.q(ctx).CreateCategory(ctx, p)
	return id, database.MapError(err)
}

func (r *PGRepository) UpdateCategory(ctx context.Context, p db.UpdateCategoryParams) error {
	return database.MapError(r.q(ctx).UpdateCategory(ctx, p))
}

// ---- helpers ---------------------------------------------------------------------------

func fieldErr(field, code, msg string) *apperror.Error {
	return apperror.ErrValidation.WithFields(apperror.FieldError{Field: field, Code: code, Message: msg})
}

func dateStr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.DateOnly)
	return &s
}

func parseDay(field string, v *string) (*time.Time, error) {
	if v == nil || *v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, *v)
	if err != nil {
		return nil, fieldErr(field, "date", "use YYYY-MM-DD")
	}
	return &t, nil
}

var errNoMedia = apperror.ErrServiceUnavailable.WithDetail("file storage is not configured")

// resolveModel picks the catalogue model for a create/update request.
// It returns uuid.Nil when the request names no model.
func (s *Service) resolveModel(ctx context.Context, id *uuid.UUID, category, manufacturer, model string) (uuid.UUID, error) {
	category, manufacturer, model = strings.TrimSpace(category), strings.TrimSpace(manufacturer), strings.TrimSpace(model)
	switch {
	case id != nil && *id != uuid.Nil:
		if s.ext != nil {
			if _, active, err := s.ext.ModelCategory(ctx, *id); err != nil {
				return uuid.Nil, err
			} else if !active {
				return uuid.Nil, fieldErr("model_id", "inactive", "this category is not available for new listings")
			}
		}
		return *id, nil
	case category == "" && manufacturer == "" && model == "":
		return uuid.Nil, nil
	case category == "" || manufacturer == "" || model == "":
		return uuid.Nil, apperror.ErrValidation.WithDetail("give category, manufacturer and model together (or a catalogue model_id)")
	case s.ext == nil:
		return uuid.Nil, errNoMedia
	}
	return s.ext.ResolveModel(ctx, category, manufacturer, model)
}

// decorate signs photo links (and, for moderators, document links).
func (s *Service) decorate(l *Listing) {
	if s.media == nil {
		return
	}
	for i := range l.Photos {
		l.Photos[i].URL = s.media.CacheableURL(l.Photos[i].ObjectKey, s.mediaOpt.PhotoTTL).URL
	}
}

func (s *Service) cards(rows []Summary) []Summary {
	for i := range rows {
		if rows[i].CoverObjectKey != "" && s.media != nil {
			rows[i].CoverURL = s.media.CacheableURL(rows[i].CoverObjectKey, s.mediaOpt.PhotoTTL).URL
		}
	}
	return rows
}

// withDocuments adds the documents (owner and moderator views).
func (s *Service) withDocuments(ctx context.Context, l *Listing) error {
	if s.ext == nil {
		return nil
	}
	docs, err := s.ext.Documents(ctx, l.ID)
	if err != nil {
		return err
	}
	l.Documents = docs
	return nil
}

// contentChanged sends a live listing back to review after edits to what
// customers rely on (identity, photos, documents).
func (s *Service) contentChanged(ctx context.Context, id uuid.UUID) error {
	if s.ext == nil {
		return nil
	}
	_, err := s.ext.RequireReview(ctx, id)
	return err
}

// ---- photos ----------------------------------------------------------------------------

// AddPhoto attaches an uploaded photograph (purpose equipment_photo).
func (s *Service) AddPhoto(ctx context.Context, p *httpx.Principal, owner, id uuid.UUID, req AddPhotoRequest) (Listing, error) {
	if s.media == nil || s.ext == nil {
		return Listing{}, errNoMedia
	}
	l, err := s.Mine(ctx, owner, id)
	if err != nil {
		return Listing{}, err
	}
	if l.Status == StatusRemoved {
		return Listing{}, apperror.ErrInvalidTransition.WithDetail("this listing was removed")
	}
	if n, err := s.ext.PhotoCount(ctx, id); err != nil {
		return Listing{}, err
	} else if n >= MaxPhotos {
		return Listing{}, apperror.ErrBusinessRule.WithDetail("a listing can have at most 20 photos")
	}
	up, err := s.media.Claim(ctx, p.UserID, req.UploadID, filecheck.EquipmentPhoto)
	if err != nil {
		return Listing{}, err
	}
	photoID, err := s.ext.AddListingPhoto(ctx, db.AddListingPhotoParams{
		EquipmentID: id, ObjectKey: up.ObjectKey, ContentType: up.ContentType, SizeBytes: up.SizeBytes, Sha256: up.Sha256,
		Angle: req.Angle, UploadedBy: uuid.NullUUID{UUID: p.UserID, Valid: true},
	})
	if err != nil {
		return Listing{}, err
	}
	if req.IsCover {
		if _, err := s.ext.SetCover(ctx, id, photoID); err != nil {
			return Listing{}, err
		}
	}
	if err := s.contentChanged(ctx, id); err != nil {
		return Listing{}, err
	}
	return s.ownerView(ctx, owner, id)
}

func (s *Service) DeletePhoto(ctx context.Context, owner, id, photoID uuid.UUID) error {
	if s.ext == nil {
		return errNoMedia
	}
	if _, err := s.Mine(ctx, owner, id); err != nil {
		return err
	}
	ok, err := s.ext.DeletePhoto(ctx, id, photoID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.ErrNotFound
	}
	return nil
}

func (s *Service) SetCover(ctx context.Context, owner, id, photoID uuid.UUID) (Listing, error) {
	if s.ext == nil {
		return Listing{}, errNoMedia
	}
	if _, err := s.Mine(ctx, owner, id); err != nil {
		return Listing{}, err
	}
	ok, err := s.ext.SetCover(ctx, id, photoID)
	if err != nil {
		return Listing{}, err
	}
	if !ok {
		return Listing{}, apperror.ErrNotFound
	}
	return s.ownerView(ctx, owner, id)
}

// ---- documents -------------------------------------------------------------------------

func (s *Service) AddDocument(ctx context.Context, p *httpx.Principal, owner, id uuid.UUID, req DocumentRequest) (Document, error) {
	if s.media == nil || s.ext == nil {
		return Document{}, errNoMedia
	}
	if _, err := s.Mine(ctx, owner, id); err != nil {
		return Document{}, err
	}
	from, err := parseDay("valid_from", req.ValidFrom)
	if err != nil {
		return Document{}, err
	}
	until, err := parseDay("valid_until", req.ValidUntil)
	if err != nil {
		return Document{}, err
	}
	if from != nil && until != nil && until.Before(*from) {
		return Document{}, fieldErr("valid_until", "order", "must be after valid_from")
	}
	if until != nil && until.Before(time.Now().Truncate(24*time.Hour)) {
		return Document{}, fieldErr("valid_until", "expired", "this document has expired")
	}
	up, err := s.media.Claim(ctx, p.UserID, req.UploadID, filecheck.EquipmentDocument)
	if err != nil {
		return Document{}, err
	}
	docID, err := s.ext.AddDocument(ctx, db.AddEquipmentDocumentParams{
		EquipmentID: id, DocType: db.EquipmentDocumentType(req.DocType), DocumentNumber: req.DocumentNumber,
		ObjectKey: up.ObjectKey, ContentType: up.ContentType, SizeBytes: up.SizeBytes, Sha256: up.Sha256,
		ValidFrom: from, ValidUntil: until, UploadedBy: uuid.NullUUID{UUID: p.UserID, Valid: true},
	})
	if err != nil {
		return Document{}, err
	}
	return s.ext.Document(ctx, id, docID)
}

func (s *Service) DeleteDocument(ctx context.Context, owner, id, docID uuid.UUID) error {
	if s.ext == nil {
		return errNoMedia
	}
	if _, err := s.Mine(ctx, owner, id); err != nil {
		return err
	}
	ok, err := s.ext.DeleteDocument(ctx, id, docID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.ErrNotFound
	}
	return nil
}

// DocumentURL issues a short-lived link to a machine document; the caller
// has already been authorised (owner of the listing, or moderator).
func (s *Service) documentURL(ctx context.Context, p *httpx.Principal, role string, d Document) (storage.SignedURL, error) {
	reason := "equipment document link issued"
	reqID := httpx.RequestIDFromContext(ctx)
	if err := s.ext.Audit(ctx, db.InsertListingAuditParams{
		ActorUserID: uuid.NullUUID{UUID: p.UserID, Valid: true}, ActorRole: &role, Action: "equipment_document.url_issued",
		EntityType: "equipment_documents", EntityID: uuid.NullUUID{UUID: d.ID, Valid: true}, Reason: &reason, RequestID: &reqID,
	}); err != nil {
		return storage.SignedURL{}, err
	}
	return s.media.SignedURL(d.ObjectKey, s.mediaOpt.DocumentTTL), nil
}

func (s *Service) OwnerDocumentURL(ctx context.Context, p *httpx.Principal, owner, id, docID uuid.UUID) (storage.SignedURL, error) {
	if s.media == nil || s.ext == nil {
		return storage.SignedURL{}, errNoMedia
	}
	if _, err := s.Mine(ctx, owner, id); err != nil {
		return storage.SignedURL{}, err
	}
	d, err := s.ext.Document(ctx, id, docID)
	if err != nil {
		return storage.SignedURL{}, err
	}
	return s.documentURL(ctx, p, "owner", d)
}

// ---- browsing ----------------------------------------------------------------------------

// Browse lists approved listings for customers.
func (s *Service) Browse(ctx context.Context, q BrowseQuery) (SummaryPage, error) {
	if s.ext == nil {
		return SummaryPage{Data: []Summary{}}, nil
	}
	k, err := httpx.PageQuery{Limit: q.Limit, Cursor: q.Cursor}.Keyset()
	if err != nil {
		return SummaryPage{}, err
	}
	p := db.BrowseListingsParams{PublicOnly: true, DeliveryOnly: q.DeliveryOnly, VerifiedOnly: q.VerifiedOnly, After: k.After, ResultLimit: k.Fetch()}
	if q.Category != "" {
		p.CategorySlug = &q.Category
	}
	if q.Condition != "" {
		p.Condition = &q.Condition
	}
	if t := strings.TrimSpace(q.Q); t != "" {
		t = strings.NewReplacer("%", "", "_", "").Replace(t)
		p.Query = &t
	}
	if q.CityID != "" {
		p.CityID = uuid.NullUUID{UUID: uuid.MustParse(q.CityID), Valid: true}
	}
	rows, err := s.ext.Browse(ctx, p)
	if err != nil {
		return SummaryPage{}, err
	}
	pg := httpx.Paginate(s.cards(rows), k, func(s Summary) uuid.UUID { return s.ID })
	for i := range pg.Data {
		pg.Data[i].ModerationNote = nil
	}
	return SummaryPage{Data: pg.Data, NextCursor: pg.NextCursor}, nil
}

// ---- moderation --------------------------------------------------------------------------

// AdminDetail is the moderator's view: everything, documents with audited
// links.
func (s *Service) AdminDetail(ctx context.Context, p *httpx.Principal, id uuid.UUID) (Listing, error) {
	l, err := s.repo.Get(ctx, id)
	if err != nil {
		return Listing{}, err
	}
	s.decorate(&l)
	if err := s.withDocuments(ctx, &l); err != nil {
		return Listing{}, err
	}
	for i := range l.Documents {
		if s.media == nil {
			break
		}
		u, err := s.documentURL(ctx, p, "admin", l.Documents[i])
		if err != nil {
			return Listing{}, err
		}
		l.Documents[i].Download = &u
	}
	return l, nil
}

// Moderate runs an admin action and records the reviewer's note, which the
// owner sees. Rejecting, suspending and removing need a reason.
func (s *Service) Moderate(ctx context.Context, p *httpx.Principal, id uuid.UUID, action Action, reason string) (StatusChange, error) {
	if p == nil || !p.HasPlatformRole(ModeratorRoles...) {
		return StatusChange{}, apperror.ErrForbidden
	}
	if action != ActionApprove && action != ActionReject {
		return StatusChange{}, apperror.ErrForbidden.WithDetail("only approval or rejection is available in equipment verification")
	}
	t, ok := Transitions[action]
	if !ok || t.ownerOK {
		return StatusChange{}, apperror.ErrNotFound
	}
	reason = strings.TrimSpace(reason)
	needsReason := action == ActionReject || action == ActionSuspend || action == ActionRemove
	if needsReason && len(reason) < 5 {
		return StatusChange{}, fieldErr("reason", "required", "explain why (at least 5 characters); the owner sees this")
	}
	var note *string
	if reason != "" {
		note = &reason
	}
	if s.ext == nil {
		return s.Act(ctx, uuid.Nil, id, action)
	}
	ch, err := s.ext.Moderate(ctx, id, t.from, t.to, note, p.UserID)
	if errors.Is(err, apperror.ErrNotFound) {
		_, current, ownerErr := s.repo.Owner(ctx, id)
		if ownerErr != nil {
			return StatusChange{}, ownerErr
		}
		return StatusChange{}, apperror.ErrInvalidTransition.WithDetail("cannot " + string(action) + " a listing that is " + current)
	}
	return ch, err
}

// ---- categories ----------------------------------------------------------------------------

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func categoryName(en, hi string) json.RawMessage {
	m := map[string]string{"en": strings.TrimSpace(en)}
	if hi = strings.TrimSpace(hi); hi != "" {
		m["hi"] = hi
	}
	b, _ := json.Marshal(m)
	return b
}

func specSchema(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{}`), nil
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fieldErr("spec_schema", "object", `must map spec keys to "number" or "text"`)
	}
	for k, v := range m {
		if !specKeyPattern.MatchString(k) || (v != "number" && v != "text") {
			return nil, fieldErr("spec_schema", "invalid", `keys are lower_snake_case, values "number" or "text"`)
		}
	}
	return raw, nil
}

func (s *Service) AdminCategories(ctx context.Context) (AdminCategoryList, error) {
	if s.ext == nil {
		return AdminCategoryList{Data: []AdminCategory{}}, nil
	}
	list, err := s.ext.AdminCategories(ctx)
	return AdminCategoryList{Data: list}, err
}

func (s *Service) CreateCategory(ctx context.Context, req CategoryRequest) (AdminCategory, error) {
	slug := strings.TrimSpace(strings.ToLower(req.Slug))
	if !slugPattern.MatchString(slug) {
		return AdminCategory{}, fieldErr("slug", "format", "use lower-case words joined by hyphens, e.g. mini-excavator")
	}
	schema, err := specSchema(req.SpecSchema)
	if err != nil {
		return AdminCategory{}, err
	}
	id, err := s.ext.CreateCategory(ctx, db.CreateCategoryParams{
		Slug: slug, Name: categoryName(req.NameEn, req.NameHi), SortOrder: req.SortOrder, SpecSchema: schema, IconKey: req.IconKey,
	})
	if err != nil {
		return AdminCategory{}, err
	}
	if req.IsActive != nil && !*req.IsActive {
		if _, err := s.UpdateCategory(ctx, id, req); err != nil {
			return AdminCategory{}, err
		}
	}
	return s.adminCategory(ctx, id)
}

func (s *Service) UpdateCategory(ctx context.Context, id uuid.UUID, req CategoryRequest) (AdminCategory, error) {
	cur, err := s.ext.Category(ctx, id)
	if err != nil {
		return AdminCategory{}, err
	}
	schema := cur.SpecSchema
	if len(req.SpecSchema) > 0 {
		if schema, err = specSchema(req.SpecSchema); err != nil {
			return AdminCategory{}, err
		}
	}
	active := cur.IsActive
	if req.IsActive != nil {
		active = *req.IsActive
	}
	icon := cur.IconKey
	if req.IconKey != nil {
		icon = req.IconKey
	}
	if err := s.ext.UpdateCategory(ctx, db.UpdateCategoryParams{
		ID: id, Name: categoryName(req.NameEn, req.NameHi), SortOrder: req.SortOrder, IsActive: active, SpecSchema: schema, IconKey: icon,
	}); err != nil {
		return AdminCategory{}, err
	}
	return s.adminCategory(ctx, id)
}

func (s *Service) adminCategory(ctx context.Context, id uuid.UUID) (AdminCategory, error) {
	list, err := s.ext.AdminCategories(ctx)
	if err != nil {
		return AdminCategory{}, err
	}
	i := slices.IndexFunc(list, func(c AdminCategory) bool { return c.ID == id })
	if i < 0 {
		return AdminCategory{}, apperror.ErrNotFound
	}
	return list[i], nil
}

// ---- routes --------------------------------------------------------------------------------

func (h *Handler) listingRoutes(r *httpx.Router) {
	pub := r.Tag("equipment")
	pub.GET("/equipment", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q BrowseQuery) (any, error) {
		return h.svc.Browse(c.Request.Context(), q)
	}), httpx.Doc{Summary: "Browse approved listings", Query: BrowseQuery{}, Response: SummaryPage{},
		Description: "Only approved (active) listings in active categories. Photos are signed, time-limited links."})

	o := r.Tag("owner-equipment").Authed()
	org := httpx.Doc{OrgScoped: true}
	o.POST("/owner/equipment/:id/photos", h.ownedID(func(c *gin.Context, owner orgCtx) (any, error) {
		var req AddPhotoRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.AddPhoto(c.Request.Context(), owner.p, owner.org, owner.id, req)
	}, 201), with(org, httpx.Doc{Summary: "Attach an uploaded photo (purpose equipment_photo)", Request: AddPhotoRequest{}, Response: Listing{}, Status: 201,
		Description: "Up to 20 photos. Photos are re-encoded (EXIF and GPS removed). Adding a photo to a live listing sends it back to review."}))
	o.DELETE("/owner/equipment/:id/photos/:photo_id", h.ownedID(func(c *gin.Context, owner orgCtx) (any, error) {
		pid, err := httpx.Param(c, "photo_id")
		if err != nil {
			return nil, err
		}
		return httpx.Nothing, h.svc.DeletePhoto(c.Request.Context(), owner.org, owner.id, pid)
	}, 204), with(org, httpx.Doc{Summary: "Remove a photo", Status: 204}))
	o.POST("/owner/equipment/:id/photos/:photo_id/cover", h.ownedID(func(c *gin.Context, owner orgCtx) (any, error) {
		pid, err := httpx.Param(c, "photo_id")
		if err != nil {
			return nil, err
		}
		return h.svc.SetCover(c.Request.Context(), owner.org, owner.id, pid)
	}), with(org, httpx.Doc{Summary: "Make a photo the cover", Response: Listing{}}))
	o.POST("/owner/equipment/:id/documents", h.ownedID(func(c *gin.Context, owner orgCtx) (any, error) {
		var req DocumentRequest
		if err := httpx.Bind(c, &req); err != nil {
			return nil, err
		}
		return h.svc.AddDocument(c.Request.Context(), owner.p, owner.org, owner.id, req)
	}, 201), with(org, httpx.Doc{Summary: "Attach a machine document (RC, insurance, fitness…)", Request: DocumentRequest{}, Response: Document{}, Status: 201,
		Description: "Private: visible to the owner and moderators only, never to customers."}))
	o.DELETE("/owner/equipment/:id/documents/:doc_id", h.ownedID(func(c *gin.Context, owner orgCtx) (any, error) {
		did, err := httpx.Param(c, "doc_id")
		if err != nil {
			return nil, err
		}
		return httpx.Nothing, h.svc.DeleteDocument(c.Request.Context(), owner.org, owner.id, did)
	}, 204), with(org, httpx.Doc{Summary: "Remove a machine document", Status: 204}))
	o.GET("/owner/equipment/:id/documents/:doc_id/url", h.ownedID(func(c *gin.Context, owner orgCtx) (any, error) {
		did, err := httpx.Param(c, "doc_id")
		if err != nil {
			return nil, err
		}
		return h.svc.OwnerDocumentURL(c.Request.Context(), owner.p, owner.org, owner.id, did)
	}), with(org, httpx.Doc{Summary: "Short-lived link to a machine document (audited)", Response: storage.SignedURL{}}))

	ad := r.Tag("admin").Admin(ModeratorRoles...)
	ad.GET("/admin/listings/:id", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.AdminDetail(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Listing under review, with documents (audited links)", Response: Listing{}})
	ad.GET("/admin/categories", httpx.Handle(200, func(c *gin.Context, _ *httpx.Principal) (any, error) {
		return h.svc.AdminCategories(c.Request.Context())
	}), httpx.Doc{Summary: "All categories, including hidden ones", Response: AdminCategoryList{}})
	ad.POST("/admin/categories", httpx.HandleJSON(201, func(c *gin.Context, _ *httpx.Principal, req CategoryRequest) (any, error) {
		return h.svc.CreateCategory(c.Request.Context(), req)
	}), httpx.Doc{Summary: "Add a category", Request: CategoryRequest{}, Response: AdminCategory{}, Status: 201})
	ad.PUT("/admin/categories/:id", httpx.HandleJSON(200, func(c *gin.Context, _ *httpx.Principal, req CategoryRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.UpdateCategory(c.Request.Context(), id, req)
	}), httpx.Doc{Summary: "Edit, reorder, hide or show a category", Request: CategoryRequest{}, Response: AdminCategory{},
		Description: "Hidden categories disappear from browsing and cannot take new listings; existing listings keep their category."})
}
