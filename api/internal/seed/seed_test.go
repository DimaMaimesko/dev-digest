package seed_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/onboarding"
	"github.com/DimaMaimesko/dev-digest/api/internal/pgtest"
	"github.com/DimaMaimesko/dev-digest/api/internal/seed"
	"github.com/DimaMaimesko/dev-digest/api/internal/tour"
)

// counts returns how many rows each seeded table has.
func counts(t *testing.T, db *pgxpool.Pool) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, table := range []string{"workspaces", "users", "workspace_members", "settings", "repos", "pull_requests",
		"pr_files", "pr_commits", "pr_intent", "agent_runs", "run_traces", "reviews", "findings", "agents",
		"skills", "skill_versions", "agent_skills", "onboarding", "onboarding_runs"} {
		var n int
		if err := db.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[table] = n
	}
	return out
}

var seeded = map[string]int{"workspaces": 1, "users": 1, "workspace_members": 1, "settings": 4, "repos": 1,
	"pull_requests": 1, "pr_files": 4, "pr_commits": 1, "pr_intent": 1, "agent_runs": 1, "run_traces": 1, "reviews": 1,
	"findings": 2, "agents": 3, "skills": 4, "skill_versions": 4, "agent_skills": 2, "onboarding": 1, "onboarding_runs": 0}

