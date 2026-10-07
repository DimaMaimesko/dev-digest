package runner_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
	"github.com/DimaMaimesko/dev-digest/api/internal/review"
	"github.com/DimaMaimesko/dev-digest/api/internal/runner"
)

// validIntentJSON is a valid answer to the intent model: one statement, one
// in-scope item and one out-of-scope item.
const validIntentJSON = `{"intent": "Add per-client rate limiting to the public API.", "in_scope": ["Token-bucket middleware"], "out_of_scope": ["Authenticated endpoints"]}`

// intentConfig wires llm as the intent model for any provider/model choice.
func intentConfig(llm review.LLM) func(*runner.Config) {
	return func(c *runner.Config) { c.IntentLLM = func(string, string) (review.LLM, error) { return llm, nil } }
}

// intentTraceOf reads a run's trace "intent" object, or nil when there is
// none.
func intentTraceOf(t *testing.T, r run) map[string]any {
	t.Helper()
	v, ok := r.Trace["intent"]
	if !ok || v == nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("trace intent = %#v, want an object", v)
	}
	return m
}

// AC-1, AC-2, AC-30: one request with 3 agents derives intent exactly once,
// every run's trace shows the same intent, its text is in the prompt sent to
// the review model wrapped as untrusted data, and only the trigger run's
// (the first) usage includes the intent call's tokens and cost.
func TestIntentDerivedOncePerRequest(t *testing.T) {
	reviewLLM := &fakeLLM{answer: reviewJSON, cost: cost(0.01)}
	intentLLM := &fakeLLM{answer: validIntentJSON, cost: cost(0.002)}
	f := newFixture(t, reviewLLM, intentConfig(intentLLM))

	agents := []postgres.Agent{f.agent(t, "First"), f.agent(t, "Second"), f.agent(t, "Third")}
	ids := f.start(t, agents...)
	f.runner.Wait()

	if n := len(intentLLM.requests); n != 1 {
		t.Fatalf("intent model called %d time(s), want exactly 1", n)
	}

	var traces []map[string]any
	for i, id := range ids {
		r := f.run(t, id)
		if r.Status != "done" {
			t.Fatalf("run %d: status %s", i, r.Status)
		}
		it := intentTraceOf(t, r)
		if it == nil {
			t.Fatalf("run %d: no intent in trace", i)
		}
		traces = append(traces, it)
	}
	for i, it := range traces[1:] {
		if got, want := it, traces[0]; got["status"] != want["status"] || got["confidence"] != want["confidence"] ||
			got["provider"] != want["provider"] || got["model"] != want["model"] {
			t.Errorf("run %d intent = %v, want the same as run 0's %v", i+1, got, want)
		}
	}
	if traces[0]["status"] != "derived" {
		t.Errorf("status = %v, want derived", traces[0]["status"])
	}

	// AC-2: the rendered intent is in the user message, wrapped as untrusted
	// data under the "pr-intent" source.
	user := reviewLLM.requests[0].Messages[0].Content
	if !strings.Contains(user, `<untrusted source="pr-intent">`) || !strings.Contains(user, "Token-bucket middleware") {
		t.Errorf("the review prompt lacks the intent block:\n%s", user)
	}
	assembly, _ := f.run(t, ids[0]).Trace["prompt_assembly"].(map[string]any)
	if got, _ := assembly["intent"].(string); !strings.Contains(got, "Token-bucket middleware") {
		t.Errorf("prompt_assembly.intent = %q", got)
	}

	// AC-30: the trigger run (the first) paid for the intent call on top of
	// its own review; the others didn't.
	trigger, other := f.run(t, ids[0]), f.run(t, ids[1])
	if trigger.TokensIn != 200 || !hasCost(trigger.CostUSD, cost(0.012)) {
		t.Errorf("trigger run usage = %d tokens, cost %v; want 200 and 0.012", trigger.TokensIn, show(trigger.CostUSD))
	}
	if other.TokensIn != 100 || !hasCost(other.CostUSD, cost(0.01)) {
		t.Errorf("other run usage = %d tokens, cost %v; want 100 and 0.01 (no intent cost)", other.TokensIn, show(other.CostUSD))
	}
	if stats := trigger.traceStats(); stats["tokens_in"] != 200.0 || stats["cost_usd"] != 0.012 {
		t.Errorf("trigger trace stats = %v", stats)
	}

	// AC-24: exactly one "intent:" live-log line per run.
	for i, id := range ids {
		n := 0
		for _, msg := range f.run(t, id).traceLog() {
			if strings.HasPrefix(msg, "intent:") {
				n++
			}
		}
		if n != 1 {
			t.Errorf("run %d: %d \"intent:\" line(s), want exactly 1", i, n)
		}
	}
}

