package runner_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/pgtest"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
	"github.com/DimaMaimesko/dev-digest/api/internal/review"
	"github.com/DimaMaimesko/dev-digest/api/internal/runner"
)

// The pull request's saved patch adds line 11 of src/config.ts.
const patch = "@@ -10,3 +10,4 @@\n   port: 3000,\n+  stripeKey: \"sk_live_xxx\",\n   redisUrl: x,"

// A review with a finding on line 11, which the diff shows, and one on line
// 999, which grounding drops.
const reviewJSON = `{"verdict": "request_changes", "summary": "A live key.", "score": 42, "findings": [
	{"id": "a", "severity": "CRITICAL", "category": "security", "title": "Hardcoded key", "file": "src/config.ts",
	 "start_line": 11, "end_line": 11, "rationale": "A live key.", "suggestion": "Use env.", "confidence": 0.9, "kind": "finding"},
	{"id": "b", "severity": "WARNING", "category": "bug", "title": "Phantom", "file": "src/config.ts",
	 "start_line": 999, "end_line": 999, "rationale": "Not in the diff.", "confidence": 0.5, "kind": "finding"}]}`

// fakeLLM answers every request with answer. With a gate, a call waits for
// it to close first, or for its context, unless ignoreCtx is set, like a
// model client that doesn't stop when asked.
type fakeLLM struct {
	answer    string
	err       error
	gate      chan struct{}
	ignoreCtx bool
	cost      *float64      // each answer's cost; nil like a provider that doesn't say
	started   chan struct{} // gets a value when a call starts

	mu       sync.Mutex
	requests []review.JSONRequest
}

func (f *fakeLLM) CompleteJSON(ctx context.Context, req review.JSONRequest) (review.JSONResponse, error) {
	if err := ctx.Err(); err != nil {
		// A real HTTP-based client fails once its request's context is
		// already done; model that here too, so a fake with no gate still
		// reports a cancel or timeout instead of answering regardless.
		return review.JSONResponse{}, err
	}
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()
	if f.started != nil {
		f.started <- struct{}{}
	}
	if f.gate != nil {
		if f.ignoreCtx {
			<-f.gate
		} else {
			select {
			case <-f.gate:
			case <-ctx.Done():
				return review.JSONResponse{}, ctx.Err()
			}
		}
	}
	return review.JSONResponse{Text: f.answer, TokensIn: 100, TokensOut: 20, CostUSD: f.cost}, f.err
}

func cost(f float64) *float64 { return &f }

// fixture is a database with a pull request #7 of o/n that changes
// src/config.ts, and a runner whose model is llm.
type fixture struct {
	db       *pgxpool.Pool
	q        *postgres.Queries
	pull     postgres.PullRequest
	repo     postgres.Repo
	llm      *fakeLLM
	cloneDir string
	runner   *runner.Runner
}

func newFixture(t *testing.T, llm *fakeLLM, cfg func(*runner.Config)) *fixture {
	t.Helper()
	ctx := context.Background()
	f := &fixture{db: pgtest.New(t), llm: llm, cloneDir: t.TempDir()}
	f.q = postgres.New(f.db)
	var workspace, repo, pull uuid.UUID
	f.scan(t, &workspace, `INSERT INTO workspaces (name) VALUES ('default') RETURNING id`)
	f.scan(t, &repo, `INSERT INTO repos (workspace_id, owner, name, full_name) VALUES ($1, 'o', 'n', 'o/n') RETURNING id`, workspace)
	f.scan(t, &pull, `INSERT INTO pull_requests (workspace_id, repo_id, number, title, author, branch, base, head_sha, status, body)
		VALUES ($1, $2, 7, 'Rate limits', 'ann', 'feat', 'main', 'abc123', 'open', 'Adds limits.') RETURNING id`, workspace, repo)
	f.exec(t, `INSERT INTO pr_files (pr_id, path, additions, deletions, patch) VALUES ($1, 'src/config.ts', 1, 0, $2), ($1, 'logo.png', 0, 0, NULL)`, pull, patch)
	var err error
	if f.repo, err = f.q.GetRepo(ctx, postgres.GetRepoParams{WorkspaceID: workspace, ID: repo}); err != nil {
		t.Fatal(err)
	}
	if f.pull, err = f.q.GetPull(ctx, postgres.GetPullParams{WorkspaceID: workspace, ID: pull}); err != nil {
		t.Fatal(err)
	}
	c := runner.Config{
		DB:        f.db,
		LLM:       func(string) (review.LLM, error) { return llm, nil },
		CloneDir:  f.cloneDir,
		RepoIntel: true,
		Log:       slog.New(slog.DiscardHandler),
	}
	if cfg != nil {
		cfg(&c)
	}
	f.runner = runner.New(c)
	t.Cleanup(f.runner.Close)
	return f
}

