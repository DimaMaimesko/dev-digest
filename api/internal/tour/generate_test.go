package tour

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func testInputs() Inputs {
	return Inputs{
		Tree: []Entry{
			{Path: "README.md", Dir: false},
			{Path: "src", Dir: true},
			{Path: "src/api", Dir: true},
			{Path: "src/api/users.ts", Dir: false},
			{Path: "src/middleware", Dir: true},
			{Path: "src/middleware/ratelimit.ts", Dir: false},
		},
		RepoMap:     "repo map text",
		README:      "# demo",
		RankedFiles: []string{"src/api/users.ts", "src/middleware/ratelimit.ts"},
		RoutesCrons: "src/api/users.ts: GET /users",
	}
}

func validAnswers() map[string]string {
	return map[string]string{
		"Architecture":  `{"summary":"ok","nodes":[],"edges":[]}`,
		"CriticalPaths": `{"items":[{"path":"README.md","reason":"r"}]}`,
		"RunLocally":    `{"steps":[{"command":"pnpm install","comment":null}]}`,
		"ReadingPath":   `{"items":[{"path":"README.md","reason":"a"},{"path":"src/api/users.ts","reason":"b"},{"path":"src/middleware/ratelimit.ts","reason":"c"}]}`,
		"FirstTasks":    `{"tasks":[{"title":"t","description":"d","size":"small","paths":["README.md"],"todo":null}]}`,
	}
}

func scriptAllValid(f *fakeLLM) {
	for schema, text := range validAnswers() {
		f.script(schema, text)
	}
}

func TestGenerate_AllSectionsDone(t *testing.T) {
	f := newFakeLLM()
	scriptAllValid(f)

	result := Generate(context.Background(), f, "test-model", testInputs(), Options{}, nil)

	for _, sr := range result.Sections {
		if sr.Status != StatusDone {
			t.Errorf("%s status = %s, reason = %q, want done", sr.Kind, sr.Status, sr.Reason)
		}
	}
}

// TestGenerate_OneSectionFailingDoesNotStopOthers proves AC-7.
func TestGenerate_OneSectionFailingDoesNotStopOthers(t *testing.T) {
	f := newFakeLLM()
	scriptAllValid(f)
	f.answers["RunLocally"] = nil
	f.script("RunLocally", "not json", "still not json", "nope")

	result := Generate(context.Background(), f, "test-model", testInputs(), Options{}, nil)

	var runLocally SectionResult
	doneCount := 0
	for _, sr := range result.Sections {
		if sr.Kind == KindRunLocally {
			runLocally = sr
			continue
		}
		if sr.Status == StatusDone {
			doneCount++
		}
	}
	if runLocally.Status != StatusFailed {
		t.Fatalf("run_locally status = %s, want failed", runLocally.Status)
	}
	if runLocally.Reason != "model gave no valid answer in 3 attempts" {
		t.Fatalf("run_locally reason = %q", runLocally.Reason)
	}
	if doneCount != 4 {
		t.Fatalf("done sections = %d, want 4", doneCount)
	}
}

// TestGenerate_RepairSucceedsOnAttempt3 and fails after 3 (AC-39).
func TestGenerate_RepairThenSucceed(t *testing.T) {
	f := newFakeLLM()
	scriptAllValid(f)
	f.answers["CriticalPaths"] = nil
	f.script("CriticalPaths", "not json at all", "{\"items\": \"nope\"}", `{"items":[{"path":"README.md","reason":"ok"}]}`)

	result := Generate(context.Background(), f, "test-model", testInputs(), Options{}, nil)

	for _, sr := range result.Sections {
		if sr.Kind == KindCriticalPaths {
			if sr.Status != StatusDone {
				t.Fatalf("status = %s, reason = %q, want done", sr.Status, sr.Reason)
			}
		}
	}
	if got := len(f.requestsFor("CriticalPaths")); got != 3 {
		t.Fatalf("requests = %d, want 3", got)
	}
}

