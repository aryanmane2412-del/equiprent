-- Fleet and operator operations: named fleets (groups of an owner's
-- machines with their managers), one scope view used by every access
-- check, job evidence photos, issue (damage) reports and account
-- restrictions.

-- ---- fleets ------------------------------------------------------------------------------------
CREATE TABLE fleets (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  owner_org_id uuid NOT NULL REFERENCES organizations (id),
  name         text NOT NULL CHECK (length(btrim(name)) BETWEEN 2 AND 80),
  description  text CHECK (description IS NULL OR length(description) <= 500),
  status       text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
  archived_reason text CHECK (archived_reason IS NULL OR length(archived_reason) <= 300),
  created_by   uuid REFERENCES users (id),
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX fleets_name_uq ON fleets (owner_org_id, lower(name)) WHERE status = 'active';
CREATE INDEX fleets_org_idx ON fleets (owner_org_id, status);
CREATE TRIGGER fleets_updated_at BEFORE UPDATE ON fleets FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER fleets_audit AFTER INSERT OR UPDATE OR DELETE ON fleets FOR EACH ROW EXECUTE FUNCTION audit_row_change();

-- A machine is in at most one fleet, and only in a fleet of its own owner.
CREATE TABLE fleet_equipment (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  fleet_id     uuid NOT NULL REFERENCES fleets (id) ON DELETE CASCADE,
  equipment_id uuid NOT NULL UNIQUE REFERENCES equipment (id) ON DELETE CASCADE,
  added_by     uuid REFERENCES users (id),
  added_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX fleet_equipment_fleet_idx ON fleet_equipment (fleet_id);
CREATE TRIGGER fleet_equipment_audit AFTER INSERT OR DELETE ON fleet_equipment FOR EACH ROW EXECUTE FUNCTION audit_row_change();

CREATE TABLE fleet_managers (
  id       uuid PRIMARY KEY DEFAULT uuidv7(),
  fleet_id uuid NOT NULL REFERENCES fleets (id) ON DELETE CASCADE,
  user_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  added_by uuid REFERENCES users (id),
  added_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (fleet_id, user_id)
);
CREATE INDEX fleet_managers_user_idx ON fleet_managers (user_id);
CREATE TRIGGER fleet_managers_audit AFTER INSERT OR DELETE ON fleet_managers FOR EACH ROW EXECUTE FUNCTION audit_row_change();

-- Same-organisation rules, enforced in the database.
CREATE OR REPLACE FUNCTION fleet_membership_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE v_org uuid;
BEGIN
  SELECT owner_org_id INTO v_org FROM fleets WHERE id = NEW.fleet_id;
  IF TG_TABLE_NAME = 'fleet_equipment' THEN
    IF NOT EXISTS (SELECT 1 FROM equipment e WHERE e.id = NEW.equipment_id AND e.owner_org_id = v_org AND e.deleted_at IS NULL) THEN
      RAISE EXCEPTION 'equipment % does not belong to the fleet''s organisation', NEW.equipment_id USING ERRCODE = 'EQ004';
    END IF;
  ELSE
    IF NOT EXISTS (SELECT 1 FROM user_roles ur WHERE ur.user_id = NEW.user_id AND ur.organization_id = v_org
                     AND ur.role_code = 'fleet_manager' AND ur.revoked_at IS NULL) THEN
      RAISE EXCEPTION 'user % is not a fleet manager of the fleet''s organisation', NEW.user_id USING ERRCODE = 'EQ004';
    END IF;
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER fleet_equipment_guard BEFORE INSERT OR UPDATE ON fleet_equipment FOR EACH ROW EXECUTE FUNCTION fleet_membership_guard();
CREATE TRIGGER fleet_managers_guard BEFORE INSERT OR UPDATE ON fleet_managers FOR EACH ROW EXECUTE FUNCTION fleet_membership_guard();

-- Every machine a fleet manager may manage: direct grants plus the machines
-- of the active fleets they manage, while their role is current and no
-- restriction blocks fleet management. (all_equipment is checked
-- separately.) Every access check reads this view.
CREATE TABLE account_restrictions (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  kind        text NOT NULL CHECK (kind IN ('operator_assignments', 'fleet_management', 'new_bookings')),
  reason      text NOT NULL CHECK (length(btrim(reason)) BETWEEN 3 AND 500),
  created_by  uuid REFERENCES users (id),
  created_at  timestamptz NOT NULL DEFAULT now(),
  expires_at  timestamptz,
  lifted_at   timestamptz,
  lifted_by   uuid REFERENCES users (id),
  lift_reason text,
  CHECK (expires_at IS NULL OR expires_at > created_at),
  CHECK ((lifted_at IS NULL) = (lifted_by IS NULL))
);
CREATE UNIQUE INDEX account_restrictions_active_uq ON account_restrictions (user_id, kind) WHERE lifted_at IS NULL;
CREATE TRIGGER account_restrictions_audit AFTER INSERT OR UPDATE ON account_restrictions FOR EACH ROW EXECUTE FUNCTION audit_row_change();

CREATE OR REPLACE FUNCTION is_restricted(p_user uuid, p_kind text) RETURNS boolean
LANGUAGE sql STABLE AS $$
  SELECT EXISTS (SELECT 1 FROM account_restrictions r
                  WHERE r.user_id = p_user AND r.kind = p_kind AND r.lifted_at IS NULL
                    AND (r.expires_at IS NULL OR r.expires_at > now()))
$$;

CREATE VIEW fleet_manager_scope AS
SELECT s.user_id, s.organization_id, s.equipment_id
FROM (
  SELECT g.user_id, g.organization_id, g.equipment_id FROM fleet_manager_equipment g
  UNION
  SELECT fm.user_id, f.owner_org_id, fe.equipment_id
  FROM fleet_managers fm
  JOIN fleets f ON f.id = fm.fleet_id AND f.status = 'active'
  JOIN fleet_equipment fe ON fe.fleet_id = f.id
) s
JOIN equipment e ON e.id = s.equipment_id AND e.deleted_at IS NULL
WHERE EXISTS (SELECT 1 FROM user_roles ur WHERE ur.user_id = s.user_id AND ur.organization_id = s.organization_id
                AND ur.role_code = 'fleet_manager' AND ur.revoked_at IS NULL)
  AND NOT is_restricted(s.user_id, 'fleet_management');

-- ---- job evidence ---------------------------------------------------------------------------
ALTER TABLE uploads DROP CONSTRAINT IF EXISTS uploads_purpose_check;
ALTER TABLE uploads ADD CONSTRAINT uploads_purpose_check
  CHECK (purpose IN ('identity_document', 'business_document', 'operator_document', 'avatar', 'equipment_photo',
                     'equipment_document', 'chat_image', 'maintenance_document', 'agreement_document', 'job_photo'));

ALTER TABLE checklists ADD COLUMN notes text CHECK (notes IS NULL OR length(notes) <= 1000);
ALTER TABLE checklists ADD COLUMN photo_upload_ids uuid[] NOT NULL DEFAULT '{}';

-- ---- reported issues ---------------------------------------------------------------------------
CREATE TYPE issue_status AS ENUM ('open', 'under_review', 'resolved', 'dismissed');

CREATE TABLE issue_reports (
  id               uuid PRIMARY KEY DEFAULT uuidv7(),
  booking_id       uuid NOT NULL REFERENCES bookings (id),
  equipment_id     uuid NOT NULL REFERENCES equipment (id),
  kind             text NOT NULL CHECK (kind IN ('damage', 'breakdown', 'safety', 'site_access', 'other')),
  stage            text NOT NULL CHECK (stage IN ('before_handover', 'during_rental', 'at_return', 'inspection_handover', 'inspection_return')),
  severity         text NOT NULL CHECK (severity IN ('minor', 'major', 'critical')),
  description      text NOT NULL CHECK (length(btrim(description)) BETWEEN 5 AND 2000),
  photo_upload_ids uuid[] NOT NULL DEFAULT '{}',
  reported_by      uuid NOT NULL REFERENCES users (id),
  reporter_role    text NOT NULL CHECK (reporter_role IN ('operator', 'owner', 'customer')),
  status           issue_status NOT NULL DEFAULT 'open',
  resolution_note  text CHECK (resolution_note IS NULL OR length(resolution_note) <= 1000),
  reviewed_by      uuid REFERENCES users (id),
  reviewed_at      timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  CHECK (status IN ('open', 'under_review') OR (reviewed_at IS NOT NULL AND resolution_note IS NOT NULL))
);
CREATE INDEX issue_reports_booking_idx ON issue_reports (booking_id, created_at);
CREATE INDEX issue_reports_queue_idx ON issue_reports (status, created_at);
CREATE TRIGGER issue_reports_updated_at BEFORE UPDATE ON issue_reports FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER issue_reports_audit AFTER INSERT OR UPDATE ON issue_reports FOR EACH ROW EXECUTE FUNCTION audit_row_change();
