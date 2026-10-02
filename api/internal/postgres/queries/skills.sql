-- name: ListSkills :many
-- Oldest first, like agents, each with the number of agents using it.
SELECT sqlc.embed(s),
       (SELECT count(*) FROM agent_skills l WHERE l.skill_id = s.id) AS agent_count
FROM skills s
WHERE s.workspace_id = $1
ORDER BY s.created_at, s.id;

-- name: GetSkill :one
SELECT sqlc.embed(s),
       (SELECT count(*) FROM agent_skills l WHERE l.skill_id = s.id) AS agent_count
FROM skills s
WHERE s.workspace_id = $1 AND s.id = $2;

-- name: SkillExists :one
SELECT EXISTS (SELECT 1 FROM skills WHERE workspace_id = $1 AND id = $2);

-- name: ListSkillVersions :many
-- A skill's body history, newest first.
SELECT * FROM skill_versions WHERE skill_id = $1 ORDER BY version DESC;

-- name: ListSkillAgents :many
-- The agents using a skill, oldest first, each with the number of skills it links.
SELECT sqlc.embed(a),
       (SELECT count(*) FROM agent_skills k WHERE k.agent_id = a.id) AS skill_count
FROM agents a
JOIN agent_skills l ON l.agent_id = a.id
WHERE a.workspace_id = $1 AND l.skill_id = $2
ORDER BY a.created_at, a.id;
