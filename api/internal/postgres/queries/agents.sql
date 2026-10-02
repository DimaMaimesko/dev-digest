-- name: ListAgents :many
-- Oldest first, so the order is stable. (The TS server has no ORDER BY.)
-- Each with the number of skills it links.
SELECT sqlc.embed(a),
       (SELECT count(*) FROM agent_skills l WHERE l.agent_id = a.id) AS skill_count
FROM agents a
WHERE a.workspace_id = $1
ORDER BY a.created_at, a.id;

-- name: GetAgent :one
SELECT * FROM agents WHERE workspace_id = $1 AND id = $2;

-- name: AgentExists :one
SELECT EXISTS (SELECT 1 FROM agents WHERE workspace_id = $1 AND id = $2);

-- name: ListAgentVersions :many
-- An agent's config history, newest first.
SELECT * FROM agent_versions WHERE agent_id = $1 ORDER BY version DESC;

-- name: GetAgentVersion :one
-- One config snapshot, if the agent is in the workspace.
SELECT v.*
FROM agent_versions v
JOIN agents a ON a.id = v.agent_id
WHERE a.workspace_id = $1 AND v.agent_id = $2 AND v.version = $3;

-- name: ListAgentSkills :many
-- The skills linked to an agent, in the order the agent uses them.
SELECT l.skill_id, l."order"
FROM agent_skills l
JOIN skills s ON s.id = l.skill_id
WHERE l.agent_id = $1
ORDER BY l."order", l.skill_id;

-- name: ListAgentSkillBodies :many
-- What a review's prompt gets from the agent's skills: the enabled ones with
-- a body that isn't blank, in the agent's order.
SELECT s.name, s.body
FROM agent_skills l
JOIN skills s ON s.id = l.skill_id
WHERE l.agent_id = $1 AND s.enabled AND btrim(s.body, E' \t\n\r') <> ''
ORDER BY l."order", l.skill_id;
