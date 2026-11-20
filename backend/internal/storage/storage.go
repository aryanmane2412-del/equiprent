// Package storage is the PRIVATE object store for uploaded documents and
// images, plus HMAC-signed, short-lived download URLs. Objects are never
// served from a public path: a URL is only minted after the caller passes an
// authorisation check, and it expires within minutes.
//
// Drivers: Local keeps files on disk (development, single server); S3 talks
// to any S3-compatible bucket with SigV4 (production). The bucket stays
// private either way; downloads are proxied through the signed /files route.
package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrNotFound is returned for missing objects.
var ErrNotFound = errors.New("storage: object not found")

// Store keeps private objects.
type Store interface {
	Put(ctx context.Context, key string, data []byte) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// keyPattern mirrors the SQL is_object_key() check: no absolute paths, no
// "..", only safe characters.
var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*/[A-Za-z0-9._/-]+$`)

// ValidKey reports whether key is a safe object key.
func ValidKey(key string) bool {
	return keyPattern.MatchString(key) && !strings.Contains(key, "..") && len(key) <= 512
}

// Local stores objects under a directory with 0600 permissions.
type Local struct{ Dir string }

func (l Local) path(key string) (string, error) {
	if !ValidKey(key) {
		return "", fmt.Errorf("storage: invalid key %q", key)
	}
	root, err := filepath.Abs(l.Dir)
	if err != nil {
		return "", err
	}
	p := filepath.Join(root, filepath.FromSlash(key))
	if !strings.HasPrefix(p, root+string(filepath.Separator)) {
		return "", fmt.Errorf("storage: key escapes the store")
	}
	return p, nil
}

func (l Local) Put(_ context.Context, key string, data []byte) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (l Local) Open(_ context.Context, key string) (io.ReadCloser, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return f, err
}

func (l Local) Delete(_ context.Context, key string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Signer mints and checks signed download URLs.
type Signer struct {
	secret []byte
	base   string // e.g. http://localhost:8080/api/v1/files
	now    func() time.Time
}

// NewSigner creates a signer; base is the absolute URL of the download route.
func NewSigner(secret, base string) *Signer {
	return &Signer{secret: []byte(secret), base: base, now: time.Now}
}

// SignedURL is a time-limited capability to read one object.
type SignedURL struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Signer) mac(key string, exp int64) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(key + "\n" + strconv.FormatInt(exp, 10)))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Sign returns a URL for key valid for ttl.
func (s *Signer) Sign(key string, ttl time.Duration) SignedURL {
	exp := s.now().Add(ttl).Unix()
	q := url.Values{"key": {key}, "exp": {strconv.FormatInt(exp, 10)}, "sig": {s.mac(key, exp)}}
	return SignedURL{URL: s.base + "?" + q.Encode(), ExpiresAt: time.Unix(exp, 0).UTC()}
}

// SignCacheable returns a link valid for at least ttl whose expiry is
// rounded to a ttl boundary, so repeated requests within the window get the
// same URL and browsers can cache the image. Use for non-sensitive images
// (listing photos, avatars), never for identity documents.
func (s *Signer) SignCacheable(key string, ttl time.Duration) SignedURL {
	step := int64(ttl / time.Second)
	if step <= 0 {
		return s.Sign(key, ttl)
	}
	exp := (s.now().Unix()/step + 2) * step
	q := url.Values{"key": {key}, "exp": {strconv.FormatInt(exp, 10)}, "sig": {s.mac(key, exp)}}
	return SignedURL{URL: s.base + "?" + q.Encode(), ExpiresAt: time.Unix(exp, 0).UTC()}
}

// Errors from Verify.
var (
	ErrBadSignature = errors.New("storage: invalid signature")
	ErrExpired      = errors.New("storage: link expired")
)

// Verify checks a key / exp / sig triple from a download request.
func (s *Signer) Verify(key, expRaw, sig string) error {
	exp, err := strconv.ParseInt(expRaw, 10, 64)
	if err != nil || !ValidKey(key) {
		return ErrBadSignature
	}
	if !hmac.Equal([]byte(sig), []byte(s.mac(key, exp))) {
		return ErrBadSignature
	}
	if s.now().Unix() > exp {
		return ErrExpired
	}
	return nil
}
