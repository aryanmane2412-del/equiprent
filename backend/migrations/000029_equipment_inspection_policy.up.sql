CREATE TABLE equipment_inspection_policy_consents (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  policy_version text NOT NULL CHECK (length(policy_version) BETWEEN 1 AND 40),
  accepted_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, policy_version)
);
CREATE INDEX equipment_inspection_policy_consents_user_idx
  ON equipment_inspection_policy_consents(user_id, accepted_at DESC);
CREATE TRIGGER equipment_inspection_policy_consents_audit
  AFTER INSERT ON equipment_inspection_policy_consents
  FOR EACH ROW EXECUTE FUNCTION audit_row_change();
