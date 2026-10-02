-- name: CreateSkill :one
INSERT INTO skills (workspace_id, name, description, type, source, body, enabled, version)
VALUES ($1, $2, $3, $4, $5, $6, $7, 1)
RETURNING *;

-- name: LockSkill :one
-- The skill, locked until the transaction ends, so two updates can't both
-- read the same version number.
SELECT * FROM skills WHERE workspace_id = $1 AND id = $2 FOR UPDATE;

-- name: UpdateSkill :one
UPDATE skills
SET name = $2, description = $3, type = $4, body = $5, enabled = $6, version = $7
WHERE id = $1
RETURNING *;

-- name: InsertSkillVersion :exec
INSERT INTO skill_versions (skill_id, version, body, message) VALUES ($1, $2, $3, $4);

-- name: GetSkillVersionBody :one
SELECT body FROM skill_versions WHERE skill_id = $1 AND version = $2;

-- name: DeleteSkill :execrows
DELETE FROM skills WHERE workspace_id = $1 AND id = $2;
