CREATE TABLE credentials (
    id            uuid PRIMARY KEY,
    email         text NOT NULL,
    password_hash text,
    firebase_uid  text,
    provider      text NOT NULL DEFAULT 'password'
                  CHECK (provider IN ('password', 'firebase', 'linked')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX credentials_email_key ON credentials (lower(email));
CREATE UNIQUE INDEX credentials_firebase_uid_key ON credentials (firebase_uid)
    WHERE firebase_uid IS NOT NULL;

-- One row per issued refresh token. Tokens are opaque random strings; only the
-- SHA-256 digest is stored. Rotation keeps a session-family id so that a replayed
-- token can revoke the whole family.
CREATE TABLE refresh_tokens (
    id          uuid PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES credentials(id) ON DELETE CASCADE,
    family_id   uuid NOT NULL,
    token_hash  bytea NOT NULL UNIQUE,
    user_agent  text NOT NULL DEFAULT '',
    ip          text NOT NULL DEFAULT '',
    expires_at  timestamptz NOT NULL,
    revoked_at  timestamptz,
    replaced_by uuid,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX refresh_tokens_user_idx ON refresh_tokens (user_id);
CREATE INDEX refresh_tokens_family_idx ON refresh_tokens (family_id);
CREATE INDEX refresh_tokens_expires_idx ON refresh_tokens (expires_at);
