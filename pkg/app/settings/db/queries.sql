-- name: GetPromptSetting :one
SELECT * FROM app_settings_prompt WHERE name = $1;

-- name: CreatePromptSetting :one
INSERT INTO app_settings_prompt (name, value)
VALUES ($1, $2)
RETURNING *;

-- name: UpdatePromptSetting :exec
UPDATE app_settings_prompt SET value = $1, updated_at = now()
WHERE name = $2;
