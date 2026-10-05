-- name: ListConventions :many
-- A repository's conventions: accepted first, then the most confident
-- (unknown confidence last), then by rule.
SELECT * FROM conventions
WHERE workspace_id = $1 AND repo_id = $2
ORDER BY accepted DESC, confidence DESC NULLS LAST, rule, id;