func TestGenerate_FailsAfterThreeAttempts(t *testing.T) {
	f := newFakeLLM()
	scriptAllValid(f)
	f.answers["FirstTasks"] = nil
	f.script("FirstTasks", "nope", "still nope", "nope again")

	result := Generate(context.Background(), f, "test-model", testInputs(), Options{}, nil)

	for _, sr := range result.Sections {
		if sr.Kind == KindFirstTasks {
			if sr.Status != StatusFailed {
				t.Fatalf("status = %s, want failed", sr.Status)
			}
			if sr.Reason != "model gave no valid answer in 3 attempts" {
				t.Fatalf("reason = %q", sr.Reason)
			}
		}
	}
	if got := len(f.requestsFor("FirstTasks")); got != 3 {
		t.Fatalf("requests = %d, want 3", got)
	}
}

// TestGenerate_UsageSumsEvenFailedSections proves AC-32.
func TestGenerate_UsageSumsEvenFailedSections(t *testing.T) {
	f := newFakeLLM()
	scriptAllValid(f)
	f.answers["RunLocally"] = nil
	f.script("RunLocally", "nope", "nope", "nope")

	result := Generate(context.Background(), f, "test-model", testInputs(), Options{}, nil)

	// 4 sections succeed on attempt 1 (10 in), run_locally fails after 3
	// attempts (30 in): 70 in total.
	if result.Usage.TokensIn != 70 {
		t.Fatalf("tokens in = %d, want 70", result.Usage.TokensIn)
	}
	if result.Usage.CostUSD != nil {
		t.Fatalf("cost = %v, want nil (no call reported one)", *result.Usage.CostUSD)
	}
}

func TestGenerate_UsageCostReported(t *testing.T) {
	f := newFakeLLM()
	scriptAllValid(f)
	cost := 0.01
	f.cost = &cost

	result := Generate(context.Background(), f, "test-model", testInputs(), Options{}, nil)

	if result.Usage.CostUSD == nil {
		t.Fatalf("cost = nil, want a reported sum")
	}
}

// TestGenerate_EveryRequestHasSystemAndUntrustedData proves the
// api/INSIGHTS.md 2026-10-06 rule and that repository text is wrapped as
// untrusted data.
func TestGenerate_EveryRequestHasSystemAndUntrustedData(t *testing.T) {
	f := newFakeLLM()
	scriptAllValid(f)

	Generate(context.Background(), f, "test-model", testInputs(), Options{}, nil)

	for _, schema := range []string{"Architecture", "CriticalPaths", "RunLocally", "ReadingPath", "FirstTasks"} {
		reqs := f.requestsFor(schema)
		if len(reqs) == 0 {
			t.Fatalf("no requests for %s", schema)
		}
		req := reqs[0]
		if strings.TrimSpace(req.System) == "" {
			t.Errorf("%s: System is empty", schema)
		}
		if len(req.Messages) == 0 || !strings.Contains(req.Messages[0].Content, "<untrusted") {
			t.Errorf("%s: user message doesn't wrap data as <untrusted>: %q", schema, req.Messages[0].Content)
		}
	}
}

func TestGenerate_ProgressReportsGeneratingThenFinalStatus(t *testing.T) {
	f := newFakeLLM()
	scriptAllValid(f)

	type event struct {
		kind   Kind
		status Status
	}
	var (
		mu     sync.Mutex
		events []event
	)
	Generate(context.Background(), f, "test-model", testInputs(), Options{}, func(kind Kind, status Status, reason string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event{kind, status})
	})

	counts := map[Kind]int{}
	for _, e := range events {
		counts[e.kind]++
		if e.status != StatusGenerating && e.status != StatusDone && e.status != StatusFailed {
			t.Fatalf("unexpected status %s", e.status)
		}
	}
	for _, kind := range Kinds {
		if counts[kind] != 2 {
			t.Errorf("%s got %d progress calls, want 2 (generating, then done/failed)", kind, counts[kind])
		}
	}
}

