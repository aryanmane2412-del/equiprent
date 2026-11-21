package tests

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand/v2"
	"net/http"
	"strings"
	"testing"
)

// photoJPEG is a listing-sized photo.
func photoJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	c := color.RGBA{R: uint8(rand.IntN(255)), G: 160, B: 40, A: 255}
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: c.R, G: uint8((x + y) % 255), B: c.B, A: 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// attachPhoto uploads a photo and attaches it to a listing.
func (u *user) attachPhoto(eqID string) resp {
	u.e.t.Helper()
	id := u.uploadID("equipment_photo", "front.jpg", photoJPEG(u.e.t, 640, 480))
	return must(u.e.t, u.do(http.MethodPost, "/owner/equipment/"+eqID+"/photos", map[string]any{"upload_id": id, "angle": "front"}), 201, "attach photo")
}

func (u *user) attachEquipmentDocument(eqID string) {
	u.e.t.Helper()
	upload := u.uploadID("equipment_document", "ownership.pdf", []byte(samplePDF))
	must(u.e.t, u.do(http.MethodPost, "/owner/equipment/"+eqID+"/documents", map[string]any{
		"doc_type": "invoice", "upload_id": upload,
	}), 201, "attach equipment document")
}

// verifiedOwner registers an owner whose business verification is approved.
func (e *env) verifiedOwner(admin *user, name string) *user {
	e.t.Helper()
	o := e.registerAndVerify("equipment_owner", name)
	e.verifyOwner(o, admin)
	return o
}

func listingBody(extra map[string]any) map[string]any {
	b := map[string]any{
		"title": "Liugong 835H wheel loader for hire", "category": "wheel-loader", "manufacturer": "Liugong", "model": "835H",
		"manufacture_year": 2021, "condition": "excellent", "delivery_available": true, "location_label": "Chakan MIDC, Pune",
		"registration_no": fmt.Sprintf("MH14%s%04d", randomLetters(3), rand.IntN(10000)),
		"current_hmr":     2150, "base_location": yard, "service_radius_km": 40, "dry_hire": true,
		"description": "Well maintained loader, 1.8 m³ bucket, serviced every 250 hours.",
	}
	for k, v := range extra {
		b[k] = v
	}
	return b
}

// publish takes a listing through pricing, photo, submit and approval.
func (e *env) publish(owner, ops *user, body map[string]any) string {
	t := e.t
	t.Helper()
	l := must(t, owner.do(http.MethodPost, "/owner/equipment", body), 201, "create listing")
	id := l.str("id")
	must(t, owner.do(http.MethodPut, "/owner/equipment/"+id+"/pricing", map[string]any{
		"plans": []map[string]any{{"unit": "day", "with_operator": true, "rate_paise": 950000}, {"unit": "hour", "with_operator": true, "rate_paise": 140000}},
	}), 200, "pricing")
	owner.attachPhoto(id)
	owner.attachEquipmentDocument(id)
	must(t, owner.do(http.MethodPost, "/owner/equipment/"+id+"/submit", nil), 200, "submit")
	return id
}

func (e *env) opsAdmin() *user {
	e.t.Helper()
	a := e.login(randomPhone())
	e.exec(`INSERT INTO user_roles (user_id, role_code) VALUES ($1, 'admin_ops')`, a.id)
	return a
}

func browseIDs(r resp) string { return r.raw }

