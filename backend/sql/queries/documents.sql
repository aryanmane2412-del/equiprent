-- Documents: legal templates, agreement versions, invoice details, credit
-- notes, tax rule reviews and the document audit history.

-- ---- legal templates --------------------------------------------------------------------------

-- name: GetPublishedLegalTemplate :one
SELECT * FROM legal_templates WHERE kind = @kind AND status = 'published';

-- name: GetLegalTemplate :one
SELECT * FROM legal_templates WHERE id = @id;

-- name: ListLegalTemplates :many
SELECT * FROM legal_templates WHERE kind = @kind ORDER BY created_at DESC;

-- name: CreateLegalTemplate :one
INSERT INTO legal_templates (kind, version, title, jurisdiction, body, created_by)
VALUES (@kind, @version, @title, @jurisdiction, @body, @created_by)
RETURNING *;

-- name: UpdateLegalTemplateDraft :one
UPDATE legal_templates SET title = @title, body = @body, jurisdiction = @jurisdiction
WHERE id = @id AND status = 'draft'
RETURNING *;

-- name: ReviewLegalTemplate :one
-- Records the legal review of a draft (who, in what capacity, their reference).
UPDATE legal_templates SET reviewed_by_name = @reviewed_by_name, reviewer_capacity = @reviewer_capacity,
       review_reference = sqlc.narg(review_reference), reviewed_at = now()
WHERE id = @id AND status = 'draft'
RETURNING *;

-- name: RetirePublishedLegalTemplate :exec
UPDATE legal_templates SET status = 'retired', retired_at = now() WHERE kind = @kind AND status = 'published';

-- name: PublishLegalTemplate :one
UPDATE legal_templates SET status = 'published', published_at = now(), published_by = @published_by
WHERE id = @id AND status = 'draft'
RETURNING *;

-- name: DeleteLegalTemplateDraft :execrows
DELETE FROM legal_templates WHERE id = @id AND status = 'draft';

-- ---- agreement versions ---------------------------------------------------------------------

-- name: CreateRentalAgreementVersion :one
INSERT INTO rental_agreements (booking_id, template_version, terms_sha256, snapshot, version, legal_template_id)
VALUES (@booking_id, @template_version, @terms_sha256, @snapshot,
        COALESCE((SELECT max(version) FROM rental_agreements WHERE booking_id = @booking_id), 0) + 1,
        sqlc.narg(legal_template_id))
RETURNING id;

-- name: SupersedeRentalAgreement :execrows
UPDATE rental_agreements SET superseded_at = now(), supersede_reason = @reason
WHERE id = @id AND superseded_at IS NULL;

-- name: ListRentalAgreementVersions :many
SELECT a.id, a.version, a.template_version, a.terms_sha256, a.created_at, a.superseded_at, a.supersede_reason,
       a.customer_accepted_at, a.owner_accepted_at, COALESCE(t.version, '')::text AS legal_template_version
FROM rental_agreements a LEFT JOIN legal_templates t ON t.id = a.legal_template_id
WHERE a.booking_id = @booking_id
ORDER BY a.version DESC;

-- name: GetRentalAgreementVersion :one
SELECT * FROM rental_agreements WHERE booking_id = @booking_id AND version = @version;

-- ---- invoices --------------------------------------------------------------------------------

-- name: CreateInvoiceV2 :one
INSERT INTO invoices (series_id, invoice_number, issuer_org_id, recipient_org_id, booking_id, invoice_type, original_invoice_id,
                      place_of_supply, supply_type, sac_code, subtotal_paise, cgst_paise, sgst_paise, igst_paise, total_paise,
                      title, details, reason)
VALUES (@series_id, @invoice_number, sqlc.narg(issuer_org_id), @recipient_org_id, sqlc.narg(booking_id), @invoice_type,
        sqlc.narg(original_invoice_id), @place_of_supply, @supply_type, @sac_code, @subtotal_paise, @cgst_paise, @sgst_paise,
        @igst_paise, @total_paise, @title, @details, sqlc.narg(reason))
RETURNING *;

