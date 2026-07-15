ALTER TABLE transfer_tasks ADD COLUMN IF NOT EXISTS group_deleted_at timestamptz;
