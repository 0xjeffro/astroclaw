-- name: CreateSkill :one
INSERT INTO app_skills (author, name, description, when_to_use, tags, version)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetSkillByName :one
SELECT * FROM app_skills WHERE author = $1 AND name = $2;

-- name: GetSkill :one
SELECT * FROM app_skills WHERE id = $1;

-- name: ListSkills :many
SELECT * FROM app_skills ORDER BY author, name;

-- name: UpdateSkill :exec
UPDATE app_skills
SET description = $1, when_to_use = $2, tags = $3, version = $4, updated_at = now()
WHERE id = $5;

-- name: DeleteSkill :exec
DELETE FROM app_skills WHERE id = $1;
