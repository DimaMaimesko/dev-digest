package review_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

// scriptedLLM answers each request with reply(request) and records the
// requests it gets.
type scriptedLLM struct {
	reply func(review.JSONRequest) string
	err   error    // returned instead of an answer when set
	cost  *float64 // each answer's cost; nil like a provider that doesn't say
	reqs  []review.JSONRequest
}

func (s *scriptedLLM) CompleteJSON(_ context.Context, req review.JSONRequest) (review.JSONResponse, error) {
	s.reqs = append(s.reqs, req)
	if s.err != nil {
		return review.JSONResponse{}, s.err
	}
	return review.JSONResponse{Text: s.reply(req), TokensIn: 100, TokensOut: 10, CostUSD: s.cost}, nil
}

func cost(f float64) *float64 { return &f }

// always returns a reply function that gives the same answer to every request.
func always(answer string) func(review.JSONRequest) string {
	return func(review.JSONRequest) string { return answer }
}

// answer returns a model's answer: a Review as JSON.
func answer(t *testing.T, v review.Verdict, summary string, modelScore int, findings ...review.Finding) string {
	t.Helper()
	if findings == nil {
		findings = []review.Finding{}
	}
	b, err := json.Marshal(review.Review{Verdict: v, Summary: summary, Score: modelScore, Findings: findings})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// withSeverity returns f with severity s.
func withSeverity(f review.Finding, s review.Severity) review.Finding {
	f.Severity = s
	return f
}

func userMessage(req review.JSONRequest) string { return req.Messages[0].Content }

func TestRunSinglePass(t *testing.T) {
	real := withSeverity(finding("src/config.ts", 11, 11), review.SeverityCritical)
	madeUp := finding("src/config.ts", 999, 999)
	modelAnswer := answer(t, review.VerdictRequestChanges, "secret key committed", 38, real, madeUp)
	llm := &scriptedLLM{reply: always(modelAnswer)}
	var events []string

	res, err := review.Run(context.Background(), llm, review.Input{
		Model:   "gpt-test",
		Prompt:  review.Prompt{System: "security reviewer", Task: "Review PR #482"},
		Diff:    parse(t, sample),
		OnEvent: func(e review.Event) { events = append(events, e.Message) },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.Strategy != review.StrategySinglePass || !slices.Equal(res.Chunks, []string{"all files"}) {
		t.Errorf("Strategy = %q, Chunks = %q; want one call for all files", res.Strategy, res.Chunks)
	}
	if got := res.Grounding.Summary(); got != "1/2 passed" {
		t.Errorf("grounding = %q, want 1/2 passed", got)
	}
	if len(res.Review.Findings) != 1 || res.Review.Findings[0] != real {
		t.Errorf("Findings = %+v, want only the real one", res.Review.Findings)
	}
	// One critical finding survives: 100 - 35. The model's own 38 is ignored.
	if res.Review.Score != 65 {
		t.Errorf("Score = %d, want 65", res.Review.Score)
	}
	if res.Review.Verdict != review.VerdictRequestChanges || res.Review.Summary != "secret key committed" {
		t.Errorf("Review = %+v", res.Review)
	}
	if res.TokensIn != 100 || res.TokensOut != 10 || res.Raw != modelAnswer {
		t.Errorf("tokens %d/%d, raw %q", res.TokensIn, res.TokensOut, res.Raw)
	}

	if len(llm.reqs) != 1 {
		t.Fatalf("made %d calls, want 1", len(llm.reqs))
	}
	if user := userMessage(llm.reqs[0]); user != res.Assembly.User || !strings.Contains(user, sample) {
		t.Error("the call should send the whole diff, as recorded in Assembly")
	}
	if !slices.Contains(events, "Citation grounding: 1/2 passed") {
		t.Errorf("events = %q, want the grounding result", events)
	}
}

func TestRunMapReduce(t *testing.T) {
	llm := &scriptedLLM{reply: func(req review.JSONRequest) string {
		if strings.Contains(userMessage(req), "src/api/users.ts") {
			return answer(t, review.VerdictComment, "N+1 query.", 70, finding("src/api/users.ts", 46, 46))
		}
		critical := withSeverity(finding("src/config.ts", 11, 11), review.SeverityCritical)
		return answer(t, review.VerdictRequestChanges, "Secret in code.", 20, critical)
	}, cost: cost(0.25)}

	res, err := review.Run(context.Background(), llm, review.Input{
		Model:     "m",
		Prompt:    review.Prompt{System: "sys"},
		Diff:      parse(t, sample),
		Strategy:  review.StrategyMapReduce,
		SessionID: "sess-abc",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !slices.Equal(res.Chunks, []string{"src/config.ts", "src/api/users.ts"}) {
		t.Errorf("Chunks = %q, want one per file", res.Chunks)
	}
	// Each call sees only its own file.
	for i, other := range []string{"src/api/users.ts", "src/config.ts"} {
		if strings.Contains(userMessage(llm.reqs[i]), other) {
			t.Errorf("call %d also contains %s", i, other)
		}
		if llm.reqs[i].SessionID != "sess-abc" {
			t.Errorf("call %d SessionID = %q", i, llm.reqs[i].SessionID)
		}
	}
	// The trace records the prompt for the whole diff.
	if !strings.Contains(res.Assembly.User, "src/config.ts") || !strings.Contains(res.Assembly.User, "src/api/users.ts") {
		t.Error("Assembly should hold the prompt for the whole diff")
	}

	r := res.Review
	if r.Verdict != review.VerdictRequestChanges {
		t.Errorf("Verdict = %q, want the most serious one", r.Verdict)
	}
	if r.Summary != "Secret in code. N+1 query." {
		t.Errorf("Summary = %q", r.Summary)
	}
	if len(r.Findings) != 2 || r.Score != 100-35-12 {
		t.Errorf("got %d findings, score %d; want 2 and 53", len(r.Findings), r.Score)
	}
	if res.TokensIn != 200 || strings.Count(res.Raw, "\n---\n") != 1 {
		t.Errorf("TokensIn = %d, Raw = %q", res.TokensIn, res.Raw)
	}
	if res.CostUSD == nil || *res.CostUSD != 0.5 {
		t.Errorf("CostUSD = %v, want 0.5: two calls at 0.25", res.CostUSD)
	}
}

// The intent, when set, is untrusted context like the PR description: every
// per-file call of a map-reduce run must see it, not only the first.
func TestRunMapReduceSendsIntentToEveryCall(t *testing.T) {
	llm := &scriptedLLM{reply: always(answer(t, review.VerdictApprove, "ok", 95))}

	res, err := review.Run(context.Background(), llm, review.Input{
		Model:    "m",
		Prompt:   review.Prompt{System: "sys", Intent: "Confidence: low\nIntent: do the thing."},
		Diff:     parse(t, sample),
		Strategy: review.StrategyMapReduce,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(llm.reqs) != 2 {
		t.Fatalf("made %d calls, want 2 (one per file)", len(llm.reqs))
	}
	for i, req := range llm.reqs {
		if !strings.Contains(userMessage(req), "<untrusted source=\"pr-intent\">\nConfidence: low\nIntent: do the thing.\n</untrusted>") {
			t.Errorf("call %d is missing the intent section:\n%s", i, userMessage(req))
		}
	}
	if !strings.Contains(res.Assembly.User, "pr-intent") {
		t.Error("Assembly should hold the intent section for the whole diff")
	}
}

// Each file's call only sees that file. A finding it reports about another
// file is dropped, even on a line the whole diff shows: the model made it up.
func TestRunMapReduceDropsFindingsAboutOtherFiles(t *testing.T) {
	config := withSeverity(finding("src/config.ts", 11, 11), review.SeverityCritical)
	users := finding("src/api/users.ts", 46, 46)
	llm := &scriptedLLM{reply: always(answer(t, review.VerdictComment, "s", 50, config, users))}

	res, err := review.Run(context.Background(), llm, review.Input{
		Model: "m", Prompt: review.Prompt{System: "s"}, Diff: parse(t, sample), Strategy: review.StrategyMapReduce,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Each finding is kept once, from the call for its own file.
	if want := []review.Finding{config, users}; !slices.Equal(res.Review.Findings, want) {
		t.Errorf("Findings = %+v, want each file's finding once", res.Review.Findings)
	}
	if got := res.Grounding.Summary(); got != "2/4 passed" {
		t.Errorf("grounding = %q, want 2/4 passed", got)
	}
	wantReasons := []string{
		`reported by the call for "src/config.ts", which was not shown "src/api/users.ts"`,
		`reported by the call for "src/api/users.ts", which was not shown "src/config.ts"`,
	}
	for i, d := range res.Grounding.Dropped {
		if d.Reason != wantReasons[i] {
			t.Errorf("Dropped[%d].Reason = %q, want %q", i, d.Reason, wantReasons[i])
		}
	}
}

func TestRunMapReduceSkipsEmptySummaries(t *testing.T) {
	llm := &scriptedLLM{reply: func(req review.JSONRequest) string {
		if strings.Contains(userMessage(req), "src/api/users.ts") {
			return answer(t, review.VerdictApprove, "", 95)
		}
		return answer(t, review.VerdictApprove, "Looks fine.", 95)
	}}
	res, err := review.Run(context.Background(), llm, review.Input{
		Model: "m", Prompt: review.Prompt{System: "s"}, Diff: parse(t, sample), Strategy: review.StrategyMapReduce,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Review.Summary != "Looks fine." {
		t.Errorf("Summary = %q, want no stray space from the empty one", res.Review.Summary)
	}
}

func TestRunStrategy(t *testing.T) {
	small := sample          // 2 files, 5 changed lines
	large := bigDiff(3, 150) // 3 files, 450 changed lines
	largeOneFile := bigDiff(1, 500)

	tests := []struct {
		name     string
		strategy review.Strategy
		diff     string
		want     review.Strategy
		calls    int
	}{
		{"auto, small diff", review.StrategyAuto, small, review.StrategySinglePass, 1},
		{"auto, large diff", review.StrategyAuto, large, review.StrategyMapReduce, 3},
		{"auto, large diff of one file", review.StrategyAuto, largeOneFile, review.StrategySinglePass, 1},
		{"empty means auto", "", large, review.StrategyMapReduce, 3},
		{"single-pass, large diff", review.StrategySinglePass, large, review.StrategySinglePass, 1},
		{"map-reduce, small diff", review.StrategyMapReduce, small, review.StrategyMapReduce, 2},
		{"map-reduce, one file", review.StrategyMapReduce, largeOneFile, review.StrategySinglePass, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			llm := &scriptedLLM{reply: always(answer(t, review.VerdictApprove, "ok", 95))}
			res, err := review.Run(context.Background(), llm, review.Input{
				Model: "m", Prompt: review.Prompt{System: "s"}, Diff: parse(t, tt.diff), Strategy: tt.strategy,
			})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if res.Strategy != tt.want || len(llm.reqs) != tt.calls {
				t.Errorf("got %s with %d calls, want %s with %d", res.Strategy, len(llm.reqs), tt.want, tt.calls)
			}
		})
	}
}

// bigDiff returns a diff that adds lines to files f0.go, f1.go, and so on.
func bigDiff(files, lines int) string {
	var b strings.Builder
	for i := range files {
		fmt.Fprintf(&b, "diff --git a/f%d.go b/f%d.go\n--- a/f%d.go\n+++ b/f%d.go\n@@ -0,0 +1,%d @@\n", i, i, i, i, lines)
		for n := range lines {
			fmt.Fprintf(&b, "+line %d\n", n)
		}
	}
	return b.String()
}

func TestRunScore(t *testing.T) {
	inDiff := finding("src/config.ts", 11, 11)
	tests := []struct {
		name     string
		findings []review.Finding
		want     int
	}{
		{"no findings: 100, even if the model said 10", nil, 100},
		{"one suggestion", []review.Finding{withSeverity(inDiff, review.SeveritySuggestion)}, 97},
		{"one warning", []review.Finding{withSeverity(inDiff, review.SeverityWarning)}, 88},
		{"one critical", []review.Finding{withSeverity(inDiff, review.SeverityCritical)}, 65},
		{"never below 0", slices.Repeat([]review.Finding{withSeverity(inDiff, review.SeverityCritical)}, 3), 0},
		{"dropped findings don't count", []review.Finding{finding("src/config.ts", 999, 999)}, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			llm := &scriptedLLM{reply: always(answer(t, review.VerdictComment, "s", 10, tt.findings...))}
			res, err := review.Run(context.Background(), llm, review.Input{
				Model: "m", Prompt: review.Prompt{System: "s"}, Diff: parse(t, sample),
			})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if res.Review.Score != tt.want {
				t.Errorf("Score = %d, want %d", res.Review.Score, tt.want)
			}
		})
	}
}

func TestRunStops(t *testing.T) {
	in := review.Input{Model: "m", Prompt: review.Prompt{System: "s"}, Diff: parse(t, sample)}

	t.Run("context already cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		llm := &scriptedLLM{reply: always(answer(t, review.VerdictApprove, "ok", 95))}

		_, err := review.Run(ctx, llm, in)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		if len(llm.reqs) != 0 {
			t.Errorf("made %d calls, want none", len(llm.reqs))
		}
	})

	t.Run("provider error", func(t *testing.T) {
		boom := errors.New("rate limited")
		_, err := review.Run(context.Background(), &scriptedLLM{err: boom}, in)
		if !errors.Is(err, boom) || !strings.Contains(err.Error(), "review all files") {
			t.Errorf("err = %v, want it to wrap %v and name the chunk", err, boom)
		}
	})

	t.Run("model never gives a valid review", func(t *testing.T) {
		_, err := review.Run(context.Background(), &scriptedLLM{reply: always("no")}, in)
		if !errors.Is(err, review.ErrInvalidReview) {
			t.Errorf("err = %v, want ErrInvalidReview", err)
		}
	})

	// A failed run still reports what its answered calls cost: they were paid for.
	t.Run("usage spent before the failure", func(t *testing.T) {
		llm := &scriptedLLM{reply: func(req review.JSONRequest) string {
			if strings.Contains(userMessage(req), "src/api/users.ts") {
				return "no" // the second file's call never gives a valid review
			}
			return answer(t, review.VerdictApprove, "ok", 95)
		}, cost: cost(0.5)}
		mapReduce := in
		mapReduce.Strategy = review.StrategyMapReduce

		res, err := review.Run(context.Background(), llm, mapReduce)
		if !errors.Is(err, review.ErrInvalidReview) {
			t.Fatalf("err = %v, want ErrInvalidReview", err)
		}
		// 1 call for the first file, 3 (1 + 2 retries) for the second.
		if res.TokensIn != 400 || res.TokensOut != 40 || res.CostUSD == nil || *res.CostUSD != 2 {
			t.Errorf("Usage = %d/%d tokens, cost %v; want 400/40 and 2", res.TokensIn, res.TokensOut, res.CostUSD)
		}
		if res.Review.Findings != nil || res.Chunks != nil {
			t.Error("a failed Result should hold only the Usage")
		}
	})
}
