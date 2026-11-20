package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

const samplePDF = "%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n"

func samplePNG(t *testing.T) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 96, 96))
	for x := range 96 {
		img.Set(x, x, color.RGBA{R: 200, A: 255})
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func randomLicence() string { return fmt.Sprintf("MH12%011d", rand.Int64N(1e11)) }

func randomPAN() string {
	return randomLetters(5) + fmt.Sprintf("%04d", rand.IntN(10000)) + randomLetters(1)
}

// upload posts a multipart file.
func (u *user) upload(purpose, name string, data []byte) resp {
	u.e.t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("purpose", purpose)
	fw, _ := w.CreateFormFile("file", name)
	_, _ = fw.Write(data)
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/uploads", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+u.token)
	rec := httptest.NewRecorder()
	u.e.router.ServeHTTP(rec, req)
	out := resp{code: rec.Code, raw: rec.Body.String(), header: rec.Header()}
	_ = json.Unmarshal(rec.Body.Bytes(), &out.body)
	return out
}

// uploadID uploads and returns the id.
func (u *user) uploadID(purpose, name string, data []byte) string {
	u.e.t.Helper()
	return must(u.e.t, u.upload(purpose, name, data), 201, "upload "+name).str("id")
}

// fetch follows a signed link without credentials.
func (e *env) fetch(link string) *httptest.ResponseRecorder {
	e.t.Helper()
	parsed, err := url.Parse(link)
	if err != nil {
		e.t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, parsed.RequestURI(), nil)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

// kycAdmin is a signed-in verification reviewer.
func (e *env) kycAdmin() *user {
	e.t.Helper()
	a := e.login(randomPhone())
	e.exec(`INSERT INTO user_roles (user_id, role_code) VALUES ($1, 'admin_kyc')`, a.id)
	return a
}

// ownerBusiness is a valid business-details request for a limited company.
func ownerBusiness(u *user) map[string]any {
	pan := randomPAN()
	return map[string]any{
		"account_kind": "company", "org_type": "private_limited", "legal_name": "Patil Earthmovers Pvt Ltd",
		"business_name": "Patil Earthmovers", "pan": pan, "gstin": "27" + pan + "1Z5", "cin": "U45200MH2015PTC123456",
		"contact_name": "Ravi Patil", "contact_phone": u.phone, "contact_email": randomEmail(),
		"business_address": map[string]any{"line1": "Plot 7, MIDC Bhosari", "city": "Pune", "state_code": "27", "pincode": "411026"},
	}
}

// prepareOwner completes and submits owner verification; returns the
// application id.
func (e *env) prepareOwner(owner *user) string {
	t := e.t
	t.Helper()
	must(t, owner.do(http.MethodPut, "/verification/owner/business", ownerBusiness(owner)), 200, "business details")
	must(t, owner.do(http.MethodPost, "/verification/owner/documents", map[string]any{
		"doc_type": "pan", "upload_id": owner.uploadID("identity_document", "pan.pdf", []byte(samplePDF)), "document_number": "ABCDE1234F",
	}), 201, "identity doc")
	must(t, owner.do(http.MethodPost, "/verification/owner/documents", map[string]any{
		"doc_type": "gst_certificate", "upload_id": owner.uploadID("business_document", "gst.pdf", []byte(samplePDF)),
	}), 201, "business doc")
	must(t, owner.do(http.MethodPut, "/verification/owner/bank-account", map[string]any{
		"account_holder_name": "Patil Earthmovers Pvt Ltd", "account_number": "50100123456789",
		"confirm_account_number": "50100123456789", "ifsc": "HDFC0001234",
	}), 200, "bank account")
	return must(t, owner.do(http.MethodPost, "/verification/owner/submit", nil), 200, "submit owner").str("id")
}

// verifyOwner takes an owner organisation through verification.
func (e *env) verifyOwner(owner, admin *user) {
	e.t.Helper()
	id := e.prepareOwner(owner)
	must(e.t, admin.do(http.MethodPost, "/admin/verifications/"+id+"/approve", map[string]any{}), 200, "approve owner")
}

// prepareOperator completes and submits operator verification.
func (e *env) prepareOperator(op *user) string {
	t := e.t
	t.Helper()
	must(t, op.do(http.MethodPut, "/verification/operator/profile", map[string]any{
		"licence_number": randomLicence(), "licence_class": "HTV / earthmover", "licence_expires_on": time.Now().AddDate(3, 0, 0).Format(time.DateOnly),
		"experience_years": 7, "skills": []string{"excavator", "wheel-loader", "backhoe-loader"},
		"availability": map[string]any{"days": []string{"mon", "tue", "wed", "thu", "fri", "sat"}, "start": "08:00", "end": "18:00"},
	}), 200, "operator profile")
	must(t, op.do(http.MethodPut, "/verification/operator/address", map[string]any{
		"line1": "12 Station Road", "city": "Pune", "state_code": "27", "pincode": "411001", "privacy_consent": true,
	}), 200, "operator address")
	must(t, op.do(http.MethodPost, "/verification/operator/documents", map[string]any{
		"doc_type": "driving_licence", "upload_id": op.uploadID("operator_document", "licence.pdf", []byte(samplePDF)),
	}), 201, "licence doc")
	must(t, op.do(http.MethodPost, "/verification/operator/documents", map[string]any{
		"doc_type": "voter_id", "upload_id": op.uploadID("identity_document", "voter.pdf", []byte(samplePDF)),
	}), 201, "identity doc")
	return must(t, op.do(http.MethodPost, "/verification/operator/submit", nil), 200, "submit operator").str("id")
}

func (e *env) verifyOperator(op, admin *user) {
	e.t.Helper()
	id := e.prepareOperator(op)
	must(e.t, admin.do(http.MethodPost, "/admin/verifications/"+id+"/approve", nil), 200, "approve operator")
	must(e.t, op.do(http.MethodPost, "/operator/availability", map[string]any{"on_duty": true}), 200, "operator on duty")
}

func historyActions(r resp) []string {
	var out []string
	for _, h := range r.list("history") {
		out = append(out, fmt.Sprint(h.(map[string]any)["action"]))
	}
	return out
}

func TestCustomerVerification(t *testing.T) {
	e := newEnv(t)
	c := e.registerAndVerify("customer", "")

	st := must(t, c.do(http.MethodGet, "/verification/customer", nil), 200, "status")
	if st.str("complete") != "false" || len(st.list("checks")) != 5 {
		t.Fatalf("status = %s", st.raw)
	}

	// Only real images are accepted as avatars; PDFs and spoofed files are refused.
	must(t, c.upload("avatar", "me.png", []byte(samplePDF)), 400, "pdf as avatar")
	must(t, c.upload("avatar", "me.png", []byte("<svg onload=alert(1)>")), 400, "svg as avatar")
	must(t, c.upload("selfie", "me.png", samplePNG(t)), 400, "unknown purpose")
	up := must(t, c.upload("avatar", "me.png", samplePNG(t)), 201, "avatar upload")
	if strings.Contains(up.raw, "http") || strings.Contains(up.raw, "avatars/") {
		t.Fatalf("upload response leaks a location: %s", up.raw)
	}

	// Someone else cannot use this upload.
	other := e.registerAndVerify("customer", "")
	must(t, other.do(http.MethodPut, "/me/avatar", map[string]any{"upload_id": up.str("id")}), 400, "foreign upload")

	st = must(t, c.do(http.MethodPut, "/me/avatar", map[string]any{"upload_id": up.str("id")}), 200, "set avatar")
	link := st.str("avatar_url.url")
	if !strings.Contains(link, "sig=") || !strings.Contains(link, "exp=") {
		t.Fatalf("avatar link not signed: %s", link)
	}
	got := e.fetch(link)
	if got.Code != 200 || got.Header().Get("Content-Type") != "image/png" || got.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("avatar fetch: %d %v", got.Code, got.Header())
	}
	if rec := e.fetch(strings.Replace(link, "sig=", "sig=x", 1)); rec.Code != 403 {
		t.Fatalf("tampered link: %d", rec.Code)
	}
	if rec := e.fetch("/api/v1/files?key=" + url.QueryEscape(strings.Split(strings.Split(link, "key=")[1], "&")[0])); rec.Code != 403 {
		t.Fatalf("unsigned link: %d", rec.Code)
	}

	must(t, c.do(http.MethodPost, "/orgs/"+c.org+"/addresses", map[string]any{
		"label": "Home", "line1": "Flat 3, Shanti Nagar", "city": "Pune", "state_code": "27", "pincode": "411001",
	}), 201, "address")
	st = must(t, c.do(http.MethodGet, "/verification/customer", nil), 200, "status")
	done := map[string]bool{}
	for _, ch := range st.list("checks") {
		m := ch.(map[string]any)
		done[fmt.Sprint(m["key"])] = m["done"] == true
	}
	if !done["mobile"] || !done["address"] || !done["profile_image"] || !done["personal_details"] {
		t.Fatalf("checks = %v", done)
	}

	// Customers have no owner/operator verification and no admin access.
	must(t, c.do(http.MethodGet, "/verification/owner", nil), 403, "customer owner view")
	must(t, c.do(http.MethodGet, "/verification/operator", nil), 403, "customer operator view")
	must(t, c.do(http.MethodGet, "/admin/verifications", nil), 403, "customer admin queue")
	must(t, e.call(http.MethodGet, "/verification/customer", "", "", nil, nil), 401, "anonymous")
}

func TestOwnerVerificationWorkflow(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner := e.registerAndVerify("equipment_owner", "Patil Earthmovers")
	rival := e.registerAndVerify("equipment_owner", "Rival Rentals")
	customer := e.registerAndVerify("customer", "")
	admin := e.kycAdmin()

	v := must(t, owner.do(http.MethodGet, "/verification/owner", nil), 200, "owner view")
	if v.str("application.status") != "draft" || v.str("editable") != "true" {
		t.Fatalf("initial = %s", v.raw)
	}
	early := must(t, owner.do(http.MethodPost, "/verification/owner/submit", nil), 422, "incomplete submit")
	// Business documents are only required for companies; a new owner
	// organisation starts as an individual account.
	if n := len(early.list("errors")); n < 4 {
		t.Fatalf("missing steps = %s", early.raw)
	}

	// Business details are validated.
	bad := ownerBusiness(owner)
	bad["pan"] = "12345ABCDE"
	must(t, owner.do(http.MethodPut, "/verification/owner/business", bad), 400, "bad PAN")
	bad = ownerBusiness(owner)
	bad["gstin"] = "27" + randomPAN() + "1Z5"
	must(t, owner.do(http.MethodPut, "/verification/owner/business", bad), 400, "GSTIN of another PAN")
	bad = ownerBusiness(owner)
	bad["account_kind"] = "individual"
	must(t, owner.do(http.MethodPut, "/verification/owner/business", bad), 400, "kind/type mismatch")
	biz := must(t, owner.do(http.MethodPut, "/verification/owner/business", ownerBusiness(owner)), 200, "business")
	if biz.str("pan_last4") == "" || strings.Contains(biz.raw, `"pan":`) {
		t.Fatalf("PAN exposure: %s", biz.raw)
	}

	// Uploads: content is validated; other users' uploads cannot be attached.
	must(t, owner.upload("identity_document", "id.pdf", []byte("MZ\x90\x00 not a pdf")), 400, "exe as pdf")
	must(t, owner.upload("identity_document", "id.pdf", []byte("%PDF-1.4\n<< /OpenAction << /JS (x) >> >>\n%%EOF")), 400, "pdf with script")
	foreign := rival.uploadID("identity_document", "rival.pdf", []byte(samplePDF))
	must(t, owner.do(http.MethodPost, "/verification/owner/documents", map[string]any{"doc_type": "pan", "upload_id": foreign}), 400, "foreign upload")
	avatar := owner.uploadID("avatar", "a.png", samplePNG(t))
	must(t, owner.do(http.MethodPost, "/verification/owner/documents", map[string]any{"doc_type": "pan", "upload_id": avatar}), 400, "avatar as document")
	must(t, owner.do(http.MethodPost, "/verification/owner/documents", map[string]any{
		"doc_type": "driving_licence", "upload_id": owner.uploadID("identity_document", "dl.pdf", []byte(samplePDF)), "expires_on": "2001-01-01",
	}), 400, "expired document")

	// Bank accounts are verified by the payout provider before being saved.
	must(t, owner.do(http.MethodPut, "/verification/owner/bank-account", map[string]any{
		"account_holder_name": "Patil", "account_number": "50100123450000", "confirm_account_number": "50100123450000", "ifsc": "HDFC0001234",
	}), 422, "provider rejects account")
	must(t, owner.do(http.MethodPut, "/verification/owner/bank-account", map[string]any{
		"account_holder_name": "Patil", "account_number": "50100123456789", "confirm_account_number": "50100123456780", "ifsc": "HDFC0001234",
	}), 400, "confirmation mismatch")

	appID := e.prepareOwner(owner)
	v = must(t, owner.do(http.MethodGet, "/verification/owner", nil), 200, "owner view")
	if v.str("application.status") != "submitted" || v.str("editable") != "false" || v.str("kyc_status") != "submitted" {
		t.Fatalf("after submit = %s", v.raw)
	}
	if strings.Contains(v.raw, "50100123456789") || v.str("bank_account.account_last4") != "6789" || !strings.HasPrefix(v.str("bank_account.provider_reference"), "fa_sbx_") {
		t.Fatalf("bank account view = %s", v.str("bank_account"))
	}
	if strings.Contains(v.raw, "http") || strings.Contains(v.raw, "kyc/") {
		t.Fatalf("owner view exposes document locations: %s", v.raw)
	}
	must(t, owner.do(http.MethodPut, "/verification/owner/business", ownerBusiness(owner)), 409, "edit while submitted")
	docID := v.str("documents.0.id")
	must(t, owner.do(http.MethodDelete, "/verification/owner/documents/"+docID, nil), 409, "delete while submitted")

	// Document links: only the owner organisation and reviewers; every link audited.
	link := must(t, owner.do(http.MethodGet, "/verification/documents/"+docID+"/url", nil), 200, "owner link")
	must(t, rival.do(http.MethodGet, "/verification/documents/"+docID+"/url", nil), 404, "rival link")
	must(t, customer.do(http.MethodGet, "/verification/documents/"+docID+"/url", nil), 404, "customer link")
	must(t, e.call(http.MethodGet, "/verification/documents/"+docID+"/url", "", "", nil, nil), 401, "anonymous link")
	file := e.fetch(link.str("url"))
	if file.Code != 200 || file.Header().Get("Content-Type") != "application/pdf" ||
		!strings.HasPrefix(file.Header().Get("Content-Disposition"), "attachment") || !strings.Contains(file.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("document fetch: %d %v", file.Code, file.Header())
	}
	if exp, _ := time.Parse(time.RFC3339, link.str("expires_at")); time.Until(exp) > 15*time.Minute {
		t.Fatalf("link lifetime too long: %s", link.str("expires_at"))
	}
	for _, k := range []string{"exp=", "key="} {
		tampered := strings.Replace(link.str("url"), k, k+"1", 1)
		if rec := e.fetch(tampered); rec.Code != 403 {
			t.Fatalf("tampered %s: %d", k, rec.Code)
		}
	}

	// Reviewing is limited to KYC admins.
	must(t, owner.do(http.MethodGet, "/admin/verifications", nil), 403, "owner queue")
	ops := e.login(randomPhone())
	e.exec(`INSERT INTO user_roles (user_id, role_code) VALUES ($1, 'admin_ops')`, ops.id)
	must(t, ops.do(http.MethodGet, "/admin/verifications", nil), 403, "ops admin queue")
	must(t, ops.do(http.MethodPost, "/admin/verifications/"+appID+"/approve", nil), 403, "ops admin approve")

	q := must(t, admin.do(http.MethodGet, "/admin/verifications?status=submitted&role=owner", nil), 200, "queue")
	if !strings.Contains(q.raw, appID) {
		t.Fatalf("queue = %s", q.raw)
	}
	detail := must(t, admin.do(http.MethodGet, "/admin/verifications/"+appID, nil), 200, "detail")
	if len(detail.list("owner.documents")) != 2 || detail.str("owner.documents.0.download.url") == "" {
		t.Fatalf("detail = %s", detail.raw)
	}
	var audits int
	if err := e.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'document.url_issued' AND entity_id = $1`, docID).Scan(&audits); err != nil || audits < 2 {
		t.Fatalf("document link audits = %d %v", audits, err)
	}

	// A reviewer cannot decide their own organisation's application.
	e.exec(`INSERT INTO user_roles (user_id, role_code) VALUES ($1, 'admin_kyc')`, owner.id)
	must(t, owner.do(http.MethodPost, "/admin/verifications/"+appID+"/approve", nil), 403, "self approval")
	e.exec(`DELETE FROM user_roles WHERE user_id = $1 AND role_code = 'admin_kyc'`, owner.id)

	// Rejections need a reason, which the applicant sees.
	must(t, admin.do(http.MethodPost, "/admin/verifications/"+appID+"/documents/"+docID+"/review", map[string]any{"decision": "reject"}), 400, "doc reject without reason")
	must(t, admin.do(http.MethodPost, "/admin/verifications/"+appID+"/documents/"+docID+"/review", map[string]any{
		"decision": "reject", "reason": "The PAN card image is blurred",
	}), 200, "doc rejected")
	must(t, admin.do(http.MethodPost, "/admin/verifications/"+appID+"/approve", nil), 422, "approve with rejected document")
	must(t, admin.do(http.MethodPost, "/admin/verifications/"+appID+"/reject", map[string]any{}), 400, "reject without reason")
	must(t, admin.do(http.MethodPost, "/admin/verifications/"+appID+"/reject", map[string]any{"reason": "Upload a clear PAN card"}), 200, "reject")
	v = must(t, owner.do(http.MethodGet, "/verification/owner", nil), 200, "owner view")
	if v.str("application.status") != "rejected" || v.str("application.rejection_reason") != "Upload a clear PAN card" ||
		v.str("editable") != "true" || v.str("kyc_status") != "rejected" {
		t.Fatalf("after reject = %s", v.raw)
	}

	// Unverified owners cannot publish listings.
	models := must(t, owner.do(http.MethodGet, "/models?category=excavator", nil), 200, "models")
	eq := must(t, owner.do(http.MethodPost, "/owner/equipment", map[string]any{
		"model_id": models.str("data.0.id"), "title": "JCB 3DX backhoe loader", "registration_no": fmt.Sprintf("MH14%s%04d", randomLetters(2), rand.IntN(10000)),
		"current_hmr": 1200, "base_location": yard, "service_radius_km": 40,
	}), 201, "listing")
	must(t, owner.do(http.MethodPut, "/owner/equipment/"+eq.str("id")+"/pricing", map[string]any{
		"plans": []map[string]any{{"unit": "day", "with_operator": true, "rate_paise": 900000}},
	}), 200, "pricing")
	if r := must(t, owner.do(http.MethodPost, "/owner/equipment/"+eq.str("id")+"/submit", nil), 422, "submit unverified"); r.str("code") != "KYC_REQUIRED" {
		t.Fatalf("code = %s", r.raw)
	}

	// Fix and resubmit: the rejected document stays in the history and a
	// replacement is uploaded.
	must(t, owner.do(http.MethodDelete, "/verification/owner/documents/"+docID, nil), 409, "remove reviewed doc")
	must(t, owner.do(http.MethodPost, "/verification/owner/documents", map[string]any{
		"doc_type": "pan", "upload_id": owner.uploadID("identity_document", "pan2.pdf", []byte(samplePDF)),
	}), 201, "replacement doc")
	must(t, owner.do(http.MethodPost, "/verification/owner/submit", nil), 200, "resubmit")
	must(t, admin.do(http.MethodPost, "/admin/verifications/"+appID+"/start-review", nil), 200, "start review")
	ok := must(t, admin.do(http.MethodPost, "/admin/verifications/"+appID+"/approve", map[string]any{"reason": "all clear"}), 200, "approve")
	if ok.str("status") != "approved" {
		t.Fatalf("approve = %s", ok.raw)
	}
	must(t, admin.do(http.MethodPost, "/admin/verifications/"+appID+"/approve", nil), 409, "approve twice")
	v = must(t, owner.do(http.MethodGet, "/verification/owner", nil), 200, "owner view")
	// documents: rejected PAN (kept as evidence), GST certificate, replacement PAN.
	if v.str("kyc_status") != "verified" || v.str("documents.0.status") != "rejected" ||
		v.str("documents.1.status") != "verified" || v.str("documents.2.status") != "verified" {
		t.Fatalf("after approve = %s", v.raw)
	}
	owner.attachPhoto(eq.str("id"))
	owner.attachEquipmentDocument(eq.str("id"))
	must(t, owner.do(http.MethodPost, "/owner/equipment/"+eq.str("id")+"/submit", nil), 200, "submit verified")

	// Revocation and the append-only history.
	must(t, admin.do(http.MethodPost, "/admin/verifications/"+appID+"/reject", map[string]any{"reason": "GST registration cancelled"}), 200, "revoke")
	detail = must(t, admin.do(http.MethodGet, "/admin/verifications/"+appID, nil), 200, "detail")
	want := []string{"submitted", "review_started", "document_rejected", "rejected", "submitted", "review_started", "approved", "revoked"}
	got := slices.DeleteFunc(historyActions(detail), func(a string) bool { return a == "bank_account_verified" })
	if !slices.Equal(got, want) {
		t.Fatalf("history = %v", got)
	}
	if _, err := e.pool.Exec(ctx, `DELETE FROM verification_events WHERE application_id = $1`, appID); err == nil {
		t.Fatal("verification history must be append-only")
	}
	var notes int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1 AND event_type LIKE 'verification.%'`, appID).Scan(&notes)
	// 3 review outcomes plus 2 submissions (the admin "verification.submitted" alert).
	if notes != 5 {
		t.Fatalf("verification events in outbox = %d", notes)
	}
}

