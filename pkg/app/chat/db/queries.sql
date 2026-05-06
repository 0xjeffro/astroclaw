-- name: CreateSession :one
INSERT INTO app_chat_sessions (title, user_id, model, system_prompt, context_window, context_messages, context_summary)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetSession :one
SELECT * FROM app_chat_sessions WHERE id = $1 AND deleted_at IS NULL;

-- name: UpdateSession :exec
UPDATE app_chat_sessions
SET title = $1, model = $2, system_prompt = $3, context_window = $4,
    context_messages = $5, context_summary = $6, updated_at = now()
WHERE id = $7 AND deleted_at IS NULL;

-- name: ListSessions :many
SELECT * FROM app_chat_sessions WHERE deleted_at IS NULL ORDER BY created_at DESC;

-- name: ListSessionsByUser :many
SELECT * FROM app_chat_sessions WHERE user_id = $1 AND deleted_at IS NULL ORDER BY created_at DESC;

-- name: AddSessionAgent :exec
INSERT INTO app_chat_session_agents (session_id, agent_id)
VALUES ($1, $2);

-- name: ListSessionAgents :many
SELECT * FROM app_chat_session_agents WHERE session_id = $1;

-- name: SoftDeleteSession :exec
UPDATE app_chat_sessions SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL;

-- name: CreateMessage :one
INSERT INTO app_chat_messages (session_id, role, content, tool_calls, tool_call_id, sequence_number, forwarded_from, reply_to)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListMessages :many
SELECT * FROM app_chat_messages WHERE session_id = $1 ORDER BY sequence_number;


-- name: AddSessionMember :exec
INSERT INTO app_chat_session_members (session_id, user_id, role)
VALUES ($1, $2, $3);

-- name: ListSessionMembers :many
SELECT * FROM app_chat_session_members WHERE session_id = $1;

-- name: DeleteSessionMember :exec
DELETE FROM app_chat_session_members WHERE session_id = $1 AND user_id = $2;