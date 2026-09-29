-- name: ListRepos :many
-- Oldest first, so the order is stable. (The TS server has no ORDER BY.)
SELECT * FROM repos WHERE workspace_id = $1 ORDER BY created_at, id;

-- name: GetRepo :one
SELECT * FROM repos WHERE workspace_id = $1 AND id = $2;

-- name: MarkRepoPolled :exec
UPDATE repos SET last_polled_at = now() WHERE id = $1;

-- name: InsertRepo :one
-- Adds a repository, unless the workspace has it already: then no row.
INSERT INTO repos (workspace_id, owner, name, full_name, created_by)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, full_name) DO NOTHING
RETURNING *;

-- name: RepoByFullName :one
SELECT * FROM repos WHERE workspace_id = $1 AND full_name = $2;

-- name: SetClonePath :exec
UPDATE repos SET clone_path = $2, last_polled_at = now() WHERE id = $1;

-- name: DeleteRepo :execrows
DELETE FROM repos WHERE workspace_id = $1 AND id = $2;
