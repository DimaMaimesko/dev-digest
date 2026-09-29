-- name: ListRepos :many
-- Oldest first, so the order is stable. (The TS server has no ORDER BY.)
SELECT * FROM repos WHERE workspace_id = $1 ORDER BY created_at, id;
