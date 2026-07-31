CREATE TABLE enrollment_grants (
    id uuid PRIMARY KEY,
    node_id text NOT NULL,
    owner_user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    device_type text,
    display_name_hint text,
    permissions jsonb NOT NULL DEFAULT '{}'::jsonb,
    maximum_uses integer NOT NULL CHECK (maximum_uses > 0),
    used_count integer NOT NULL DEFAULT 0 CHECK (used_count >= 0),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (used_count <= maximum_uses)
);

CREATE INDEX enrollment_grants_active_idx
    ON enrollment_grants(expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE device_credentials (
    device_id uuid PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
    credential_hash bytea NOT NULL UNIQUE,
    permissions jsonb NOT NULL DEFAULT '{}'::jsonb,
    issued_from_grant_id uuid REFERENCES enrollment_grants(id) ON DELETE SET NULL,
    last_used_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE device_route_history (
    device_id uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    route_id text NOT NULL,
    route_kind text NOT NULL,
    endpoint_hash bytea NOT NULL,
    handshake_latency_ms bigint,
    throughput_bytes_per_second bigint,
    success_count bigint NOT NULL DEFAULT 0,
    failure_count bigint NOT NULL DEFAULT 0,
    last_success_at timestamptz,
    last_failure_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (device_id, route_id)
);

CREATE TABLE transfer_recovery_queue (
    transfer_id uuid NOT NULL REFERENCES transfer_tasks(id) ON DELETE CASCADE,
    target_device_id uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    classification text NOT NULL,
    retry_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz,
    lease_expires_at timestamptz,
    stable_error_code text,
    recovery_reason text,
    dead_lettered_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (transfer_id, target_device_id)
);

CREATE INDEX transfer_recovery_due_idx
    ON transfer_recovery_queue(next_attempt_at, updated_at)
    WHERE dead_lettered_at IS NULL;

CREATE TABLE relays (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    endpoint text NOT NULL UNIQUE,
    region text NOT NULL DEFAULT '',
    identity_public_key bytea NOT NULL,
    capacity_bytes bigint NOT NULL CHECK (capacity_bytes >= 0),
    used_bytes bigint NOT NULL DEFAULT 0 CHECK (used_bytes >= 0),
    healthy boolean NOT NULL DEFAULT false,
    draining boolean NOT NULL DEFAULT false,
    last_seen_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (used_bytes <= capacity_bytes)
);

CREATE TABLE relay_chunk_assignments (
    relay_id uuid NOT NULL REFERENCES relays(id) ON DELETE CASCADE,
    file_id uuid NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    first_chunk integer NOT NULL CHECK (first_chunk >= 0),
    last_chunk integer NOT NULL CHECK (last_chunk >= first_chunk),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (relay_id, file_id, first_chunk)
);

CREATE TABLE transfer_delivery_policies (
    transfer_id uuid NOT NULL REFERENCES transfer_tasks(id) ON DELETE CASCADE,
    target_device_id uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    priority integer NOT NULL DEFAULT 1 CHECK (priority BETWEEN 0 AND 2),
    wifi_only boolean NOT NULL DEFAULT false,
    charging_only boolean NOT NULL DEFAULT false,
    maximum_mobile_data_bytes bigint NOT NULL DEFAULT 0 CHECK (maximum_mobile_data_bytes >= -1),
    automatic_download boolean NOT NULL DEFAULT false,
    foreground_only boolean NOT NULL DEFAULT false,
    metadata_synchronized_at timestamptz,
    body_downloaded_at timestamptz,
    state text NOT NULL DEFAULT 'QUEUED',
    reason_code text,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (transfer_id, target_device_id)
);

CREATE TABLE folder_manifests (
    transfer_id uuid PRIMARY KEY REFERENCES transfer_tasks(id) ON DELETE CASCADE,
    manifest_version integer NOT NULL,
    root_name text NOT NULL,
    entry_count integer NOT NULL CHECK (entry_count >= 0),
    total_size bigint NOT NULL CHECK (total_size >= 0),
    manifest_hash bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE folder_manifest_entries (
    transfer_id uuid NOT NULL REFERENCES folder_manifests(transfer_id) ON DELETE CASCADE,
    entry_index integer NOT NULL CHECK (entry_index >= 0),
    normalized_path text NOT NULL,
    entry_type text NOT NULL,
    file_id uuid REFERENCES files(id) ON DELETE CASCADE,
    modified_at timestamptz,
    selected boolean NOT NULL DEFAULT true,
    conflict_action text,
    PRIMARY KEY (transfer_id, entry_index),
    UNIQUE (transfer_id, normalized_path)
);

CREATE TABLE message_read_cursors (
    device_id uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    group_id uuid REFERENCES groups(id) ON DELETE CASCADE,
    last_message_id uuid REFERENCES messages(id) ON DELETE SET NULL,
    last_created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (device_id, group_id)
);

CREATE TABLE message_tombstones (
    message_id uuid PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
    scope text NOT NULL,
    actor_device_id uuid NOT NULL REFERENCES devices(id),
    version integer NOT NULL DEFAULT 1,
    signature bytea NOT NULL,
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL
);

CREATE TABLE message_retention_policies (
    group_id uuid PRIMARY KEY REFERENCES groups(id) ON DELETE CASCADE,
    message_mode text NOT NULL DEFAULT 'PERMANENT',
    message_days integer,
    attachment_mode text NOT NULL DEFAULT 'PERMANENT',
    attachment_days integer,
    tombstone_days integer NOT NULL DEFAULT 90 CHECK (tombstone_days > 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);
