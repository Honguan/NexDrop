CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username text NOT NULL,
    email text NOT NULL,
    password_hash text NOT NULL,
    is_admin boolean NOT NULL DEFAULT false,
    disabled_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX users_username_lower_key ON users (lower(username));
CREATE UNIQUE INDEX users_email_lower_key ON users (lower(email));

CREATE TABLE user_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id uuid,
    access_token_hash bytea NOT NULL UNIQUE,
    access_expires_at timestamptz NOT NULL,
    refresh_token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE devices (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    display_name text NOT NULL,
    device_type text NOT NULL,
    trust_status text NOT NULL DEFAULT 'PENDING',
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE user_sessions
    ADD CONSTRAINT user_sessions_device_fk
    FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE CASCADE;

CREATE TABLE device_keys (
    device_id uuid PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
    public_key bytea NOT NULL,
    key_algorithm text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE device_connections (
    device_id uuid PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
    connected_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    disconnected_at timestamptz,
    protocol_version text NOT NULL,
    client_version text NOT NULL
);

CREATE TABLE device_pairing_codes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    target_device_id uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    created_by_session_id uuid NOT NULL REFERENCES user_sessions(id) ON DELETE CASCADE,
    code_hash bytea NOT NULL,
    attempt_count integer NOT NULL DEFAULT 0,
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX device_pairing_codes_target_idx
    ON device_pairing_codes(target_device_id, expires_at DESC);

CREATE TABLE groups (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id uuid NOT NULL REFERENCES users(id),
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE group_members (
    group_id uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role text NOT NULL,
    joined_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);

CREATE TABLE group_devices (
    group_id uuid NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    device_id uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    added_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, device_id)
);

CREATE TABLE messages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id uuid UNIQUE,
    sender_user_id uuid NOT NULL REFERENCES users(id),
    sender_device_id uuid NOT NULL REFERENCES devices(id),
    group_id uuid REFERENCES groups(id) ON DELETE CASCADE,
    content_type text NOT NULL,
    encrypted_content bytea,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz
);

CREATE TABLE message_targets (
    message_id uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    target_device_id uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    wrapped_content_key bytea,
    PRIMARY KEY (message_id, target_device_id)
);

CREATE TABLE transfer_tasks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sender_user_id uuid NOT NULL REFERENCES users(id),
    sender_device_id uuid NOT NULL REFERENCES devices(id),
    group_id uuid REFERENCES groups(id),
    target_type text NOT NULL,
    content_type text NOT NULL,
    total_file_count integer NOT NULL DEFAULT 0,
    total_size bigint NOT NULL DEFAULT 0,
    status text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz
);

ALTER TABLE messages
    ADD CONSTRAINT messages_transfer_fk
    FOREIGN KEY (transfer_id) REFERENCES transfer_tasks(id) ON DELETE CASCADE;

CREATE TABLE transfer_content_keys (
    transfer_id uuid NOT NULL REFERENCES transfer_tasks(id) ON DELETE CASCADE,
    target_device_id uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    wrapped_content_key bytea NOT NULL,
    PRIMARY KEY (transfer_id, target_device_id)
);

CREATE TABLE transfer_targets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id uuid NOT NULL REFERENCES transfer_tasks(id) ON DELETE CASCADE,
    target_device_id uuid NOT NULL REFERENCES devices(id),
    selected_route text NOT NULL,
    status text NOT NULL,
    bytes_transferred bigint NOT NULL DEFAULT 0,
    started_at timestamptz,
    completed_at timestamptz,
    read_at timestamptz,
    error_code text,
    UNIQUE (transfer_id, target_device_id)
);

CREATE TABLE transfer_routes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_target_id uuid NOT NULL REFERENCES transfer_targets(id) ON DELETE CASCADE,
    route text NOT NULL,
    started_at timestamptz NOT NULL DEFAULT now(),
    ended_at timestamptz,
    error_code text
);

