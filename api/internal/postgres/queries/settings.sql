-- name: ListSettings :many
-- The workspace's preferences. A key can have a workspace-wide row
-- (user_id NULL) and per-user rows; the per-user rows come last, so they win
-- when the caller folds the rows into one object.
SELECT key, value
FROM settings
WHERE workspace_id = $1
ORDER BY key, user_id NULLS FIRST;

-- name: UpsertSetting :exec
INSERT INTO settings (workspace_id, user_id, key, value)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, user_id, key) DO UPDATE SET value = EXCLUDED.value;
