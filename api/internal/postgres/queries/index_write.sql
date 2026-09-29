-- name: LockRepoIndex :exec
-- Serializes the indexing of one repository until the transaction ends.
SELECT pg_advisory_xact_lock(hashtextextended('repo-index:' || @repo_id::text, 0));

-- name: IndexBasics :one
-- What indexing a repository needs, or no row when it was deleted.
SELECT id, owner, name, clone_path, default_branch FROM repos WHERE id = $1;

-- name: IndexStateOf :one
SELECT last_indexed_sha, indexer_version, status, files_indexed, files_skipped
FROM repo_index_state WHERE repo_id = $1;

-- name: DeleteSymbols :exec
DELETE FROM symbols WHERE repo_id = $1;

-- name: DeleteReferences :exec
DELETE FROM "references" WHERE repo_id = $1;

-- name: DeleteSymbolsIn :exec
DELETE FROM symbols WHERE repo_id = @repo_id AND path = ANY(@paths::text[]);

-- name: DeleteReferencesFrom :exec
DELETE FROM "references" WHERE repo_id = @repo_id AND from_path = ANY(@paths::text[]);

-- name: CopySymbols :copyfrom
INSERT INTO symbols (repo_id, path, name, kind, line, end_line, exported, signature, content_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: CopyReferences :copyfrom
INSERT INTO "references" (repo_id, from_path, to_symbol, line, content_hash)
VALUES ($1, $2, $3, $4, $5);

-- name: DeleteEdges :exec
DELETE FROM file_edges WHERE repo_id = $1;

-- name: CopyEdges :copyfrom
INSERT INTO file_edges (repo_id, from_file, to_file) VALUES ($1, $2, $3);

-- name: ClearDeclFiles :exec
UPDATE "references" SET decl_file = NULL WHERE repo_id = $1;

-- name: ResolveDeclFiles :exec
-- A reference names the file it resolves to when the file it is in imports
-- exactly one file exporting a symbol of that name; else it stays NULL.
WITH cand AS (
    SELECT r.id AS ref_id, e.to_file AS decl
    FROM "references" r
    JOIN file_edges e ON e.repo_id = r.repo_id AND e.from_file = r.from_path
    JOIN symbols s ON s.repo_id = r.repo_id AND s.path = e.to_file
                  AND s.name = r.to_symbol AND s.exported = true
    WHERE r.repo_id = $1
    GROUP BY r.id, e.to_file
),
uniq AS (
    SELECT ref_id FROM cand GROUP BY ref_id HAVING count(*) = 1
)
UPDATE "references" r
SET decl_file = c.decl
FROM cand c
JOIN uniq u ON u.ref_id = c.ref_id
WHERE r.id = c.ref_id;

-- name: DeleteFileRanks :exec
DELETE FROM file_rank WHERE repo_id = $1;

-- name: CopyFileRanks :copyfrom
INSERT INTO file_rank (repo_id, file_path, pagerank, hotness, rank, percentile)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: DeleteFileFacts :exec
DELETE FROM file_facts WHERE repo_id = $1;

-- name: DeleteFileFactsIn :exec
DELETE FROM file_facts WHERE repo_id = @repo_id AND file_path = ANY(@paths::text[]);

-- name: InsertFileFacts :exec
INSERT INTO file_facts (repo_id, file_path, endpoints, crons) VALUES ($1, $2, $3, $4);

-- name: MapCandidates :many
-- The symbols with a signature, most depended-on file first; then exported
-- ones, by line and name, and by path, so ties have one order.
SELECT s.path, s.signature
FROM symbols s
JOIN file_rank f ON f.repo_id = s.repo_id AND f.file_path = s.path
WHERE s.repo_id = $1 AND s.signature IS NOT NULL
ORDER BY f.rank DESC, s.exported DESC, s.line, s.name, s.path;

-- name: DeleteRepoMaps :exec
DELETE FROM repo_map_cache WHERE repo_id = $1;

-- name: PutRepoMap :exec
INSERT INTO repo_map_cache (repo_id, commit_sha, token_budget, map_text, token_count)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (repo_id, commit_sha, token_budget)
DO UPDATE SET map_text = EXCLUDED.map_text, token_count = EXCLUDED.token_count, created_at = now();

-- name: UpsertIndexState :exec
INSERT INTO repo_index_state (repo_id, last_indexed_sha, indexer_version, status, files_indexed, files_skipped, stats, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, now())
ON CONFLICT (repo_id) DO UPDATE
SET last_indexed_sha = EXCLUDED.last_indexed_sha, indexer_version = EXCLUDED.indexer_version,
    status = EXCLUDED.status, files_indexed = EXCLUDED.files_indexed,
    files_skipped = EXCLUDED.files_skipped, stats = EXCLUDED.stats, updated_at = now();

-- name: TouchIndexState :exec
UPDATE repo_index_state SET updated_at = now() WHERE repo_id = $1;

-- name: AdvanceIndexedSha :exec
UPDATE repo_index_state SET last_indexed_sha = $2, updated_at = now() WHERE repo_id = $1;