func TestOperatorVerificationWorkflow(t *testing.T) {
	e := newEnv(t)
	owner := e.registerAndVerify("equipment_owner", "Shinde Cranes")
	admin := e.kycAdmin()
	opPhone := randomPhone()
	must(t, owner.do(http.MethodPost, "/orgs/"+owner.org+"/members", map[string]any{"phone": opPhone, "role": "operator", "full_name": "Suresh Jadhav"}), 201, "add operator")
	op := e.login(opPhone)
	other := e.registerAndVerify("customer", "")

	v := must(t, op.do(http.MethodGet, "/verification/operator", nil), 200, "operator view")
	if v.str("verified") != "false" || v.str("application.status") != "draft" {
		t.Fatalf("initial = %s", v.raw)
	}
	profile := map[string]any{
		"licence_number": randomLicence(), "licence_class": "HTV", "licence_expires_on": "2020-01-01",
		"experience_years": 7, "skills": []string{"excavator", "wheel-loader", "backhoe-loader"},
		"availability": map[string]any{"days": []string{"mon"}, "start": "08:00", "end": "18:00"},
	}
	must(t, op.do(http.MethodPut, "/verification/operator/profile", profile), 400, "expired licence")
	profile["licence_expires_on"] = time.Now().AddDate(2, 0, 0).Format(time.DateOnly)
	profile["skills"] = []string{"spaceship"}
	must(t, op.do(http.MethodPut, "/verification/operator/profile", profile), 400, "unknown skill")
	profile["skills"] = []string{"excavator"}
	profile["availability"] = map[string]any{"days": []string{"mon"}, "start": "18:00", "end": "08:00"}
	must(t, op.do(http.MethodPut, "/verification/operator/profile", profile), 400, "availability order")

	q := must(t, op.do(http.MethodPost, "/verification/operator/qualifications", map[string]any{
		"title": "Excavator operator certificate", "issuer": "NSDC", "issued_on": "2019-06-01",
		"upload_id": op.uploadID("operator_document", "cert.pdf", []byte(samplePDF)),
	}), 201, "qualification")
	if q.str("document_id") == "" {
		t.Fatalf("qualification = %s", q.raw)
	}
	must(t, other.do(http.MethodDelete, "/verification/operator/qualifications/"+q.str("id"), nil), 403, "non-operator delete")

	appID := e.prepareOperator(op)
	v = must(t, op.do(http.MethodGet, "/verification/operator", nil), 200, "operator view")
	if len(v.str("licence_last4")) != 4 || strings.Contains(v.raw, `"licence_number"`) || len(v.list("qualifications")) != 1 {
		t.Fatalf("operator view = %s", v.raw)
	}
	docID := v.str("documents.0.id")
	must(t, op.do(http.MethodGet, "/verification/documents/"+docID+"/url", nil), 200, "own document link")
	must(t, owner.do(http.MethodGet, "/verification/documents/"+docID+"/url", nil), 404, "employer cannot read operator documents")

	must(t, admin.do(http.MethodPost, "/admin/verifications/"+appID+"/approve", nil), 200, "approve")
	v = must(t, op.do(http.MethodGet, "/verification/operator", nil), 200, "operator view")
	if v.str("verified") != "true" || v.str("application.status") != "approved" {
		t.Fatalf("after approve = %s", v.raw)
	}
	profile["availability"] = map[string]any{"days": []string{"mon"}, "start": "08:00", "end": "18:00"}
	must(t, op.do(http.MethodPut, "/verification/operator/profile", profile), 409, "edit after approval")
	must(t, admin.do(http.MethodPost, "/admin/verifications/"+appID+"/reject", map[string]any{"reason": "Licence suspended by RTO"}), 200, "revoke")
	v = must(t, op.do(http.MethodGet, "/verification/operator", nil), 200, "operator view")
	if v.str("verified") != "false" || v.str("application.rejection_reason") != "Licence suspended by RTO" {
		t.Fatalf("after revoke = %s", v.raw)
	}

	// The reviewer can suspend the applicant's account; their session ends.
	detail := must(t, admin.do(http.MethodGet, "/admin/verifications/"+appID, nil), 200, "detail")
	if detail.str("account_user_id") != op.id {
		t.Fatalf("account_user_id = %s", detail.str("account_user_id"))
	}
	must(t, other.do(http.MethodPost, "/admin/users/"+op.id+"/suspend", map[string]any{"reason": "forged licence"}), 403, "non-admin suspend")
	must(t, admin.do(http.MethodPost, "/admin/users/"+op.id+"/suspend", map[string]any{"reason": "Forged licence document"}), 403, "admins cannot suspend (read-only)")
}