func (f *fixture) scan(t *testing.T, dest any, sql string, args ...any) {
	t.Helper()
	if err := f.db.QueryRow(context.Background(), sql, args...).Scan(dest); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func (f *fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.db.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// agent adds an agent; set changes its columns with SQL, such as
// "ci_fail_on = 'warning'".
func (f *fixture) agent(t *testing.T, name string, set ...string) postgres.Agent {
	t.Helper()
	var id uuid.UUID
	f.scan(t, &id, `INSERT INTO agents (workspace_id, name, provider, model, system_prompt)
		VALUES ($1, $2, 'openai', 'gpt-test', 'You review code.') RETURNING id`, f.pull.WorkspaceID, name)
	for _, s := range set {
		f.exec(t, `UPDATE agents SET `+s+` WHERE id = $1`, id)
	}
	a, err := f.q.GetAgent(context.Background(), postgres.GetAgentParams{WorkspaceID: f.pull.WorkspaceID, ID: id})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// start starts runs of the agents and returns their IDs.
func (f *fixture) start(t *testing.T, agents ...postgres.Agent) []uuid.UUID {
	t.Helper()
	started, err := f.runner.Start(context.Background(), f.pull, f.repo, agents)
	if err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	for i, s := range started {
		if s.AgentID != agents[i].ID || s.AgentName != agents[i].Name {
			t.Errorf("started %+v for agent %s", s, agents[i].Name)
		}
		ids = append(ids, s.RunID)
	}
	return ids
}

// run is what the database holds about a run.
type run struct {
	Status, Error, Grounding                  string
	DurationMs, TokensIn, TokensOut, Findings int
	Score, Blockers                           *int
	CostUSD                                   *float64
	Reviews, SavedFindings                    int
	Trace                                     map[string]any
}

func (f *fixture) run(t *testing.T, id uuid.UUID) run {
	t.Helper()
	var r run
	var errText, grounding *string
	var ms, in, out, findings *int
	err := f.db.QueryRow(context.Background(), `SELECT status, error, grounding, duration_ms, tokens_in, tokens_out, findings_count, score, blockers, cost_usd
		FROM agent_runs WHERE id = $1`, id).Scan(&r.Status, &errText, &grounding, &ms, &in, &out, &findings, &r.Score, &r.Blockers, &r.CostUSD)
	if err != nil {
		t.Fatal(err)
	}
	deref := func(p *int) int {
		if p == nil {
			return -1
		}
		return *p
	}
	if errText != nil {
		r.Error = *errText
	}
	if grounding != nil {
		r.Grounding = *grounding
	}
	r.DurationMs, r.TokensIn, r.TokensOut, r.Findings = deref(ms), deref(in), deref(out), deref(findings)
	f.scan(t, &r.Reviews, `SELECT count(*) FROM reviews WHERE run_id = $1`, id)
	f.scan(t, &r.SavedFindings, `SELECT count(*) FROM findings f JOIN reviews r ON r.id = f.review_id WHERE r.run_id = $1`, id)
	var trace []byte
	if err := f.db.QueryRow(context.Background(), `SELECT trace FROM run_traces WHERE run_id = $1`, id).Scan(&trace); err == nil {
		json.Unmarshal(trace, &r.Trace)
	}
	return r
}

// traceStats is a saved trace's stats.
func (r run) traceStats() map[string]any {
	stats, _ := r.Trace["stats"].(map[string]any)
	return stats
}

// show prints a cost in a test message: its value, or nil.
func show(c *float64) any {
	if c == nil {
		return nil
	}
	return *c
}

// hasCost reports whether got is want, both possibly nil (unknown).
func hasCost(got, want *float64) bool {
	if got == nil || want == nil {
		return got == want
	}
	return *got == *want
}

// traceLog is the messages of a saved trace's log.
func (r run) traceLog() []string {
	var msgs []string
	lines, _ := r.Trace["log"].([]any)
	for _, l := range lines {
		msgs = append(msgs, l.(map[string]any)["msg"].(string))
	}
	return msgs
}

// liveLog follows a run's live log to its end and returns its messages.
func (f *fixture) liveLog(t *testing.T, id uuid.UUID) []string {
	t.Helper()
	var msgs []string
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	known, err := f.runner.Follow(ctx, id, func(e runner.Event) error {
		if e.Seq != len(msgs)+1 || e.RunID != id.String() {
			t.Errorf("event %+v after %d", e, len(msgs))
		}
		msgs = append(msgs, e.Msg)
		return nil
	})
	if !known || err != nil {
		t.Fatalf("Follow: known %v, err %v", known, err)
	}
	return msgs
}

// hasPrefixes checks that msgs has lines starting with each prefix, in
// order.
func hasPrefixes(t *testing.T, what string, msgs []string, prefixes ...string) {
	t.Helper()
	i := 0
	for _, m := range msgs {
		if i < len(prefixes) && strings.HasPrefix(m, prefixes[i]) {
			i++
		}
	}
	if i < len(prefixes) {
		t.Errorf("%s: no line %q in order in:\n%s", what, prefixes[i], strings.Join(msgs, "\n"))
	}
}

func TestRunSavesTheReview(t *testing.T) {
	f := newFixture(t, &fakeLLM{answer: reviewJSON, cost: cost(0.0125)}, nil)
	// The gate never fails: no blockers, though a finding is kept.
	agent := f.agent(t, "General", "ci_fail_on = 'never'")
	id := f.start(t, agent)[0]
	live := f.liveLog(t, id)
	f.runner.Wait()

	r := f.run(t, id)
	if r.Status != "done" || r.Error != "" || r.Grounding != "1/2 passed" || r.Findings != 1 ||
		r.TokensIn != 100 || r.TokensOut != 20 || r.Reviews != 1 || r.SavedFindings != 1 ||
		r.Score == nil || *r.Score != 65 || // 100 - 35 for the critical finding kept, not the model's 42
		r.Blockers == nil || *r.Blockers != 0 || r.DurationMs < 0 || !hasCost(r.CostUSD, cost(0.0125)) {
		t.Errorf("run = %+v", r)
	}
	if stats := r.traceStats(); stats["cost_usd"] != 0.0125 || stats["tokens_in"] != 100.0 {
		t.Errorf("trace stats = %v", stats)
	}
	var reviewed string
	f.scan(t, &reviewed, `SELECT last_reviewed_sha FROM pull_requests WHERE id = $1`, f.pull.ID)
	if reviewed != "abc123" {
		t.Errorf("last reviewed commit = %q", reviewed)
	}

	steps := []string{
		"Loading PR diff…", "Loading PR diff done (", "Diff ready — 1 changed file(s); starting 1 agent run(s)",
		`Starting review with agent "General" (openai/gpt-test)`, "Resolving openai provider…", "Resolving openai provider done (",
		"Reviewing 1 changed file(s) in one pass", "Reviewing all files in one pass", "all files: 2 candidate finding(s)",
		"Citation grounding: 1/2 passed", "Persisted review ",
	}
	hasPrefixes(t, "live log", live, append(steps, "Run complete — $0.0125; trace persisted")...)
	hasPrefixes(t, "trace log", r.traceLog(), steps...)
	if slices.Contains(r.traceLog(), "Run complete — $0.0125; trace persisted") {
		t.Error("the trace's log has the line announcing it was saved; TS's doesn't")
	}

	// The trace, as the TS server writes it.
	cfg, _ := json.Marshal(r.Trace["config"])
	if string(cfg) != `{"agent":"General","model":"gpt-test","pr":7,"provider":"openai","source":"local","version":"1"}` {
		t.Errorf("trace config = %s", cfg)
	}
	calls, _ := r.Trace["tool_calls"].([]any)
	if len(calls) != 1 || calls[0].(map[string]any)["args"] != "all files" || calls[0].(map[string]any)["meta"] != "single-pass" {
		t.Errorf("tool_calls = %v", calls)
	}
	assembly, _ := r.Trace["prompt_assembly"].(map[string]any)
	if !strings.HasPrefix(assembly["system"].(string), "You review code.") || assembly["skills"] != nil || !strings.Contains(assembly["user"].(string), "sk_live_xxx") {
		t.Errorf("prompt_assembly = %v", assembly)
	}

	// What the model was asked.
	req := f.llm.requests[0]
	if req.Model != "gpt-test" || req.SessionID != "o/n#7:General" || !strings.Contains(req.Messages[0].Content, `Review pull request #7 "Rate limits" by ann.`) ||
		!strings.Contains(req.Messages[0].Content, "Adds limits.") {
		t.Errorf("request: model %q, session %q, user message:\n%s", req.Model, req.SessionID, req.Messages[0].Content)
	}
}

func TestRunFails(t *testing.T) {
	tests := []struct {
		name    string
		llm     *fakeLLM
		llmFor  func(string) (review.LLM, error)
		wantErr string
		logLine string
	}{
		{"no API key", nil, func(string) (review.LLM, error) { return nil, errors.New("OPENAI_API_KEY is not configured") },
			"OPENAI_API_KEY is not configured", "Resolving openai provider failed ("},
		{"the model fails", &fakeLLM{err: errors.New("rate limited")}, nil,
			"ask gpt-test for a review: rate limited", "Run failed: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, tt.llm, func(c *runner.Config) {
				if tt.llmFor != nil {
					c.LLM = tt.llmFor
				}
			})
			id := f.start(t, f.agent(t, "General"))[0]
			f.runner.Wait()
			r := f.run(t, id)
			if r.Status != "failed" || !strings.Contains(r.Error, tt.wantErr) || r.Reviews != 0 || r.Grounding != "0/0 passed" ||
				r.Findings != 0 || r.Score != nil || r.Blockers != nil || r.CostUSD != nil {
				t.Errorf("run = %+v", r)
			}
			if stats := r.traceStats(); stats["cost_usd"] != nil {
				t.Errorf("trace cost = %v, want null: no call reported one", stats["cost_usd"])
			}
			hasPrefixes(t, "trace log", r.traceLog(), "Loading PR diff…", "Starting review", tt.logLine)
			if a := r.Trace["prompt_assembly"].(map[string]any); a["system"] != "You review code." || a["user"] != "" {
				t.Errorf("failure trace prompt_assembly = %v", a)
			}
		})
	}
}

