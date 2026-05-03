-- name: CreateAgent :one
INSERT INTO app_agents_profiles (name, soul, model)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetAgent :one
SELECT * FROM app_agents_profiles WHERE id = $1 AND deleted_at IS NULL;

-- name: ListAgents :many
SELECT * FROM app_agents_profiles WHERE deleted_at IS NULL ORDER BY created_at;

-- name: UpdateAgent :exec
UPDATE app_agents_profiles
SET name = $1, soul = $2, model = $3, updated_at = now()
WHERE id = $4 AND deleted_at IS NULL;

-- name: SoftDeleteAgent :exec
UPDATE app_agents_profiles SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL;
