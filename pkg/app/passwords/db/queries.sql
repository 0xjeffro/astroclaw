-- =====================================================================
-- System scope
-- =====================================================================

-- name: GetSystemDataKey :one
SELECT * FROM app_passwords_system_data_key WHERE name = 'system';

-- name: UpsertSystemDataKey :exec
INSERT INTO app_passwords_system_data_key (encrypted_data_key)
VALUES ($1)
ON CONFLICT (name) DO UPDATE
SET encrypted_data_key = EXCLUDED.encrypted_data_key;

-- name: GetSystemCredential :one
SELECT * FROM app_passwords_system_credentials WHERE name = $1;

-- name: UpsertSystemCredential :exec
INSERT INTO app_passwords_system_credentials (name, description, nonce, ciphertext)
VALUES ($1, $2, $3, $4)
ON CONFLICT (name) DO UPDATE
SET description = EXCLUDED.description,
    nonce       = EXCLUDED.nonce,
    ciphertext  = EXCLUDED.ciphertext,
    updated_at  = now();

-- name: DeleteSystemCredential :exec
DELETE FROM app_passwords_system_credentials WHERE name = $1;

-- name: ListSystemCredentials :many
-- Metadata only, no nonce/ciphertext. Use GetSystemCredential to read one value.
SELECT name, description, created_at, updated_at
FROM app_passwords_system_credentials
ORDER BY name;

-- =====================================================================
-- Workspace scope
-- =====================================================================

-- name: GetWorkspaceDataKey :one
SELECT * FROM app_passwords_workspace_data_keys WHERE workspace_id = $1;

-- name: UpsertWorkspaceDataKey :exec
INSERT INTO app_passwords_workspace_data_keys (workspace_id, encrypted_data_key)
VALUES ($1, $2)
ON CONFLICT (workspace_id) DO UPDATE
SET encrypted_data_key = EXCLUDED.encrypted_data_key;

-- name: GetWorkspaceCredential :one
SELECT * FROM app_passwords_workspace_credentials
WHERE workspace_id = $1 AND name = $2;

-- name: UpsertWorkspaceCredential :exec
INSERT INTO app_passwords_workspace_credentials (workspace_id, name, description, nonce, ciphertext)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, name) DO UPDATE
SET description = EXCLUDED.description,
    nonce       = EXCLUDED.nonce,
    ciphertext  = EXCLUDED.ciphertext,
    updated_at  = now();

-- name: DeleteWorkspaceCredential :exec
DELETE FROM app_passwords_workspace_credentials
WHERE workspace_id = $1 AND name = $2;

-- name: ListWorkspaceCredentials :many
SELECT workspace_id, name, description, created_at, updated_at
FROM app_passwords_workspace_credentials
WHERE workspace_id = $1
ORDER BY name;

-- =====================================================================
-- User scope
-- =====================================================================

-- name: GetUserDataKey :one
SELECT * FROM app_passwords_user_data_keys WHERE user_id = $1;

-- name: UpsertUserDataKey :exec
INSERT INTO app_passwords_user_data_keys (user_id, encrypted_data_key)
VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE
SET encrypted_data_key = EXCLUDED.encrypted_data_key;

-- name: GetUserCredential :one
SELECT * FROM app_passwords_user_credentials
WHERE user_id = $1 AND name = $2;

-- name: UpsertUserCredential :exec
INSERT INTO app_passwords_user_credentials (user_id, name, description, nonce, ciphertext)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, name) DO UPDATE
SET description = EXCLUDED.description,
    nonce       = EXCLUDED.nonce,
    ciphertext  = EXCLUDED.ciphertext,
    updated_at  = now();

-- name: DeleteUserCredential :exec
DELETE FROM app_passwords_user_credentials
WHERE user_id = $1 AND name = $2;

-- name: ListUserCredentials :many
SELECT user_id, name, description, created_at, updated_at
FROM app_passwords_user_credentials
WHERE user_id = $1
ORDER BY name;
