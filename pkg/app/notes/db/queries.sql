-- name: CreateMemory :one
INSERT INTO memories (content, session_id)
VALUES ($1, $2)
RETURNING *;

-- name: GetMemory :one
SELECT * FROM memories WHERE id = $1;

-- name: ListMemories :many
SELECT * FROM memories ORDER BY created_at DESC;

-- name: ListRecentMemories :many
SELECT * FROM memories ORDER BY created_at DESC LIMIT $1;

-- name: AddMemorySource :exec
INSERT INTO memory_sources (memory_id, message_id)
VALUES ($1, $2);

-- name: GetMemorySources :many
SELECT message_id FROM memory_sources WHERE memory_id = $1;
