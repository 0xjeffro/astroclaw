-- General key-value settings table.
-- Stores system-wide configuration (user profile, default agent ID, etc.).

CREATE TABLE app_settings_kv (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL UNIQUE,
    value      TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
