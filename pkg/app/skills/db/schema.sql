-- Skills installed in a workspace. The skill content (SKILL.md + supporting
-- files) lives in S3 at skills/{author}/{name}/{version}/..., shared across all workspaces.
--
-- This table stores a per-workspace snapshot of the metadata so the agent
-- can build the system prompt without calling the S3.
--
-- A workspace installs at most one version of a given (author, name) at a
-- time. Switching version is an UPDATE on the version column.

CREATE TABLE app_skills (
    workspace_id UUID NOT NULL,
    author       TEXT NOT NULL,
    name         TEXT NOT NULL,
    version      TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    when_to_use  TEXT NOT NULL DEFAULT '',
    tags         TEXT NOT NULL DEFAULT '',
    installed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, author, name)
);
