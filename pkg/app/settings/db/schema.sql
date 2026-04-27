-- Stores system prompt sections (SOUL, USER, etc.) as key-value pairs.
-- Each name maps to one prompt fragment that gets injected into the
-- system prompt at assembly time.

CREATE TABLE settings_prompt (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL UNIQUE,
    value      TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
