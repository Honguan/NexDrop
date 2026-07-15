ALTER TABLE transfer_tasks ADD COLUMN IF NOT EXISTS client_batch_id uuid;
CREATE INDEX IF NOT EXISTS idx_transfer_tasks_client_batch_id ON transfer_tasks(client_batch_id);
