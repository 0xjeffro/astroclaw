-- name: GetKVSetting :one
SELECT * FROM app_settings_kv WHERE name = $1;

-- name: CreateKVSetting :one
INSERT INTO app_settings_kv (name, value)
VALUES ($1, $2)
RETURNING *;

-- name: UpdateKVSetting :exec
UPDATE app_settings_kv SET value = $1, updated_at = now()
WHERE name = $2;
