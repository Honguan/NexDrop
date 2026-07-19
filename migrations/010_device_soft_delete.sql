ALTER TABLE devices ADD COLUMN IF NOT EXISTS deleted_at timestamptz;
UPDATE devices SET trust_status='TRUSTED' WHERE trust_status='PENDING' AND deleted_at IS NULL;
DELETE FROM device_pairing_codes;
CREATE INDEX IF NOT EXISTS devices_active_user_created_idx ON devices(user_id, created_at DESC) WHERE deleted_at IS NULL;
