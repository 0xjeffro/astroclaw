-- Users

-- name: CreateUser :one
INSERT INTO app_system_users (email, name, role)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetUser :one
SELECT * FROM app_system_users WHERE id = $1;

-- name: GetOwner :one
SELECT * FROM app_system_users WHERE role = 'owner' LIMIT 1;

-- name: ListUsers :many
SELECT * FROM app_system_users ORDER BY created_at;

-- Connections

-- name: CreateConnection :exec
INSERT INTO app_system_connections (connection_id, user_id)
VALUES ($1, $2);

-- name: DeleteConnection :exec
DELETE FROM app_system_connections WHERE connection_id = $1;

-- name: GetConnectionsByUser :many
SELECT * FROM app_system_connections WHERE user_id = $1;
