-- name: UpsertPull :exec
-- Adds a pull request from GitHub's list, or updates the fields that change.
-- A new one has no diff stats yet: the list leaves them out. opened_at is
-- filled in if an earlier sync left it empty.
INSERT INTO pull_requests (workspace_id, repo_id, number, title, author, branch, base,
                           head_sha, status, opened_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (repo_id, number) DO UPDATE
SET title = EXCLUDED.title, head_sha = EXCLUDED.head_sha, status = EXCLUDED.status,
    updated_at = EXCLUDED.updated_at,
    opened_at = COALESCE(pull_requests.opened_at, EXCLUDED.opened_at);

-- name: PullsWithoutStats :many
-- The numbers of a repository's pull requests with no diff stats yet,
-- newest first.
SELECT number FROM pull_requests
WHERE repo_id = $1 AND additions = 0 AND deletions = 0 AND files_count = 0
ORDER BY number DESC
LIMIT $2;

-- name: SetPullStats :exec
UPDATE pull_requests SET additions = $3, deletions = $4, files_count = $5
WHERE repo_id = $1 AND number = $2;

-- name: RefreshPull :one
-- Saves a pull request's detail from GitHub and returns its ID. Updating the
-- row locks it until the transaction ends, so two refreshes can't interleave
-- their deletes and inserts of its files and commits.
UPDATE pull_requests
SET title = @title, head_sha = @head_sha, status = @status, updated_at = @updated_at,
    opened_at = COALESCE(opened_at, @opened_at), body = @body,
    additions = @additions, deletions = @deletions, files_count = @files_count
WHERE repo_id = @repo_id AND number = @number
RETURNING id;

-- name: DeletePullFiles :exec
DELETE FROM pr_files WHERE pr_id = $1;

-- name: InsertPullFile :exec
INSERT INTO pr_files (pr_id, path, additions, deletions, patch) VALUES ($1, $2, $3, $4, $5);

-- name: DeletePullCommits :exec
DELETE FROM pr_commits WHERE pr_id = $1;

-- name: InsertPullCommit :exec
INSERT INTO pr_commits (pr_id, sha, message, author, committed_at) VALUES ($1, $2, $3, $4, $5);
