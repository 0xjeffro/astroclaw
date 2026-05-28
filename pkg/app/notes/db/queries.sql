-- name: CreateMemoryForUser :one
INSERT INTO app_notes_memories (agent_id, user_id, content, session_id)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetMemory :one
SELECT * FROM app_notes_memories WHERE id = $1;

-- name: ListMemoriesByAgentAndUser :many
SELECT * FROM app_notes_memories
WHERE agent_id = $1 AND user_id = $2
ORDER BY created_at DESC;

-- name: ListRecentMemoriesByAgentAndUser :many
SELECT * FROM app_notes_memories
WHERE agent_id = $1 AND user_id = $2
ORDER BY created_at DESC
LIMIT $3;

-- name: AddMemorySource :exec
INSERT INTO app_notes_memory_sources (memory_id, message_id)
VALUES ($1, $2);

-- name: GetMemorySources :many
SELECT message_id FROM app_notes_memory_sources WHERE memory_id = $1;
