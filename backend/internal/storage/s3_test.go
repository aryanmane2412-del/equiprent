package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// AWS's published S3 SigV4 example ("GET Object", Authenticating Requests:
// Using the Authorization Header).
func TestSignV4MatchesAWSExample(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	emptyHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	SignV4(req, emptyHash, "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1", "s3",
		time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, " +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("authorization\n got %s\nwant %s", got, want)
	}
}

// fakeS3 is a minimal in-memory S3 that checks every request is signed by
// the expected key for the expected bucket path.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	paths   []string
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=minio/") || !strings.Contains(auth, "/us-east-1/s3/aws4_request") {
		http.Error(w, "SignatureDoesNotMatch", http.StatusForbidden)
		return
	}
	body, _ := io.ReadAll(r.Body)
	sum := sha256.Sum256(body)
	if r.Header.Get("X-Amz-Content-Sha256") != hex.EncodeToString(sum[:]) {
		http.Error(w, "XAmzContentSHA256Mismatch", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, r.Method+" "+r.URL.Path)
	switch r.Method {
	case http.MethodPut:
		f.objects[r.URL.Path] = body
	case http.MethodGet:
		data, ok := f.objects[r.URL.Path]
		if !ok {
			http.Error(w, "NoSuchKey", http.StatusNotFound)
			return
		}
		_, _ = w.Write(data)
	case http.MethodDelete:
		delete(f.objects, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}
}

func TestS3StoreRoundTrip(t *testing.T) {
	fake := &fakeS3{objects: map[string][]byte{}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	s := &S3{Endpoint: srv.URL, Region: "us-east-1", Bucket: "equiprent-private", AccessKey: "minio", SecretKey: "minio-secret", PathStyle: true}
	ctx := context.Background()

	if err := s.Put(ctx, "media/u1/photo.jpg", []byte("jpeg bytes")); err != nil {
		t.Fatal(err)
	}
	r, err := s.Open(ctx, "media/u1/photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	r.Close()
	if string(got) != "jpeg bytes" {
		t.Fatalf("read %q", got)
	}
	if err := s.Delete(ctx, "media/u1/photo.jpg"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(ctx, "media/u1/photo.jpg"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted object: %v", err)
	}
	if fake.paths[0] != "PUT /equiprent-private/media/u1/photo.jpg" {
		t.Fatalf("path-style addressing: %v", fake.paths)
	}
	if err := s.Put(ctx, "../etc/passwd", []byte("x")); err == nil {
		t.Fatal("unsafe key accepted")
	}

	bad := &S3{Endpoint: srv.URL, Region: "us-east-1", Bucket: "b", AccessKey: "someone-else", SecretKey: "x", PathStyle: true}
	if err := bad.Put(ctx, "media/x.jpg", []byte("x")); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("rejected request should surface: %v", err)
	}
}

func TestS3VirtualHostedURL(t *testing.T) {
	s := &S3{Endpoint: "https://s3.ap-south-1.amazonaws.com", Bucket: "equiprent-docs"}
	u, err := s.objectURL("kyc/a/b.pdf")
	if err != nil || u.String() != "https://equiprent-docs.s3.ap-south-1.amazonaws.com/kyc/a/b.pdf" {
		t.Fatalf("url = %v %v", u, err)
	}
}