// A failed run keeps what its answered model calls took: they were paid for.
func TestFailedRunKeepsItsUsage(t *testing.T) {
	f := newFixture(t, &fakeLLM{answer: "not a review", cost: cost(0.01)}, nil)
	id := f.start(t, f.agent(t, "General"))[0]
	f.runner.Wait()

	// 1 call + 2 retries, none valid.
	r := f.run(t, id)
	if r.Status != "failed" || r.TokensIn != 300 || r.TokensOut != 60 || !hasCost(r.CostUSD, cost(0.03)) {
		t.Errorf("run = %+v", r)
	}
	if stats := r.traceStats(); stats["tokens_in"] != 300.0 || stats["tokens_out"] != 60.0 || stats["cost_usd"] != 0.03 {
		t.Errorf("trace stats = %v", stats)
	}
}

// Each agent's run is its own: one failing doesn't stop the next.
func TestRunsAreIndependent(t *testing.T) {
	llm := &fakeLLM{answer: reviewJSON}
	f := newFixture(t, llm, func(c *runner.Config) {
		c.LLM = func(provider string) (review.LLM, error) {
			if provider == "anthropic" {
				return nil, errors.New("ANTHROPIC_API_KEY is not configured")
			}
			return llm, nil
		}
	})
	ids := f.start(t, f.agent(t, "First", "provider = 'anthropic'"), f.agent(t, "Second"))
	f.runner.Wait()
	if a, b := f.run(t, ids[0]), f.run(t, ids[1]); a.Status != "failed" || b.Status != "done" || b.Reviews != 1 {
		t.Errorf("first %s, second %s with %d review(s)", a.Status, b.Status, b.Reviews)
	}
	// The shared work is in both logs.
	for _, id := range ids {
		hasPrefixes(t, "trace log", f.run(t, id).traceLog(), "Loading PR diff…", "Diff ready — 1 changed file(s); starting 2 agent run(s)")
	}
}

