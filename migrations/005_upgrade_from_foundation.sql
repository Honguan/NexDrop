ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_secret text;
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_admin boolean NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS disabled_at timestamptz;
CREATE UNIQUE INDEX IF NOT EXISTS users_username_lower_key ON users (lower(username));
CREATE UNIQUE INDEX IF NOT EXISTS users_email_lower_key ON users (lower(email));

DELETE FROM user_sessions;
ALTER TABLE user_sessions ADD COLUMN IF NOT EXISTS device_id uuid;
ALTER TABLE user_sessions ADD COLUMN IF NOT EXISTS access_token_hash bytea;
ALTER TABLE user_sessions ADD COLUMN IF NOT EXISTS access_expires_at timestamptz;
ALTER TABLE user_sessions ADD COLUMN IF NOT EXISTS admin_verified_at timestamptz;
ALTER TABLE user_sessions ALTER COLUMN refresh_token_hash TYPE bytea USING convert_to(refresh_token_hash::text, 'UTF8');
ALTER TABLE user_sessions ALTER COLUMN access_token_hash SET NOT NULL;
ALTER TABLE user_sessions ALTER COLUMN access_expires_at SET NOT NULL;
DO $$ BEGIN
    ALTER TABLE user_sessions ADD CONSTRAINT user_sessions_device_fk
        FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS device_lan_identities (
    device_id uuid PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
    short_device_id text NOT NULL UNIQUE,
    certificate_fingerprint text NOT NULL UNIQUE,
    certificate_pem text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS device_session_challenges (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    session_id uuid NOT NULL REFERENCES user_sessions(id) ON DELETE CASCADE,
    proof_hash bytea NOT NULL,
    attempt_count integer NOT NULL DEFAULT 0,
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS device_session_challenges_session_idx ON device_session_challenges(session_id, expires_at DESC);
ALTER TABLE device_connections ADD COLUMN IF NOT EXISTS disconnected_at timestamptz;
CREATE TABLE IF NOT EXISTS device_pairing_codes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    target_device_id uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    created_by_session_id uuid NOT NULL REFERENCES user_sessions(id) ON DELETE CASCADE,
    code_hash bytea NOT NULL,
    attempt_count integer NOT NULL DEFAULT 0,
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS device_pairing_codes_target_idx ON device_pairing_codes(target_device_id, expires_at DESC);

ALTER TABLE messages ADD COLUMN IF NOT EXISTS transfer_id uuid UNIQUE;
ALTER TABLE transfer_tasks ADD COLUMN IF NOT EXISTS group_id uuid REFERENCES groups(id);
ALTER TABLE transfer_tasks ADD COLUMN IF NOT EXISTS group_deleted_at timestamptz;
DO $$ BEGIN
    ALTER TABLE messages ADD CONSTRAINT messages_transfer_fk
        FOREIGN KEY (transfer_id) REFERENCES transfer_tasks(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
CREATE TABLE IF NOT EXISTS transfer_content_keys (
    transfer_id uuid NOT NULL REFERENCES transfer_tasks(id) ON DELETE CASCADE,
    target_device_id uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    wrapped_content_key bytea NOT NULL,
    PRIMARY KEY (transfer_id, target_device_id)
);
CREATE TABLE IF NOT EXISTS transfer_hidden_users (
    transfer_id uuid NOT NULL REFERENCES transfer_tasks(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    hidden_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (transfer_id, user_id)
);

ALTER TABLE files ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'UPLOADING';
ALTER TABLE files ADD COLUMN IF NOT EXISTS completed_at timestamptz;
CREATE TABLE IF NOT EXISTS transfer_file_targets (
    file_id uuid NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    target_device_id uuid NOT NULL REFERENCES devices(id),
    selected_route text NOT NULL,
    status text NOT NULL,
    bytes_transferred bigint NOT NULL DEFAULT 0,
    error_code text,
    PRIMARY KEY (file_id, target_device_id)
);
ALTER TABLE file_chunks ADD COLUMN IF NOT EXISTS storage_path text NOT NULL DEFAULT '';
ALTER TABLE file_chunks ADD COLUMN IF NOT EXISTS completed_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE transfer_metrics DROP CONSTRAINT IF EXISTS transfer_metrics_transfer_id_fkey;
ALTER TABLE transfer_metrics DROP CONSTRAINT IF EXISTS transfer_metrics_sender_device_id_fkey;
ALTER TABLE transfer_metrics DROP CONSTRAINT IF EXISTS transfer_metrics_receiver_device_id_fkey;
ALTER TABLE transfer_metrics DROP CONSTRAINT IF EXISTS transfer_metrics_group_id_fkey;
ALTER TABLE transfer_metrics ADD COLUMN IF NOT EXISTS sender_user_id uuid;
UPDATE transfer_metrics metric SET sender_user_id = device.user_id
FROM devices device WHERE device.id = metric.sender_device_id AND metric.sender_user_id IS NULL;
ALTER TABLE transfer_metrics ALTER COLUMN sender_user_id SET NOT NULL;

CREATE TABLE IF NOT EXISTS node_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    single_file_limit_bytes bigint NOT NULL DEFAULT 2147483648,
    default_user_quota_bytes bigint NOT NULL DEFAULT 10737418240,
    default_group_quota_bytes bigint NOT NULL DEFAULT 21474836480,
    node_cache_limit_bytes bigint NOT NULL DEFAULT 107374182400,
    default_user_daily_bytes bigint NOT NULL DEFAULT 53687091200,
    default_group_daily_bytes bigint NOT NULL DEFAULT 107374182400,
    disk_warning_percent integer NOT NULL DEFAULT 80,
    disk_stop_percent integer NOT NULL DEFAULT 95,
    CHECK (disk_warning_percent > 0 AND disk_warning_percent < disk_stop_percent),
    CHECK (disk_stop_percent <= 100)
);
INSERT INTO node_settings (singleton) VALUES (true) ON CONFLICT (singleton) DO NOTHING;
