CREATE TABLE password_reset_codes (
    id            uuid PRIMARY KEY,
    credential_id uuid NOT NULL REFERENCES credentials(id) ON DELETE CASCADE,
    code_hash     bytea NOT NULL,
    salt          bytea NOT NULL,
    expires_at    timestamptz NOT NULL,
    consumed_at   timestamptz,
    attempts      integer NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX password_reset_codes_credential_idx
    ON password_reset_codes (credential_id, created_at DESC);

CREATE INDEX password_reset_codes_expires_idx ON password_reset_codes (expires_at);
