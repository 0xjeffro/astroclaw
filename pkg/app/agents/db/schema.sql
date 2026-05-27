-- Agent profiles. Each agent has its own personality (soul), model.
-- Agents are attached to one or more workspaces via app_agents_workspaces.

CREATE TABLE app_agents_profiles (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    soul       TEXT NOT NULL DEFAULT '',
    model      TEXT NOT NULL DEFAULT 'claude-sonnet-4-20250514',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

-- Many-to-many: which agents are available in which workspaces.
CREATE TABLE app_agents_workspaces (
    agent_id     UUID NOT NULL,
    workspace_id UUID NOT NULL,
    attached_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, workspace_id)
);
