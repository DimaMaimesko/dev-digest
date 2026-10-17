package onboarding_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/jobs"
	"github.com/DimaMaimesko/dev-digest/api/internal/onboarding"
	"github.com/DimaMaimesko/dev-digest/api/internal/pgtest"
	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

// fakeLLM is a hand-written review.LLM. It answers every schema with a valid
// answer grounded against the fixture's repository, unless told to fail a
// schema or to block until its context ends.
type fakeLLM struct {
	mu       sync.Mutex
	calls    map[string]int
	requests []review.JSONRequest
	fail     map[string]bool // a schema that fails every attempt
	block    bool            // every call waits for ctx to end
}

func newFakeLLM() *fakeLLM {
	return &fakeLLM{calls: map[string]int{}, fail: map[string]bool{}}
}

func (f *fakeLLM) CompleteJSON(ctx context.Context, req review.JSONRequest) (review.JSONResponse, error) {
	f.mu.Lock()
	f.calls[req.SchemaName]++
	f.requests = append(f.requests, req)
	block := f.block
	fail := f.fail[req.SchemaName]
	f.mu.Unlock()

	if block {
		<-ctx.Done()
		return review.JSONResponse{}, ctx.Err()
	}
	if fail {
		return review.JSONResponse{TokensIn: 1, TokensOut: 1}, errors.New("the model refused")
	}
	return review.JSONResponse{Text: validAnswer(req.SchemaName), TokensIn: 5, TokensOut: 5}, nil
}

func (f *fakeLLM) totalCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		n += c
	}
	return n
}

func (f *fakeLLM) allRequests() []review.JSONRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]review.JSONRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

// validAnswer is a schema's answer, grounded against the fixture
// repository's tree (README.md, go.mod, cmd/main.go).
func validAnswer(schema string) string {
	switch schema {
	case "Architecture":
		return `{"summary":"ok","nodes":[],"edges":[]}`
	case "CriticalPaths":
		return `{"items":[{"path":"README.md","reason":"r"}]}`
	case "RunLocally":
		return `{"steps":[{"command":"go build ./...","comment":null}]}`
	case "ReadingPath":
		return `{"items":[{"path":"README.md","reason":"a"},{"path":"go.mod","reason":"b"},{"path":"cmd/main.go","reason":"c"}]}`
	case "FirstTasks":
		return `{"tasks":[{"title":"t","description":"d","size":"small","paths":["README.md"],"todo":null}]}`
	default:
		return "{}"
	}
}

// fixture is a workspace with one indexed repository, backed by a real
// temporary git clone, and a Store wired to a fakeLLM.
type fixture struct {
	t         *testing.T
	db        *pgxpool.Pool
	workspace uuid.UUID
	repoID    uuid.UUID
	cloneDir  string
	commit    string
	jobsR     *jobs.Runner
	llm       *fakeLLM
	store     *onboarding.Store
}

