CREATE TABLE booking_inspection_acknowledgements (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  booking_id uuid NOT NULL REFERENCES bookings(id),
  stage text NOT NULL CHECK (stage IN ('handover', 'return')),
  user_id uuid NOT NULL REFERENCES users(id),
  role text NOT NULL CHECK (role IN ('customer', 'owner', 'operator')),
  acknowledged_at timestamptz NOT NULL DEFAULT now(),
  disagreement text CHECK (disagreement IS NULL OR length(disagreement) <= 2000),
  UNIQUE (booking_id, stage, user_id)
);
CREATE INDEX booking_inspection_ack_booking_idx ON booking_inspection_acknowledgements(booking_id, stage);
CREATE TRIGGER booking_inspection_ack_audit AFTER INSERT ON booking_inspection_acknowledgements
  FOR EACH ROW EXECUTE FUNCTION audit_row_change();

CREATE TABLE inspection_decision_history (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  issue_id uuid NOT NULL REFERENCES issue_reports(id),
  actor_user_id uuid NOT NULL REFERENCES users(id),
  action text NOT NULL CHECK (action IN ('request_explanation', 'decision')),
  party_role text CHECK (party_role IS NULL OR party_role IN ('customer', 'owner', 'operator')),
  note text NOT NULL CHECK (length(btrim(note)) BETWEEN 3 AND 2000),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX inspection_decision_history_issue_idx ON inspection_decision_history(issue_id, created_at);
CREATE TRIGGER inspection_decision_history_audit AFTER INSERT ON inspection_decision_history
  FOR EACH ROW EXECUTE FUNCTION audit_row_change();