// AC-16: a saved feature_models.review_intent choice names the provider and
// model intentFor asks for, instead of the Go default.
func TestIntentUsesTheSavedModelChoice(t *testing.T) {
	intentLLM := &fakeLLM{answer: validIntentJSON}
	var gotProvider, gotModel string
	f := newFixture(t, &fakeLLM{answer: reviewJSON}, func(c *runner.Config) {
		c.IntentLLM = func(provider, model string) (review.LLM, error) {
			gotProvider, gotModel = provider, model
			return intentLLM, nil
		}
	})
	f.exec(t, `INSERT INTO settings (workspace_id, user_id, key, value) VALUES
		($1, NULL, 'feature_models', '{"review_intent": {"provider": "openai", "model": "gpt-intent"}}')`, f.pull.WorkspaceID)

	f.start(t, f.agent(t, "General"))
	f.runner.Wait()

	if gotProvider != "openai" || gotModel != "gpt-intent" {
		t.Errorf("asked for %s/%s, want openai/gpt-intent", gotProvider, gotModel)
	}
}

// AC-5: a derived intent's stored row has every field set.
func TestIntentStoresEveryField(t *testing.T) {
	f := newFixture(t, &fakeLLM{answer: reviewJSON}, intentConfig(&fakeLLM{answer: validIntentJSON, cost: cost(0.003)}))
	f.start(t, f.agent(t, "General"))
	f.runner.Wait()

	row, err := f.q.GetPullIntent(context.Background(), f.pull.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Intent == "" || string(row.InScope) == "" || string(row.OutOfScope) == "" || row.Confidence == "" ||
		string(row.Sources) == "" || string(row.Unresolved) == "" ||
		row.HeadSha == nil || *row.HeadSha != f.pull.HeadSha ||
		row.Fingerprint == nil || row.Provider == nil || *row.Provider != "anthropic" ||
		row.Model == nil || *row.Model != "haiku" ||
		row.TokensIn == nil || row.TokensOut == nil || row.CostUsd == nil || *row.CostUsd != 0.003 {
		t.Errorf("stored intent row = %+v", row)
	}
	var sources []map[string]any
	if err := json.Unmarshal(row.Sources, &sources); err != nil || len(sources) == 0 {
		t.Errorf("sources = %s, err %v", row.Sources, err)
	}
	for _, s := range sources {
		for _, key := range []string{"kind", "label", "ref", "truncated"} {
			if _, ok := s[key]; !ok {
				t.Errorf("source %v lacks %q", s, key)
			}
		}
	}
}

// AC-18: a second request with an unchanged fingerprint (same head commit,
// title and body) reuses the stored intent at no cost.
func TestIntentReusedWhenFingerprintMatches(t *testing.T) {
	intentLLM := &fakeLLM{answer: validIntentJSON}
	f := newFixture(t, &fakeLLM{answer: reviewJSON}, intentConfig(intentLLM))

	f.start(t, f.agent(t, "General"))
	f.runner.Wait()
	if n := len(intentLLM.requests); n != 1 {
		t.Fatalf("first request: %d call(s), want 1", n)
	}

	id := f.start(t, f.agent(t, "Second"))[0]
	f.runner.Wait()
	if n := len(intentLLM.requests); n != 1 {
		t.Errorf("second request: %d call(s), want still 1 (reused)", n)
	}
	r := f.run(t, id)
	if it := intentTraceOf(t, r); it == nil || it["status"] != "reused" {
		t.Errorf("intent = %v, want status reused", it)
	}
	found := false
	for _, msg := range r.traceLog() {
		found = found || msg == "intent: reused"
	}
	if !found {
		t.Errorf("trace log lacks \"intent: reused\":\n%s", strings.Join(r.traceLog(), "\n"))
	}
}

// AC-17a, AC-25: when the intent model can't be resolved (here, the AC-17a
// message), the review still runs to completion without an intent, and any
// previously stored intent is left untouched.
func TestIntentFailureDoesNotBlockTheReview(t *testing.T) {
	const wantErr = "intent model `haiku` needs ANTHROPIC_VIA_CLAUDE_CODE=true; pick another intent model in Settings"
	f := newFixture(t, &fakeLLM{answer: reviewJSON}, func(c *runner.Config) {
		c.IntentLLM = func(string, string) (review.LLM, error) { return nil, errors.New(wantErr) }
	})
	id := f.start(t, f.agent(t, "General"))[0]
	f.runner.Wait()

	r := f.run(t, id)
	if r.Status != "done" || r.Reviews != 1 {
		t.Fatalf("run = %+v, want a completed review despite the intent failure", r)
	}
	it := intentTraceOf(t, r)
	if it == nil || it["status"] != "failed" || it["reason"] != wantErr {
		t.Errorf("intent = %v, want status failed with reason %q", it, wantErr)
	}
	found := false
	for _, msg := range r.traceLog() {
		found = found || msg == "intent: failed — "+wantErr+"; reviewing without intent"
	}
	if !found {
		t.Errorf("trace log lacks the failure line:\n%s", strings.Join(r.traceLog(), "\n"))
	}
}

// AC-25: an invalid answer, twice, also leaves the review running without
// intent and the previously stored row untouched.
func TestIntentInvalidAnswerKeepsThePreviousRow(t *testing.T) {
	intentLLM := &fakeLLM{answer: validIntentJSON}
	f := newFixture(t, &fakeLLM{answer: reviewJSON}, intentConfig(intentLLM))
	f.start(t, f.agent(t, "General"))
	f.runner.Wait()
	before, err := f.q.GetPullIntent(context.Background(), f.pull.ID)
	if err != nil {
		t.Fatal(err)
	}

	// A new title changes the fingerprint, so the next request tries to
	// re-derive; this time the model never answers validly.
	f.exec(t, `UPDATE pull_requests SET title = 'Add a different rate limit' WHERE id = $1`, f.pull.ID)
	var err2 error
	f.pull, err2 = f.q.GetPull(context.Background(), postgres.GetPullParams{WorkspaceID: f.pull.WorkspaceID, ID: f.pull.ID})
	if err2 != nil {
		t.Fatal(err2)
	}
	intentLLM.answer = "not a JSON object"
	id := f.start(t, f.agent(t, "Second"))[0]
	f.runner.Wait()

	if n := len(intentLLM.requests); n != 3 { // 1 (first request) + 1 try + 1 retry
		t.Fatalf("intent model called %d time(s), want 3", n)
	}
	r := f.run(t, id)
	if r.Status != "done" {
		t.Fatalf("run = %+v", r)
	}
	if it := intentTraceOf(t, r); it == nil || it["status"] != "failed" || it["reason"] != "model gave no valid intent in 2 attempts" {
		t.Errorf("intent = %v", it)
	}

	after, err := f.q.GetPullIntent(context.Background(), f.pull.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Intent != before.Intent || !samePtr(after.Fingerprint, before.Fingerprint) {
		t.Errorf("stored intent changed: before %+v, after %+v", before, after)
	}
}

func samePtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// AC-26: cancelling the trigger run while it derives intent ends that run
// cancelled; the other run reviews without intent, with the same "run
// cancelled" reason.
func TestCancelDuringIntentDerivation(t *testing.T) {
	intentLLM := &fakeLLM{gate: make(chan struct{}), started: make(chan struct{}, 1)}
	f := newFixture(t, &fakeLLM{answer: reviewJSON}, intentConfig(intentLLM))
	ids := f.start(t, f.agent(t, "First"), f.agent(t, "Second"))
	<-intentLLM.started
	if err := f.runner.Cancel(context.Background(), f.pull.WorkspaceID, ids[0]); err != nil {
		t.Fatal(err)
	}
	close(intentLLM.gate)
	f.runner.Wait()

	trigger, other := f.run(t, ids[0]), f.run(t, ids[1])
	if trigger.Status != "cancelled" {
		t.Errorf("trigger run status = %s, want cancelled", trigger.Status)
	}
	if other.Status != "done" || other.Reviews != 1 {
		t.Fatalf("other run = %+v, want a completed review", other)
	}
	it := intentTraceOf(t, other)
	if it == nil || it["status"] != "failed" || it["reason"] != "run cancelled" {
		t.Errorf("other run's intent = %v, want status failed, reason \"run cancelled\"", it)
	}
}

// AC-28: an intent call that doesn't return within Config.IntentTimeout
// fails with a "timed out" reason, and the review still runs.
func TestIntentTimesOut(t *testing.T) {
	intentLLM := &fakeLLM{gate: make(chan struct{}), started: make(chan struct{}, 1)}
	f := newFixture(t, &fakeLLM{answer: reviewJSON}, func(c *runner.Config) {
		c.IntentLLM = func(string, string) (review.LLM, error) { return intentLLM, nil }
		c.IntentTimeout = 50 * time.Millisecond
	})
	id := f.start(t, f.agent(t, "General"))[0]
	f.runner.Wait()
	close(intentLLM.gate) // release the goroutine; the run already timed out

	r := f.run(t, id)
	if r.Status != "done" {
		t.Fatalf("run = %+v", r)
	}
	it := intentTraceOf(t, r)
	if it == nil || it["status"] != "failed" || it["reason"] != "timed out after 50ms" {
		t.Errorf("intent = %v, want status failed, reason \"timed out after 50ms\"", it)
	}
}

// AC-28: Config.IntentTimeout bounds Gather too, not only Derive — a linked
// issue whose fetch never returns (here, an httptest handler that blocks on
// a channel) still times out the request, and the review still runs.
func TestIntentTimeoutCoversGather(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // never answers within the test's timeout
	}))
	// Cleanups run last-registered-first: release the blocked handler
	// before srv.Close waits for its connection to end, or Close hangs.
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(block) })

	intentLLM := &fakeLLM{answer: validIntentJSON}
	f := newFixture(t, &fakeLLM{answer: reviewJSON}, func(c *runner.Config) {
		c.IntentLLM = func(string, string) (review.LLM, error) { return intentLLM, nil }
		c.GitHubToken = func() (string, error) { return "tok", nil }
		c.GitHubAPI = srv.URL
		c.IntentTimeout = 50 * time.Millisecond
	})
	// A referenced issue makes intentFor's Gather call out to GitHub.
	f.exec(t, `UPDATE pull_requests SET body = 'See #1 for details.' WHERE id = $1`, f.pull.ID)
	var err error
	f.pull, err = f.q.GetPull(context.Background(), postgres.GetPullParams{WorkspaceID: f.pull.WorkspaceID, ID: f.pull.ID})
	if err != nil {
		t.Fatal(err)
	}

	id := f.start(t, f.agent(t, "General"))[0]
	f.runner.Wait()

	r := f.run(t, id)
	if r.Status != "done" {
		t.Fatalf("run = %+v", r)
	}
	it := intentTraceOf(t, r)
	if it == nil || it["status"] != "failed" || it["reason"] != "timed out after 50ms" {
		t.Errorf("intent = %v, want status failed, reason \"timed out after 50ms\"", it)
	}
}

// AC-4: with no intent system wired (Config.IntentLLM nil, as every other
// runner test leaves it), there is no "intent" key in the trace and no
// "intent:" log line — the pre-existing behaviour, unchanged.
func TestNoIntentSystemWired(t *testing.T) {
	f := newFixture(t, &fakeLLM{answer: reviewJSON}, nil)
	id := f.start(t, f.agent(t, "General"))[0]
	f.runner.Wait()

	r := f.run(t, id)
	if v, ok := r.Trace["intent"]; !ok || v != nil {
		t.Errorf(`trace["intent"] = %v, %v, want present and null`, v, ok)
	}
	for _, msg := range r.traceLog() {
		if strings.HasPrefix(msg, "intent:") {
			t.Errorf("unexpected intent log line: %q", msg)
		}
	}
}