// The TS server kept saving a review cancelled while the model answered,
// and marked the run done again; after a cancel and a delete it saved a
// review for a run that no longer existed.
func TestCancelDuringTheModelCall(t *testing.T) {
	tests := []struct {
		name      string
		ignoreCtx bool // the model's answer comes anyway
		delete    bool // the run is deleted after the cancel
	}{
		{"the model call stops", false, false},
		{"the model answers anyway", true, false},
		{"the run is deleted, then the model answers", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			llm := &fakeLLM{answer: reviewJSON, gate: make(chan struct{}), ignoreCtx: tt.ignoreCtx, cost: cost(0.02), started: make(chan struct{}, 1)}
			f := newFixture(t, llm, nil)
			id := f.start(t, f.agent(t, "General"))[0]
			<-llm.started
			if err := f.runner.Cancel(context.Background(), f.pull.WorkspaceID, id); err != nil {
				t.Fatal(err)
			}
			if r := f.run(t, id); r.Status != "cancelled" {
				t.Errorf("status right after the cancel: %s", r.Status)
			}
			if tt.delete {
				f.exec(t, `DELETE FROM agent_runs WHERE id = $1`, id)
			}
			close(llm.gate)
			f.runner.Wait()

			var reviews int
			f.scan(t, &reviews, `SELECT count(*) FROM reviews WHERE run_id = $1`, id)
			if reviews != 0 {
				t.Errorf("%d review(s) saved for the cancelled run", reviews)
			}
			if tt.delete {
				return
			}
			r := f.run(t, id)
			if r.Status != "cancelled" || r.Error != "" {
				t.Errorf("run = %+v", r)
			}
			// An answer that came anyway was paid for; a stopped call wasn't answered.
			wantIn, wantCost := 0, (*float64)(nil)
			if tt.ignoreCtx {
				wantIn, wantCost = 100, cost(0.02)
			}
			if r.TokensIn != wantIn || !hasCost(r.CostUSD, wantCost) {
				t.Errorf("usage = %d tokens in, cost %v; want %d and %v", r.TokensIn, show(r.CostUSD), wantIn, show(wantCost))
			}
			if !tt.ignoreCtx {
				hasPrefixes(t, "trace log", r.traceLog(), "Cancellation requested — stopping…", "Run cancelled by user")
			}
		})
	}
}

