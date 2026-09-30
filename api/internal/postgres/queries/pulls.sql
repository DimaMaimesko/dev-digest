-- name: ListPulls :many
-- A repository's pull requests, each with the score of its latest review
-- (NULL when never reviewed) and what all its runs cost, newest first. The web
-- app sorts the list itself. The cost is known only when priced_runs > 0:
-- sqlc makes any computed column non-null, so NULL can't say "unknown" here.
SELECT p.id, p.number, p.title, p.author, p.branch, p.base, p.head_sha,
       p.last_reviewed_sha, p.additions, p.deletions, p.files_count, p.status,
       p.opened_at, p.updated_at,
       latest.score AS latest_score,
       COALESCE(spent.cost_usd, 0)::double precision AS cost_usd,
       COALESCE(spent.priced_runs, 0)::integer AS priced_runs
FROM pull_requests p
LEFT JOIN LATERAL (
    SELECT r.score
    FROM reviews r
    WHERE r.pr_id = p.id AND r.kind = 'review'
    ORDER BY r.created_at DESC
    LIMIT 1
) latest ON true
LEFT JOIN (
    SELECT ar.pr_id, sum(ar.cost_usd) AS cost_usd, count(ar.cost_usd) AS priced_runs
    FROM agent_runs ar
    GROUP BY ar.pr_id
) spent ON spent.pr_id = p.id
WHERE p.repo_id = $1
ORDER BY p.number DESC;

-- name: GetPull :one
SELECT * FROM pull_requests WHERE workspace_id = $1 AND id = $2;

-- name: ListPullFiles :many
SELECT path, additions, deletions, patch
FROM pr_files
WHERE pr_id = $1
ORDER BY path;

-- name: ListPullCommits :many
SELECT sha, message, author, committed_at
FROM pr_commits
WHERE pr_id = $1
ORDER BY committed_at NULLS LAST, sha;
