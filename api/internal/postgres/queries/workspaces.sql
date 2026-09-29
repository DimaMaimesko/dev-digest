-- name: WorkspaceByName :one
SELECT id FROM workspaces WHERE name = $1 LIMIT 1;