// A run cancelled before its turn doesn't call the model.
func TestCancelBeforeItsTurn(t *testing.T) {
	llm := &fakeLLM{answer: reviewJSON, gate: make(chan struct{}), started: make(chan struct{}, 2)}
	f := newFixture(t, llm, nil)
	ids := f.start(t, f.agent(t, "First"), f.agent(t, "Second"))
	<-llm.started
	f.runner.Cancel(context.Background(), f.pull.WorkspaceID, ids[1])
	close(llm.gate)
	f.runner.Wait()
	if a, b := f.run(t, ids[0]), f.run(t, ids[1]); a.Status != "done" || b.Status != "cancelled" || len(llm.requests) != 1 {
		t.Errorf("first %s, second %s, %d model call(s)", a.Status, b.Status, len(llm.requests))
	}
}

func TestCancelOnlyInTheWorkspace(t *testing.T) {
	f := newFixture(t, &fakeLLM{answer: reviewJSON}, nil)
	var stale uuid.UUID
	f.scan(t, &stale, `INSERT INTO agent_runs (workspace_id, status) VALUES ($1, 'running') RETURNING id`, f.pull.WorkspaceID)
	var other uuid.UUID
	f.scan(t, &other, `WITH w AS (INSERT INTO workspaces (name) VALUES ('other') RETURNING id)
		INSERT INTO agent_runs (workspace_id, status) SELECT id, 'running' FROM w RETURNING id`)
	for _, id := range []uuid.UUID{stale, other, uuid.New()} {
		if err := f.runner.Cancel(context.Background(), f.pull.WorkspaceID, id); err != nil {
			t.Fatal(err)
		}
	}
	// A run a stopped server left is cancelled; another workspace's isn't.
	if a, b := f.run(t, stale), f.run(t, other); a.Status != "cancelled" || b.Status != "running" {
		t.Errorf("stale run %s, other workspace's %s", a.Status, b.Status)
	}
}

