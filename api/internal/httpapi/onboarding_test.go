package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DimaMaimesko/dev-digest/api/internal/jobs"
	"github.com/DimaMaimesko/dev-digest/api/internal/onboarding"
	"github.com/DimaMaimesko/dev-digest/api/internal/review"
	"github.com/DimaMaimesko/dev-digest/api/internal/seed"
)

// fakeOnboardingLLM answers every section schema with a valid, grounded
// answer at once: the handler tests only need a generation to be accepted,
// not what it eventually produces (that's internal/tour's and
// internal/onboarding's job).
type fakeOnboardingLLM struct{}

func (fakeOnboardingLLM) CompleteJSON(ctx context.Context, req review.JSONRequest) (review.JSONResponse, error) {
	var text string
	switch req.SchemaName {
	case "Architecture":
		text = `{"summary":"ok","nodes":[],"edges":[]}`
	case "CriticalPaths":
		text = `{"items":[{"path":"README.md","reason":"r"}]}`
	case "RunLocally":
		text = `{"steps":[{"command":"go build ./...","comment":null}]}`
	case "ReadingPath":
		text = `{"items":[{"path":"README.md","reason":"a"},{"path":"go.mod","reason":"b"},{"path":"cmd/main.go","reason":"c"}]}`
	case "FirstTasks":
		text = `{"tasks":[{"title":"t","description":"d","size":"small","paths":["README.md"],"todo":null}]}`
	default:
		text = "{}"
	}
	return review.JSONResponse{Text: text, TokensIn: 1, TokensOut: 1}, nil
}

// writeOnboardingTestFile writes rel under dir, creating parent directories.
func writeOnboardingTestFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// initOnboardingTestGitRepo commits every file written so far in dir and
// returns the commit's sha.
func initOnboardingTestGitRepo(t *testing.T, dir string) string {
	t.Helper()
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")
	run("add", ".")
	run("commit", "-q", "-m", "first")
	return run("rev-parse", "HEAD")
}

// withOnboarding gives the fixture an onboarding.Store wired to a real,
// indexed temporary git clone (so Start answers 202) and a background jobs
// runner. It returns the fixture, the jobs runner (for Wait/Close) and the
// ready repository's id.
func withOnboarding(t *testing.T) (fixture, *jobs.Runner, string) {
	t.Helper()
	f := newFixture(t)

	dir := t.TempDir()
	writeOnboardingTestFile(t, dir, "README.md", "# demo\n")
	writeOnboardingTestFile(t, dir, "go.mod", "module demo\n\ngo 1.26\n")
	writeOnboardingTestFile(t, dir, "cmd/main.go", "package main\n\nfunc main() {}\n")
	commit := initOnboardingTestGitRepo(t, dir)

	repo := f.insertRepo(t, f.workspace, "o/ready")
	f.exec(t, `UPDATE repos SET clone_path = $1 WHERE id = $2`, dir, repo)
	f.exec(t, `INSERT INTO repo_index_state (repo_id, last_indexed_sha, indexer_version, status, files_indexed)
		VALUES ($1, $2, 1, 'full', 3)`, repo, commit)

	j := jobs.New(f.db, quiet)
	t.Cleanup(j.Close)
	f.onboarding = onboarding.New(onboarding.Config{
		DB: f.db, Jobs: j, LLM: func(string) (review.LLM, error) { return fakeOnboardingLLM{}, nil }, Log: quiet,
	})
	return f, j, repo.String()
}

// --- 404 for an unknown or another workspace's repo, 422 for a bad id ---

