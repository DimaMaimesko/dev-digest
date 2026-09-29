-- name: PullExists :one
SELECT EXISTS (SELECT 1 FROM pull_requests WHERE workspace_id = $1 AND id = $2);

-- name: ListReviews :many
-- A pull request's reviews, newest first, each with the name of the agent
-- that wrote it: NULL when that agent was deleted.
SELECT r.id, r.pr_id, r.agent_id, r.run_id, a.name AS agent_name, r.kind,
       r.verdict, r.summary, r.score, r.model, r.created_at
FROM reviews r
LEFT JOIN agents a ON a.id = r.agent_id AND a.workspace_id = r.workspace_id
WHERE r.pr_id = $1
ORDER BY r.created_at DESC, r.id;

-- name: ListFindings :many
-- The findings of all of a pull request's reviews, by location.
SELECT f.*
FROM findings f
JOIN reviews r ON r.id = f.review_id
WHERE r.pr_id = $1
ORDER BY f.file, f.start_line, f.end_line, f.id;

-- name: ListRuns :many
-- A pull request's review runs of any status, newest first.
SELECT r.id, r.agent_id, a.name AS agent_name, r.provider, r.model, r.status,
       r.error, r.duration_ms, r.tokens_in, r.tokens_out, r.findings_count,
       r.grounding, r.ran_at, r.score, r.blockers
FROM agent_runs r
LEFT JOIN agents a ON a.id = r.agent_id
WHERE r.workspace_id = $1 AND r.pr_id = $2
ORDER BY r.ran_at DESC, r.id;

-- name: ListActiveRuns :many
-- A pull request's runs still in progress, oldest first.
SELECT r.id, r.agent_id, a.name AS agent_name, r.ran_at
FROM agent_runs r
LEFT JOIN agents a ON a.id = r.agent_id
WHERE r.workspace_id = $1 AND r.pr_id = $2 AND r.status = 'running'
ORDER BY r.ran_at, r.id;

-- name: GetRunTrace :one
-- A run's trace document, if the run is in the workspace.
SELECT t.trace
FROM run_traces t
JOIN agent_runs r ON r.id = t.run_id
WHERE r.workspace_id = $1 AND t.run_id = $2;
