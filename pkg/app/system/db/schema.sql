-- System-level tables for user identity and WebSocket connections.

-- Users of the system. The first user created during deployment is the owner.
-- role: "owner" (the person who deployed this system) or "guest" (for invite users in the future).
CREATE TABLE app_system_users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT UNIQUE NOT NULL,
    name       TEXT NOT NULL,
    role       TEXT NOT NULL DEFAULT 'guest',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Active WebSocket connections. Written by connect Lambda, deleted by disconnect Lambda.
CREATE TABLE app_system_connections (
    connection_id TEXT PRIMARY KEY,
    user_id       UUID NOT NULL,
    connected_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
