// Package filecheck validates uploaded files by their CONTENT (never the
// file name or the client's declared type): allowed kinds per purpose, size
// limits, PDF structure and active-content rejection, image decoding with
// dimension limits. Profile images are re-encoded, which strips metadata
// such as EXIF GPS coordinates.
package filecheck

import (
	"bytes"
	"errors"
	"fmt"
	xdraw "golang.org/x/image/draw"
	"image"
	"image/jpeg"
	"image/png"
	"regexp"

	_ "golang.org/x/image/webp" // register WebP decoding
)

// Purpose of an upload.
type Purpose string

const (
	IdentityDocument Purpose = "identity_document"
	BusinessDocument Purpose = "business_document"
	OperatorDocument Purpose = "operator_document"
	Avatar           Purpose = "avatar"
	// EquipmentPhoto is a listing photograph (shown to customers).
	EquipmentPhoto Purpose = "equipment_photo"
	// EquipmentDocument is a machine document (RC, insurance, fitness…).
	EquipmentDocument Purpose = "equipment_document"
	// ChatImage is a photo sent in a chat (re-encoded, scaled down).
	ChatImage Purpose = "chat_image"
	// MaintenanceDocument is a service invoice or job card.
	MaintenanceDocument Purpose = "maintenance_document"
	// JobPhoto is handover/return or issue evidence taken by an operator or
	// a party (re-encoded, scaled down, metadata dropped).
	JobPhoto Purpose = "job_photo"
)

// MaxChatSide is the longest side chat photos are scaled down to.
const MaxChatSide = 2048

// imageOnly reports purposes that accept images but not PDFs; their images
// are re-encoded, which drops EXIF metadata (GPS position, device) and any
// payload appended to the file.
func (p Purpose) imageOnly() bool {
	return p == Avatar || p == EquipmentPhoto || p == ChatImage || p == JobPhoto
}

// Valid reports whether p is a known purpose.
func (p Purpose) Valid() bool {
	switch p {
	case IdentityDocument, BusinessDocument, OperatorDocument, Avatar, EquipmentPhoto, EquipmentDocument, ChatImage, MaintenanceDocument, JobPhoto:
		return true
	}
	return false
}

// Limits per purpose.
const (
	MaxDocumentBytes = 10 << 20
	MaxAvatarBytes   = 5 << 20
	MaxImagePixels   = 40_000_000 // e.g. 8000 x 5000
	MaxImageSide     = 10_000
	MinImageSide     = 64
	// MaxPhotoSide is the longest side listing photos are scaled down to.
	MaxPhotoSide = 2560
	// MinPhotoSide keeps listing photos useful to customers.
	MinPhotoSide = 320
)

// Result describes an accepted file.
type Result struct {
	ContentType string
	Ext         string
	Data        []byte // possibly re-encoded (avatars, listing photos)
	Width       int    // images only
	Height      int
}

// Rejection is a user-safe validation failure.
type Rejection struct{ Reason string }

func (r *Rejection) Error() string { return r.Reason }

func reject(format string, args ...any) error {
	return &Rejection{Reason: fmt.Sprintf(format, args...)}
}

// IsRejection reports whether err is a validation failure.
func IsRejection(err error) bool {
	var r *Rejection
	return errors.As(err, &r)
}

// activePDF matches PDF features that run code or launch things.
var activePDF = regexp.MustCompile(`/(JavaScript|JS|Launch|EmbeddedFile|OpenAction|AA|RichMedia|XFA)\b`)

// Check validates data for purpose and returns the detected type.
func Check(p Purpose, data []byte) (Result, error) {
	if !p.Valid() {
		return Result{}, reject("unknown upload purpose")
	}
	limit := MaxDocumentBytes
	if p == Avatar {
		limit = MaxAvatarBytes
	}
	switch {
	case len(data) == 0:
		return Result{}, reject("the file is empty")
	case len(data) > limit:
		return Result{}, reject("the file is larger than %d MB", limit>>20)
	}

	switch {
	case bytes.HasPrefix(data, []byte("%PDF-")):
		if p.imageOnly() {
			return Result{}, reject("photos must be JPEG, PNG or WebP images")
		}
		if !bytes.Contains(data[max(0, len(data)-2048):], []byte("%%EOF")) {
			return Result{}, reject("the PDF is incomplete or corrupted")
		}
		if bytes.Contains(data, []byte("/Encrypt")) {
			return Result{}, reject("password-protected PDFs cannot be reviewed; upload an unprotected copy")
		}
		if activePDF.Match(data) {
			return Result{}, reject("PDFs with scripts, attachments or auto-actions are not accepted")
		}
		return Result{ContentType: "application/pdf", Ext: "pdf", Data: data}, nil
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return checkImage(p, data, "image/jpeg", "jpg")
	case bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}):
		return checkImage(p, data, "image/png", "png")
	case len(data) > 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return checkImage(p, data, "image/webp", "webp")
	}
	return Result{}, reject("only PDF, JPEG, PNG or WebP files are accepted")
}

func checkImage(p Purpose, data []byte, ct, ext string) (Result, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Result{}, reject("the image could not be read")
	}
	if cfg.Width < MinImageSide || cfg.Height < MinImageSide {
		return Result{}, reject("the image is too small (at least %d x %d pixels)", MinImageSide, MinImageSide)
	}
	if cfg.Width > MaxImageSide || cfg.Height > MaxImageSide || cfg.Width*cfg.Height > MaxImagePixels {
		return Result{}, reject("the image is too large in pixels")
	}
	if p == EquipmentPhoto && (cfg.Width < MinPhotoSide || cfg.Height < MinPhotoSide) {
		return Result{}, reject("listing photos must be at least %d x %d pixels", MinPhotoSide, MinPhotoSide)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Result{}, reject("the image is corrupted")
	}
	if !p.imageOnly() {
		return Result{ContentType: ct, Ext: ext, Data: data, Width: cfg.Width, Height: cfg.Height}, nil
	}
	switch p {
	case EquipmentPhoto:
		img = fitWithin(img, MaxPhotoSide)
	case ChatImage, JobPhoto:
		img = fitWithin(img, MaxChatSide)
	}
	// Re-encode: drops EXIF (GPS, device) and any trailing payload.
	var buf bytes.Buffer
	b := img.Bounds()
	if ct == "image/png" && p == Avatar {
		if err := png.Encode(&buf, img); err != nil {
			return Result{}, err
		}
		return Result{ContentType: "image/png", Ext: "png", Data: buf.Bytes(), Width: b.Dx(), Height: b.Dy()}, nil
	}
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		return Result{}, err
	}
	return Result{ContentType: "image/jpeg", Ext: "jpg", Data: buf.Bytes(), Width: b.Dx(), Height: b.Dy()}, nil
}

// fitWithin scales img down so its longest side is at most side.
func fitWithin(img image.Image, side int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= side && h <= side {
		return img
	}
	if w >= h {
		h = h * side / w
		w = side
	} else {
		w = w * side / h
		h = side
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	return dst
}
