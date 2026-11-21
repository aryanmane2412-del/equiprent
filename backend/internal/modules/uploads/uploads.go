// Package uploads accepts files into the PRIVATE store and serves them back
// only through short-lived signed URLs.
//
//   - POST /uploads (multipart: purpose + file): content-based validation
//     (filecheck), random object key (the client's file name is never used
//     as a path), SHA-256 recorded, per-user hourly quota. Returns an
//     upload id that forms reference; nothing is readable yet.
//   - GET /files?key&exp&sig: verifies the HMAC signature and expiry, then
//     streams the object with no-store caching, nosniff and (for documents)
//     attachment disposition. Signed URLs are minted by the modules that own
//     the data, after their own authorisation checks.
package uploads

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/filecheck"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/storage"
)

// MaxPerHour bounds uploads per user.
const MaxPerHour = 40

// Upload is a stored, validated file (not yet readable by anyone).
type Upload struct {
	ID          uuid.UUID `json:"id"`
	Purpose     string    `json:"purpose"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	SHA256      string    `json:"sha256"`
	FileName    *string   `json:"file_name,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// UploadForm documents the multipart fields (OpenAPI only).
type UploadForm struct {
	Purpose string `json:"purpose" binding:"required,oneof=identity_document business_document operator_document avatar equipment_photo equipment_document chat_image maintenance_document job_photo" doc:"multipart field"`
	File    string `json:"file" binding:"required" doc:"multipart file field: PDF, JPEG, PNG or WebP; documents up to 10 MB, profile images up to 5 MB"`
}

// Record is the stored upload row.
type Record = db.Upload

// Repository persists uploads.
type Repository interface {
	Create(ctx context.Context, p db.CreateUploadParams) (db.Upload, error)
	Get(ctx context.Context, id uuid.UUID) (db.Upload, error)
	ByKey(ctx context.Context, key string) (db.Upload, error)
	MarkAttached(ctx context.Context, id uuid.UUID) error
	RecentCount(ctx context.Context, userID uuid.UUID) (int, error)
}

type PGRepository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *PGRepository { return &PGRepository{pool: pool} }

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

func (r *PGRepository) Create(ctx context.Context, p db.CreateUploadParams) (db.Upload, error) {
	u, err := r.q(ctx).CreateUpload(ctx, p)
	return u, database.MapError(err)
}
func (r *PGRepository) Get(ctx context.Context, id uuid.UUID) (db.Upload, error) {
	u, err := r.q(ctx).GetUpload(ctx, id)
	return u, database.MapError(err)
}
func (r *PGRepository) ByKey(ctx context.Context, key string) (db.Upload, error) {
	u, err := r.q(ctx).GetUploadByKey(ctx, key)
	return u, database.MapError(err)
}
func (r *PGRepository) MarkAttached(ctx context.Context, id uuid.UUID) error {
	return database.MapError(r.q(ctx).MarkUploadAttached(ctx, id))
}
func (r *PGRepository) RecentCount(ctx context.Context, userID uuid.UUID) (int, error) {
	n, err := r.q(ctx).CountRecentUploads(ctx, userID)
	return int(n), database.MapError(err)
}

// Service validates and stores uploads, and resolves them for other modules.
type Service struct {
	repo   Repository
	store  storage.Store
	signer *storage.Signer
	log    *slog.Logger
}

func NewService(repo Repository, store storage.Store, signer *storage.Signer, log *slog.Logger) *Service {
	return &Service{repo: repo, store: store, signer: signer, log: log}
}

var keyPrefix = map[filecheck.Purpose]string{
	filecheck.IdentityDocument:    "kyc",
	filecheck.BusinessDocument:    "kyc",
	filecheck.OperatorDocument:    "kyc",
	filecheck.Avatar:              "avatars",
	filecheck.EquipmentPhoto:      "media",
	filecheck.EquipmentDocument:   "eqdocs",
	filecheck.ChatImage:           "chat",
	filecheck.MaintenanceDocument: "maint",
	filecheck.JobPhoto:            "jobs",
}

// AgreementDocument is the purpose of server-generated agreement PDFs (never
// uploaded by users).
const AgreementDocument = "agreement_document"

// Inline reports purposes whose files are images shown in the app (served
// inline and cacheable); everything else downloads as an attachment.
func Inline(purpose string) bool {
	return purpose == string(filecheck.Avatar) || purpose == string(filecheck.EquipmentPhoto) || purpose == string(filecheck.ChatImage) ||
		purpose == string(filecheck.JobPhoto)
}

// StoreGenerated saves a server-generated file (agreement PDFs) under the
// uploads registry so it is served only through signed links. It skips
// content validation: the server wrote the bytes itself.
func (s *Service) StoreGenerated(ctx context.Context, userID uuid.UUID, purpose, contentType, ext, fileName string, data []byte) (db.Upload, error) {
	sum := sha256.Sum256(data)
	key := fmt.Sprintf("generated/%s/%s.%s", purpose, uuid.Must(uuid.NewV7()), ext)
	if err := s.store.Put(ctx, key, data); err != nil {
		return db.Upload{}, fmt.Errorf("store generated file: %w", err)
	}
	name := cleanName(fileName)
	row, err := s.repo.Create(ctx, db.CreateUploadParams{
		UserID: userID, Purpose: purpose, ObjectKey: key, ContentType: contentType,
		SizeBytes: int64(len(data)), Sha256: sum[:], OriginalName: &name,
	})
	if err != nil {
		_ = s.store.Delete(context.WithoutCancel(ctx), key)
		return db.Upload{}, err
	}
	return row, s.repo.MarkAttached(ctx, row.ID)
}

// Get returns an upload by id (for modules that reference uploads).
func (s *Service) Get(ctx context.Context, id uuid.UUID) (db.Upload, error) {
	return s.repo.Get(ctx, id)
}

// Open reads a stored object (server-side use, e.g. sending a PDF to a provider).
func (s *Service) Open(ctx context.Context, key string) ([]byte, error) {
	r, err := s.store.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, 20<<20))
}

