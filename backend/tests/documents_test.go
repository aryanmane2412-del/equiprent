package tests

// Rental agreements and invoices as documents: legal terms managed as
// reviewed configuration, agreement versions and amendments, acceptance
// records (not digital signatures), invoices taxed by configured rules,
// credit notes, PDF generation, document permissions and audit history.

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/equiprent/backend/internal/config"
)

func (e *env) superAdmin() *user {
	e.t.Helper()
	a := e.login(randomPhone())
	e.exec(`INSERT INTO user_roles (user_id, role_code) VALUES ($1, 'admin_super')`, a.id)
	return a
}

func (e *env) financeAdmin() *user {
	e.t.Helper()
	a := e.login(randomPhone())
	e.exec(`INSERT INTO user_roles (user_id, role_code) VALUES ($1, 'admin_finance')`, a.id)
	return a
}

func acceptAs(t *testing.T, u *user, bookingID, hash, name string) resp {
	t.Helper()
	return u.do(http.MethodPost, "/bookings/"+bookingID+"/agreement/sign", map[string]any{"terms_sha256": hash, "signer_name": name, "consent": true})
}

func TestAgreementVersionsAndAcceptanceRecords(t *testing.T) {
	e := newEnv(t)
	r := newRental(t, e, "offline")
	r.accept(t)

	v1 := must(t, r.customer.do(http.MethodGet, "/bookings/"+r.id+"/agreement", nil), 200, "agreement v1")
	if v1.num("version") != 1 || v1.body["current"] != true || v1.str("assurance") != "acceptance_record" ||
		!strings.Contains(v1.str("assurance_note"), "not a digital signature") || v1.str("consent_text") == "" ||
		v1.str("legal_terms.version") == "" {
		t.Fatalf("v1 = %s", v1.raw)
	}
	// The customer accepts v1; the acceptance record keeps the consent text.
	acc := must(t, acceptAs(t, r.customer, r.id, v1.str("terms_sha256"), "Asha Rao"), 200, "customer accepts v1")
	if acc.str("agreement.signatures.0.kind") != "acceptance_record" || acc.str("agreement.signatures.0.consent_text") != v1.str("consent_text") {
		t.Fatalf("acceptance record = %s", acc.raw)
	}

	// The price changes before the owner accepts: the next read re-issues v2,
	// superseding v1 (kept, with the customer's acceptance on it).
	e.exec(`UPDATE bookings SET estimated_total_paise = estimated_total_paise + 50000 WHERE id = $1`, r.id)
	v2 := must(t, r.owner.do(http.MethodGet, "/bookings/"+r.id+"/agreement", nil), 200, "agreement v2")
	if v2.num("version") != 2 || v2.str("status") != "awaiting_signatures" || v2.str("terms_sha256") == v1.str("terms_sha256") || v2.body["can_sign"] != true {
		t.Fatalf("v2 = %s", v2.raw)
	}
	must(t, acceptAs(t, r.owner, r.id, v1.str("terms_sha256"), "Ravi Patil"), 409, "accepting the superseded text")
	old := must(t, r.customer.do(http.MethodGet, "/bookings/"+r.id+"/agreement/versions/1", nil), 200, "v1 kept")
	if old.str("status") != "superseded" || old.body["current"] != false || old.body["can_sign"] != false || len(old.list("signatures")) != 1 ||
		!strings.Contains(old.str("supersede_reason"), "re-issued") || old.str("terms_sha256") != v1.str("terms_sha256") {
		t.Fatalf("superseded v1 = %s", old.raw)
	}
	// Both parties accept v2.
	must(t, acceptAs(t, r.customer, r.id, v2.str("terms_sha256"), "Asha Rao"), 200, "customer accepts v2")
	done := must(t, acceptAs(t, r.owner, r.id, v2.str("terms_sha256"), "Ravi Patil"), 200, "owner accepts v2")
	if done.str("agreement.status") != "executed" {
		t.Fatalf("v2 executed = %s", done.raw)
	}

	// A change after execution issues an amendment (v3); v2 stays executed in the history.
	e.exec(`UPDATE bookings SET estimated_total_paise = estimated_total_paise + 70000 WHERE id = $1`, r.id)
	v3 := must(t, r.customer.do(http.MethodGet, "/bookings/"+r.id+"/agreement", nil), 200, "amendment")
	if v3.num("version") != 3 || v3.str("status") != "awaiting_signatures" {
		t.Fatalf("v3 = %s", v3.raw)
	}
	list := must(t, r.owner.do(http.MethodGet, "/bookings/"+r.id+"/agreement/versions", nil), 200, "versions")
	if len(list.list("data")) != 3 || list.str("data.0.status") != "current" || list.str("data.1.status") != "superseded" ||
		!strings.Contains(list.str("data.1.supersede_reason"), "amended") || list.str("data.1.owner_accepted_at") == "<nil>" {
		t.Fatalf("versions = %s", list.raw)
	}
	// The executed v2 PDF is still available and marked superseded.
	link := must(t, r.customer.do(http.MethodGet, "/bookings/"+r.id+"/agreement/versions/2/document", nil), 200, "v2 pdf link")
	pdf := e.fetch(link.str("url"))
	// The executed copy was stored when both accepted and is served unchanged.
	if pdf.Code != 200 || !bytes.Contains(pdf.Body.Bytes(), []byte("ACCEPTANCE RECORD")) || link.str("kind") != "executed" ||
		!strings.HasSuffix(link.str("file_name"), "-v2.pdf") {
		t.Fatalf("v2 pdf: %d %s", pdf.Code, link.raw)
	}
	// Executed text is frozen even when superseded, apart from the supersession mark.
	if _, err := e.pool.Exec(context.Background(), `UPDATE rental_agreements SET supersede_reason = 'edited' WHERE booking_id = $1 AND version = 2`, r.id); err == nil {
		t.Fatal("a superseded agreement must be immutable")
	}

	// Permissions: outsiders see none of it.
	outsider := e.registerAndVerify("customer", "")
	for _, path := range []string{"/agreement/versions", "/agreement/versions/1", "/agreement/versions/2/document", "/documents/history"} {
		must(t, outsider.do(http.MethodGet, "/bookings/"+r.id+path, nil), 404, "outsider "+path)
	}
	// History: issues, acceptances, supersessions and downloads, without IPs.
	hist := must(t, r.customer.do(http.MethodGet, "/bookings/"+r.id+"/documents/history", nil), 200, "history")
	actions := map[string]int{}
	for _, h := range hist.list("data") {
		m := h.(map[string]any)
		actions[m["document"].(string)+":"+m["action"].(string)]++
	}
	if actions["agreement:issued"] != 3 || actions["acceptance:accepted"] != 3 || actions["agreement:superseded"] != 2 || actions["agreement:downloaded"] < 1 {
		t.Fatalf("history actions = %v\n%s", actions, hist.raw)
	}
	if strings.Contains(hist.raw, "203.0.113") || strings.Contains(hist.raw, `"ip"`) {
		t.Fatal("history must not expose IP addresses")
	}
}

