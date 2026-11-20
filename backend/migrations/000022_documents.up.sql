-- Documents: legal templates managed as data (with a legal-review record),
-- agreement versions, acceptance consent text, reviewable tax rules and
-- invoice detail snapshots.

-- ---- legal templates --------------------------------------------------------------------------
-- The terms an agreement states are configuration, not code. A published
-- template is immutable; changes are a new version. In staging/production
-- only a template with a recorded legal review can be published.
CREATE TABLE legal_templates (
  id                uuid PRIMARY KEY DEFAULT uuidv7(),
  kind              text NOT NULL CHECK (kind IN ('rental_agreement')),
  version           text NOT NULL CHECK (version ~ '^[a-z0-9][a-z0-9._-]{1,40}$'),
  title             text NOT NULL CHECK (length(btrim(title)) BETWEEN 3 AND 120),
  jurisdiction      text NOT NULL DEFAULT 'IN' CHECK (jurisdiction ~ '^[A-Z]{2}$'),
  body              jsonb NOT NULL CHECK (jsonb_typeof(body) = 'object'),
  status            text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'retired')),
  reviewed_by_name  text CHECK (reviewed_by_name IS NULL OR length(btrim(reviewed_by_name)) BETWEEN 2 AND 120),
  reviewer_capacity text CHECK (reviewer_capacity IS NULL OR length(reviewer_capacity) <= 120),
  review_reference  text CHECK (review_reference IS NULL OR length(review_reference) <= 200),
  reviewed_at       timestamptz,
  created_by        uuid REFERENCES users (id),
  published_by      uuid REFERENCES users (id),
  published_at      timestamptz,
  retired_at        timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  UNIQUE (kind, version),
  CHECK ((reviewed_at IS NULL) = (reviewed_by_name IS NULL)),
  CHECK ((status = 'draft') = (published_at IS NULL))
);
CREATE UNIQUE INDEX legal_templates_published_uq ON legal_templates (kind) WHERE status = 'published';

CREATE OR REPLACE FUNCTION legal_templates_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF OLD.status <> 'draft' THEN
      RAISE EXCEPTION 'published templates are never deleted' USING ERRCODE = 'EQ001';
    END IF;
    RETURN OLD;
  END IF;
  IF OLD.status <> 'draft' THEN
    IF NOT (OLD.status = 'published' AND NEW.status = 'retired'
            AND (to_jsonb(NEW) - ARRAY['status', 'retired_at', 'updated_at']) = (to_jsonb(OLD) - ARRAY['status', 'retired_at', 'updated_at'])) THEN
      RAISE EXCEPTION 'legal template % is % and immutable', OLD.version, OLD.status USING ERRCODE = 'EQ001';
    END IF;
  END IF;
  NEW.updated_at := now();
  RETURN NEW;
END;
$$;
CREATE TRIGGER legal_templates_guard BEFORE UPDATE OR DELETE ON legal_templates FOR EACH ROW EXECUTE FUNCTION legal_templates_guard();
CREATE TRIGGER legal_templates_audit AFTER INSERT OR UPDATE OR DELETE ON legal_templates FOR EACH ROW EXECUTE FUNCTION audit_row_change();

-- The platform's starting wording, published for development. It carries
-- NO legal review: staging/production refuse to generate agreements from an
-- unreviewed template (LEGAL_REQUIRE_REVIEWED_TEMPLATES).
INSERT INTO legal_templates (kind, version, title, body, status, published_at) VALUES (
  'rental_agreement', 'rental-terms-2026.1', 'Equipment rental terms (initial draft)',
  $json${
    "preamble": "This agreement is made on the EQUIPRENT platform between the owner and the customer named below for the rental of the equipment described in it. EQUIPRENT is a marketplace and is not a party to the rental.",
    "condition_clauses": [
      "The owner hands over the equipment in safe working condition, with valid registration, insurance and fitness documents where the law requires them.",
      "The condition of the equipment at handover and at return is recorded on EQUIPRENT in the handover and return checklists, with hour-meter readings and photographs. Those records form part of this agreement.",
      "The customer reports any defect noticed at handover before the equipment is put to work."
    ],
    "use_clauses": [
      "The customer uses the equipment only at the work site stated, for the work described, within its rated capacity and in line with applicable safety rules.",
      "Working hours are measured by the hour meter and recorded in daily logs. Logs not disputed within 48 hours are treated as approved.",
      "Breakdowns are reported immediately on EQUIPRENT. Hours lost to a breakdown that is not the customer's fault are not billed.",
      "Routine maintenance during the rental is the owner's responsibility; the customer allows reasonable access for it.",
      "The equipment may not be sublet, moved to another site or used by anyone other than the agreed operator without the owner's written consent."
    ],
    "damage_clauses": [
      "The customer is responsible for loss of or damage to the equipment beyond normal wear and tear while it is in the customer's care, assessed at return against the handover checklist and photographs.",
      "Where the owner supplies the operator, damage caused by the operator's own negligence is the owner's responsibility.",
      "Damage charges are claimed through EQUIPRENT with supporting evidence. They may be set off against the security deposit; any shortfall is payable by the customer and any balance of the deposit is refunded.",
      "Either party may dispute a damage claim on EQUIPRENT before it is settled."
    ],
    "deposit_clauses": [
      "The security deposit is refundable. It is held for the rental and returned after the equipment is returned, less any damage or charges settled under this agreement."
    ],
    "general_terms": [
      "Each party complies with the laws that apply to it, including labour, safety and environmental rules at the site.",
      "Neither party is liable for delays caused by events beyond its reasonable control, such as natural disasters, strikes or government orders.",
      "Disputes are first raised on EQUIPRENT. Payments held by the platform are released according to its terms and the outcome of the dispute.",
      "This agreement is governed by the laws of India."
    ],
    "acceptance_statement": "Each party records its acceptance of this exact text on EQUIPRENT. An acceptance record holds the name typed by the person accepting, their account, the time, the IP address, the device and the SHA-256 fingerprint of this text. An acceptance record is evidence of agreement; it is not a digital signature or Aadhaar eSign under the Information Technology Act, 2000. Where the parties need such a signature, they sign through the e-signature provider offered on EQUIPRENT.",
    "consent_text": "I have read this agreement and accept it on behalf of my organisation. I understand that my acceptance is recorded electronically with my name, the time, my IP address and a fingerprint of this text."
  }$json$::jsonb,
  'published', now());

