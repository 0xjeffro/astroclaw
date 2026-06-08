-- Passwords app schema.
-- Stores credentials (API tokens, secrets) encrypted with envelope encryption.
-- Each scope (system, workspace, user) has its own data key
-- wrapped by the KMS CMK, plus a credentials table whose value column
-- holds AES-256-GCM ciphertext encrypted with that scope's data key.
--
-- Why three scopes:
--   - system: deploy-wide secrets like the platform anthropic-api-key,
--     readable only by admin and infra Lambdas.
--   - workspace: credentials shared across workspace members, like a
--     team's GitHub bot token.
--   - user: per-user private credentials, like a personal API key (BYOK).

-- ---------------------------------------------------------------------
-- System scope (singleton: one data key for the whole deployment)
-- ---------------------------------------------------------------------

CREATE TABLE app_passwords_system_data_key (
    name               TEXT PRIMARY KEY DEFAULT 'system',
    encrypted_data_key BYTEA NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE app_passwords_system_credentials (
    name        TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    nonce       BYTEA NOT NULL,
    ciphertext  BYTEA NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------
-- Workspace scope (one data key per workspace)
-- ---------------------------------------------------------------------

CREATE TABLE app_passwords_workspace_data_keys (
    workspace_id       UUID PRIMARY KEY,
    encrypted_data_key BYTEA NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE app_passwords_workspace_credentials (
    workspace_id UUID NOT NULL,
    name         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    nonce        BYTEA NOT NULL,
    ciphertext   BYTEA NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, name)
);

-- ---------------------------------------------------------------------
-- User scope (one data key per user)
-- ---------------------------------------------------------------------

CREATE TABLE app_passwords_user_data_keys (
    user_id            UUID PRIMARY KEY,
    encrypted_data_key BYTEA NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE app_passwords_user_credentials (
    user_id     UUID NOT NULL,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    nonce       BYTEA NOT NULL,
    ciphertext  BYTEA NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, name)
);