func TestOnboardingTourUnknownAndMalformedRepo(t *testing.T) {
	f, _, _ := withOnboarding(t)
	notFound := `{"error": {"code": "not_found", "message": "Repo not found"}}`
	missing := uuid.NewString()
	other := f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`)
	otherRepo := f.insertRepo(t, other, "x/y").String()

	for _, repo := range []string{missing, otherRepo} {
		assertJSON(t, f.get(t, "/repos/"+repo+"/onboarding"), http.StatusNotFound, notFound)
		assertJSON(t, f.send(t, http.MethodPost, "/repos/"+repo+"/onboarding", ``), http.StatusNotFound, notFound)
	}

	for _, r := range []struct{ method, path string }{
		{http.MethodGet, "/repos/42/onboarding"},
		{http.MethodPost, "/repos/42/onboarding"},
	} {
		if paths := issuePaths(t, f.send(t, r.method, r.path, ``)); len(paths) != 1 {
			t.Errorf("%s %s: issues at %v, want one", r.method, r.path, paths)
		}
	}
}

// --- AC-4: 409 repo_not_indexed for a repository with no completed index ---

func TestGenerateOnboardingTourNotIndexed(t *testing.T) {
	f := newFixture(t)
	repo := f.insertRepo(t, f.workspace, "o/none")
	j := jobs.New(f.db, quiet)
	t.Cleanup(j.Close)
	f.onboarding = onboarding.New(onboarding.Config{
		DB: f.db, Jobs: j, LLM: func(string) (review.LLM, error) { return fakeOnboardingLLM{}, nil }, Log: quiet,
	})

	assertJSON(t, f.send(t, http.MethodPost, "/repos/"+repo.String()+"/onboarding", ``), http.StatusConflict,
		`{"error": {"code": "repo_not_indexed", "message": "Repository is not indexed"}}`)
}

// --- AC-33: 400 config_error naming the provider, before anything is inserted ---

func TestGenerateOnboardingTourConfigError(t *testing.T) {
	f, _, repo := withOnboarding(t)
	j := jobs.New(f.db, quiet)
	t.Cleanup(j.Close)
	f.onboarding = onboarding.New(onboarding.Config{
		DB:   f.db,
		Jobs: j,
		LLM: func(provider string) (review.LLM, error) {
			return nil, errors.New("OPENROUTER_API_KEY is not configured")
		},
		Log: quiet,
	})

	assertJSON(t, f.send(t, http.MethodPost, "/repos/"+repo+"/onboarding", ``), http.StatusBadRequest,
		`{"error": {"code": "config_error", "message": "OPENROUTER_API_KEY is not configured for provider openrouter"}}`)

	var n int
	f.db.QueryRow(context.Background(), `SELECT count(*) FROM onboarding_runs WHERE repo_id = $1`, repo).Scan(&n)
	if n != 0 {
		t.Errorf("%d runs inserted after a config error, want 0", n)
	}
}

// --- AC-5/AC-6: 202 with a generation; a second call answers the same one ---

func TestGenerateOnboardingTourTwiceSameGeneration(t *testing.T) {
	f, j, repo := withOnboarding(t)

	var first, second struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	decode(t, f.send(t, http.MethodPost, "/repos/"+repo+"/onboarding", ``), http.StatusAccepted, &first)
	decode(t, f.send(t, http.MethodPost, "/repos/"+repo+"/onboarding", ``), http.StatusAccepted, &second)

	if uuid.Validate(first.ID) != nil || first.ID != second.ID {
		t.Errorf("generations: %+v, %+v, want the same non-empty id", first, second)
	}
	if first.Status != "queued" && first.Status != "running" && first.Status != "done" {
		t.Errorf("status = %q", first.Status)
	}
	j.Wait()
}

// --- GET on a fresh repository: no tour, no generation, not indexed ---

func TestGetOnboardingTourFreshRepo(t *testing.T) {
	f := newFixture(t)
	repo := f.insertRepo(t, f.workspace, "o/fresh")
	j := jobs.New(f.db, quiet)
	t.Cleanup(j.Close)
	f.onboarding = onboarding.New(onboarding.Config{
		DB: f.db, Jobs: j, LLM: func(string) (review.LLM, error) { return fakeOnboardingLLM{}, nil }, Log: quiet,
	})

	assertJSON(t, f.get(t, "/repos/"+repo.String()+"/onboarding"), http.StatusOK, `{
		"index": {"state": "not_indexed", "commit_sha": null, "files_indexed": 0, "partial": false, "code_index_empty": false},
		"tour": null, "generation": null}`)
}

// --- GET on the seeded demo repository: the seeded tour, not indexed, no generation ---

func TestGetOnboardingTourSeed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ws, _, err := seed.Run(ctx, f.db)
	if err != nil {
		t.Fatal(err)
	}
	if ws != f.workspace {
		t.Fatalf("seed.Run made a different workspace than the fixture's")
	}
	var repoID string
	if err := f.db.QueryRow(ctx, `SELECT id FROM repos WHERE workspace_id = $1 AND full_name = 'acme/payments-api'`, f.workspace).
		Scan(&repoID); err != nil {
		t.Fatal(err)
	}
	j := jobs.New(f.db, quiet)
	t.Cleanup(j.Close)
	f.onboarding = onboarding.New(onboarding.Config{
		DB: f.db, Jobs: j, LLM: func(string) (review.LLM, error) { return fakeOnboardingLLM{}, nil }, Log: quiet,
	})

	// No clone, no index: Regenerate stays disabled and Generate is refused.
	assertJSON(t, f.send(t, http.MethodPost, "/repos/"+repoID+"/onboarding", ``), http.StatusConflict,
		`{"error": {"code": "repo_not_indexed", "message": "Repository is not indexed"}}`)

	var body struct {
		Index struct {
			State string `json:"state"`
		} `json:"index"`
		Tour struct {
			CommitSha      string  `json:"commit_sha"`
			FilesIndexed   int32   `json:"files_indexed"`
			IndexStatus    string  `json:"index_status"`
			CodeIndexEmpty bool    `json:"code_index_empty"`
			Provider       string  `json:"provider"`
			Model          string  `json:"model"`
			CostUsd        float64 `json:"cost_usd"`
			Sections       []struct {
				Kind   string `json:"kind"`
				Status string `json:"status"`
			} `json:"sections"`
		} `json:"tour"`
		Generation *struct{} `json:"generation"`
	}
	decode(t, f.get(t, "/repos/"+repoID+"/onboarding"), http.StatusOK, &body)

	if body.Index.State != "not_indexed" {
		t.Errorf("index.state = %q, want not_indexed", body.Index.State)
	}
	if body.Generation != nil {
		t.Errorf("generation = %+v, want null", body.Generation)
	}
	if body.Tour.CommitSha != "a1b2c3d4e5f6" || body.Tour.FilesIndexed != 42 || body.Tour.IndexStatus != "full" ||
		body.Tour.CodeIndexEmpty || body.Tour.Provider != "openrouter" || body.Tour.Model != "deepseek/deepseek-v4-flash" ||
		body.Tour.CostUsd != 0.0042 {
		t.Errorf("tour = %+v", body.Tour)
	}
	if len(body.Tour.Sections) != 5 {
		t.Fatalf("%d sections, want 5", len(body.Tour.Sections))
	}
	for _, s := range body.Tour.Sections {
		if s.Status != "done" {
			t.Errorf("section %s status = %q, want done", s.Kind, s.Status)
		}
	}
}

// jsTimeShape matches the "JS time format" root CLAUDE.md names: an ISO
// string with millisecond precision, UTC ("Z"), the same format jsTime
// (server.go) produces and the contract's created_at/generated_at fields
// are typed z.string() for (client/src/vendor/shared/contracts/onboarding.ts).
var jsTimeShape = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)

// --- wire-shape: a generation's created_at/finished_at/reason, and a
// stored tour section's segment "type" tag, is_dir and removed_refs ---
// (client/src/vendor/shared/contracts/onboarding.ts OnboardingGeneration,
// TourSummarySegment, TourSection).

func TestOnboardingTourWireShape_Generation(t *testing.T) {
	f, j, repo := withOnboarding(t)

	var gen map[string]any
	decode(t, f.send(t, http.MethodPost, "/repos/"+repo+"/onboarding", ``), http.StatusAccepted, &gen)

	createdAt, ok := gen["created_at"].(string)
	if !ok || !jsTimeShape.MatchString(createdAt) {
		t.Errorf("generation.created_at = %v, want a JS time string (contracts/onboarding.ts OnboardingGeneration.created_at)", gen["created_at"])
	}
	if fa, ok := gen["finished_at"]; !ok || fa != nil {
		t.Errorf("generation.finished_at = %v, want null for a just-started generation", fa)
	}

	sections, ok := gen["sections"].([]any)
	if !ok || len(sections) == 0 {
		t.Fatalf("generation.sections = %v, want a non-empty array", gen["sections"])
	}
	for _, raw := range sections {
		sec, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("section = %v, want an object", raw)
		}
		if reason, ok := sec["reason"]; !ok || reason != nil {
			t.Errorf("section.reason = %v, want null before any section has failed", reason)
		}
		if _, ok := sec["kind"]; !ok {
			t.Errorf("section = %+v, missing kind", sec)
		}
		if _, ok := sec["status"]; !ok {
			t.Errorf("section = %+v, missing status", sec)
		}
	}

	j.Wait()
}

func TestOnboardingTourWireShape_SeededTourSections(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, _, err := seed.Run(ctx, f.db); err != nil {
		t.Fatal(err)
	}
	var repoID string
	if err := f.db.QueryRow(ctx, `SELECT id FROM repos WHERE workspace_id = $1 AND full_name = 'acme/payments-api'`, f.workspace).
		Scan(&repoID); err != nil {
		t.Fatal(err)
	}
	j := jobs.New(f.db, quiet)
	t.Cleanup(j.Close)
	f.onboarding = onboarding.New(onboarding.Config{
		DB: f.db, Jobs: j, LLM: func(string) (review.LLM, error) { return fakeOnboardingLLM{}, nil }, Log: quiet,
	})

	var body struct {
		Tour struct {
			GeneratedAt string           `json:"generated_at"`
			Sections    []map[string]any `json:"sections"`
		} `json:"tour"`
	}
	decode(t, f.get(t, "/repos/"+repoID+"/onboarding"), http.StatusOK, &body)

	if !jsTimeShape.MatchString(body.Tour.GeneratedAt) {
		t.Errorf("tour.generated_at = %q, want a JS time string (contracts/onboarding.ts OnboardingTour.generated_at)", body.Tour.GeneratedAt)
	}

	var sawTextSegment, sawPathSegment, sawRemovedRefs, sawIsDir bool
	for _, sec := range body.Tour.Sections {
		if _, ok := sec["removed_refs"]; ok {
			sawRemovedRefs = true
		}
		content, ok := sec["content"].(map[string]any)
		if !ok {
			continue
		}
		if sec["kind"] == "architecture" {
			summary, _ := content["summary"].([]any)
			for _, raw := range summary {
				seg, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				switch seg["type"] {
				case "text":
					sawTextSegment = true
				case "path":
					sawPathSegment = true
					if _, ok := seg["is_dir"]; ok {
						sawIsDir = true
					}
				default:
					t.Errorf("summary segment = %+v, want type \"text\" or \"path\"", seg)
				}
			}
		}
	}
	if !sawTextSegment || !sawPathSegment {
		t.Errorf("architecture summary: text segment = %v, path segment = %v, want both present (TourSummarySegment's \"type\" tag)", sawTextSegment, sawPathSegment)
	}
	if !sawIsDir {
		t.Error("no path segment carried is_dir (TourPathRef.is_dir)")
	}
	if !sawRemovedRefs {
		t.Error("no section carried removed_refs (TourSection.removed_refs)")
	}
}

// TestOnboardingTourWireShape_MarshalsAsJSONObject is a cheap, dependency-free
// cross-check: encoding/json on the response bytes must parse as a plain
// object, matching the contract's top-level shape (no envelope wrapping).
func TestOnboardingTourWireShape_MarshalsAsJSONObject(t *testing.T) {
	f, _, repo := withOnboarding(t)
	res := f.get(t, "/repos/"+repo+"/onboarding")
	var raw json.RawMessage
	decode(t, res, http.StatusOK, &raw)
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("GET /repos/{id}/onboarding body is not a JSON object: %v", err)
	}
	for _, key := range []string{"index", "tour", "generation"} {
		if _, ok := obj[key]; !ok {
			t.Errorf("response missing %q (OnboardingTourResponse)", key)
		}
	}
}
