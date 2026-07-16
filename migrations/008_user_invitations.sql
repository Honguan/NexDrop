CREATE TABLE user_invitations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    invited_by uuid NOT NULL REFERENCES users(id),
    username text NOT NULL,
    email text NOT NULL,
    is_admin boolean NOT NULL DEFAULT false,
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX user_invitations_active_username_key
    ON user_invitations (lower(username)) WHERE accepted_at IS NULL;
CREATE UNIQUE INDEX user_invitations_active_email_key
    ON user_invitations (lower(email)) WHERE accepted_at IS NULL;
