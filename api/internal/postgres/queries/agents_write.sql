-- name: CreateAgent :one
INSERT INTO agents (workspace_id, name, description, provider, model, system_prompt,
                    output_schema, strategy, ci_fail_on, repo_intel, enabled, version, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 1, $12)
RETURNING *;

-- name: LockAgent :one
-- The agent, locked until the transaction ends, so two updates can't both
-- read the same version number.
SELECT * FROM agents WHERE workspace_id = $1 AND id = $2 FOR UPDATE;

-- name: UpdateAgent :one
UPDATE agents
SET name = $2, description = $3, provider = $4, model = $5, system_prompt = $6,
    output_schema = $7, strategy = $8, ci_fail_on = $9, repo_intel = $10,
    enabled = $11, version = $12
WHERE id = $1
RETURNING *;

-- name: InsertAgentVersion :exec
INSERT INTO agent_versions (agent_id, version, config_json) VALUES ($1, $2, $3);

-- name: DeleteAgent :execrows
DELETE FROM agents WHERE workspace_id = $1 AND id = $2;

-- name: UnlinkAllSkills :exec
DELETE FROM agent_skills WHERE agent_id = $1;

-- name: LinkSkill :exec
INSERT INTO agent_skills (agent_id, skill_id, "order") VALUES ($1, $2, $3)
ON CONFLICT (agent_id, skill_id) DO UPDATE SET "order" = EXCLUDED."order";

-- name: CountLinkedSkills :one
SELECT count(*) FROM agent_skills WHERE agent_id = $1;

-- name: SkillsInWorkspace :many
-- Which of the given skill IDs belong to the workspace.
SELECT id FROM skills WHERE workspace_id = $1 AND id = ANY(sqlc.arg(ids)::uuid[]);
