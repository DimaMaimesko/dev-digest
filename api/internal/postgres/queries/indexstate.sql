-- name: GetIndexState :one
-- The repository's code index state, if the repository is in the workspace.
SELECT s.*
FROM repo_index_state s
JOIN repos r ON r.id = s.repo_id
WHERE r.workspace_id = $1 AND s.repo_id = $2;
