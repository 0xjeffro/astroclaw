-- name: CreateCredential :one
INSERT INTO app_passwords_credentials (name, value, description)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetCredentialByName :one
SELECT * FROM app_passwords_credentials WHERE name = $1 AND deleted_at IS NULL;

-- name: UpdateCredential :exec
UPDATE app_passwords_credentials
SET value = $1, description = $2, updated_at = now()
WHERE name = $3 AND deleted_at IS NULL;

-- name: SoftDeleteCredential :exec
UPDATE app_passwords_credentials SET deleted_at = now() WHERE name = $1 AND deleted_at IS NULL;
