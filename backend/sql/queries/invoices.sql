-- name: EnsureInvoiceSeries :exec
INSERT INTO invoice_series (issuer_org_id, financial_year, prefix)
VALUES (sqlc.narg(issuer_org_id), @financial_year, @prefix)
ON CONFLICT DO NOTHING;

-- name: GetInvoiceSeries :one
SELECT id FROM invoice_series
WHERE issuer_org_id IS NOT DISTINCT FROM sqlc.narg(issuer_org_id) AND financial_year = @financial_year AND prefix = @prefix;

-- name: NextInvoiceNumber :one
SELECT next_invoice_number(@series_id)::text AS number;

-- name: CreateInvoice :one
INSERT INTO invoices (series_id, invoice_number, issuer_org_id, recipient_org_id, booking_id, invoice_type,
                      place_of_supply, supply_type, sac_code, subtotal_paise, cgst_paise, sgst_paise, igst_paise, total_paise)
VALUES (@series_id, @invoice_number, sqlc.narg(issuer_org_id), @recipient_org_id, sqlc.narg(booking_id), @invoice_type,
        @place_of_supply, @supply_type, @sac_code, @subtotal_paise, @cgst_paise, @sgst_paise, @igst_paise, @total_paise)
RETURNING *;

-- name: AddInvoiceLine :exec
INSERT INTO invoice_lines (invoice_id, line_no, description, sac_code, quantity, unit, rate_paise, taxable_paise, gst_rate_bps)
VALUES (@invoice_id, @line_no, @description, @sac_code, @quantity, @unit, @rate_paise, @taxable_paise, @gst_rate_bps);

-- name: IssueInvoice :one
UPDATE invoices SET status = 'issued', issued_at = now() WHERE id = @id AND status = 'draft'
RETURNING *;

-- name: GetInvoice :one
SELECT * FROM invoices WHERE id = @id;

-- name: ListInvoiceLines :many
SELECT * FROM invoice_lines WHERE invoice_id = @invoice_id ORDER BY line_no;

-- name: ListBookingInvoices :many
SELECT * FROM invoices WHERE booking_id = @booking_id ORDER BY created_at;

-- name: ListOrgInvoices :many
SELECT * FROM invoices
WHERE (issuer_org_id = @org_id OR recipient_org_id = @org_id)
  AND (sqlc.narg(after)::uuid IS NULL OR id < sqlc.narg(after))
ORDER BY id DESC
LIMIT @result_limit;

-- name: GetInvoiceContext :one
-- Tax facts for a booking: supplier state vs place of supply (site state).
SELECT b.id AS booking_id, b.status, b.owner_org_id, b.customer_org_id, b.price_snapshot, b.reference,
       ow.gst_state_code AS owner_state_code, ow.gstin AS owner_gstin, ad.state_code AS site_state_code,
       EXISTS (SELECT 1 FROM invoices i WHERE i.booking_id = b.id AND i.invoice_type = 'rental' AND i.status <> 'cancelled') AS has_rental_invoice
FROM bookings b
JOIN organizations ow ON ow.id = b.owner_org_id
JOIN addresses ad ON ad.id = b.site_address_id
WHERE b.id = @booking_id;
