-- H9: failed-attempt counter per email verification token.
ALTER TABLE auth_tokens ADD COLUMN attempts int NOT NULL DEFAULT 0 CHECK (attempts >= 0);
CREATE INDEX auth_tokens_email_idx ON auth_tokens (email, purpose, created_at DESC) WHERE consumed_at IS NULL;

-- H8: organisation invitations. A role (operator) is granted only when the invited
-- user accepts; nothing about an existing operator profile changes before that.
CREATE TABLE org_invitations (
  id              uuid PRIMARY KEY DEFAULT uuidv7(),
  organization_id uuid NOT NULL REFERENCES organizations (id),
  invitee_user_id uuid NOT NULL REFERENCES users (id),
  role_code       text NOT NULL CHECK (role_code = 'operator'),
  invited_by      uuid NOT NULL REFERENCES users (id),
  status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'declined', 'revoked')),
  expires_at      timestamptz NOT NULL DEFAULT now() + interval '14 days',
  responded_at    timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CHECK ((status = 'pending') = (responded_at IS NULL))
);
CREATE UNIQUE INDEX org_invitations_pending_uq ON org_invitations (organization_id, invitee_user_id, role_code) WHERE status = 'pending';
CREATE INDEX org_invitations_invitee_idx ON org_invitations (invitee_user_id, status);
CREATE TRIGGER org_invitations_audit AFTER INSERT OR UPDATE ON org_invitations FOR EACH ROW EXECUTE FUNCTION audit_row_change();