-- ---- agreement versions ----------------------------------------------------------------------
-- A booking can have several versions (re-issued when the dates or price
-- change, or amended after execution). One version is current.
ALTER TABLE rental_agreements DROP CONSTRAINT rental_agreements_booking_id_key;
ALTER TABLE rental_agreements
  ADD COLUMN version           integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  ADD COLUMN superseded_at     timestamptz,
  ADD COLUMN supersede_reason  text CHECK (supersede_reason IS NULL OR length(supersede_reason) <= 300),
  ADD COLUMN legal_template_id uuid REFERENCES legal_templates (id);
ALTER TABLE rental_agreements ADD CONSTRAINT rental_agreements_version_uq UNIQUE (booking_id, version);
CREATE UNIQUE INDEX rental_agreements_current_uq ON rental_agreements (booking_id) WHERE superseded_at IS NULL;

-- Executed agreements stay frozen; the only change allowed is marking them
-- superseded by an amendment.
CREATE OR REPLACE FUNCTION rental_agreements_freeze() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'rental agreements are never deleted' USING ERRCODE = 'EQ001';
  END IF;
  IF OLD.superseded_at IS NOT NULL THEN
    RAISE EXCEPTION 'rental agreement % is superseded and immutable', OLD.id USING ERRCODE = 'EQ001';
  END IF;
  IF OLD.customer_accepted_at IS NOT NULL AND OLD.owner_accepted_at IS NOT NULL THEN
    IF NEW.superseded_at IS NULL
       OR (to_jsonb(NEW) - ARRAY['superseded_at', 'supersede_reason']) <> (to_jsonb(OLD) - ARRAY['superseded_at', 'supersede_reason']) THEN
      RAISE EXCEPTION 'rental agreement % is signed and immutable', OLD.id USING ERRCODE = 'EQ001';
    END IF;
  END IF;
  IF NEW.terms_sha256 <> OLD.terms_sha256 OR NEW.booking_id <> OLD.booking_id OR NEW.version <> OLD.version
     OR NEW.snapshot IS DISTINCT FROM OLD.snapshot OR NEW.legal_template_id IS DISTINCT FROM OLD.legal_template_id THEN
    RAISE EXCEPTION 'agreement terms cannot change after creation' USING ERRCODE = 'EQ001';
  END IF;
  RETURN NEW;
END;
$$;

-- The checkbox text the person agreed to, kept with the acceptance record.
ALTER TABLE agreement_signatures ADD COLUMN consent_text text CHECK (consent_text IS NULL OR length(consent_text) <= 1000);

-- ---- tax rules -------------------------------------------------------------------------------
-- Rates stay configuration (tax_rates); each rule now records its SAC and
-- who reviewed it against current GST notifications.
ALTER TABLE tax_rates
  ADD COLUMN sac_code         text NOT NULL DEFAULT '997313' CHECK (sac_code ~ '^99[0-9]{4}$'),
  ADD COLUMN legal_basis      text CHECK (legal_basis IS NULL OR length(legal_basis) <= 300),
  ADD COLUMN reviewed_by_name text CHECK (reviewed_by_name IS NULL OR length(btrim(reviewed_by_name)) BETWEEN 2 AND 120),
  ADD COLUMN reviewed_at      timestamptz,
  ADD CONSTRAINT tax_rates_review_ck CHECK ((reviewed_at IS NULL) = (reviewed_by_name IS NULL));
CREATE TRIGGER tax_rates_audit AFTER INSERT OR UPDATE OR DELETE ON tax_rates FOR EACH ROW EXECUTE FUNCTION audit_row_change();

-- Line rates come from the configured rules, not a fixed list of slabs.
ALTER TABLE invoice_lines DROP CONSTRAINT invoice_lines_gst_rate_bps_check;
ALTER TABLE invoice_lines ADD CONSTRAINT invoice_lines_gst_rate_bps_check CHECK (gst_rate_bps BETWEEN 0 AND 10000);
ALTER TABLE invoice_lines ADD COLUMN tax_code text NOT NULL DEFAULT 'gst' CHECK (tax_code ~ '^[a-z0-9_]+$');

-- ---- invoices --------------------------------------------------------------------------------
-- Parties, equipment, rental dates and payment references as at issue
-- (part of the immutable issued invoice), plus the credit note reason.
ALTER TABLE invoices
  ADD COLUMN title   text NOT NULL DEFAULT 'Tax invoice' CHECK (title IN ('Tax invoice', 'Bill of supply', 'Credit note')),
  ADD COLUMN details jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(details) = 'object'),
  ADD COLUMN reason  text CHECK (reason IS NULL OR length(reason) <= 300);
UPDATE invoices SET title = 'Credit note' WHERE invoice_type = 'credit_note';
CREATE INDEX invoices_original_idx ON invoices (original_invoice_id) WHERE original_invoice_id IS NOT NULL;

-- Undo 000021's one-open-order index on databases that applied it: an
-- abandoned order may still be paid late and must be capturable.
DROP INDEX IF EXISTS payments_open_order_uq;
