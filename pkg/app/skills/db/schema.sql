-- Skill metadata. The actual skill content (SKILL.md + supporting files) lives in S3.
-- This table stores the index for discovery and loading.

CREATE TABLE app_skills (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    author      TEXT NOT NULL DEFAULT '',        -- skill author, used with name for uniqueness
    name        TEXT NOT NULL,                   -- skill identifier, unique within the same author
    description TEXT NOT NULL,                   -- brief summary, shown in system prompt index
    when_to_use TEXT NOT NULL DEFAULT '',        -- helps agent decide when to load this skill,
                                                 -- more specifically than description
    tags        TEXT NOT NULL DEFAULT '',        -- comma-separated tags for filtering
    version     TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (author, name)
);
