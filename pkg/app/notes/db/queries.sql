-- name: CreateMemory :one
INSERT INTO app_notes_memories (agent_id, content, session_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetMemory :one
SELECT * FROM app_notes_memories WHERE id = $1;

-- name: ListMemories :many
SELECT * FROM app_notes_memories WHERE agent_id = $1 ORDER BY created_at DESC;

-- name: ListRecentMemories :many
SELECT * FROM app_notes_memories WHERE agent_id = $1 ORDER BY created_at DESC LIMIT $2;

-- name: AddMemorySource :exec
INSERT INTO app_notes_memory_sources (memory_id, message_id)
VALUES ($1, $2);

-- name: GetMemorySources :many
SELECT message_id FROM app_notes_memory_sources WHERE memory_id = $1;