func TestRun(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	ws, user, err := seed.Run(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	for table, n := range counts(t, db) {
		if n != seeded[table] {
			t.Errorf("%s: %d rows, want %d", table, n, seeded[table])
		}
	}
	var role, theme, prompt, model string
	db.QueryRow(ctx, `SELECT role FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`, ws, user).Scan(&role)
	db.QueryRow(ctx, `SELECT value #>> '{}' FROM settings WHERE key = 'theme'`).Scan(&theme)
	db.QueryRow(ctx, `SELECT system_prompt, model FROM agents WHERE name = 'Security Reviewer'`).Scan(&prompt, &model)
	if role != "owner" || theme != "dark" || model != "deepseek/deepseek-v4-flash" || !strings.HasPrefix(prompt, "# Role") || strings.HasSuffix(prompt, "\n") {
		t.Errorf("role %q, theme %q, model %q, prompt %.20q…", role, theme, model, prompt)
	}
	// The demo review came from a finished run, whose cost the screens show.
	var status string
	var cost, traceCost float64
	err = db.QueryRow(ctx, `SELECT r.status, r.cost_usd, (t.trace #>> '{stats,cost_usd}')::float8
		FROM reviews v JOIN agent_runs r ON r.id = v.run_id JOIN run_traces t ON t.run_id = r.id`).Scan(&status, &cost, &traceCost)
	if err != nil || status != "done" || cost != 0.0021 || traceCost != 0.0021 {
		t.Errorf("demo run: status %q, cost %v, trace cost %v, err %v", status, cost, traceCost, err)
	}

	// The demo PR has a seeded intent, so the Overview panel and the e2e
	// flow have something to show without any API key.
	var intentConfidence, intentProvider, intentModel string
	var fingerprint *string
	db.QueryRow(ctx, `SELECT confidence, provider, model, fingerprint FROM pr_intent`).
		Scan(&intentConfidence, &intentProvider, &intentModel, &fingerprint)
	if intentConfidence != "medium" || intentProvider != "anthropic" || intentModel != "haiku" || fingerprint != nil {
		t.Errorf("intent: confidence %q, provider %q, model %q, fingerprint %v",
			intentConfidence, intentProvider, intentModel, fingerprint)
	}
	// Source labels must match what intent.Gather actually writes
	// (internal/intent/gather.go), not a display-style name.
	var sourceLabels []string
	labelRows, _ := db.Query(ctx, `SELECT jsonb_array_elements(sources) ->> 'label' FROM pr_intent ORDER BY 1`)
	for labelRows.Next() {
		var l string
		labelRows.Scan(&l)
		sourceLabels = append(sourceLabels, l)
	}
	if got := strings.Join(sourceLabels, ", "); got != "branch, changed-files, commits, pr-description, pr-title" {
		t.Errorf("intent sources labels: %s", got)
	}

	// The demo skills are linked to the agents seeded with them.
	var links []string
	rows, _ := db.Query(ctx, `SELECT a.name || ' → ' || s.name FROM agent_skills l
		JOIN agents a ON a.id = l.agent_id JOIN skills s ON s.id = l.skill_id ORDER BY 1`)
	for rows.Next() {
		var l string
		rows.Scan(&l)
		links = append(links, l)
	}
	if got := strings.Join(links, ", "); got != "General Reviewer → pr-quality-rubric, Security Reviewer → secret-leakage-gate" {
		t.Errorf("links: %s", got)
	}
	var body string
	db.QueryRow(ctx, `SELECT body FROM skills WHERE name = 'pr-quality-rubric'`).Scan(&body)
	if !strings.HasPrefix(body, "# PR Quality Rubric") || strings.HasSuffix(body, "\n") {
		t.Errorf("rubric body %.30q…", body)
	}

	// Again, after the user changed a setting and a skill: nothing added,
	// nothing overwritten.
	db.Exec(ctx, `UPDATE settings SET value = '"light"' WHERE key = 'theme'`)
	db.Exec(ctx, `UPDATE skills SET body = 'mine' WHERE name = 'pr-quality-rubric'`)
	ws2, user2, err := seed.Run(ctx, db)
	if err != nil || ws2 != ws || user2 != user {
		t.Fatalf("second run: %v %v %v", ws2, user2, err)
	}
	for table, n := range counts(t, db) {
		if n != seeded[table] {
			t.Errorf("after a second run, %s: %d rows", table, n)
		}
	}
	db.QueryRow(ctx, `SELECT value #>> '{}' FROM settings WHERE key = 'theme'`).Scan(&theme)
	if theme != "light" {
		t.Errorf("the user's theme was overwritten: %q", theme)
	}
	db.QueryRow(ctx, `SELECT body FROM skills WHERE name = 'pr-quality-rubric'`).Scan(&body)
	if body != "mine" {
		t.Errorf("the user's skill was overwritten: %.30q…", body)
	}
}

// Seeding a database whose agents exist adds the skills without linking
// them, so its agents' prompts don't change.
func TestRunKeepsExistingAgentsSkills(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	if _, _, err := seed.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	db.Exec(ctx, `DELETE FROM skills`)
	if _, _, err := seed.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	if c := counts(t, db); c["skills"] != 4 || c["agent_skills"] != 0 {
		t.Errorf("skills %d, links %d; want 4 skills, no links", c["skills"], c["agent_skills"])
	}
}

// The demo repository's stored onboarding tour: files_indexed 42, commit
// a1b2c3d4e5f6, five done sections that parse as internal/tour's wire
// content types, and a run_locally section whose first step is "pnpm
// install" (e2e flow 09 relies on it). A second Run doesn't touch it.
func TestSeedOnboardingTour(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	if _, _, err := seed.Run(ctx, db); err != nil {
		t.Fatal(err)
	}

	var commitSha, indexStatus, provider, model string
	var filesIndexed int32
	var codeIndexEmpty bool
	var costUsd float64
	var raw []byte
	var generatedAt1 string
	if err := db.QueryRow(ctx, `SELECT o.commit_sha, o.files_indexed, o.index_status, o.code_index_empty,
			o.provider, o.model, o.cost_usd, o.json, o.generated_at::text
		FROM onboarding o JOIN repos r ON r.id = o.repo_id WHERE r.full_name = 'acme/payments-api'`).
		Scan(&commitSha, &filesIndexed, &indexStatus, &codeIndexEmpty, &provider, &model, &costUsd, &raw, &generatedAt1); err != nil {
		t.Fatalf("no seeded tour: %v", err)
	}
	if commitSha != "a1b2c3d4e5f6" || filesIndexed != 42 || indexStatus != "full" || codeIndexEmpty ||
		provider != "openrouter" || model != "deepseek/deepseek-v4-flash" || costUsd != 0.0042 {
		t.Errorf("tour = sha %q, files %d, status %q, codeIndexEmpty %v, provider %q, model %q, cost %v",
			commitSha, filesIndexed, indexStatus, codeIndexEmpty, provider, model, costUsd)
	}

	var sections []onboarding.TourSection
	if err := json.Unmarshal(raw, &sections); err != nil {
		t.Fatalf("sections don't parse as onboarding.TourSection: %v", err)
	}
	if len(sections) != 5 {
		t.Fatalf("%d sections, want 5", len(sections))
	}
	wantKinds := tour.Kinds
	for i, s := range sections {
		if s.Kind != wantKinds[i] || s.Status != "done" || s.Reason != nil {
			t.Errorf("section %d = %+v", i, s)
		}
	}
	runLocally, ok := sections[2].Content.(map[string]any)
	if !ok {
		t.Fatalf("run_locally content = %T", sections[2].Content)
	}
	steps, _ := runLocally["steps"].([]any)
	if len(steps) == 0 {
		t.Fatal("no run_locally steps")
	}
	first, _ := steps[0].(map[string]any)
	if first["command"] != "pnpm install" {
		t.Errorf("first run_locally step = %v, want command pnpm install", first)
	}

	// A second Run is idempotent: the row, and its generated_at, don't change.
	if _, _, err := seed.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	var n int
	var generatedAt2 string
	db.QueryRow(ctx, `SELECT count(*), max(generated_at::text) FROM onboarding`).Scan(&n, &generatedAt2)
	if n != 1 {
		t.Errorf("%d onboarding rows after a second Run, want 1", n)
	}
	if generatedAt2 != generatedAt1 {
		t.Errorf("generated_at changed on a second Run: %q -> %q", generatedAt1, generatedAt2)
	}
}

// Seeds at once make one workspace; the TS seed could make two.
func TestRunConcurrently(t *testing.T) {
	db := pgtest.New(t)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, _, err := seed.Run(context.Background(), db); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if c := counts(t, db); c["workspaces"] != 1 || c["agents"] != 3 {
		t.Errorf("counts %v", c)
	}
}

// The embedded prompts are docs/agent-prompts, for as long as both exist.
func TestPromptsMatchTheDocs(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	docs := filepath.Join(filepath.Dir(file), "..", "..", "..", "docs", "agent-prompts")
	for _, name := range []string{"general-reviewer", "security-reviewer", "performance-reviewer"} {
		want, err := os.ReadFile(filepath.Join(docs, name+".md"))
		if err != nil {
			t.Skip("no docs:", err)
		}
		got, _ := os.ReadFile(filepath.Join(filepath.Dir(file), "prompts", name+".md"))
		if string(got) != string(want) {
			t.Errorf("prompts/%s.md differs from docs/agent-prompts", name)
		}
	}
}
