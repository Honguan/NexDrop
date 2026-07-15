ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_secret text;
ALTER TABLE user_sessions ADD COLUMN IF NOT EXISTS admin_verified_at timestamptz;