func TestFleetManagerEquipmentAccess(t *testing.T) {
	e := newEnv(t)
	owner := e.registerAndVerify("equipment_owner", "Kale Infra")
	rival := e.registerAndVerify("equipment_owner", "Other Infra")
	fmPhone := randomPhone()
	must(t, owner.do(http.MethodPost, "/orgs/"+owner.org+"/members", map[string]any{"phone": fmPhone, "role": "fleet_manager", "full_name": "Meena Kale"}), 201, "add fleet manager")
	fm := e.login(fmPhone)
	fm.org = owner.org

	models := must(t, owner.do(http.MethodGet, "/models?category=excavator", nil), 200, "models")
	machine := func(u *user, title string) string {
		return must(t, u.do(http.MethodPost, "/owner/equipment", map[string]any{
			"model_id": models.str("data.0.id"), "title": title, "registration_no": fmt.Sprintf("MH12%s%04d", randomLetters(2), rand.IntN(10000)),
			"current_hmr": 100, "base_location": yard, "service_radius_km": 30,
		}), 201, "create "+title).str("id")
	}
	a, b := machine(owner, "Excavator A unit"), machine(owner, "Excavator B unit")
	foreign := machine(rival, "Rival excavator")

	view := must(t, fm.do(http.MethodGet, "/verification/fleet-manager", nil), 200, "fleet view")
	if view.str("organization_id") != owner.org || len(view.list("equipment")) != 0 || view.str("all_equipment") != "false" {
		t.Fatalf("fleet view = %s", view.raw)
	}
	if n := len(must(t, fm.do(http.MethodGet, "/owner/equipment", nil), 200, "fm list").list("data")); n != 0 {
		t.Fatalf("fleet manager sees %d unassigned machines", n)
	}
	from := time.Now().UTC().Truncate(time.Hour)
	cal := "/calendar?from=" + url.QueryEscape(from.Format(time.RFC3339)) + "&to=" + url.QueryEscape(from.AddDate(0, 1, 0).Format(time.RFC3339))
	for _, path := range []string{"/owner/equipment/" + a, "/owner/equipment/" + a + cal, "/owner/equipment/" + a + "/maintenance/plans"} {
		must(t, fm.do(http.MethodGet, path, nil), 404, "unassigned "+path)
	}
	must(t, fm.do(http.MethodPatch, "/owner/equipment/"+a, map[string]any{"title": "Hijacked title"}, "If-Match", `"1"`), 404, "edit unassigned")

	// Only owners manage access, and only to their own machines.
	must(t, fm.do(http.MethodPut, "/verification/owner/fleet-managers/"+fm.id, map[string]any{"all_equipment": true}), 403, "fm grants itself")
	must(t, owner.do(http.MethodPut, "/verification/owner/fleet-managers/"+fm.id, map[string]any{"equipment_ids": []string{a, foreign}}), 400, "grant foreign machine")
	must(t, owner.do(http.MethodPut, "/verification/owner/fleet-managers/"+rival.id, map[string]any{"equipment_ids": []string{a}}), 404, "grant non-member")
	g := must(t, owner.do(http.MethodPut, "/verification/owner/fleet-managers/"+fm.id, map[string]any{"equipment_ids": []string{a}}), 200, "grant A")
	if g.num("granted_count") != 1 {
		t.Fatalf("grant = %s", g.raw)
	}
	must(t, fm.do(http.MethodGet, "/owner/equipment/"+a, nil), 200, "assigned machine")
	must(t, fm.do(http.MethodGet, "/owner/equipment/"+a+cal, nil), 200, "assigned calendar")
	must(t, fm.do(http.MethodGet, "/owner/equipment/"+b, nil), 404, "still unassigned")
	if list := must(t, fm.do(http.MethodGet, "/owner/equipment", nil), 200, "fm list"); len(list.list("data")) != 1 || list.str("data.0.id") != a {
		t.Fatalf("fm list = %s", list.raw)
	}

	// Machines a fleet manager adds are theirs to manage.
	c := machine(fm, "Excavator C unit")
	must(t, fm.do(http.MethodGet, "/owner/equipment/"+c, nil), 200, "own machine")

	mgrs := must(t, owner.do(http.MethodGet, "/verification/owner/fleet-managers", nil), 200, "managers")
	if len(mgrs.list("data")) != 1 || mgrs.num("data.0.granted_count") != 2 {
		t.Fatalf("managers = %s", mgrs.raw)
	}
	must(t, owner.do(http.MethodPut, "/verification/owner/fleet-managers/"+fm.id, map[string]any{"all_equipment": true}), 200, "grant all")
	must(t, fm.do(http.MethodGet, "/owner/equipment/"+b, nil), 200, "all machines")
	if n := len(must(t, fm.do(http.MethodGet, "/owner/equipment", nil), 200, "fm list").list("data")); n != 3 {
		t.Fatalf("all-equipment list = %d", n)
	}
	must(t, fm.do(http.MethodGet, "/owner/equipment/"+foreign, nil), 404, "other company's machine")
	must(t, fm.do(http.MethodGet, "/verification/owner", nil), 403, "fm cannot see owner KYC")
}