func TestListingCreationAndEditing(t *testing.T) {
	e := newEnv(t)
	kyc := e.kycAdmin()
	owner := e.verifiedOwner(kyc, "Chakan Loaders")

	// The category list is the marketplace set.
	cats := must(t, e.call(http.MethodGet, "/categories", "", "", nil, nil), 200, "categories")
	want := "backhoe-loader excavator bulldozer crane dump-truck wheel-loader road-roller forklift concrete-mixer tractor other"
	var slugs []string
	for _, c := range cats.list("data") {
		if slug := fmt.Sprint(c.(map[string]any)["slug"]); strings.Contains(" "+want+" ", " "+slug+" ") {
			slugs = append(slugs, slug) // other tests add their own categories
		}
	}
	if strings.Join(slugs, " ") != want {
		t.Fatalf("categories = %v", slugs)
	}

	// Manufacturer and model can be typed; new ones join the catalogue,
	// existing ones are reused case-insensitively.
	newMake := "Zoomlion" + randomLetters(3)
	l := must(t, owner.do(http.MethodPost, "/owner/equipment", listingBody(map[string]any{"manufacturer": newMake, "model": "ZL50", "category": "wheel-loader"})), 201, "create with new make")
	if l.str("make_name") != newMake || l.str("model_name") != "ZL50" || l.str("category_slug") != "wheel-loader" {
		t.Fatalf("created = %s", l.raw)
	}
	again := must(t, owner.do(http.MethodPost, "/owner/equipment", listingBody(map[string]any{"manufacturer": strings.ToUpper(newMake), "model": "zl50"})), 201, "same model")
	if again.str("model_id") != l.str("model_id") {
		t.Fatal("the existing model should be reused")
	}
	must(t, owner.do(http.MethodPost, "/owner/equipment", listingBody(map[string]any{"category": "crane"})), 400, "model of another category")
	must(t, owner.do(http.MethodPost, "/owner/equipment", listingBody(map[string]any{"category": "spaceship"})), 400, "unknown category")
	must(t, owner.do(http.MethodPost, "/owner/equipment", listingBody(map[string]any{"model": nil, "manufacturer": nil})), 400, "no model")
	must(t, owner.do(http.MethodPost, "/owner/equipment", listingBody(map[string]any{"condition": "brand-new"})), 400, "bad condition")

	id := l.str("id")
	if l.str("condition") != "excellent" || l.str("delivery_available") != "true" || l.str("location_label") != "Chakan MIDC, Pune" ||
		l.num("manufacture_year") != 2021 || l.str("status") != "draft" {
		t.Fatalf("fields = %s", l.raw)
	}

	// Editing: optimistic locking, new fields.
	v := int(l.num("version"))
	ed := must(t, owner.do(http.MethodPatch, "/owner/equipment/"+id, map[string]any{
		"version": v, "condition": "good", "delivery_available": false, "location_label": "Talegaon, Pune", "manufacture_year": 2020,
	}), 200, "edit")
	if ed.str("condition") != "good" || ed.str("delivery_available") != "false" || ed.num("manufacture_year") != 2020 {
		t.Fatalf("edited = %s", ed.raw)
	}
	must(t, owner.do(http.MethodPatch, "/owner/equipment/"+id, map[string]any{"version": v, "title": "Stale edit title"}), 412, "stale version")

	// Specifications.
	sp := must(t, owner.do(http.MethodPut, "/owner/equipment/"+id+"/specs", map[string]any{"specs": []map[string]any{
		{"key": "bucket_m3", "value_numeric": 3.0, "unit": "m3"}, {"key": "engine_hp", "value_numeric": 162},
	}}), 200, "specs")
	if len(sp.list("specs")) != 2 {
		t.Fatalf("specs = %s", sp.raw)
	}
	must(t, owner.do(http.MethodPut, "/owner/equipment/"+id+"/specs", map[string]any{"specs": []map[string]any{{"key": "Bucket Size", "value_text": "big"}}}), 400, "bad spec key")
}