CREATE TABLE files (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id uuid NOT NULL REFERENCES transfer_tasks(id) ON DELETE CASCADE,
    original_name text NOT NULL,
    mime_type text NOT NULL,
    size bigint NOT NULL,
    sha256 bytea NOT NULL,
    chunk_size integer NOT NULL DEFAULT 8388608,
    chunk_count integer NOT NULL,
    storage_path text NOT NULL,
    status text NOT NULL DEFAULT 'UPLOADING',
    completed_at timestamptz,
    expires_at timestamptz NOT NULL
);

CREATE TABLE transfer_file_targets (
    file_id uuid NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    target_device_id uuid NOT NULL REFERENCES devices(id),
    selected_route text NOT NULL,
    status text NOT NULL,
    bytes_transferred bigint NOT NULL DEFAULT 0,
    error_code text,
    PRIMARY KEY (file_id, target_device_id)
);

CREATE TABLE file_chunks (
    file_id uuid NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    chunk_index integer NOT NULL,
    size integer NOT NULL,
    sha256 bytea NOT NULL,
    status text NOT NULL,
    storage_path text NOT NULL,
    completed_at timestamptz NOT NULL,
    PRIMARY KEY (file_id, chunk_index)
);

CREATE TABLE delivery_status (
    transfer_id uuid NOT NULL REFERENCES transfer_tasks(id) ON DELETE CASCADE,
    device_id uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    delivered_at timestamptz,
    read_at timestamptz,
    PRIMARY KEY (transfer_id, device_id)
);

CREATE TABLE notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    notification_type text NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    acknowledged_at timestamptz
);

CREATE TABLE transfer_metrics (
    event_id uuid PRIMARY KEY,
    transfer_id uuid NOT NULL,
    sender_user_id uuid NOT NULL,
    sender_device_id uuid NOT NULL,
    receiver_device_id uuid,
    group_id uuid,
    content_type text NOT NULL,
    route text NOT NULL,
    file_size bigint NOT NULL DEFAULT 0,
    started_at timestamptz NOT NULL,
    completed_at timestamptz,
    average_bytes_per_second bigint,
    retry_count integer NOT NULL DEFAULT 0,
    succeeded boolean,
    error_code text
);

CREATE TABLE daily_statistics (
    statistic_date date NOT NULL,
    owner_type text NOT NULL,
    owner_id uuid,
    transfer_count bigint NOT NULL DEFAULT 0,
    transfer_bytes bigint NOT NULL DEFAULT 0,
    lan_bytes bigint NOT NULL DEFAULT 0,
    node_bytes bigint NOT NULL DEFAULT 0,
    failed_count bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (statistic_date, owner_type, owner_id)
);

CREATE TABLE system_metrics (
    recorded_at timestamptz NOT NULL,
    cpu_percent real NOT NULL,
    memory_bytes bigint NOT NULL,
    disk_bytes bigint NOT NULL,
    cache_bytes bigint NOT NULL,
    network_upload_bytes bigint NOT NULL,
    network_download_bytes bigint NOT NULL,
    online_devices integer NOT NULL,
    active_transfers integer NOT NULL,
    PRIMARY KEY (recorded_at)
);

CREATE TABLE audit_logs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_user_id uuid REFERENCES users(id),
    actor_device_id uuid REFERENCES devices(id),
    action text NOT NULL,
    target_type text NOT NULL,
    target_id uuid,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE storage_quotas (
    owner_type text NOT NULL,
    owner_id uuid,
    byte_limit bigint NOT NULL,
    bytes_used bigint NOT NULL DEFAULT 0,
    daily_transfer_limit bigint,
    daily_transfer_used bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_type, owner_id)
);

CREATE TABLE node_settings (
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

INSERT INTO node_settings (singleton) VALUES (true);

CREATE INDEX transfer_tasks_sender_created_idx ON transfer_tasks(sender_user_id, created_at DESC);
CREATE INDEX transfer_targets_device_status_idx ON transfer_targets(target_device_id, status);
CREATE INDEX messages_group_created_idx ON messages(group_id, created_at DESC);
CREATE INDEX files_expires_idx ON files(expires_at);
CREATE INDEX transfer_metrics_started_idx ON transfer_metrics(started_at);
CREATE INDEX notifications_device_created_idx ON notifications(device_id, created_at DESC);
CREATE INDEX audit_logs_created_idx ON audit_logs(created_at DESC);
