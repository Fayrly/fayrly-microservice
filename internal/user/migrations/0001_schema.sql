CREATE TABLE users (
    id           uuid PRIMARY KEY,
    email        text NOT NULL,
    name         text NOT NULL,
    phone        text NOT NULL DEFAULT '',
    avatar_url   text NOT NULL DEFAULT '',
    status       text NOT NULL DEFAULT 'active'
                 CHECK (status IN ('active', 'inactive', 'suspended')),
    last_seen_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    deleted_at   timestamptz
);

CREATE UNIQUE INDEX users_email_key ON users (lower(email)) WHERE deleted_at IS NULL;
CREATE INDEX users_status_idx ON users (status) WHERE deleted_at IS NULL;
CREATE INDEX users_created_at_idx ON users (created_at DESC);

CREATE TABLE roles (
    id          uuid PRIMARY KEY,
    key         text NOT NULL UNIQUE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    is_system   boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE permissions (
    id          uuid PRIMARY KEY,
    key         text NOT NULL UNIQUE,
    resource    text NOT NULL,
    action      text NOT NULL,
    description text NOT NULL DEFAULT ''
);

CREATE TABLE role_permissions (
    role_id       uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id uuid NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE user_roles (
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id     uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    assigned_by uuid,
    assigned_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role_id)
);

CREATE INDEX user_roles_role_idx ON user_roles (role_id);