// Closing the runner, as the server does when it stops, ends the runs in
// progress as failed.
func TestClose(t *testing.T) {
	llm := &fakeLLM{answer: reviewJSON, gate: make(chan struct{}), started: make(chan struct{}, 1)}
	f := newFixture(t, llm, nil)
	id := f.start(t, f.agent(t, "General"))[0]
	<-llm.started
	f.runner.Close()
	if r := f.run(t, id); r.Status != "failed" || r.Error != "the server stopped during the run" {
		t.Errorf("run = %+v", r)
	}
}

func TestFailStale(t *testing.T) {
	f := newFixture(t, nil, nil)
	for _, status := range []string{"running", "running", "done"} {
		f.exec(t, `INSERT INTO agent_runs (workspace_id, status) VALUES ($1, $2)`, f.pull.WorkspaceID, status)
	}
	if n, err := f.runner.FailStale(context.Background()); n != 2 || err != nil {
		t.Errorf("FailStale = %d, %v", n, err)
	}
	var running int
	f.scan(t, &running, `SELECT count(*) FROM agent_runs WHERE status = 'running'`)
	if running != 0 {
		t.Errorf("%d still running", running)
	}
}

func TestFollow(t *testing.T) {
	llm := &fakeLLM{answer: reviewJSON, gate: make(chan struct{}), started: make(chan struct{}, 1)}
	f := newFixture(t, llm, nil)
	id := f.start(t, f.agent(t, "General"))[0]
	<-llm.started

	// A follower that joins mid-run gets the earlier events first, then the
	// rest as they come, and stops at the end.
	done := make(chan []string)
	go func() { done <- f.liveLog(t, id) }()
	time.Sleep(50 * time.Millisecond)
	close(llm.gate)
	msgs := <-done
	hasPrefixes(t, "live log", msgs, "Loading PR diff…", "Reviewing all files in one pass", "Run complete; trace persisted")

	// A run this server doesn't know.
	if known, err := f.runner.Follow(context.Background(), uuid.New(), func(runner.Event) error { return nil }); known || err != nil {
		t.Errorf("unknown run: known %v, err %v", known, err)
	}
	// A follower of a finished run gets its log and stops, even with its
	// context done.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if known, err := f.runner.Follow(ctx, id, func(runner.Event) error { return nil }); !known || err != nil {
		t.Errorf("finished run: known %v, err %v", known, err)
	}
}

