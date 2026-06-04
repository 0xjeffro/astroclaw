-- System scope (deploy-wide)

-- name: GetSystemSetting :one
SELECT * FROM app_settings_system WHERE name = $1;

-- name: UpsertSystemSetting :exec
INSERT INTO app_settings_system (name, value)
VALUES ($1, $2)
ON CONFLICT (name) DO UPDATE
SET value = EXCLUDED.value, updated_at = now();

-- Workspace scope

-- name: GetWorkspaceSetting :one
SELECT * FROM app_settings_workspace
WHERE workspace_id = $1 AND name = $2;

-- name: UpsertWorkspaceSetting :exec
INSERT INTO app_settings_workspace (workspace_id, name, value)
VALUES ($1, $2, $3)
ON CONFLICT (workspace_id, name) DO UPDATE
SET value = EXCLUDED.value, updated_at = now();

-- User scope

-- name: GetUserSetting :one
SELECT * FROM app_settings_user
WHERE user_id = $1 AND name = $2;

-- name: UpsertUserSetting :exec
INSERT INTO app_settings_user (user_id, name, value)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, name) DO UPDATE
SET value = EXCLUDED.value, updated_at = now();
