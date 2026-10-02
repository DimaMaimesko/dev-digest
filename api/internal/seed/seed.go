// Package seed fills a new database with DevDigest's starting data: the
// default workspace and its user, default settings, a demo repository with
// a reviewed pull request, the three built-in reviewer agents, and demo
// skills.
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

// The demo skills' bodies.
//
//go:embed skills/*.md
var skillBodies embed.FS

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
		added, err := agents(ctx, q, workspace, user)
		if err != nil {
			return err
		}
		return skills(ctx, q, workspace, added)
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
// #482, with files, a commit, and a finished run with its review and trace,
// so the web app has something to show before the first real repository.
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
	run, err := demoRun(ctx, q, workspace, pull)
	if err != nil {
		return err
	}
	review, err := q.InsertReview(ctx, postgres.InsertReviewParams{
		WorkspaceID: workspace, PrID: pull, RunID: &run, Verdict: new("request_changes"), Score: new(int32(61)), Model: new("seed"),
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

// Demo run's outcome, as a finished review run records it.
const (
	demoCost      = 0.0021 // US dollars
	demoTokensIn  = 5120
	demoTokensOut = 860
	demoMs        = 8400
)

// demoRun adds the finished run the demo review came from, with its trace.
func demoRun(ctx context.Context, q *postgres.Queries, workspace, pull uuid.UUID) (uuid.UUID, error) {
	run, err := q.CreateRun(ctx, postgres.CreateRunParams{
		WorkspaceID: workspace, PrID: &pull, Provider: new(agentProvider), Model: new(agentModel),
	})
	if err != nil {
		return uuid.Nil, err
	}
	err = q.FinishRun(ctx, postgres.FinishRunParams{
		ID: run, Status: new("done"), DurationMs: new(int32(demoMs)),
		TokensIn: new(int32(demoTokensIn)), TokensOut: new(int32(demoTokensOut)), CostUsd: new(demoCost),
		FindingsCount: new(int32(2)), Grounding: new("2/2 passed"), Score: new(int32(61)), Blockers: new(int32(1)),
	})
	if err != nil {
		return uuid.Nil, err
	}
	// The shape the runner saves (RunTrace in the client's contracts).
	trace, err := json.Marshal(map[string]any{
		"config": map[string]any{"agent": "General Reviewer", "version": "1", "provider": agentProvider,
			"model": agentModel, "pr": 482, "source": "local"},
		"stats": map[string]any{"duration_ms": demoMs, "tokens_in": demoTokensIn, "tokens_out": demoTokensOut,
			"cost_usd": demoCost, "findings": 2, "grounding": "2/2 passed"},
		"prompt_assembly": map[string]any{"system": "You are a senior code reviewer.",
			"user": "Review pull request #482 \"Add rate limiting to public API endpoints\" by marisa.koch."},
		"tool_calls":    []map[string]any{{"tool": "review_file", "args": "all files", "meta": "single-pass", "ms": demoMs}},
		"raw_output":    "",
		"memory_pulled": []any{},
		"specs_read":    []any{},
		"log": []map[string]any{
			{"t": "00.00", "kind": "info", "msg": "Seeded demo run"},
			{"t": "08.40", "kind": "result", "msg": "Citation grounding: 2/2 passed"},
		},
	})
	if err != nil {
		return uuid.Nil, err
	}
	return run, q.SaveTrace(ctx, postgres.SaveTraceParams{RunID: run, Trace: trace})
}

// agents adds the built-in reviewer agents the workspace hasn't got, and
// returns the ones it added, by name.
func agents(ctx context.Context, q *postgres.Queries, workspace, user uuid.UUID) (map[string]uuid.UUID, error) {
	added := map[string]uuid.UUID{}
	for _, a := range []struct{ name, description, prompt string }{
		{"General Reviewer", "Reviews a PR diff for bugs, correctness, and clarity.", "general-reviewer"},
		{"Security Reviewer", "Flags secrets, injection, SSRF and the lethal trifecta before merge.", "security-reviewer"},
		{"Performance Reviewer", "Catches N+1 queries, missing indexes, and hot-path allocations.", "performance-reviewer"},
	} {
		exists, err := q.AgentNamed(ctx, postgres.AgentNamedParams{WorkspaceID: workspace, Name: a.name})
		if err != nil {
			return nil, err
		}
		if exists {
			continue
		}
		created, err := q.CreateAgent(ctx, postgres.CreateAgentParams{
			WorkspaceID: workspace, Name: a.name, Description: a.description,
			Provider: agentProvider, Model: agentModel, SystemPrompt: prompt(a.prompt),
			Strategy: "single-pass", CiFailOn: "critical", RepoIntel: true, Enabled: true, CreatedBy: &user,
		})
		if err != nil {
			return nil, err
		}
		added[a.name] = created.ID
	}
	return added, nil
}

// skills adds the demo skills the workspace hasn't got, each at version 1
// with its snapshot. A new skill is linked to its agent only when that agent
// was added in this run too, so seeding an existing database never changes
// what its agents' reviews send to the model.
func skills(ctx context.Context, q *postgres.Queries, workspace uuid.UUID, addedAgents map[string]uuid.UUID) error {
	for _, sk := range []struct {
		name, description, typ, source string
		enabled                        bool
		agent                          string // the built-in agent it's linked to, if any
	}{
		{"pr-quality-rubric", "Rubric for evaluating overall PR quality across correctness, tests, and clarity.",
			"rubric", "manual", true, "General Reviewer"},
		{"secret-leakage-gate", "Detects sk_live, service_role, and NEXT_PUBLIC_ secrets before they ship.",
			"security", "community", true, "Security Reviewer"},
		{"no-then-chains", "House rule: always use async/await instead of .then() chains.",
			"convention", "extracted", true, ""},
		{"test-coverage-nudge", "Suggests tests when new branches lack coverage.",
			"custom", "manual", false, ""},
	} {
		exists, err := q.SkillNamed(ctx, postgres.SkillNamedParams{WorkspaceID: workspace, Name: sk.name})
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		body, err := skillBodies.ReadFile("skills/" + sk.name + ".md")
		if err != nil {
			panic(err) // embedded above: can't happen
		}
		created, err := q.CreateSkill(ctx, postgres.CreateSkillParams{
			WorkspaceID: workspace, Name: sk.name, Description: sk.description, Type: sk.typ,
			Source: sk.source, Body: strings.TrimSuffix(string(body), "\n"), Enabled: sk.enabled,
		})
		if err != nil {
			return err
		}
		if err := q.InsertSkillVersion(ctx, postgres.InsertSkillVersionParams{
			SkillID: created.ID, Version: created.Version, Body: created.Body, Message: new("Initial version"),
		}); err != nil {
			return err
		}
		if agent, ok := addedAgents[sk.agent]; ok {
			if err := q.LinkSkill(ctx, postgres.LinkSkillParams{AgentID: agent, SkillID: created.ID, Order: 0}); err != nil {
				return err
			}
		}
	}
	return nil
}
