DROP TABLE IF EXISTS org_invitations;
DROP INDEX IF EXISTS auth_tokens_email_idx;
ALTER TABLE auth_tokens DROP COLUMN attempts;
