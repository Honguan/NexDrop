CREATE TABLE IF NOT EXISTS transfer_hidden_users (
    transfer_id uuid NOT NULL REFERENCES transfer_tasks(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    hidden_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (transfer_id, user_id)
);