func TestListingPhotosAndDocuments(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	kyc := e.kycAdmin()
	ops := e.opsAdmin()
	owner := e.verifiedOwner(kyc, "Photo Rentals")
	rival := e.verifiedOwner(kyc, "Rival Rentals")
	customer := e.registerAndVerify("customer", "")

	l := must(t, owner.do(http.MethodPost, "/owner/equipment", listingBody(nil)), 201, "create")
	id := l.str("id")

	// Upload validation: photos must be real, large-enough images.
	must(t, owner.upload("equipment_photo", "doc.pdf", []byte(samplePDF)), 400, "pdf as photo")
	must(t, owner.upload("equipment_photo", "tiny.png", samplePNG(t)), 400, "tiny photo")
	must(t, owner.upload("equipment_photo", "fake.jpg", []byte("GIF89a not really")), 400, "spoofed photo")
	foreign := rival.uploadID("equipment_photo", "theirs.jpg", photoJPEG(t, 640, 480))
	must(t, owner.do(http.MethodPost, "/owner/equipment/"+id+"/photos", map[string]any{"upload_id": foreign}), 400, "someone else's upload")
	doc := owner.uploadID("equipment_document", "rc.pdf", []byte(samplePDF))
	must(t, owner.do(http.MethodPost, "/owner/equipment/"+id+"/photos", map[string]any{"upload_id": doc}), 400, "document as photo")

	// Multiple photos; the first is the cover, the cover can change.
	first := owner.attachPhoto(id)
	second := owner.attachPhoto(id)
	third := owner.attachPhoto(id)
	photos := third.list("photos")
	if len(photos) != 3 || first.str("photos.0.is_cover") != "true" {
		t.Fatalf("photos = %s", third.raw)
	}
	if strings.Contains(third.raw, "object_key") || strings.Contains(third.raw, `"media/`) {
		t.Fatalf("storage keys leaked: %s", third.raw)
	}
	secondID := second.str("photos.1.id")
	cov := must(t, owner.do(http.MethodPost, "/owner/equipment/"+id+"/photos/"+secondID+"/cover", nil), 200, "set cover")
	if cov.str("photos.0.id") != secondID || cov.str("photos.0.is_cover") != "true" {
		t.Fatalf("cover = %s", cov.raw)
	}
	must(t, owner.do(http.MethodDelete, "/owner/equipment/"+id+"/photos/"+secondID, nil), 204, "delete cover")
	after := must(t, owner.do(http.MethodGet, "/owner/equipment/"+id, nil), 200, "listing")
	if len(after.list("photos")) != 2 || after.str("photos.0.is_cover") != "true" {
		t.Fatalf("after deleting the cover = %s", after.raw)
	}
	img := e.fetch(after.str("photos.0.url"))
	if img.Code != 200 || img.Header().Get("Content-Type") != "image/jpeg" || img.Header().Get("Content-Disposition") != "" ||
		!strings.Contains(img.Header().Get("Cache-Control"), "max-age") {
		t.Fatalf("photo fetch: %d %v", img.Code, img.Header())
	}
	if bytes.Contains(img.Body.Bytes(), []byte("Exif")) {
		t.Fatal("photo kept EXIF metadata")
	}
	must(t, owner.do(http.MethodPut, "/owner/equipment/"+id+"/pricing", map[string]any{
		"plans": []map[string]any{{"unit": "day", "with_operator": true, "rate_paise": 900000}},
	}), 200, "pricing before document check")
	must(t, owner.do(http.MethodPost, "/owner/equipment/"+id+"/submit", nil), 422, "cannot submit without equipment document")
	must(t, rival.do(http.MethodPost, "/owner/equipment/"+id+"/photos", map[string]any{"upload_id": foreign}), 404, "rival adds photo to my listing")
	must(t, rival.do(http.MethodDelete, "/owner/equipment/"+id+"/photos/"+after.str("photos.0.id"), nil), 404, "rival deletes my photo")

	// Documents are private to the owner and moderators.
	must(t, owner.do(http.MethodPost, "/owner/equipment/"+id+"/documents", map[string]any{
		"doc_type": "insurance", "upload_id": owner.uploadID("equipment_document", "ins.pdf", []byte(samplePDF)), "valid_until": "2001-01-01",
	}), 400, "expired insurance")
	rc := must(t, owner.do(http.MethodPost, "/owner/equipment/"+id+"/documents", map[string]any{
		"doc_type": "rc", "upload_id": doc, "document_number": "MH14AB1234", "valid_until": "2035-03-31",
	}), 201, "attach RC")
	docID := rc.str("id")
	link := must(t, owner.do(http.MethodGet, "/owner/equipment/"+id+"/documents/"+docID+"/url", nil), 200, "owner doc link")
	if f := e.fetch(link.str("url")); f.Code != 200 || !strings.HasPrefix(f.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("doc fetch: %d %v", f.Code, f.Header())
	}
	must(t, rival.do(http.MethodGet, "/owner/equipment/"+id+"/documents/"+docID+"/url", nil), 404, "rival doc link")
	must(t, customer.do(http.MethodGet, "/owner/equipment/"+id+"/documents/"+docID+"/url", nil), 403, "customer doc link")

	// Publish; customers see photos and specs but never documents.
	must(t, owner.do(http.MethodPut, "/owner/equipment/"+id+"/pricing", map[string]any{
		"plans": []map[string]any{{"unit": "day", "with_operator": true, "rate_paise": 900000}, {"unit": "day", "with_operator": false, "rate_paise": 700000}},
	}), 200, "pricing")
	must(t, owner.do(http.MethodPost, "/owner/equipment/"+id+"/submit", nil), 200, "submit")
	review := must(t, ops.do(http.MethodGet, "/admin/listings/"+id, nil), 200, "admin detail")
	if review.str("documents.0.download.url") == "" {
		t.Fatalf("moderator needs document links: %s", review.raw)
	}
	var audits int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'equipment_document.url_issued' AND entity_id = $1`, docID).Scan(&audits)
	if audits < 2 {
		t.Fatalf("document link audits = %d", audits)
	}
	pub := must(t, customer.do(http.MethodGet, "/equipment/"+id, nil), 200, "public detail")
	if len(pub.list("photos")) != 2 || pub.str("documents") != "<nil>" || pub.str("registration_no") != "<nil>" || pub.str("owner_verified") != "true" {
		t.Fatalf("public detail = %s", pub.raw)
	}
	if strings.Contains(pub.raw, "MH14AB1234") {
		t.Fatal("document number leaked to customers")
	}
}

func TestListingModerationAndBrowsing(t *testing.T) {
	e := newEnv(t)
	kyc := e.kycAdmin()
	ops := e.opsAdmin()
	owner := e.verifiedOwner(kyc, "Browse Rentals")
	customer := e.registerAndVerify("customer", "")
	_ = customer
	tag := randomLetters(6)

	live := e.publish(owner, ops, listingBody(map[string]any{"title": "Wheel loader " + tag + " live"}))

	// Drafts and listings under review are not browsable.
	draft := must(t, owner.do(http.MethodPost, "/owner/equipment", listingBody(map[string]any{"title": "Wheel loader " + tag + " draft"})), 201, "draft").str("id")
	b := must(t, e.call(http.MethodGet, "/equipment?q="+tag, "", "", nil, nil), 200, "browse")
	if !strings.Contains(b.raw, live) || strings.Contains(b.raw, draft) || len(b.list("data")) != 1 {
		t.Fatalf("browse = %s", b.raw)
	}
	card := b.list("data")[0].(map[string]any)
	if card["cover_url"] == nil || card["price_paise"].(float64) != 950000 || card["price_unit"] != "day" || card["owner_verified"] != true ||
		card["condition"] != "excellent" || card["location_label"] != "Chakan MIDC, Pune" {
		t.Fatalf("card = %v", card)
	}
	if n := len(must(t, e.call(http.MethodGet, "/equipment?q="+tag+"&category=crane", "", "", nil, nil), 200, "browse crane").list("data")); n != 0 {
		t.Fatal("category filter ignored")
	}
	must(t, e.call(http.MethodGet, "/equipment/"+draft, "", "", nil, nil), 404, "draft detail is private")
	must(t, e.call(http.MethodGet, fmt.Sprintf("/equipment/%s/availability?from=2026-12-01T00:00:00Z&to=2026-12-31T00:00:00Z", live), "", "", nil, nil), 200, "availability")

	// Content edits to a live listing send it back to review; price edits do not.
	cur := must(t, owner.do(http.MethodGet, "/owner/equipment/"+live, nil), 200, "mine")
	must(t, owner.do(http.MethodPut, "/owner/equipment/"+live+"/pricing", map[string]any{"plans": []map[string]any{{"unit": "day", "with_operator": true, "rate_paise": 990000}}}), 200, "reprice")
	if s := must(t, owner.do(http.MethodGet, "/owner/equipment/"+live, nil), 200, "mine").str("status"); s != "active" {
		t.Fatalf("price change status = %s", s)
	}
	cur = must(t, owner.do(http.MethodGet, "/owner/equipment/"+live, nil), 200, "mine")
	ed := must(t, owner.do(http.MethodPatch, "/owner/equipment/"+live, map[string]any{"version": int(cur.num("version")), "title": "Wheel loader " + tag + " renamed"}), 200, "retitle")
	if ed.str("status") != "pending_review" {
		t.Fatalf("title change status = %s", ed.str("status"))
	}
	if strings.Contains(must(t, e.call(http.MethodGet, "/equipment?q="+tag, "", "", nil, nil), 200, "browse").raw, live) {
		t.Fatal("listing under re-review still browsable")
	}

	// Rejection needs a reason, which only the owner sees.
	must(t, ops.do(http.MethodPost, "/admin/listings/"+live+"/reject", map[string]any{"reason": "Photos show a different machine"}), 403, "admins cannot reject (read-only)")
}

func TestListingPermissions(t *testing.T) {
	e := newEnv(t)
	kyc := e.kycAdmin()
	owner := e.verifiedOwner(kyc, "Perm Rentals")
	rival := e.verifiedOwner(kyc, "Other Perm Rentals")
	customer := e.registerAndVerify("customer", "")
	unverified := e.registerAndVerify("equipment_owner", "New Owner")

	must(t, customer.do(http.MethodPost, "/owner/equipment", listingBody(nil)), 403, "customer creates listing")
	must(t, e.call(http.MethodPost, "/owner/equipment", "", owner.org, listingBody(nil), nil), 401, "anonymous creates listing")
	id := must(t, owner.do(http.MethodPost, "/owner/equipment", listingBody(nil)), 201, "create").str("id")
	v := 1
	for name, r := range map[string]resp{
		"rival reads":    rival.do(http.MethodGet, "/owner/equipment/"+id, nil),
		"rival edits":    rival.do(http.MethodPatch, "/owner/equipment/"+id, map[string]any{"version": v, "title": "Hijacked listing"}),
		"rival prices":   rival.do(http.MethodPut, "/owner/equipment/"+id+"/pricing", map[string]any{"plans": []map[string]any{{"unit": "day", "with_operator": true, "rate_paise": 100}}}),
		"rival submits":  rival.do(http.MethodPost, "/owner/equipment/"+id+"/submit", nil),
		"rival deletes":  rival.do(http.MethodDelete, "/owner/equipment/"+id, nil),
		"rival docs":     rival.do(http.MethodPost, "/owner/equipment/"+id+"/documents", map[string]any{"doc_type": "rc", "upload_id": id}),
		"customer reads": customer.do(http.MethodGet, "/owner/equipment/"+id, nil),
	} {
		if r.code != 404 && r.code != 403 {
			t.Errorf("%s: %d %s", name, r.code, r.raw)
		}
	}

	// Unverified owners can prepare listings but not publish them.
	uid := must(t, unverified.do(http.MethodPost, "/owner/equipment", listingBody(nil)), 201, "unverified draft").str("id")
	if r := must(t, unverified.do(http.MethodPost, "/owner/equipment/"+uid+"/submit", nil), 422, "unverified submit"); r.str("code") != "KYC_REQUIRED" {
		t.Fatalf("code = %s", r.raw)
	}

	// Owners delete (retire) their listings.
	must(t, owner.do(http.MethodDelete, "/owner/equipment/"+id, nil), 204, "delete")
	must(t, owner.do(http.MethodGet, "/owner/equipment/"+id, nil), 404, "deleted listing")
}

func TestCategoryManagement(t *testing.T) {
	e := newEnv(t)
	kyc := e.kycAdmin()
	ops := e.opsAdmin()
	owner := e.verifiedOwner(kyc, "Category Rentals")
	slug := "piling-rig-" + strings.ToLower(randomLetters(4))

	must(t, owner.do(http.MethodPost, "/admin/categories", map[string]any{"slug": slug, "name_en": "Piling rig"}), 403, "owner adds category")
	must(t, ops.do(http.MethodPost, "/admin/categories", map[string]any{"slug": slug, "name_en": "Piling rig"}), 201, "ops adds a category")
}
