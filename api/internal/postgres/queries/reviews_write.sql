-- name: DecideFinding :one
-- Records the user's decision on a finding: accepted_at or dismissed_at is
-- set and the other cleared. Only a finding of a pull request in the
-- workspace matches.
UPDATE findings f
SET accepted_at = @accepted_at, dismissed_at = @dismissed_at
FROM reviews r, pull_requests p
WHERE f.id = @id AND r.id = f.review_id AND p.id = r.pr_id AND p.workspace_id = @workspace_id
RETURNING f.*;

-- name: DeleteReview :execrows
-- Deletes a review; its findings go with it (ON DELETE CASCADE).
DELETE FROM reviews WHERE workspace_id = $1 AND id = $2;

-- name: DeleteRun :execrows
-- Deletes a run, its trace (ON DELETE CASCADE) and the review it produced,
-- in one statement. reviews.run_id has no foreign key, so the review must be
-- deleted by hand. The count is of runs deleted.
WITH deleted_reviews AS (
    DELETE FROM reviews r WHERE r.workspace_id = @workspace_id AND r.run_id = @id
)
DELETE FROM agent_runs a WHERE a.workspace_id = @workspace_id AND a.id = @id;
