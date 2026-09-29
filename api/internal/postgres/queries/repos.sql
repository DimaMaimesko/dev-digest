-- name: ListRepos :many
-- Oldest first, so the order is stable. (The TS server has no ORDER BY.)
SELECT * FROM repos WHERE workspace_id = $1 ORDER BY created_at, id;

-- name: GetRepo :one
SELECT * FROM repos WHERE workspace_id = $1 AND id = $2;

-- name: MarkRepoPolled :exec
UPDATE repos SET last_polled_at = now() WHERE id = $1;