// TestGenerate_ProviderErrorIsReportedVerbatim proves the fourth failure
// reason (Approach -> Domain, failure reasons): a provider error that isn't
// a context cancel or deadline is reported as the provider's own text.
func TestGenerate_ProviderErrorIsReportedVerbatim(t *testing.T) {
	f := newFakeLLM()
	scriptAllValid(f)
	f.scriptErr("RunLocally", errors.New("rate limited by provider"))

	result := Generate(context.Background(), f, "test-model", testInputs(), Options{}, nil)

	for _, sr := range result.Sections {
		if sr.Kind == KindRunLocally {
			if sr.Status != StatusFailed || sr.Reason != "rate limited by provider" {
				t.Fatalf("status = %s, reason = %q, want failed, \"rate limited by provider\"", sr.Status, sr.Reason)
			}
		}
	}
}

func TestGenerate_TimesOutAfterSectionTimeout(t *testing.T) {
	f := newFakeLLM()
	scriptAllValid(f)
	f.blockOn("Architecture")

	result := Generate(context.Background(), f, "test-model", testInputs(), Options{SectionTimeout: 20 * time.Millisecond}, nil)

	for _, sr := range result.Sections {
		if sr.Kind == KindArchitecture {
			if sr.Status != StatusFailed || sr.Reason != "timed out after 90s" {
				// The reason text is fixed regardless of the actual
				// configured timeout (Approach -> Domain, failure
				// reasons), so it still reads "after 90s" in this test
				// even though SectionTimeout was lowered for speed.
				t.Fatalf("status = %s, reason = %q", sr.Status, sr.Reason)
			}
		}
	}
}

// TestGenerate_NoVerifiableEntriesFailsWithoutRetrying proves AC-19 at the
// Generate level: a structurally valid answer with every path made up fails
// the section at once, in a single request.
func TestGenerate_NoVerifiableEntriesFailsWithoutRetrying(t *testing.T) {
	f := newFakeLLM()
	scriptAllValid(f)
	f.answers["CriticalPaths"] = nil
	f.script("CriticalPaths", `{"items":[{"path":"made/up.go","reason":"nope"}]}`)

	result := Generate(context.Background(), f, "test-model", testInputs(), Options{}, nil)

	for _, sr := range result.Sections {
		if sr.Kind == KindCriticalPaths {
			if sr.Status != StatusFailed || sr.Reason != "no verifiable entries" {
				t.Fatalf("status = %s, reason = %q", sr.Status, sr.Reason)
			}
		}
	}
	if got := len(f.requestsFor("CriticalPaths")); got != 1 {
		t.Fatalf("requests = %d, want 1 (no repair retry for ErrNoEntries)", got)
	}
}

func TestGenerate_CancelReportsServerStopped(t *testing.T) {
	f := newFakeLLM()
	scriptAllValid(f)
	f.blockOn("Architecture")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	result := Generate(ctx, f, "test-model", testInputs(), Options{}, nil)

	for _, sr := range result.Sections {
		if sr.Kind == KindArchitecture {
			if sr.Status != StatusFailed || sr.Reason != "the server stopped during generation" {
				t.Fatalf("status = %s, reason = %q", sr.Status, sr.Reason)
			}
		}
	}
}

// TestGenerate_ParentDeadlineReadsTimedOutNotAfter90s proves the job-cap
// case (AC-47): a parent context (the 2-minute job slot) that times out on
// its own reads "timed out", never "timed out after 90s" (that reason names
// the section's own 90s timeout, Options.SectionTimeout here set much
// higher so the two can't be confused) and never "the server stopped during
// generation" (that's reserved for an actual cancel).
func TestGenerate_ParentDeadlineReadsTimedOutNotAfter90s(t *testing.T) {
	f := newFakeLLM()
	scriptAllValid(f)
	f.blockOn("Architecture")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	result := Generate(ctx, f, "test-model", testInputs(), Options{SectionTimeout: time.Minute}, nil)

	for _, sr := range result.Sections {
		if sr.Kind == KindArchitecture {
			if sr.Status != StatusFailed || sr.Reason != "timed out" {
				t.Fatalf("status = %s, reason = %q, want failed, \"timed out\"", sr.Status, sr.Reason)
			}
		}
	}
}