-- name: AddInvoiceLineV2 :exec
INSERT INTO invoice_lines (invoice_id, line_no, description, sac_code, quantity, unit, rate_paise, taxable_paise, gst_rate_bps, tax_code)
VALUES (@invoice_id, @line_no, @description, @sac_code, @quantity, @unit, @rate_paise, @taxable_paise, @gst_rate_bps, @tax_code);

-- name: CreditedTotal :one
-- What credit notes already returned against an invoice (total and taxable).
SELECT COALESCE(sum(total_paise), 0)::bigint AS total_paise, COALESCE(sum(subtotal_paise), 0)::bigint AS subtotal_paise
FROM invoices WHERE original_invoice_id = @original_invoice_id AND status = 'issued';

-- name: ListCreditNotes :many
SELECT * FROM invoices WHERE original_invoice_id = @original_invoice_id ORDER BY created_at;

-- name: HasIssuedInvoice :one
SELECT EXISTS (SELECT 1 FROM invoices WHERE booking_id = @booking_id AND invoice_type = @invoice_type AND status <> 'cancelled')::bool;

-- name: CountUnreviewedTaxRates :one
SELECT count(*)::int FROM tax_rates
WHERE is_active AND valid_from <= current_date AND (valid_until IS NULL OR valid_until >= current_date) AND reviewed_at IS NULL;

-- name: ReviewTaxRate :one
UPDATE tax_rates SET reviewed_by_name = @reviewed_by_name, reviewed_at = now(), legal_basis = COALESCE(sqlc.narg(legal_basis), legal_basis)
WHERE id = @id
RETURNING id;

-- ---- document audit history ------------------------------------------------------------------

-- name: BookingDocumentHistory :many
-- Every recorded change to, and download of, the booking's agreements,
-- acceptance records and invoices, newest first.
WITH docs AS (
  SELECT 'rental_agreements'::text AS t, a.id FROM rental_agreements a WHERE a.booking_id = @booking_id
  UNION ALL SELECT 'rental_agreement', a.id FROM rental_agreements a WHERE a.booking_id = @booking_id
  UNION ALL SELECT 'agreement_signatures', s.id FROM agreement_signatures s JOIN rental_agreements a ON a.id = s.agreement_id WHERE a.booking_id = @booking_id
  UNION ALL SELECT 'invoices', i.id FROM invoices i WHERE i.booking_id = @booking_id
  UNION ALL SELECT 'invoice', i.id FROM invoices i WHERE i.booking_id = @booking_id
)
SELECT l.id, l.occurred_at, l.action, l.entity_type, l.entity_id, l.reason, l.actor_role,
       COALESCE(u.full_name, '')::text AS actor_name, l.actor_user_id,
       COALESCE(l.after->>'status', '')::text AS new_status,
       COALESCE(l.after->>'version', '')::text AS version,
       COALESCE(l.after->>'invoice_number', '')::text AS invoice_number,
       COALESCE(l.after->>'party', '')::text AS party,
       (l.after->>'superseded_at' IS NOT NULL AND l.before->>'superseded_at' IS NULL)::bool AS superseded_now,
       COALESCE(l.after->>'supersede_reason', '')::text AS supersede_reason
FROM audit_logs l
JOIN docs d ON d.t = l.entity_type AND d.id = l.entity_id
LEFT JOIN users u ON u.id = l.actor_user_id
ORDER BY l.occurred_at DESC, l.id DESC
LIMIT @lim;

-- name: GetRentalAgreementByID :one
SELECT * FROM rental_agreements WHERE id = @id;

-- name: AcceptAgreementVersionAsCustomer :execrows
UPDATE rental_agreements
SET customer_accepted_by = @user_id, customer_accepted_at = now(), customer_accept_ip = sqlc.narg(ip)
WHERE id = @id AND customer_accepted_at IS NULL AND superseded_at IS NULL;

-- name: AcceptAgreementVersionAsOwner :execrows
UPDATE rental_agreements
SET owner_accepted_by = @user_id, owner_accepted_at = now()
WHERE id = @id AND owner_accepted_at IS NULL AND superseded_at IS NULL;
