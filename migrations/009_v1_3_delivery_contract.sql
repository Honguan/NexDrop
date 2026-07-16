ALTER TABLE transfer_tasks ADD COLUMN IF NOT EXISTS idempotency_key uuid;
ALTER TABLE transfer_tasks ADD COLUMN IF NOT EXISTS request_fingerprint bytea;

CREATE UNIQUE INDEX IF NOT EXISTS transfer_tasks_sender_idempotency_idx
    ON transfer_tasks(sender_device_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE TABLE IF NOT EXISTS transfer_executions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id uuid NOT NULL REFERENCES transfer_tasks(id) ON DELETE CASCADE,
    target_device_id uuid NOT NULL REFERENCES devices(id),
    attempt integer NOT NULL CHECK (attempt > 0),
    status text NOT NULL,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    error_code text,
	idempotency_key uuid,
    UNIQUE (transfer_id, target_device_id, attempt)
);

CREATE UNIQUE INDEX IF NOT EXISTS transfer_executions_idempotency_idx
    ON transfer_executions(transfer_id, target_device_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE TABLE IF NOT EXISTS idempotency_records (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    actor_device_id uuid NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    method text NOT NULL,
    resource text NOT NULL,
    idempotency_key uuid NOT NULL,
    request_hash bytea NOT NULL,
    state text NOT NULL CHECK (state IN ('PROCESSING', 'COMPLETED')),
    response_status integer,
    response_body jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    UNIQUE (actor_device_id, method, resource, idempotency_key)
);
