-- name: CreateCredential :one
INSERT INTO credentials (name, value, description)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetCredentialByName :one
SELECT * FROM credentials WHERE name = $1 AND deleted_at IS NULL;

-- name: UpdateCredential :exec
UPDATE credentials
SET value = $1, description = $2, updated_at = now()
WHERE name = $3 AND deleted_at IS NULL;

-- name: SoftDeleteCredential :exec
UPDATE credentials SET deleted_at = now() WHERE name = $1 AND deleted_at IS NULL;
