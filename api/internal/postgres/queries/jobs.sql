-- name: CreateJob :one
INSERT INTO jobs (workspace_id, kind, payload, status) VALUES ($1, $2, $3, 'queued') RETURNING id;

-- name: StartJob :exec
UPDATE jobs SET status = 'running', started_at = now(), attempts = attempts + 1 WHERE id = $1;

-- name: FinishJob :exec
-- Records how a job ended: done, or failed with its error.
UPDATE jobs SET status = @status, finished_at = now(), error = @error WHERE id = @id;
