-- name: LockSeed :exec
SELECT pg_advisory_xact_lock(hashtextextended('devdigest-seed', 0));

-- name: InsertWorkspace :one
INSERT INTO workspaces (name) VALUES ($1) RETURNING id;

-- name: InsertUser :one
INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id;

-- name: AddOwner :exec
INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
ON CONFLICT DO NOTHING;

-- name: SeedSetting :exec
-- A default preference, unless the user has one.
INSERT INTO settings (workspace_id, user_id, key, value) VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, user_id, key) DO NOTHING;

-- name: InsertSeedRepo :one
INSERT INTO repos (workspace_id, owner, name, full_name, default_branch, created_by)
VALUES ($1, $2, $3, $4, 'main', $5)
RETURNING id;

-- name: SeedPullExists :one
SELECT EXISTS (SELECT 1 FROM pull_requests WHERE repo_id = $1 AND number = $2);

-- name: InsertSeedPull :one
INSERT INTO pull_requests (workspace_id, repo_id, number, title, author, branch, base, head_sha,
                           additions, deletions, files_count, status, body)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING id;

-- name: AgentNamed :one
SELECT EXISTS (SELECT 1 FROM agents WHERE workspace_id = $1 AND name = $2);

-- name: SkillNamed :one
SELECT EXISTS (SELECT 1 FROM skills WHERE workspace_id = $1 AND name = $2);
