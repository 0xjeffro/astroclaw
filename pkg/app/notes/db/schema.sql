-- Notes App schema.
-- Stores agent-accumulated knowledge (memories) from conversations.
-- Each memory is a single fact or observation that the agent decided
-- was worth persisting via the memory_save tool.
--
-- A memory is produced by an agent during a conversation with a specific
-- user. Looking ahead to group chat (one agent participating in multiple
-- users' conversations), memories are isolated by user: agent X's memory
-- about user Y is only injected into prompts when Y is talking with X.

CREATE TABLE app_notes_memories (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id   UUID NOT NULL,
    user_id    UUID NOT NULL,
    content    TEXT NOT NULL,
    session_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_app_notes_memories_agent_user ON app_notes_memories(agent_id, user_id);

-- Tracks which messages a memory was derived from.
-- One memory can come from multiple messages.
CREATE TABLE app_notes_memory_sources (
    memory_id  UUID NOT NULL,
    message_id UUID NOT NULL,
    PRIMARY KEY (memory_id, message_id)
);
