-- System-level tables for user identity and WebSocket connections.

-- Users of the system. The first user created during deployment is the owner.
-- role: "admin" (the person who deployed this system) or "user" (for invite users in the future).
CREATE TABLE app_system_users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT UNIQUE NOT NULL,
    name       TEXT NOT NULL,
    role       TEXT NOT NULL DEFAULT 'user',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Workspaces.
CREATE TABLE app_system_workspaces (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);

-- Workspace Member: which users belong to which workspace and with what role.
-- role: "owner" ｜ "member"
CREATE TABLE app_system_workspace_members (
    workspace_id UUID NOT NULL,
    user_id      UUID NOT NULL,
    role         TEXT NOT NULL DEFAULT 'member',
    joined_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, workspace_id)
);

-- Password credential for users that authenticate via password. A user with
-- no row here has no password set (e.g. they only log in via OAuth or API
-- token in the future). password_hash stores the full PHC-encoded Argon2id
-- string (salt and parameters included), produced by crypto.HashPassword.
CREATE TABLE app_system_user_passwords (
    user_id        UUID PRIMARY KEY,
    password_hash  TEXT NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Active WebSocket connections. Written by connect Lambda, deleted by disconnect Lambda.
CREATE TABLE app_system_connections (
    connection_id TEXT PRIMARY KEY,
    user_id       UUID NOT NULL,
    workspace_id  UUID NOT NULL,
    connected_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
