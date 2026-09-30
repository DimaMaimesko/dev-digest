-- name: EnabledAgents :many
-- The workspace's agents that are on, oldest first.
SELECT * FROM agents WHERE workspace_id = $1 AND enabled ORDER BY created_at, id;

-- name: CreateRun :one
-- A run of an agent on a pull request, running from now.
INSERT INTO agent_runs (workspace_id, agent_id, pr_id, provider, model, status, source)
VALUES ($1, $2, $3, $4, $5, 'running', 'local')
RETURNING id;

-- name: LockRunningRun :one
-- The run, locked until the transaction ends, if it is still running: not
-- cancelled or deleted meanwhile.
SELECT id FROM agent_runs WHERE id = $1 AND status = 'running' FOR UPDATE;

-- name: FinishRun :exec
-- Records how a run ended, unless it isn't running any more: a cancelled
-- run stays cancelled.
UPDATE agent_runs
SET status = @status, duration_ms = @duration_ms, tokens_in = @tokens_in, tokens_out = @tokens_out,
    cost_usd = @cost_usd, findings_count = @findings_count, grounding = @grounding, score = @score,
    blockers = @blockers, error = @error
WHERE id = @id AND status = 'running';

-- name: RecordRunUsage :exec
-- Records what a run's model calls took, whatever its status: a run cancelled
-- while its model answered was still billed, and FinishRun skips it.
UPDATE agent_runs
SET tokens_in = @tokens_in, tokens_out = @tokens_out, cost_usd = @cost_usd
WHERE id = @id;

-- name: InsertReview :one
INSERT INTO reviews (workspace_id, pr_id, agent_id, run_id, kind, verdict, summary, score, model)
VALUES ($1, $2, $3, $4, 'review', $5, $6, $7, $8)
RETURNING id;

-- name: InsertFinding :exec
INSERT INTO findings (review_id, file, start_line, end_line, severity, category, title,
                      rationale, suggestion, confidence, kind)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: MarkReviewed :exec
-- Records the commit a review ran against, so the list can tell whether new
-- commits came since.
UPDATE pull_requests SET last_reviewed_sha = $2 WHERE id = $1;

-- name: SaveTrace :exec
INSERT INTO run_traces (run_id, trace) VALUES ($1, $2)
ON CONFLICT (run_id) DO UPDATE SET trace = EXCLUDED.trace;

-- name: CancelRun :execrows
-- Marks a running run of the workspace cancelled.
UPDATE agent_runs SET status = 'cancelled'
WHERE workspace_id = $1 AND id = $2 AND status = 'running';

-- name: FailStaleRuns :execrows
-- At startup: runs still marked running were left by a server that stopped.
UPDATE agent_runs SET status = 'failed' WHERE status = 'running';