// A follower whose context ends stops waiting.
func TestFollowerLeaves(t *testing.T) {
	llm := &fakeLLM{answer: reviewJSON, gate: make(chan struct{}), started: make(chan struct{}, 1)}
	f := newFixture(t, llm, nil)
	id := f.start(t, f.agent(t, "General"))[0]
	<-llm.started
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if known, err := f.runner.Follow(ctx, id, func(runner.Event) error { return nil }); !known || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("known %v, err %v", known, err)
	}
	// A send that fails stops it too.
	boom := errors.New("client gone")
	if _, err := f.runner.Follow(context.Background(), id, func(runner.Event) error { return boom }); !errors.Is(err, boom) {
		t.Errorf("err %v", err)
	}
	close(llm.gate)
}

// The diff comes from the clone when it has the commits: here it shows a
// file the saved patches don't have.
func TestDiffFromTheClone(t *testing.T) {
	f := newFixture(t, &fakeLLM{answer: `{"verdict": "approve", "summary": "ok", "score": 90, "findings": []}`}, nil)
	dir := filepath.Join(f.cloneDir, "o", "n")
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	os.MkdirAll(dir, 0o755)
	git("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644)
	git("add", ".")
	git("commit", "-qm", "base")
	git("checkout", "-qb", "feat")
	os.WriteFile(filepath.Join(dir, "from_git.go"), []byte("package a\n\nvar x = 1\n"), 0o644)
	git("add", ".")
	git("commit", "-qm", "feat")
	f.pull.HeadSha = git("rev-parse", "HEAD")

	id := f.start(t, f.agent(t, "General"))[0]
	f.runner.Wait()
	user := f.llm.requests[0].Messages[0].Content
	if !strings.Contains(user, "from_git.go") || strings.Contains(user, "sk_live_xxx") {
		t.Errorf("the prompt's diff isn't the clone's:\n%s", user)
	}
	hasPrefixes(t, "trace log", f.run(t, id).traceLog(), "Diff ready — 1 changed file(s)")
}

// The repo-intel context goes into the prompt when the runner and the agent
// have it on.
func TestRepoIntelContext(t *testing.T) {
	setup := func(t *testing.T, repoIntel bool, agentSet ...string) (*fixture, uuid.UUID) {
		f := newFixture(t, &fakeLLM{answer: reviewJSON}, func(c *runner.Config) { c.RepoIntel = repoIntel })
		f.exec(t, `INSERT INTO repo_index_state (repo_id, last_indexed_sha, indexer_version, status) VALUES ($1, 's', 2, 'full')`, f.repo.ID)
		f.exec(t, `INSERT INTO repo_map_cache (repo_id, commit_sha, token_budget, map_text, token_count) VALUES ($1, 's', 1500, 'src/config.ts: config', 7)`, f.repo.ID)
		f.exec(t, `INSERT INTO file_rank (repo_id, file_path, pagerank, hotness, rank, percentile) VALUES ($1, 'src/config.ts', 0, 0, 0, 95)`, f.repo.ID) // exactly the threshold
		// A clone where src/app.ts calls a function src/config.ts declares.
		dir := filepath.Join(t.TempDir(), "clone")
		os.MkdirAll(filepath.Join(dir, "src"), 0o755)
		os.WriteFile(filepath.Join(dir, "src/config.ts"), []byte("export function loadConfig() {\n  return 1;\n}\n"), 0o644)
		os.WriteFile(filepath.Join(dir, "src/app.ts"), []byte("export function main() {\n  return loadConfig();\n}\n"), 0o644)
		f.exec(t, `UPDATE repos SET clone_path = $2 WHERE id = $1`, f.repo.ID, dir)
		f.repo.ClonePath = &dir
		return f, f.start(t, f.agent(t, "General", agentSet...))[0]
	}

	f, id := setup(t, true)
	f.runner.Wait()
	req := f.llm.requests[0]
	user := req.Messages[0].Content
	for _, want := range []string{"src/config.ts: config", "### src/app.ts\n- `main` — function main()",
		"1 of 1 changed file(s) are in the top 5% most-depended-on (high blast risk) — prioritise their correctness."} {
		if !strings.Contains(user, want) {
			t.Errorf("the prompt lacks %q:\n%s", want, user)
		}
	}
	hasPrefixes(t, "trace log", f.run(t, id).traceLog(),
		"callers digest: 1 caller signature(s) attached", "repo map: 7 token(s) attached (cached=true)", "file rank: 1/1 changed file(s) in top 5%")

	for _, off := range []struct {
		name      string
		repoIntel bool
		agentSet  []string
		logLine   string
	}{
		{"off for the agent", true, []string{"repo_intel = false"}, "Repo intel disabled for this agent — skipping context enrichment"},
		{"off for the server", false, nil, ""},
	} {
		t.Run(off.name, func(t *testing.T) {
			f, id := setup(t, off.repoIntel, off.agentSet...)
			f.runner.Wait()
			user := f.llm.requests[0].Messages[0].Content
			if strings.Contains(user, "src/config.ts: config") || strings.Contains(user, "loadConfig") || strings.Contains(user, "top 5%") {
				t.Errorf("repo-intel context in the prompt:\n%s", user)
			}
			if off.logLine != "" {
				hasPrefixes(t, "trace log", f.run(t, id).traceLog(), off.logLine)
			}
		})
	}
}

// The agent's enabled skills go into the prompt in its order, each under its
// name; disabled, blank and other agents' skills don't.
func TestSkillsInThePrompt(t *testing.T) {
	f := newFixture(t, &fakeLLM{answer: reviewJSON}, nil)
	agent := f.agent(t, "General")
	skill := func(name, body string, enabled bool) uuid.UUID {
		var id uuid.UUID
		f.scan(t, &id, `INSERT INTO skills (workspace_id, name, description, type, source, body, enabled)
			VALUES ($1, $2, '', 'custom', 'manual', $3, $4) RETURNING id`, f.pull.WorkspaceID, name, body, enabled)
		return id
	}
	link := func(skill uuid.UUID, order int) {
		f.exec(t, `INSERT INTO agent_skills (agent_id, skill_id, "order") VALUES ($1, $2, $3)`, agent.ID, skill, order)
	}
	link(skill("rubric", "Check tests.", true), 1)
	link(skill("secret-gate", "Detect sk_live keys.", true), 0)
	link(skill("off", "Disabled.", false), 2)
	link(skill("blank", " \n\t", true), 3)
	skill("unlinked", "Not this agent's.", true)

	id := f.start(t, agent)[0]
	f.runner.Wait()

	want := "## Skills / rules\n## secret-gate\nDetect sk_live keys.\n\n## rubric\nCheck tests.\n\n## Diff to review"
	if user := f.llm.requests[0].Messages[0].Content; !strings.Contains(user, want) {
		t.Errorf("the prompt lacks\n%s\nin:\n%s", want, user)
	}
	r := f.run(t, id)
	assembly, _ := r.Trace["prompt_assembly"].(map[string]any)
	if got := assembly["skills"]; got != "## secret-gate\nDetect sk_live keys.\n\n## rubric\nCheck tests." {
		t.Errorf("trace skills = %q", got)
	}
	hasPrefixes(t, "trace log", r.traceLog(), "skills: 2 attached (secret-gate, rubric)")
}

func TestNoSkills(t *testing.T) {
	f := newFixture(t, &fakeLLM{answer: reviewJSON}, nil)
	id := f.start(t, f.agent(t, "General"))[0]
	f.runner.Wait()

	if user := f.llm.requests[0].Messages[0].Content; strings.Contains(user, "## Skills / rules") {
		t.Errorf("a skills section without skills:\n%s", user)
	}
	r := f.run(t, id)
	if assembly, _ := r.Trace["prompt_assembly"].(map[string]any); assembly["skills"] != nil {
		t.Errorf("trace skills = %q, want null", assembly["skills"])
	}
	hasPrefixes(t, "trace log", r.traceLog(), "skills: none attached")
}
