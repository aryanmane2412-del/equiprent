package httpx

import (
	"encoding/base64"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
)

// DefaultPageSize and MaxPageSize bound list endpoints.
const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// PageQuery is embedded in list query structs (?limit=20&cursor=...).
type PageQuery struct {
	Limit  int    `form:"limit" json:"limit,omitempty" binding:"omitempty,min=1,max=100"`
	Cursor string `form:"cursor" json:"cursor,omitempty"`
}

// Page is the response envelope for cursor-paginated lists.
type Page[T any] struct {
	Data       []T    `json:"data"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// Keyset is a decoded page request. All primary keys are UUIDv7, which sort by
// creation time, so lists page with "WHERE id < after ORDER BY id DESC".
type Keyset struct {
	After uuid.NullUUID
	Limit int
}

// Fetch is the number of rows to query: one extra tells whether there is a
// next page.
func (k Keyset) Fetch() int32 { return int32(k.Limit + 1) }

// Keyset validates and decodes the cursor.
func (q PageQuery) Keyset() (Keyset, error) {
	k := Keyset{Limit: q.Limit}
	if k.Limit <= 0 {
		k.Limit = DefaultPageSize
	}
	if k.Limit > MaxPageSize {
		k.Limit = MaxPageSize
	}
	if q.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || len(raw) != 16 {
			return k, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "cursor", Code: "invalid", Message: "is not a valid cursor"})
		}
		id, _ := uuid.FromBytes(raw)
		k.After = uuid.NullUUID{UUID: id, Valid: true}
	}
	return k, nil
}

// Paginate trims the look-ahead row and builds the envelope. idOf returns the
// keyset id of an item.
func Paginate[T any](items []T, k Keyset, idOf func(T) uuid.UUID) Page[T] {
	p := Page[T]{Data: items}
	if p.Data == nil {
		p.Data = []T{}
	}
	if len(items) > k.Limit {
		p.Data = items[:k.Limit]
		last := idOf(p.Data[len(p.Data)-1])
		p.NextCursor = base64.RawURLEncoding.EncodeToString(last[:])
	}
	return p
}
