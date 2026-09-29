-- name: RepoMap :one
-- The repository map cached for the last indexed commit, rendered for a
-- token budget.
SELECT m.map_text, m.token_count
FROM repo_index_state s
JOIN repo_map_cache m ON m.repo_id = s.repo_id AND m.commit_sha = s.last_indexed_sha
WHERE s.repo_id = $1 AND m.token_budget = $2;

-- name: FileRanks :many
-- How depended-on each of the given files is, as a percentile (0-100), for
-- the ones the index ranked.
SELECT file_path, percentile
FROM file_rank
WHERE repo_id = @repo_id AND file_path = ANY(@paths::text[])
ORDER BY file_path;
