-- name: WorkspaceByName :one
SELECT id FROM workspaces WHERE name = $1 LIMIT 1;

-- name: UserByEmail :one
SELECT id FROM users WHERE email = $1 LIMIT 1;
