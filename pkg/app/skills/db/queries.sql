-- name: InstallSkill :one
INSERT INTO app_skills (workspace_id, author, name, version, description, when_to_use, tags)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetSkillFromWorkspace :one
SELECT * FROM app_skills
WHERE workspace_id = $1 AND author = $2 AND name = $3;

-- name: ListSkillsByWorkspace :many
SELECT * FROM app_skills
WHERE workspace_id = $1
ORDER BY author, name;

-- name: UpdateSkillInWorkspace :exec
UPDATE app_skills
SET version = $4, description = $5, when_to_use = $6, tags = $7, updated_at = now()
WHERE workspace_id = $1 AND author = $2 AND name = $3;

-- name: UninstallSkillInWorkspace :exec
DELETE FROM app_skills
WHERE workspace_id = $1 AND author = $2 AND name = $3;