const secretEnvValue = "sk-leak-me-not"
const secretKeyValue = "LEAK-ME-NOT-PRIVATE-KEY"

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := pgtest.New(t)
	ctx := context.Background()

	var workspace uuid.UUID
	if err := db.QueryRow(ctx, `INSERT INTO workspaces (name) VALUES ('w') RETURNING id`).Scan(&workspace); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# demo\n")
	writeFile(t, dir, "go.mod", "module demo\n\ngo 1.26\n")
	writeFile(t, dir, "cmd/main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, dir, ".env", "SECRET_KEY="+secretEnvValue+"\n")
	writeFile(t, dir, "id_rsa", "-----BEGIN PRIVATE KEY-----\n"+secretKeyValue+"\n-----END PRIVATE KEY-----\n")
	commit := initGitRepo(t, dir)

	var repoID uuid.UUID
	if err := db.QueryRow(ctx, `INSERT INTO repos (workspace_id, owner, name, full_name, clone_path)
		VALUES ($1, 'acme', 'demo', 'acme/demo', $2) RETURNING id`, workspace, dir).Scan(&repoID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO repo_index_state (repo_id, last_indexed_sha, indexer_version, status, files_indexed)
		VALUES ($1, $2, 1, 'full', 3)`, repoID, commit); err != nil {
		t.Fatal(err)
	}

	jr := jobs.New(db, slog.New(slog.DiscardHandler))
	t.Cleanup(jr.Close)

	llm := newFakeLLM()
	store := onboarding.New(onboarding.Config{
		DB:             db,
		Jobs:           jr,
		LLM:            func(provider string) (review.LLM, error) { return llm, nil },
		Log:            slog.New(slog.DiscardHandler),
		SectionTimeout: 3 * time.Second,
	})

	return &fixture{t: t, db: db, workspace: workspace, repoID: repoID, cloneDir: dir, commit: commit, jobsR: jr, llm: llm, store: store}
}

func (f *fixture) countRuns(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(context.Background(), `SELECT count(*) FROM onboarding_runs WHERE repo_id = $1`, f.repoID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *fixture) runStatus(t *testing.T, id uuid.UUID) (status string, errMsg *string) {
	t.Helper()
	if err := f.db.QueryRow(context.Background(), `SELECT status, error FROM onboarding_runs WHERE id = $1`, id).Scan(&status, &errMsg); err != nil {
		t.Fatal(err)
	}
	return status, errMsg
}

// waitForRunStatus polls until the repository's latest run has one of
// wantAny's statuses, or fails the test after a few seconds.
func waitForRunStatus(t *testing.T, db *pgxpool.Pool, repoID uuid.UUID, wantAny ...string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		err := db.QueryRow(context.Background(),
			`SELECT status FROM onboarding_runs WHERE repo_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, repoID).Scan(&status)
		if err == nil {
			for _, want := range wantAny {
				if status == want {
					return status
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run never reached any of %v", wantAny)
	return ""
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// initGitRepo commits every file written so far in dir and returns the
// commit's sha.
func initGitRepo(t *testing.T, dir string) string {
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

// --- AC-4: no completed index ---

func TestStart_NotFound(t *testing.T) {
	f := newFixture(t)
	if _, err := f.store.Start(context.Background(), f.workspace, uuid.New()); !errors.Is(err, onboarding.ErrNotFound) {
		t.Errorf("Start of an unknown repository = %v, want ErrNotFound", err)
	}
}

func TestStart_NotIndexed(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		setup func(f *fixture)
	}{
		{"no clone, no index row", func(f *fixture) {
			f.db.Exec(ctx, `UPDATE repos SET clone_path = NULL WHERE id = $1`, f.repoID)
			f.db.Exec(ctx, `DELETE FROM repo_index_state WHERE repo_id = $1`, f.repoID)
		}},
		{"index row without a clone", func(f *fixture) {
			f.db.Exec(ctx, `UPDATE repos SET clone_path = NULL WHERE id = $1`, f.repoID)
		}},
		{"degraded index", func(f *fixture) {
			f.db.Exec(ctx, `UPDATE repo_index_state SET status = 'degraded' WHERE repo_id = $1`, f.repoID)
		}},
		{"failed index", func(f *fixture) {
			f.db.Exec(ctx, `UPDATE repo_index_state SET status = 'failed' WHERE repo_id = $1`, f.repoID)
		}},
		{"an index job is still running", func(f *fixture) {
			f.db.Exec(ctx, `DELETE FROM repo_index_state WHERE repo_id = $1`, f.repoID)
			payload := []byte(`{"repoId":"` + f.repoID.String() + `"}`)
			f.db.Exec(ctx, `INSERT INTO jobs (workspace_id, kind, payload, status) VALUES ($1, 'repo-intel-index', $2, 'running')`, f.workspace, payload)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			tt.setup(f)
			_, err := f.store.Start(ctx, f.workspace, f.repoID)
			if !errors.Is(err, onboarding.ErrNotIndexed) {
				t.Errorf("Start = %v, want ErrNotIndexed", err)
			}
			if n := f.countRuns(t); n != 0 {
				t.Errorf("%d runs inserted, want 0", n)
			}
		})
	}
}

// --- AC-33: a model with no configured client ---

func TestStart_ConfigError(t *testing.T) {
	f := newFixture(t)
	calls := 0
	f.store = onboarding.New(onboarding.Config{
		DB:   f.db,
		Jobs: f.jobsR,
		LLM: func(provider string) (review.LLM, error) {
			calls++
			return nil, errors.New("OPENROUTER_API_KEY is not configured")
		},
		Log: slog.New(slog.DiscardHandler),
	})

	_, err := f.store.Start(context.Background(), f.workspace, f.repoID)
	var cfgErr *onboarding.ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("Start = %v, want *ConfigError", err)
	}
	if cfgErr.Provider != "openrouter" {
		t.Errorf("ConfigError.Provider = %q, want %q", cfgErr.Provider, "openrouter")
	}
	if n := f.countRuns(t); n != 0 {
		t.Errorf("%d runs inserted, want 0", n)
	}
	if f.llm.totalCalls() != 0 {
		t.Errorf("%d model calls, want 0", f.llm.totalCalls())
	}
}

// --- AC-6: two concurrent Starts make exactly one run, one job ---

func TestStart_Concurrent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	gens := make([]uuid.UUID, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			gen, err := f.store.Start(ctx, f.workspace, f.repoID)
			gens[i], errs[i] = gen.ID, err
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if gens[0] != gens[1] {
		t.Errorf("two different generations: %v, %v", gens[0], gens[1])
	}
	if n := f.countRuns(t); n != 1 {
		t.Errorf("%d runs, want 1", n)
	}

	waitForRunStatus(t, f.db, f.repoID, "done", "failed")
	f.jobsR.Wait()

	if n := f.countRuns(t); n != 1 {
		t.Errorf("%d runs after the job ran, want 1", n)
	}
	var jobCount int
	f.db.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind = 'onboarding-tour'`).Scan(&jobCount)
	if jobCount != 1 {
		t.Errorf("%d onboarding-tour jobs, want 1", jobCount)
	}
	if f.llm.totalCalls() != 5 {
		t.Errorf("%d model calls, want 5 (one per section)", f.llm.totalCalls())
	}
}

// --- AC-35/AC-36: partial success replaces the tour; all-failed keeps it ---

func TestFinish_PartialSuccessThenAllFailed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	gen1, err := f.store.Start(ctx, f.workspace, f.repoID)
	if err != nil {
		t.Fatal(err)
	}
	waitForRunStatus(t, f.db, f.repoID, "done", "failed")
	f.jobsR.Wait()

	status, _ := f.runStatus(t, gen1.ID)
	if status != "done" {
		t.Fatalf("first run status = %s, want done", status)
	}
	var firstGeneratedAt time.Time
	if err := f.db.QueryRow(ctx, `SELECT generated_at FROM onboarding WHERE repo_id = $1`, f.repoID).Scan(&firstGeneratedAt); err != nil {
		t.Fatalf("no tour after an all-valid generation: %v", err)
	}

	// One section fails every attempt; the rest still succeed (AC-35:
	// partial success still replaces the tour).
	f.llm.fail["RunLocally"] = true
	gen2, err := f.store.Start(ctx, f.workspace, f.repoID)
	if err != nil {
		t.Fatal(err)
	}
	waitForRunStatus(t, f.db, f.repoID, "done", "failed")
	f.jobsR.Wait()
	status, _ = f.runStatus(t, gen2.ID)
	if status != "done" {
		t.Fatalf("partially failed run status = %s, want done", status)
	}
	var secondGeneratedAt time.Time
	if err := f.db.QueryRow(ctx, `SELECT generated_at FROM onboarding WHERE repo_id = $1`, f.repoID).Scan(&secondGeneratedAt); err != nil {
		t.Fatal(err)
	}
	if !secondGeneratedAt.After(firstGeneratedAt) {
		t.Error("the tour was not replaced by the partially-successful generation")
	}

	// Every section fails: the run fails, and the previous (partial) tour
	// is kept untouched (AC-36).
	for _, schema := range []string{"Architecture", "CriticalPaths", "RunLocally", "ReadingPath", "FirstTasks"} {
		f.llm.fail[schema] = true
	}
	gen3, err := f.store.Start(ctx, f.workspace, f.repoID)
	if err != nil {
		t.Fatal(err)
	}
	waitForRunStatus(t, f.db, f.repoID, "done", "failed")
	f.jobsR.Wait()
	status, errMsg := f.runStatus(t, gen3.ID)
	if status != "failed" || errMsg == nil || *errMsg != "all sections failed" {
		t.Errorf("all-failed run = %s, %v, want failed, \"all sections failed\"", status, errMsg)
	}
	var thirdGeneratedAt time.Time
	f.db.QueryRow(ctx, `SELECT generated_at FROM onboarding WHERE repo_id = $1`, f.repoID).Scan(&thirdGeneratedAt)
	if !thirdGeneratedAt.Equal(secondGeneratedAt) {
		t.Error("the tour changed after an all-failed generation")
	}
}

// --- AC-37: deleting the repository while a generation is blocked stores no tour ---

func TestFinish_RepoDeletedMidGeneration(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.llm.block = true

	gen, err := f.store.Start(ctx, f.workspace, f.repoID)
	if err != nil {
		t.Fatal(err)
	}
	waitForRunStatus(t, f.db, f.repoID, "running")

	if _, err := f.db.Exec(ctx, `DELETE FROM repos WHERE id = $1`, f.repoID); err != nil {
		t.Fatal(err)
	}
	f.jobsR.Close() // cancels the blocked call
	f.jobsR.Wait()

	var n int
	f.db.QueryRow(ctx, `SELECT count(*) FROM onboarding WHERE repo_id = $1`, f.repoID).Scan(&n)
	if n != 0 {
		t.Errorf("%d tour rows after the repository was deleted, want 0", n)
	}
	f.db.QueryRow(ctx, `SELECT count(*) FROM onboarding_runs WHERE id = $1`, gen.ID).Scan(&n)
	if n != 0 {
		t.Errorf("the run row was not cascaded away with its repository")
	}
}

// --- AC-11: the server stopping mid-generation fails the run and keeps the
// previous tour; FailStale flips a leftover running row at the next start ---

func TestFinish_ServerStopsDuringGeneration(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	gen1, err := f.store.Start(ctx, f.workspace, f.repoID)
	if err != nil {
		t.Fatal(err)
	}
	waitForRunStatus(t, f.db, f.repoID, "done", "failed")
	f.jobsR.Wait()
	if status, _ := f.runStatus(t, gen1.ID); status != "done" {
		t.Fatalf("first run status = %s, want done", status)
	}
	var firstGeneratedAt time.Time
	f.db.QueryRow(ctx, `SELECT generated_at FROM onboarding WHERE repo_id = $1`, f.repoID).Scan(&firstGeneratedAt)

	f.llm.block = true
	gen2, err := f.store.Start(ctx, f.workspace, f.repoID)
	if err != nil {
		t.Fatal(err)
	}
	waitForRunStatus(t, f.db, f.repoID, "running")
	f.jobsR.Close()
	f.jobsR.Wait()

	status, errMsg := f.runStatus(t, gen2.ID)
	if status != "failed" || errMsg == nil || *errMsg != "the server stopped during generation" {
		t.Errorf("run after a shutdown = %s, %v, want failed, \"the server stopped during generation\"", status, errMsg)
	}
	var generatedAt time.Time
	f.db.QueryRow(ctx, `SELECT generated_at FROM onboarding WHERE repo_id = $1`, f.repoID).Scan(&generatedAt)
	if !generatedAt.Equal(firstGeneratedAt) {
		t.Error("the previous tour was touched by the cancelled generation")
	}

	// A run a crash left running is flipped the same way at the next start.
	var leftover uuid.UUID
	if err := f.db.QueryRow(ctx, `INSERT INTO onboarding_runs (repo_id, workspace_id, status, commit_sha, files_indexed,
		index_status, code_index_empty, provider, model, sections)
		VALUES ($1, $2, 'running', $3, 1, 'full', false, 'openrouter', 'm', '[]') RETURNING id`,
		f.repoID, f.workspace, f.commit).Scan(&leftover); err != nil {
		t.Fatal(err)
	}
	n, err := f.store.FailStale(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Errorf("FailStale flipped %d rows, want >= 1", n)
	}
	status, errMsg = f.runStatus(t, leftover)
	if status != "failed" || errMsg == nil || *errMsg != "the server stopped during generation" {
		t.Errorf("leftover run after FailStale = %s, %v", status, errMsg)
	}
}

// --- AC-40: secret file contents never reach a model ---

func TestGenerate_NeverSendsSecretContents(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.store.Start(ctx, f.workspace, f.repoID); err != nil {
		t.Fatal(err)
	}
	waitForRunStatus(t, f.db, f.repoID, "done", "failed")
	f.jobsR.Wait()

	for _, req := range f.llm.allRequests() {
		if strings.Contains(req.System, secretEnvValue) || strings.Contains(req.System, secretKeyValue) {
			t.Error("a secret leaked into the system prompt")
		}
		for _, m := range req.Messages {
			if strings.Contains(m.Content, secretEnvValue) {
				t.Errorf(".env contents leaked into a request: %q", m.Content)
			}
			if strings.Contains(m.Content, secretKeyValue) {
				t.Errorf("id_rsa contents leaked into a request: %q", m.Content)
			}
		}
	}
}

// --- AC-27: reading the clone never changes its work tree ---

func TestGather_NeverModifiesTheClone(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.store.Start(ctx, f.workspace, f.repoID); err != nil {
		t.Fatal(err)
	}
	waitForRunStatus(t, f.db, f.repoID, "done", "failed")
	f.jobsR.Wait()

	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = f.cloneDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("the clone's work tree changed: %s", out)
	}
}