// Store validates and saves a file for userID.
func (s *Service) Store(ctx context.Context, userID uuid.UUID, purpose filecheck.Purpose, fileName string, data []byte) (Upload, error) {
	if !purpose.Valid() {
		return Upload{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "purpose", Code: "oneof", Message: "unknown purpose"})
	}
	n, err := s.repo.RecentCount(ctx, userID)
	if err != nil {
		return Upload{}, err
	}
	if n >= MaxPerHour {
		return Upload{}, apperror.ErrRateLimited.WithDetail("too many uploads; try again later").WithRetryAfter(time.Hour)
	}
	res, err := filecheck.Check(purpose, data)
	if err != nil {
		if filecheck.IsRejection(err) {
			return Upload{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "file", Code: "invalid_file", Message: err.Error()})
		}
		return Upload{}, err
	}
	sum := sha256.Sum256(res.Data)
	key := fmt.Sprintf("%s/%s/%s.%s", keyPrefix[purpose], userID, uuid.Must(uuid.NewV7()), res.Ext)
	if err := s.store.Put(ctx, key, res.Data); err != nil {
		return Upload{}, fmt.Errorf("store upload: %w", err)
	}
	var name *string
	if clean := cleanName(fileName); clean != "" {
		name = &clean
	}
	row, err := s.repo.Create(ctx, db.CreateUploadParams{
		UserID: userID, Purpose: string(purpose), ObjectKey: key, ContentType: res.ContentType,
		SizeBytes: int64(len(res.Data)), Sha256: sum[:], OriginalName: name,
	})
	if err != nil {
		_ = s.store.Delete(context.WithoutCancel(ctx), key)
		return Upload{}, err
	}
	return toUpload(row), nil
}

func toUpload(u db.Upload) Upload {
	return Upload{ID: u.ID, Purpose: u.Purpose, ContentType: u.ContentType, SizeBytes: u.SizeBytes,
		SHA256: fmt.Sprintf("%x", u.Sha256), FileName: u.OriginalName, CreatedAt: u.CreatedAt}
}

