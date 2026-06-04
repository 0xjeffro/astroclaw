-- Settings are split into three scopes:
--   - System: deploy-wide config shared by every workspace
--     (e.g. feature flags, global default model, registry endpoints)
--   - Workspace: per-workspace config visible to all members
--     (e.g. default agent ID for that workspace, timezone, budget)
--   - User: per-user preferences private to that user
--     (e.g. user_profile, UI theme, locale)
--
-- A name (e.g. "default_agent_id") lives in exactly one scope. The service
-- layer can expose a cascading resolver (user → workspace → system) for
-- callers that want a single effective value, but the storage is segregated.

CREATE TABLE app_settings_system (
    name       TEXT PRIMARY KEY,
    value      TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE app_settings_workspace (
    workspace_id UUID NOT NULL,
    name         TEXT NOT NULL,
    value        TEXT NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, name)
);

CREATE TABLE app_settings_user (
    user_id    UUID NOT NULL,
    name       TEXT NOT NULL,
    value      TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, name)
);
