package filecheck

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// PNGBytes returns a valid w x h PNG (exported for other packages' tests).
func pngBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, x%h, color.RGBA{R: 200, A: 255})
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

const minimalPDF = "%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n"

func TestAcceptsByContentNotName(t *testing.T) {
	r, err := Check(IdentityDocument, []byte(minimalPDF))
	if err != nil || r.ContentType != "application/pdf" || r.Ext != "pdf" {
		t.Fatalf("pdf: %+v %v", r, err)
	}
	r, err = Check(BusinessDocument, pngBytes(200, 100))
	if err != nil || r.ContentType != "image/png" {
		t.Fatalf("png: %+v %v", r, err)
	}
}

func TestRejections(t *testing.T) {
	cases := map[string]struct {
		p    Purpose
		data []byte
		want string
	}{
		"empty":           {IdentityDocument, nil, "empty"},
		"executable":      {IdentityDocument, []byte("MZ\x90\x00 this is a windows exe"), "only PDF"},
		"html":            {IdentityDocument, []byte("<html><script>alert(1)</script>"), "only PDF"},
		"pdf with js":     {IdentityDocument, []byte("%PDF-1.4\n<< /OpenAction << /S /JavaScript /JS (app.alert(1)) >> >>\n%%EOF"), "scripts"},
		"encrypted pdf":   {IdentityDocument, []byte("%PDF-1.4\n<< /Encrypt 5 0 R >>\n%%EOF"), "password-protected"},
		"truncated pdf":   {IdentityDocument, []byte("%PDF-1.4\n1 0 obj"), "incomplete"},
		"pdf as avatar":   {Avatar, []byte(minimalPDF), "photos must be"},
		"tiny image":      {IdentityDocument, pngBytes(10, 10), "too small"},
		"fake png":        {IdentityDocument, append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}, []byte("garbage")...), "could not be read"},
		"oversized":       {Avatar, append([]byte{0xFF, 0xD8, 0xFF}, make([]byte, MaxAvatarBytes)...), "larger than"},
		"unknown purpose": {Purpose("selfie"), []byte(minimalPDF), "unknown"},
	}
	for name, tc := range cases {
		_, err := Check(tc.p, tc.data)
		if !IsRejection(err) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func TestAvatarsAreReencoded(t *testing.T) {
	var src bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 120, 120))
	_ = jpeg.Encode(&src, img, nil)
	// Append a fake EXIF/trailing payload after the JPEG end marker.
	data := append(src.Bytes(), []byte("Exif GPS 18.52N 73.85E secret trailing payload")...)
	r, err := Check(Avatar, data)
	if err != nil || r.ContentType != "image/jpeg" {
		t.Fatalf("avatar: %v", err)
	}
	if bytes.Contains(r.Data, []byte("GPS")) {
		t.Fatal("metadata survived re-encoding")
	}
	// Documents are stored untouched (evidence integrity).
	d, _ := Check(IdentityDocument, data)
	if !bytes.Equal(d.Data, data) {
		t.Fatal("documents must not be modified")
	}
}

func TestEquipmentPhotos(t *testing.T) {
	// A large PNG becomes a JPEG no wider than MaxPhotoSide.
	r, err := Check(EquipmentPhoto, pngBytes(3200, 1600))
	if err != nil {
		t.Fatal(err)
	}
	if r.ContentType != "image/jpeg" || r.Width != MaxPhotoSide || r.Height != MaxPhotoSide/2 {
		t.Fatalf("photo = %s %dx%d", r.ContentType, r.Width, r.Height)
	}
	// EXIF (e.g. GPS of the owner's yard) is not kept: re-encoded output has no APP1 segment.
	src := r.Data
	withExif := append([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x0E, 'E', 'x', 'i', 'f', 0, 0, 'G', 'P', 'S', '!', 0, 0}, src[2:]...)
	r2, err := Check(EquipmentPhoto, withExif)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(r2.Data, []byte("Exif")) {
		t.Fatal("EXIF survived re-encoding")
	}
	if _, err := Check(EquipmentPhoto, pngBytes(200, 200)); !IsRejection(err) {
		t.Fatalf("tiny photo accepted: %v", err)
	}
	if _, err := Check(EquipmentPhoto, []byte(minimalPDF)); !IsRejection(err) {
		t.Fatal("PDF accepted as a photo")
	}
	if r, err := Check(EquipmentDocument, []byte(minimalPDF)); err != nil || r.ContentType != "application/pdf" {
		t.Fatalf("equipment document pdf: %v", err)
	}
}
