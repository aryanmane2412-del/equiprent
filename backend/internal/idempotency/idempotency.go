// Package idempotency makes retried POSTs safe (docs/API_DESIGN.md §1): the
// first request with an Idempotency-Key runs and its response is stored;
// repeats replay that response; the same key with a different body is
// rejected; a repeat while the first is still running gets 409.
package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/httpx"
)

// Header names.
const (
	HeaderKey      = "Idempotency-Key"
	HeaderReplayed = "Idempotency-Replayed"
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

// Record is a stored key.
type Record struct {
	Method, Path string
	RequestHash  []byte
	Status       *int16
	Body         json.RawMessage
}

// Store persists keys per user.
type Store interface {
	// Claim inserts the key; when it already exists the stored record is
	// returned with claimed=false.
	Claim(ctx context.Context, user uuid.UUID, key, method, path string, hash []byte) (claimed bool, existing Record, err error)
	Complete(ctx context.Context, user uuid.UUID, key string, status int, body json.RawMessage) error
	Release(ctx context.Context, user uuid.UUID, key string) error
}

// PGStore implements Store on the idempotency_keys table (72 h retention,
// purged by the worker).
type PGStore struct{ pool *pgxpool.Pool }

func NewPGStore(pool *pgxpool.Pool) *PGStore { return &PGStore{pool: pool} }

func (s *PGStore) Claim(ctx context.Context, user uuid.UUID, key, method, path string, hash []byte) (bool, Record, error) {
	q := database.Queries(ctx, s.pool)
	_, err := q.InsertIdempotencyKey(ctx, db.InsertIdempotencyKeyParams{Key: key, UserID: user, Method: method, Path: path, RequestHash: hash})
	if err == nil {
		return true, Record{}, nil
	}
	if !errors.Is(database.MapError(err), apperror.ErrNotFound) { // ErrNoRows = conflict on the key
		return false, Record{}, database.MapError(err)
	}
	row, err := q.GetIdempotencyKey(ctx, db.GetIdempotencyKeyParams{UserID: user, Key: key})
	if err != nil {
		return false, Record{}, database.MapError(err)
	}
	return false, Record{Method: row.Method, Path: row.Path, RequestHash: row.RequestHash, Status: row.ResponseStatus, Body: row.ResponseBody}, nil
}

func (s *PGStore) Complete(ctx context.Context, user uuid.UUID, key string, status int, body json.RawMessage) error {
	st := int16(status)
	return database.MapError(database.Queries(ctx, s.pool).CompleteIdempotencyKey(ctx, db.CompleteIdempotencyKeyParams{
		UserID: user, Key: key, ResponseStatus: &st, ResponseBody: body,
	}))
}

func (s *PGStore) Release(ctx context.Context, user uuid.UUID, key string) error {
	return database.MapError(database.Queries(ctx, s.pool).DeleteIdempotencyKey(ctx, db.DeleteIdempotencyKeyParams{UserID: user, Key: key}))
}

type captureWriter struct {
	gin.ResponseWriter
	buf bytes.Buffer
}

func (w *captureWriter) Write(b []byte) (int, error) {
	w.buf.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *captureWriter) WriteString(s string) (int, error) {
	w.buf.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

// Middleware enforces Idempotency-Key on the routes it guards. It must run
// after authentication (keys are scoped per user).
func Middleware(store Store, log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := httpx.MustPrincipal(c)
		if !ok {
			return
		}
		key := c.GetHeader(HeaderKey)
		if key == "" {
			httpx.Fail(c, apperror.ErrIdempotencyRequired)
			return
		}
		if !keyPattern.MatchString(key) {
			httpx.Fail(c, apperror.ErrValidation.WithFields(apperror.FieldError{Field: HeaderKey, Code: "invalid", Message: "8-128 letters, digits, . _ : -"}))
			return
		}
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			httpx.Fail(c, httpx.BindError(err))
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		sum := sha256.Sum256(append([]byte(c.Request.Method+" "+c.Request.URL.Path+"\n"), body...))

		ctx := c.Request.Context()
		claimed, rec, err := store.Claim(ctx, p.UserID, key, c.Request.Method, c.Request.URL.Path, sum[:])
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		if !claimed {
			switch {
			case !bytes.Equal(rec.RequestHash, sum[:]):
				httpx.Fail(c, apperror.ErrIdempotencyMismatch)
			case rec.Status == nil:
				httpx.Fail(c, apperror.ErrIdempotencyInFlight)
			default:
				c.Header(HeaderReplayed, "true")
				c.Data(int(*rec.Status), "application/json; charset=utf-8", rec.Body)
				c.Abort()
			}
			return
		}

		w := &captureWriter{ResponseWriter: c.Writer}
		c.Writer = w
		c.Next()

		// Errors are rendered by the error middleware after this returns, so
		// a recorded error means the handler failed: release the key so the
		// client can retry. Successful responses are stored for replay.
		ctx = context.WithoutCancel(ctx)
		status := c.Writer.Status()
		if len(c.Errors) > 0 || status >= http.StatusInternalServerError || !json.Valid(w.buf.Bytes()) {
			if err := store.Release(ctx, p.UserID, key); err != nil {
				log.WarnContext(ctx, "release idempotency key", "error", err)
			}
			return
		}
		if err := store.Complete(ctx, p.UserID, key, status, w.buf.Bytes()); err != nil {
			log.WarnContext(ctx, "store idempotent response", "error", err)
		}
	}
}
