-- name: RepoExists :one
SELECT EXISTS (SELECT 1 FROM repos WHERE workspace_id = $1 AND id = $2);

-- name: ListPulls :many
-- A repository's pull requests, each with the score of its latest review
-- (NULL when never reviewed), newest first. The web app sorts the list itself.
SELECT p.id, p.number, p.title, p.author, p.branch, p.base, p.head_sha,
       p.last_reviewed_sha, p.additions, p.deletions, p.files_count, p.status,
       p.opened_at, p.updated_at,
       latest.score AS latest_score
FROM pull_requests p
LEFT JOIN LATERAL (
    SELECT r.score
    FROM reviews r
    WHERE r.pr_id = p.id AND r.kind = 'review'
    ORDER BY r.created_at DESC
    LIMIT 1
) latest ON true
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