// cleanName keeps a display-only file name (never used as a path).
func cleanName(n string) string {
	n = path.Base(strings.ReplaceAll(n, `\`, "/"))
	if n == "." || n == "/" || !utf8.ValidString(n) {
		return ""
	}
	n = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, n)
	if len(n) > 120 {
		n = n[:120]
	}
	return n
}

// Errors returned when another module claims an upload.
var (
	ErrNotYours     = apperror.ErrValidation.WithFields(apperror.FieldError{Field: "upload_id", Code: "not_found", Message: "unknown upload"})
	ErrWrongPurpose = apperror.ErrValidation.WithFields(apperror.FieldError{Field: "upload_id", Code: "purpose", Message: "this upload was made for a different purpose"})
)

// Claim returns the caller's upload for one of the allowed purposes and
// marks it attached. Uploads of other users are reported as unknown.
func (s *Service) Claim(ctx context.Context, userID, uploadID uuid.UUID, purposes ...filecheck.Purpose) (db.Upload, error) {
	u, err := s.repo.Get(ctx, uploadID)
	if errors.Is(err, apperror.ErrNotFound) || (err == nil && u.UserID != userID) {
		return db.Upload{}, ErrNotYours
	}
	if err != nil {
		return db.Upload{}, err
	}
	ok := false
	for _, p := range purposes {
		ok = ok || string(p) == u.Purpose
	}
	if !ok {
		return db.Upload{}, ErrWrongPurpose
	}
	return u, s.repo.MarkAttached(ctx, u.ID)
}

// SignedURL mints a download URL for an object key.
func (s *Service) SignedURL(key string, ttl time.Duration) storage.SignedURL {
	return s.signer.Sign(key, ttl)
}

// CacheableURL mints a stable link for non-sensitive images (listing photos).
func (s *Service) CacheableURL(key string, ttl time.Duration) storage.SignedURL {
	return s.signer.SignCacheable(key, ttl)
}

// Serve writes the object for a verified signed request.
func (s *Service) Serve(c *gin.Context) {
	key, exp, sig := c.Query("key"), c.Query("exp"), c.Query("sig")
	if err := s.signer.Verify(key, exp, sig); err != nil {
		code := http.StatusForbidden
		msg := "this link is invalid"
		if errors.Is(err, storage.ErrExpired) {
			msg = "this link has expired; request a new one"
		}
		httpx.Fail(c, apperror.New(code, "LINK_INVALID", "The download link is not valid").WithDetail(msg))
		return
	}
	u, err := s.repo.ByKey(c.Request.Context(), key)
	if err != nil {
		httpx.Fail(c, apperror.ErrNotFound)
		return
	}
	r, err := s.store.Open(c.Request.Context(), key)
	if err != nil {
		httpx.Fail(c, apperror.ErrNotFound)
		return
	}
	defer r.Close()
	h := c.Writer.Header()
	h.Set("Content-Type", u.ContentType)
	if Inline(u.Purpose) {
		h.Set("Cache-Control", "private, max-age=3600")
	} else {
		h.Set("Cache-Control", "private, no-store, max-age=0")
	}
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Referrer-Policy", "no-referrer")
	if !Inline(u.Purpose) {
		h.Set("Content-Disposition", `attachment; filename="`+downloadName(u)+`"`)
	}
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, r); err != nil {
		s.log.WarnContext(c.Request.Context(), "streaming file failed", "error", err)
	}
}

// downloadName is a safe file name: generated documents keep their name
// (e.g. agreement-ER-2611-AB12.pdf), uploads download as document.<ext>.
func downloadName(u db.Upload) string {
	if u.Purpose == AgreementDocument && u.OriginalName != nil {
		safe := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
				return r
			}
			return -1
		}, *u.OriginalName)
		if safe != "" && !strings.HasPrefix(safe, ".") {
			return safe
		}
	}
	return "document." + extFor(u.ContentType)
}

func extFor(ct string) string {
	switch ct {
	case "application/pdf":
		return "pdf"
	case "image/png":
		return "png"
	case "image/webp":
		return "webp"
	}
	return "jpg"
}

// ---- handler ----------------------------------------------------------------------

type Handler struct {
	svc      *Service
	maxBytes int64
}

func NewHandler(svc *Service, maxBytes int64) *Handler { return &Handler{svc: svc, maxBytes: maxBytes} }

func (h *Handler) Name() string { return "uploads" }

func (h *Handler) Routes(r *httpx.Router) {
	r = r.Tag("uploads")
	r.Authed().POST("/uploads", httpx.Handle(201, func(c *gin.Context, p *httpx.Principal) (any, error) {
		purpose := filecheck.Purpose(c.PostForm("purpose"))
		if purpose == filecheck.JobPhoto && !isBookingParty(p) {
			return nil, apperror.ErrForbidden.WithDetail("job photos can only be uploaded by the parties to a booking")
		}
		fh, err := c.FormFile("file")
		if err != nil {
			if errors.Is(err, http.ErrMissingFile) {
				return nil, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "file", Code: "required", Message: "choose a file"})
			}
			return nil, httpx.BindError(err)
		}
		if fh.Size > h.maxBytes {
			return nil, apperror.ErrPayloadTooLarge
		}
		data, err := readPart(fh, h.maxBytes)
		if err != nil {
			return nil, err
		}
		return h.svc.Store(c.Request.Context(), p.UserID, purpose, fh.Filename, data)
	}), httpx.Doc{Summary: "Upload a document or profile image (multipart/form-data)", Request: UploadForm{}, Response: Upload{}, Status: 201,
		Description: "Validated by content (PDF, JPEG, PNG, WebP). Stored privately; reference it by id. Nothing is publicly readable."})

	r.GET("/files", h.svc.Serve, httpx.Doc{Summary: "Download through a signed, short-lived link",
		Description: "Links are minted after an authorisation check and expire within minutes. Responses are never cached."})
}

func readPart(fh *multipart.FileHeader, limit int64) ([]byte, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, apperror.ErrBadRequest.Wrap(err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, apperror.ErrBadRequest.Wrap(err)
	}
	if int64(len(data)) > limit {
		return nil, apperror.ErrPayloadTooLarge
	}
	return data, nil
}

// isBookingParty reports whether the caller holds a role that takes part in bookings.
// Who may attach a job photo to a given booking is checked by the booking endpoints.
func isBookingParty(p *httpx.Principal) bool {
	for _, r := range p.Roles {
		switch r.Code {
		case httpx.RoleOperator, httpx.RoleOwner, httpx.RoleCustomer, httpx.RoleFleetManager:
			return true
		}
	}
	return false
}