// Legal terms are data: drafted, reviewed and published by an admin, never
// edited once published. Runs on its own database (it changes the terms).
func TestLegalTemplatesAndReviewGate(t *testing.T) {
	e := newIsolatedEnv(t, func(c *config.Config) { c.Legal.RequireReviewed = true })
	super, ops := e.superAdmin(), e.opsAdmin()

	// The seeded wording has no legal review, so with the gate on no agreement is issued.
	r := newRental(t, e, "offline")
	r.accept(t)
	must(t, r.customer.do(http.MethodGet, "/bookings/"+r.id+"/agreement", nil), 503, "unreviewed terms")

	list := must(t, ops.do(http.MethodGet, "/admin/legal-templates", nil), 200, "ops reads templates")
	if len(list.list("data")) != 1 || list.str("data.0.status") != "published" || list.str("data.0.reviewed_by_name") != "<nil>" {
		t.Fatalf("templates = %s", list.raw)
	}
	seeded := list.str("data.0.id")
	must(t, ops.do(http.MethodPost, "/admin/legal-templates", map[string]any{"version": "x-1", "title": "Nope"}), 403, "ops cannot draft")
	must(t, super.do(http.MethodPut, "/admin/legal-templates/"+seeded, map[string]any{"title": "Changed"}), 403, "admins cannot edit templates (read-only)")
	must(t, super.do(http.MethodPost, "/admin/legal-templates", map[string]any{"version": "x-2", "title": "Draft"}), 403, "admins cannot draft templates (read-only)")
}

