-- General key-value settings table.
-- Stores system-wide configuration (user profile, default agent ID, etc.).
--
-- TODO: redesign for multi-tenant. Settings have three scopes:
--   - system level: deploy-wide config (e.g., feature flags)
--   - workspace level: per-workspace config (e.g., default_agent_id)
--   - user level: per-user config (e.g., user_profile, UI prefs)
-- Plan: split into app_settings_system / app_settings_workspace /
-- app_settings_user, each keyed by its own scope ID + name.

CREATE TABLE app_settings_kv (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL UNIQUE,
    value      TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
