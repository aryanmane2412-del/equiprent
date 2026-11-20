package idempotency

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/logger"
)

type memStore struct {
	mu   sync.Mutex
	recs map[string]*Record
}

func (m *memStore) Claim(_ context.Context, u uuid.UUID, key, method, path string, hash []byte) (bool, Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := u.String() + key
	if r, ok := m.recs[k]; ok {
		return false, *r, nil
	}
	m.recs[k] = &Record{Method: method, Path: path, RequestHash: hash}
	return true, Record{}, nil
}

func (m *memStore) Complete(_ context.Context, u uuid.UUID, key string, status int, body json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := int16(status)
	m.recs[u.String()+key].Status = &st
	m.recs[u.String()+key].Body = append(json.RawMessage(nil), body...)
	return nil
}

func (m *memStore) Release(_ context.Context, u uuid.UUID, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.recs, u.String()+key)
	return nil
}

func setup(t *testing.T) (*gin.Engine, *int) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	calls := 0
	user := uuid.New()
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Next()
		if len(c.Errors) > 0 && !c.Writer.Written() {
			httpx.WriteProblem(c, logger.Discard(), c.Errors.Last().Err)
		}
	})
	r.POST("/things",
		func(c *gin.Context) { httpx.SetPrincipal(c, &httpx.Principal{UserID: user}) },
		Middleware(&memStore{recs: map[string]*Record{}}, logger.Discard()),
		func(c *gin.Context) {
			calls++
			if strings.Contains(c.GetHeader("X-Fail"), "yes") {
				httpx.Fail(c, apperror.ErrConflict)
				return
			}
			c.JSON(http.StatusCreated, gin.H{"n": calls})
		})
	return r, &calls
}

func post(r http.Handler, key, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/things", strings.NewReader(body))
	if key != "" {
		req.Header.Set(HeaderKey, key)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestReplayReturnsTheFirstResponse(t *testing.T) {
	r, calls := setup(t)
	first := post(r, "key-00000001", `{"a":1}`)
	second := post(r, "key-00000001", `{"a":1}`)
	if first.Code != 201 || second.Code != 201 || first.Body.String() != second.Body.String() {
		t.Fatalf("first=%d %s second=%d %s", first.Code, first.Body, second.Code, second.Body)
	}
	if second.Header().Get(HeaderReplayed) != "true" || *calls != 1 {
		t.Fatalf("handler ran %d times, replayed=%q", *calls, second.Header().Get(HeaderReplayed))
	}
}

func TestDifferentBodySameKeyIsRejected(t *testing.T) {
	r, _ := setup(t)
	post(r, "key-00000002", `{"a":1}`)
	w := post(r, "key-00000002", `{"a":2}`)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "IDEMPOTENCY_KEY_REUSED") {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}

func TestMissingKeyIsRejected(t *testing.T) {
	r, calls := setup(t)
	if w := post(r, "", `{}`); w.Code != http.StatusBadRequest || *calls != 0 {
		t.Fatalf("got %d, calls=%d", w.Code, *calls)
	}
}

func TestFailedRequestReleasesTheKey(t *testing.T) {
	r, calls := setup(t)
	if w := post(r, "key-00000003", `{}`, "X-Fail", "yes"); w.Code != http.StatusConflict {
		t.Fatalf("got %d", w.Code)
	}
	if w := post(r, "key-00000003", `{}`); w.Code != http.StatusCreated || *calls != 2 {
		t.Fatalf("retry after failure: %d calls=%d", w.Code, *calls)
	}
}
