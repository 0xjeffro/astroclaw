-- name: CreateAgentProfile :one
INSERT INTO app_agents_profiles (name, soul, model)
VALUES ($1, $2, $3)
RETURNING *;

-- name: AttachAgentToWorkspace :exec
INSERT INTO app_agents_workspaces (agent_id, workspace_id)
VALUES ($1, $2);

-- name: DetachAgentFromWorkspace :exec
DELETE FROM app_agents_workspaces
WHERE agent_id = $1 AND workspace_id = $2;

-- name: GetAgentFromWorkspace :one
-- Returns the agent only if it is attached to the given workspace.
SELECT p.* FROM app_agents_profiles p
JOIN app_agents_workspaces aw ON aw.agent_id = p.id
WHERE p.id = $1 AND aw.workspace_id = $2 AND p.deleted_at IS NULL;

-- name: ListAgentsByWorkspace :many
SELECT p.* FROM app_agents_profiles p
JOIN app_agents_workspaces aw ON aw.agent_id = p.id
WHERE aw.workspace_id = $1 AND p.deleted_at IS NULL
ORDER BY p.created_at;

-- name: ListWorkspacesForAgent :many
SELECT workspace_id FROM app_agents_workspaces
WHERE agent_id = $1
ORDER BY attached_at;

-- name: UpdateAgent :exec
UPDATE app_agents_profiles
SET name = $1, soul = $2, model = $3, updated_at = now()
WHERE id = $4 AND deleted_at IS NULL;

-- name: SoftDeleteAgent :exec
UPDATE app_agents_profiles
SET deleted_at = now()
WHERE id = $1 AND deleted_at IS NULL;
