// Package seed fills a new database with DevDigest's starting data: the
// default workspace and its user, default settings, a demo repository with
// a reviewed pull request, and the three built-in reviewer agents.
package seed

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

// The agents' system prompts. They are copies of docs/agent-prompts, which
// a test keeps in step.
//
//go:embed prompts/*.md
var prompts embed.FS

// prompt returns a built-in agent's prompt, without the file's last newline
// (the TS seed's text has none).
func prompt(name string) string {
	data, err := prompts.ReadFile("prompts/" + name + ".md")
	if err != nil {
		panic(err) // embedded above: can't happen
	}
	return strings.TrimSuffix(string(data), "\n")
}

// Names the API looks the workspace and user up by.
const (
	Workspace = "default"
	UserEmail = "you@local"
)

// The built-in agents' model.
const (
	agentProvider = "openrouter"
	agentModel    = "deepseek/deepseek-v4-flash"
)

// Beginner starts transactions: a *pgx.Conn or a *pgxpool.Pool.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Run seeds the database, in one transaction. It can run again: what
// exists already, found by name, is left as it is. It returns the default
// workspace and user.
func Run(ctx context.Context, db Beginner) (workspace, user uuid.UUID, err error) {
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		q := postgres.New(tx)
		if err := q.LockSeed(ctx); err != nil {
			return err
		}
		if workspace, err = findOrInsert(ctx,
			func() (uuid.UUID, error) { return q.WorkspaceByName(ctx, Workspace) },
			func() (uuid.UUID, error) { return q.InsertWorkspace(ctx, Workspace) }); err != nil {
			return err
		}
		if user, err = findOrInsert(ctx,
			func() (uuid.UUID, error) { return q.UserByEmail(ctx, UserEmail) },
			func() (uuid.UUID, error) {
				return q.InsertUser(ctx, postgres.InsertUserParams{Email: UserEmail, Name: "You"})
			}); err != nil {
			return err
		}
		if err := q.AddOwner(ctx, postgres.AddOwnerParams{WorkspaceID: workspace, UserID: user}); err != nil {
			return err
		}
		for key, value := range map[string]any{"polling_interval_min": 5, "theme": "dark", "density": "regular", "sync_to_folder": true} {
			data, _ := json.Marshal(value)
			if err := q.SeedSetting(ctx, postgres.SeedSettingParams{WorkspaceID: workspace, UserID: &user, Key: key, Value: data}); err != nil {
				return err
			}
		}
		if err := demo(ctx, q, workspace, user); err != nil {
			return err
		}
		return agents(ctx, q, workspace, user)
	})
	return workspace, user, err
}

func findOrInsert(ctx context.Context, find, insert func() (uuid.UUID, error)) (uuid.UUID, error) {
	id, err := find()
	if errors.Is(err, pgx.ErrNoRows) {
		return insert()
	}
	return id, err
}

// demo adds the demo repository acme/payments-api and its pull request
// #482, with files, a commit and a review, so the web app has something to
// show before the first real repository.
func demo(ctx context.Context, q *postgres.Queries, workspace, user uuid.UUID) error {
	repo, err := findOrInsert(ctx,
		func() (uuid.UUID, error) {
			r, err := q.RepoByFullName(ctx, postgres.RepoByFullNameParams{WorkspaceID: workspace, FullName: "acme/payments-api"})
			return r.ID, err
		},
		func() (uuid.UUID, error) {
			return q.InsertSeedRepo(ctx, postgres.InsertSeedRepoParams{
				WorkspaceID: workspace, Owner: "acme", Name: "payments-api", FullName: "acme/payments-api", CreatedBy: &user,
			})
		})
	if err != nil {
		return err
	}
	if exists, err := q.SeedPullExists(ctx, postgres.SeedPullExistsParams{RepoID: repo, Number: 482}); err != nil || exists {
		return err
	}
	pull, err := q.InsertSeedPull(ctx, postgres.InsertSeedPullParams{
		WorkspaceID: workspace, RepoID: repo, Number: 482,
		Title: "Add rate limiting to public API endpoints", Author: "marisa.koch",
		Branch: "feat/rate-limit-public", Base: "main", HeadSha: "a1b2c3d4e5f6",
		Additions: 247, Deletions: 38, FilesCount: 9, Status: "needs_review",
		Body: new("Add rate limiting to public API endpoints to prevent abuse from unauthenticated clients."),
	})
	if err != nil {
		return err
	}
	for _, f := range []postgres.InsertPullFileParams{
		{Path: "src/middleware/ratelimit.ts", Additions: 84},
		{Path: "src/api/public/webhooks.ts", Additions: 31, Deletions: 6},
		{Path: "src/config.ts", Additions: 4},
		{Path: "src/api/users.ts", Additions: 7, Deletions: 2},
	} {
		f.PrID = pull
		if err := q.InsertPullFile(ctx, f); err != nil {
			return err
		}
	}
	if err := q.InsertPullCommit(ctx, postgres.InsertPullCommitParams{
		PrID: pull, Sha: "a1b2c3d4e5f6", Message: "Add token-bucket rate limiter", Author: "marisa.koch",
	}); err != nil {
		return err
	}
	review, err := q.InsertReview(ctx, postgres.InsertReviewParams{
		WorkspaceID: workspace, PrID: pull, Verdict: new("request_changes"), Score: new(int32(61)), Model: new("seed"),
		Summary: new("Solid middleware approach, but a Stripe secret key is committed in plaintext and the user-list endpoint introduces an N+1 query under the new limiter."),
	})
	if err != nil {
		return err
	}
	for _, f := range []postgres.InsertFindingParams{
		{File: "src/config.ts", StartLine: 12, EndLine: 12, Severity: "CRITICAL", Category: "security",
			Title: "Hardcoded Stripe secret key in commit", Rationale: "Line 12 contains a literal `sk_live_` Stripe secret key.",
			Suggestion: new("Move to env var and rotate the key immediately."), Confidence: 0.98},
		{File: "src/api/users.ts", StartLine: 45, EndLine: 52, Severity: "WARNING", Category: "perf",
			Title: "N+1 query in user list endpoint", Rationale: "Loop issues one query per user → N+1.",
			Suggestion: new("Use a single IN query and group in memory."), Confidence: 0.86},
	} {
		f.ReviewID, f.Kind = review, "finding"
		if err := q.InsertFinding(ctx, f); err != nil {
			return err
		}
	}
	return nil
}

// agents adds the built-in reviewer agents the workspace hasn't got.
func agents(ctx context.Context, q *postgres.Queries, workspace, user uuid.UUID) error {
	for _, a := range []struct{ name, description, prompt string }{
		{"General Reviewer", "Reviews a PR diff for bugs, correctness, and clarity.", "general-reviewer"},
		{"Security Reviewer", "Flags secrets, injection, SSRF and the lethal trifecta before merge.", "security-reviewer"},
		{"Performance Reviewer", "Catches N+1 queries, missing indexes, and hot-path allocations.", "performance-reviewer"},
	} {
		exists, err := q.AgentNamed(ctx, postgres.AgentNamedParams{WorkspaceID: workspace, Name: a.name})
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		_, err = q.CreateAgent(ctx, postgres.CreateAgentParams{
			WorkspaceID: workspace, Name: a.name, Description: a.description,
			Provider: agentProvider, Model: agentModel, SystemPrompt: prompt(a.prompt),
			Strategy: "single-pass", CiFailOn: "critical", RepoIntel: true, Enabled: true, CreatedBy: &user,
		})
		if err != nil {
			return err
		}
	}
	return nil
}
