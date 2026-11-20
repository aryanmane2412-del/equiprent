package storage

import (
	"context"
	"errors"
	"io"
	"net/url"
	"testing"
	"time"
)

func TestLocalStoreRoundTripAndKeySafety(t *testing.T) {
	s := Local{Dir: t.TempDir()}
	ctx := context.Background()
	if err := s.Put(ctx, "kyc/u1/doc.pdf", []byte("data")); err != nil {
		t.Fatal(err)
	}
	r, err := s.Open(ctx, "kyc/u1/doc.pdf")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	_ = r.Close()
	if string(b) != "data" {
		t.Fatalf("read %q", b)
	}
	for _, bad := range []string{"../etc/passwd", "kyc/../../x", "/abs/path", "kyc\\..\\x", "Kyc/x", "kyc/a b"} {
		if err := s.Put(ctx, bad, []byte("x")); err == nil {
			t.Errorf("accepted unsafe key %q", bad)
		}
	}
	if _, err := s.Open(ctx, "kyc/u1/missing.pdf"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
	_ = s.Delete(ctx, "kyc/u1/doc.pdf")
	if _, err := s.Open(ctx, "kyc/u1/doc.pdf"); !errors.Is(err, ErrNotFound) {
		t.Error("delete failed")
	}
}

func TestSignedURLs(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	s := NewSigner("secret-secret-secret-secret-secret", "http://api/api/v1/files")
	s.now = func() time.Time { return now }
	u := s.Sign("kyc/u1/doc.pdf", 5*time.Minute)
	parsed, _ := url.Parse(u.URL)
	q := parsed.Query()
	if err := s.Verify(q.Get("key"), q.Get("exp"), q.Get("sig")); err != nil {
		t.Fatalf("valid url rejected: %v", err)
	}
	if err := s.Verify("kyc/u2/other.pdf", q.Get("exp"), q.Get("sig")); !errors.Is(err, ErrBadSignature) {
		t.Error("signature must bind the key")
	}
	if err := s.Verify(q.Get("key"), "9999999999", q.Get("sig")); !errors.Is(err, ErrBadSignature) {
		t.Error("signature must bind the expiry")
	}
	other := NewSigner("another-secret-another-secret-xx", "http://api/api/v1/files")
	if err := other.Verify(q.Get("key"), q.Get("exp"), q.Get("sig")); !errors.Is(err, ErrBadSignature) {
		t.Error("signature must bind the secret")
	}
	now = now.Add(6 * time.Minute)
	if err := s.Verify(q.Get("key"), q.Get("exp"), q.Get("sig")); !errors.Is(err, ErrExpired) {
		t.Errorf("expired url accepted: %v", err)
	}
}
