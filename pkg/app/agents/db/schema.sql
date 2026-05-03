-- Agent profiles. Each agent has its own personality (soul), model.
-- The first agent created during deployment is the default agent.

CREATE TABLE app_agents_profiles (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    soul       TEXT NOT NULL DEFAULT '',
    model      TEXT NOT NULL DEFAULT 'claude-sonnet-4-20250514',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);
