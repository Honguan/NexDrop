ALTER TABLE device_credentials
    ADD COLUMN credential_version integer NOT NULL DEFAULT 1,
    ADD COLUMN rotated_at timestamptz,
    ADD COLUMN last_session_id uuid REFERENCES user_sessions(id) ON DELETE SET NULL;

CREATE TABLE device_credential_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    event_type text NOT NULL,
    session_id uuid REFERENCES user_sessions(id) ON DELETE SET NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE transfer_targets
    ADD COLUMN active_route_id text,
    ADD COLUMN fallback_reason text,
    ADD COLUMN route_changed_at timestamptz;

ALTER TABLE transfer_file_targets
    ADD COLUMN verified_chunks jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE transfer_tasks
    ADD COLUMN adaptive_profile jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE transfer_recovery_queue
    ADD COLUMN lease_owner text,
    ADD COLUMN last_attempt_at timestamptz,
    ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();

CREATE TABLE transfer_recovery_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    trigger_type text NOT NULL,
    scanned integer NOT NULL DEFAULT 0,
    leased integer NOT NULL DEFAULT 0,
    completed integer NOT NULL DEFAULT 0,
    waiting integer NOT NULL DEFAULT 0,
    repaired integer NOT NULL DEFAULT 0,
    dead_letters integer NOT NULL DEFAULT 0,
    started_at timestamptz NOT NULL,
    completed_at timestamptz,
    error_code text
);

ALTER TABLE relays
    ADD COLUMN credential_hash bytea,
    ADD COLUMN latency_ms bigint,
    ADD COLUMN failure_rate double precision NOT NULL DEFAULT 0,
    ADD COLUMN retention_seconds integer NOT NULL DEFAULT 604800,
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

CREATE UNIQUE INDEX relays_credential_hash_key
    ON relays(credential_hash)
    WHERE credential_hash IS NOT NULL;

CREATE TABLE relay_grant_nonces (
    relay_id uuid NOT NULL REFERENCES relays(id) ON DELETE CASCADE,
    nonce_hash bytea NOT NULL,
    operation text NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    PRIMARY KEY (relay_id, nonce_hash)
);

ALTER TABLE transfer_delivery_policies
    ADD COLUMN expires_at timestamptz,
    ADD COLUMN size_bytes bigint NOT NULL DEFAULT 0,
    ADD COLUMN metadata_only boolean NOT NULL DEFAULT false;

ALTER TABLE folder_manifest_entries
    ADD COLUMN size bigint NOT NULL DEFAULT 0,
    ADD COLUMN sha256 text,
    ADD COLUMN mime_type text;

CREATE TABLE folder_manifest_selections (
    transfer_id uuid NOT NULL REFERENCES folder_manifests(transfer_id) ON DELETE CASCADE,
    target_device_id uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    entry_index integer NOT NULL,
    accepted boolean NOT NULL,
    conflict_action text NOT NULL DEFAULT 'ASK',
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (transfer_id, target_device_id, entry_index),
    FOREIGN KEY (transfer_id, entry_index)
        REFERENCES folder_manifest_entries(transfer_id, entry_index)
        ON DELETE CASCADE
);

ALTER TABLE messages
    ADD COLUMN pinned boolean NOT NULL DEFAULT false,
    ADD COLUMN reply_to_message_id uuid REFERENCES messages(id) ON DELETE SET NULL;

ALTER TABLE message_read_cursors
    DROP CONSTRAINT message_read_cursors_pkey;

ALTER TABLE message_read_cursors
    ALTER COLUMN group_id DROP NOT NULL,
    ADD COLUMN conversation_key text NOT NULL DEFAULT 'inbox';

ALTER TABLE message_read_cursors
    ADD PRIMARY KEY (device_id, conversation_key);

ALTER TABLE message_tombstones
    ALTER COLUMN actor_device_id DROP NOT NULL;

ALTER TABLE message_retention_policies
    DROP CONSTRAINT message_retention_policies_pkey;

ALTER TABLE message_retention_policies
    ALTER COLUMN group_id DROP NOT NULL,
    ADD COLUMN conversation_key text NOT NULL DEFAULT 'inbox';

ALTER TABLE message_retention_policies
    ADD PRIMARY KEY (conversation_key);

CREATE TABLE message_local_removals (
    message_id uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    device_id uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    removed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (message_id, device_id)
);

CREATE TABLE retention_cleanup_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    scanned integer NOT NULL DEFAULT 0,
    bodies_deleted integer NOT NULL DEFAULT 0,
    tombstones_created integer NOT NULL DEFAULT 0,
    started_at timestamptz NOT NULL,
    completed_at timestamptz,
    error_code text
);

CREATE INDEX device_route_history_rank_idx
    ON device_route_history(device_id, failure_count, handshake_latency_ms, updated_at DESC);

CREATE INDEX transfer_delivery_policy_state_idx
    ON transfer_delivery_policies(target_device_id, state, priority DESC, updated_at);

CREATE INDEX folder_manifest_selection_target_idx
    ON folder_manifest_selections(target_device_id, transfer_id);

CREATE INDEX message_tombstones_expiry_idx
    ON message_tombstones(expires_at);

CREATE INDEX message_local_removals_device_idx
    ON message_local_removals(device_id, removed_at DESC);

CREATE INDEX messages_reply_idx
    ON messages(reply_to_message_id)
    WHERE reply_to_message_id IS NOT NULL;
