DROP INDEX IF EXISTS invoices_original_idx;
ALTER TABLE invoices DROP COLUMN IF EXISTS title, DROP COLUMN IF EXISTS details, DROP COLUMN IF EXISTS reason;

ALTER TABLE invoice_lines DROP COLUMN IF EXISTS tax_code;
ALTER TABLE invoice_lines DROP CONSTRAINT invoice_lines_gst_rate_bps_check;
ALTER TABLE invoice_lines ADD CONSTRAINT invoice_lines_gst_rate_bps_check CHECK (gst_rate_bps IN (0, 500, 1200, 1800, 2800)) NOT VALID;

DROP TRIGGER IF EXISTS tax_rates_audit ON tax_rates;
ALTER TABLE tax_rates DROP CONSTRAINT IF EXISTS tax_rates_review_ck;
ALTER TABLE tax_rates DROP COLUMN IF EXISTS sac_code, DROP COLUMN IF EXISTS legal_basis,
  DROP COLUMN IF EXISTS reviewed_by_name, DROP COLUMN IF EXISTS reviewed_at;

ALTER TABLE agreement_signatures DROP COLUMN IF EXISTS consent_text;

CREATE OR REPLACE FUNCTION rental_agreements_freeze() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (OLD.customer_accepted_at IS NOT NULL AND OLD.owner_accepted_at IS NOT NULL) THEN
    RAISE EXCEPTION 'rental agreement % is signed and immutable', OLD.id USING ERRCODE = 'EQ001';
  END IF;
  IF NEW.terms_sha256 <> OLD.terms_sha256 OR NEW.booking_id <> OLD.booking_id THEN
    RAISE EXCEPTION 'agreement terms cannot change after creation' USING ERRCODE = 'EQ001';
  END IF;
  RETURN NEW;
END;
$$;
-- Only the current version of each booking survives a downgrade's unique key.
DROP INDEX IF EXISTS rental_agreements_current_uq;
ALTER TABLE rental_agreements DROP CONSTRAINT IF EXISTS rental_agreements_version_uq;
ALTER TABLE rental_agreements DROP COLUMN IF EXISTS legal_template_id, DROP COLUMN IF EXISTS supersede_reason,
  DROP COLUMN IF EXISTS superseded_at, DROP COLUMN IF EXISTS version;
ALTER TABLE rental_agreements ADD CONSTRAINT rental_agreements_booking_id_key UNIQUE (booking_id);

DROP TABLE IF EXISTS legal_templates;
DROP FUNCTION IF EXISTS legal_templates_guard();
