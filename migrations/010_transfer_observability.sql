CREATE TABLE IF NOT EXISTS transfer_timeline_events (
    id bigserial PRIMARY KEY,
    transfer_id uuid NOT NULL REFERENCES transfer_tasks(id) ON DELETE CASCADE,
    request_id text,
    file_id uuid REFERENCES files(id) ON DELETE SET NULL,
    target_device_id uuid REFERENCES devices(id) ON DELETE SET NULL,
    execution_id uuid REFERENCES transfer_executions(id) ON DELETE SET NULL,
    event_code text NOT NULL CHECK (event_code ~ '^[A-Z][A-Z0-9_]{2,63}$'),
    route text,
    status text,
    error_code text,
    duration_ms bigint CHECK (duration_ms IS NULL OR duration_ms >= 0),
    occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS transfer_timeline_events_transfer_order_idx
    ON transfer_timeline_events(transfer_id, occurred_at, id);

CREATE INDEX IF NOT EXISTS transfer_timeline_events_code_time_idx
    ON transfer_timeline_events(event_code, occurred_at);

COMMENT ON TABLE transfer_timeline_events IS
    'Content-free, append-only operational events. Client reports are idempotent through idempotency_records.';
