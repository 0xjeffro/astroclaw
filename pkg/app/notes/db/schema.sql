-- Notes App schema.
-- Stores agent-accumulated knowledge (memories) from conversations.
-- Each memory is a single fact or observation that the agent decided
-- was worth persisting via the memory_save tool.

CREATE TABLE memories (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    content    TEXT NOT NULL,
    session_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Tracks which messages a memory was derived from.
-- One memory can come from multiple messages.
CREATE TABLE memory_sources (
    memory_id  UUID NOT NULL,
    message_id UUID NOT NULL,
    PRIMARY KEY (memory_id, message_id)
);
