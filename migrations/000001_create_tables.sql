CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT UNIQUE NOT NULL,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID NOT NULL,
    title            TEXT NOT NULL DEFAULT '',
    model            TEXT NOT NULL DEFAULT '',
    system_prompt    TEXT NOT NULL DEFAULT '',
    context_window   INT NOT NULL DEFAULT 0,
    context_messages JSONB NOT NULL DEFAULT '[]',
    context_summary  TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

CREATE TABLE session_members (
    session_id UUID NOT NULL,
    user_id    UUID NOT NULL,
    role       TEXT NOT NULL DEFAULT 'guest',
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, user_id)
);

CREATE TABLE messages (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id         UUID NOT NULL,
    role               TEXT NOT NULL,
    content            TEXT NOT NULL DEFAULT '',
    tool_calls         JSONB,
    tool_call_id       TEXT NOT NULL DEFAULT '',
    sequence_number    INT NOT NULL,
    forwarded_from     UUID,
    reply_to           UUID,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_messages_session_seq ON messages(session_id, sequence_number);
