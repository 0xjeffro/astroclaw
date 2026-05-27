-- This schema is designed for Aurora DSQL compatibility.
-- DSQL offers true serverless pricing (pay-per-request, scale to zero)
-- and zero cold-start latency, but does not support:
--   - JSONB column types (use TEXT, cast to JSONB at query time)
--   - Extensions (pgcrypto, pgvector, etc.; gen_random_uuid() is built-in)
--   - Foreign key constraints
--   - Triggers
--   - PL/pgSQL (SQL-language functions only)
--   - Multiple DDL statements per transaction

-- A session belongs to exactly one workspace. All session members must be
-- members of that workspace, and all session agents must be attached to it.
-- TODO: enforce these invariants in the service layer.
CREATE TABLE app_chat_sessions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL,
    user_id          UUID NOT NULL,
    title            TEXT NOT NULL DEFAULT '',
    model            TEXT NOT NULL DEFAULT '',
    system_prompt    TEXT NOT NULL DEFAULT '',
    context_window   INT NOT NULL DEFAULT 0,
    context_messages TEXT NOT NULL DEFAULT '[]',
    context_summary  TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

CREATE INDEX idx_app_chat_sessions_workspace ON app_chat_sessions(workspace_id);

CREATE TABLE app_chat_session_members (
    session_id UUID NOT NULL,
    user_id    UUID NOT NULL,
    role       TEXT NOT NULL DEFAULT 'guest',
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, user_id)
);

-- Tracks which agents participate in a session.
-- One session can have multiple agents.
CREATE TABLE app_chat_session_agents (
    session_id UUID NOT NULL,
    agent_id   UUID NOT NULL,
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, agent_id)
);

CREATE TABLE app_chat_messages (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id         UUID NOT NULL,
    role               TEXT NOT NULL,
    content            TEXT NOT NULL DEFAULT '',
    tool_calls         TEXT,
    tool_call_id       TEXT NOT NULL DEFAULT '',
    sequence_number    INT NOT NULL,
    forwarded_from     UUID,
    reply_to           UUID,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_app_chat_messages_session_seq ON app_chat_messages(session_id, sequence_number);
