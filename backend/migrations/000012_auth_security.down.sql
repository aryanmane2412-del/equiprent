DROP TABLE IF EXISTS auth_tokens;
DELETE FROM otp_challenges WHERE purpose = 'password_reset';
ALTER TABLE otp_challenges DROP CONSTRAINT otp_challenges_purpose_check;
ALTER TABLE otp_challenges ADD CONSTRAINT otp_challenges_purpose_check
  CHECK (purpose IN ('login', 'change_phone', 'step_up', 'handover'));
DROP TRIGGER IF EXISTS users_email_change ON users;
DROP FUNCTION IF EXISTS users_email_change();
ALTER TABLE users DROP COLUMN IF EXISTS password_changed_at, DROP COLUMN IF EXISTS email_verified_at;