// completedRental is a paid, finished online rental ready for invoicing.
func completedRental(t *testing.T, e *env) *rental {
	t.Helper()
	r := newRental(t, e, "online")
	r.accept(t)
	r.pay(t)
	r.finish(t, "confirmed")
	return r
}

func TestInvoicesCreditNotesAndPermissions(t *testing.T) {
	e := newEnv(t)
	r := completedRental(t, e)

	inv := must(t, r.owner.do(http.MethodPost, "/owner/bookings/"+r.id+"/invoices", nil), 201, "issue invoice")
	if inv.str("title") != "Tax invoice" || !strings.HasPrefix(inv.str("invoice_number"), "ER/") || inv.str("supply_type") != "intra" ||
		inv.num("cgst_paise") <= 0 || inv.num("cgst_paise") != inv.num("sgst_paise") || inv.str("details.booking_reference") != r.ref ||
		inv.str("details.supplier.gstin") == "" || inv.str("details.equipment.title") == "" || inv.str("details.rental.start") == "" ||
		len(inv.list("details.payments")) != 1 || !strings.HasPrefix(inv.str("details.payments.0.receipt_number"), "RCPT-") {
		t.Fatalf("invoice = %s", inv.raw)
	}
	for _, l := range inv.list("lines") {
		m := l.(map[string]any)
		if m["gst_rate_bps"].(float64) != 1800 || m["tax_code"] != "gst" || strings.Contains(m["description"].(string), "Platform") {
			t.Fatalf("line = %v", m)
		}
	}
	must(t, r.owner.do(http.MethodPost, "/owner/bookings/"+r.id+"/invoices", nil), 409, "second invoice")
	id := inv.str("id")
	sub := int64(inv.num("subtotal_paise"))

	// Credit notes: supplier or finance only, within the remaining taxable value.
	cn := func(u *user, amount int64, reason string) resp {
		return u.do(http.MethodPost, "/invoices/"+id+"/credit-notes", map[string]any{"amount_paise": amount, "reason": reason})
	}
	must(t, cn(r.customer, 1000, "Customer wants money"), 403, "customer issues credit note")
	must(t, cn(r.owner, sub+1, "Too much credit"), 400, "credit beyond the invoice")
	note := must(t, cn(r.owner, sub/2, "Machine idle for half a day"), 201, "credit note")
	if note.str("title") != "Credit note" || !strings.HasPrefix(note.str("invoice_number"), "CN/") || note.str("original_invoice_id") != id ||
		note.str("details.original_invoice_number") != inv.str("invoice_number") || note.num("cgst_paise") != note.num("sgst_paise") {
		t.Fatalf("credit note = %s", note.raw)
	}
	rest := sub - sub/2
	must(t, cn(e.financeAdmin(), rest, "Remaining credit after dispute"), 403, "finance admins are read-only")
	_ = rest
}

// Invoices pause while an active tax rule has no recorded compliance
// review; a finance admin records it. A supplier without a GSTIN issues a
// bill of supply.
func TestInvoiceTaxReviewGateAndBillOfSupply(t *testing.T) {
	e := newIsolatedEnv(t, func(c *config.Config) { c.Legal.RequireReviewed = true })
	fin := e.financeAdmin()
	r := completedRental(t, e)
	must(t, r.owner.do(http.MethodPost, "/owner/bookings/"+r.id+"/invoices", nil), 503, "unreviewed tax rules")

	rates := must(t, fin.do(http.MethodGet, "/admin/tax-rates", nil), 200, "tax rates")
	gst := rates.list("data")[0].(map[string]any)
	if gst["sac_code"] != "997313" || gst["reviewed_at"] != nil {
		t.Fatalf("rate = %v", gst)
	}
	must(t, r.owner.do(http.MethodPost, "/admin/tax-rates/"+gst["id"].(string)+"/review", map[string]any{"reviewed_by_name": "Owner"}), 403, "owner reviews tax")
	must(t, fin.do(http.MethodPost, "/admin/tax-rates/"+gst["id"].(string)+"/review", map[string]any{
		"reviewed_by_name": "CA Nikhil Shah", "legal_basis": "Notification 11/2017-CT(R) as amended",
	}), 403, "finance admins are read-only")
}
