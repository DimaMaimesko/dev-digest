-- name: OnboardingRepoState :one
-- The repo, scoped to the workspace, with its index state if any. A row for
-- a repo in another workspace, or a missing one, is simply not found.
SELECT r.id, r.workspace_id, r.full_name, r.clone_path,
    s.last_indexed_sha, s.status AS index_status, s.files_indexed
FROM repos r
LEFT JOIN repo_index_state s ON s.repo_id = r.id
WHERE r.workspace_id = $1 AND r.id = $2;

-- name: CountActiveIndexJobs :one
-- "Indexing in progress": a clone or index job for this repo, queued or
-- running, scheduled within the last 10 minutes (the window a crash can't
-- block forever, since jobs has no stale-job sweep).
SELECT count(*) FROM jobs
WHERE kind IN ('clone', 'repo-intel-index', 'repo-intel-refresh', 'repo-intel-resync')
  AND payload->>'repoId' = @repo_id::text
  AND status IN ('queued', 'running')
  AND scheduled_at > now() - interval '10 minutes';

-- name: InsertOnboardingRun :one
-- Starts a run unless one is already queued or running for this repo. Zero
-- rows back means a run is already in progress.
INSERT INTO onboarding_runs (repo_id, workspace_id, commit_sha, files_indexed, index_status,
    code_index_empty, provider, model, sections)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (repo_id) WHERE status IN ('queued', 'running') DO NOTHING
RETURNING *;

-- name: LiveOnboardingRun :one
-- The queued or running run for a repo, if any.
SELECT * FROM onboarding_runs WHERE repo_id = $1 AND status IN ('queued', 'running');

-- name: LatestOnboardingRun :one
-- The most recent run for a repo. Rows made in one transaction share
-- created_at, so this also orders by id.
SELECT * FROM onboarding_runs WHERE repo_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1;

-- name: StartOnboardingRun :execrows
UPDATE onboarding_runs SET status = 'running', started_at = now()
WHERE id = $1 AND status = 'queued';

-- name: SetOnboardingSection :exec
-- Row-level update so 5 concurrent section writers don't lose updates.
UPDATE onboarding_runs
SET sections = jsonb_set(sections, ARRAY[@idx::text], @val::jsonb)
WHERE id = @id;

-- name: FinishOnboardingRun :one
-- Ends a live run. No row back means it was deleted (the repo was removed)
-- or it already finished (stale-failed by FailStaleOnboardingRuns).
UPDATE onboarding_runs
SET status = @status, error = @error, tokens_in = @tokens_in, tokens_out = @tokens_out,
    cost_usd = @cost_usd, finished_at = now()
WHERE id = @id AND status IN ('queued', 'running')
RETURNING repo_id;

-- name: UpsertOnboardingTour :exec
INSERT INTO onboarding (repo_id, json, generated_at, commit_sha, files_indexed, index_status,
    code_index_empty, provider, model, tokens_in, tokens_out, cost_usd)
VALUES ($1, $2, now(), $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (repo_id) DO UPDATE
SET json = EXCLUDED.json, generated_at = now(), commit_sha = EXCLUDED.commit_sha,
    files_indexed = EXCLUDED.files_indexed, index_status = EXCLUDED.index_status,
    code_index_empty = EXCLUDED.code_index_empty, provider = EXCLUDED.provider,
    model = EXCLUDED.model, tokens_in = EXCLUDED.tokens_in, tokens_out = EXCLUDED.tokens_out,
    cost_usd = EXCLUDED.cost_usd;

-- name: GetOnboardingTour :one
-- Scoped to the workspace through repos.
SELECT o.* FROM onboarding o
JOIN repos r ON r.id = o.repo_id
WHERE r.workspace_id = $1 AND o.repo_id = $2;

-- name: DeleteOtherOnboardingRuns :exec
-- Keeps only the latest run per repo.
DELETE FROM onboarding_runs doomed
WHERE doomed.repo_id = $1 AND doomed.id NOT IN (
    SELECT keep.id FROM onboarding_runs keep
    WHERE keep.repo_id = $1
    ORDER BY keep.created_at DESC, keep.id DESC LIMIT 1
);

-- name: FailStaleOnboardingRuns :execrows
-- At startup: runs still marked queued or running were left by a server
-- that stopped during generation.
UPDATE onboarding_runs
SET status = 'failed', error = 'the server stopped during generation', finished_at = now()
WHERE status IN ('queued', 'running');

-- name: TopRankedFiles :many
SELECT file_path, rank FROM file_rank WHERE repo_id = $1 ORDER BY rank DESC LIMIT $2;

-- name: FileFactsWithRoutes :many
SELECT file_path, endpoints, crons FROM file_facts
WHERE repo_id = $1 AND (endpoints != '[]'::jsonb OR crons != '[]'::jsonb)
LIMIT $2;
