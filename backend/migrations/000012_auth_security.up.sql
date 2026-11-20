-- Password accounts, email verification and password reset.
--
-- Passwords are stored as Argon2id PHC strings in users.password_hash (the
-- column already exists). Email ownership is proven separately from phone
-- ownership; each identifier can be used to sign in only once verified.

ALTER TABLE users
  ADD COLUMN email_verified_at   timestamptz,
  ADD COLUMN password_changed_at timestamptz;

-- Changing the email address always drops its verification, whatever code
-- path performs the update.
CREATE OR REPLACE FUNCTION users_email_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.email IS DISTINCT FROM OLD.email AND NEW.email_verified_at IS NOT DISTINCT FROM OLD.email_verified_at THEN
    NEW.email_verified_at := NULL;
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER users_email_change BEFORE UPDATE OF email ON users FOR EACH ROW EXECUTE FUNCTION users_email_change();

-- OTPs are also used to verify a phone after registration and to reset a password.
ALTER TABLE otp_challenges DROP CONSTRAINT otp_challenges_purpose_check;
ALTER TABLE otp_challenges ADD CONSTRAINT otp_challenges_purpose_check
  CHECK (purpose IN ('login', 'change_phone', 'step_up', 'handover', 'password_reset'));

-- Single-use, expiring tokens delivered by email. Only a SHA-256 of the
-- token is stored, so a database leak cannot be replayed.
CREATE TABLE auth_tokens (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  purpose     text NOT NULL CHECK (purpose IN ('email_verify', 'password_reset')),
  token_hash  bytea NOT NULL UNIQUE CHECK (length(token_hash) = 32),
  email       citext,               -- the address the token was sent to
  expires_at  timestamptz NOT NULL,
  consumed_at timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  CHECK (expires_at > created_at)
);
CREATE INDEX auth_tokens_user_idx ON auth_tokens (user_id, purpose) WHERE consumed_at IS NULL;
