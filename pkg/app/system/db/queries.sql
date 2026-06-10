-- Users

-- name: CreateUser :one
INSERT INTO app_system_users (email, name, role)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetUser :one
SELECT * FROM app_system_users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM app_system_users WHERE email = $1;

-- name: GetAdmin :one
SELECT * FROM app_system_users WHERE role = 'admin' LIMIT 1;

-- name: ListUsers :many
SELECT * FROM app_system_users ORDER BY created_at;

-- Workspaces

-- name: CreateWorkspace :one
INSERT INTO app_system_workspaces (name)
VALUES ($1)
RETURNING *;

-- name: GetWorkspace :one
SELECT * FROM app_system_workspaces
WHERE id = $1 AND deleted_at IS NULL;

-- name: ListWorkspaces :many
SELECT * FROM app_system_workspaces
WHERE deleted_at IS NULL
ORDER BY created_at;

-- name: ListWorkspacesForUser :many
SELECT w.* FROM app_system_workspaces w
JOIN app_system_workspace_members m ON m.workspace_id = w.id
WHERE m.user_id = $1 AND w.deleted_at IS NULL
ORDER BY w.created_at;

-- name: UpdateWorkspaceName :exec
UPDATE app_system_workspaces
SET name = $1, updated_at = now()
WHERE id = $2;

-- name: SoftDeleteWorkspace :exec
UPDATE app_system_workspaces
SET deleted_at = now(), updated_at = now()
WHERE id = $1;

-- Workspace Members

-- name: AddMembership :exec
INSERT INTO app_system_workspace_members (user_id, workspace_id, role)
VALUES ($1, $2, $3);

-- name: RemoveMembership :exec
DELETE FROM app_system_workspace_members
WHERE user_id = $1 AND workspace_id = $2;

-- name: GetMembership :one
SELECT * FROM app_system_workspace_members
WHERE user_id = $1 AND workspace_id = $2;

-- name: ListMembersByWorkspace :many
SELECT u.*, m.role AS workspace_role, m.joined_at
FROM app_system_users u
JOIN app_system_workspace_members m ON m.user_id = u.id
WHERE m.workspace_id = $1
ORDER BY m.joined_at;

-- name: UpdateMembershipRole :exec
UPDATE app_system_workspace_members
SET role = $1
WHERE user_id = $2 AND workspace_id = $3;

-- User Passwords

-- name: GetUserPasswordHash :one
SELECT password_hash FROM app_system_user_passwords WHERE user_id = $1;

-- name: UpsertUserPasswordHash :exec
INSERT INTO app_system_user_passwords (user_id, password_hash)
VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE
SET password_hash = EXCLUDED.password_hash, updated_at = now();

-- name: DeleteUserPassword :exec
DELETE FROM app_system_user_passwords WHERE user_id = $1;

-- Connections

-- name: CreateConnection :exec
INSERT INTO app_system_connections (connection_id, user_id, workspace_id)
VALUES ($1, $2, $3);

-- name: DeleteConnection :exec
DELETE FROM app_system_connections WHERE connection_id = $1;

-- name: GetConnectionsByUser :many
SELECT * FROM app_system_connections WHERE user_id = $1;
