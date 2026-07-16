ALTER TABLE node_settings
    ADD COLUMN IF NOT EXISTS public_registration_enabled boolean NOT NULL DEFAULT false;
