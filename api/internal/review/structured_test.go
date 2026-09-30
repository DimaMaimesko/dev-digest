package review

// These tests are inside the package because askForReview and parseReview are
// unexported: they are steps of the review run, not API of their own.

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

// fakeLLM answers each call with the next of its answers, and records the
// requests it gets.
type fakeLLM struct {
	answers []string
	err     error // returned instead of an answer when set
	reqs    []JSONRequest
}

func (f *fakeLLM) CompleteJSON(_ context.Context, req JSONRequest) (JSONResponse, error) {
	f.reqs = append(f.reqs, req)
	if f.err != nil {
		return JSONResponse{}, f.err
	}
	text := f.answers[0]
	f.answers = f.answers[1:]
	return JSONResponse{Text: text, TokensIn: 100, TokensOut: 10}, nil
}

const validReview = `{"verdict":"request_changes","summary":"One real bug.","score":55,"findings":[
 {"id":"f1","severity":"CRITICAL","category":"security","title":"Live Stripe key","file":"src/config.ts",
  "start_line":11,"end_line":11,"rationale":"Secret in code.","suggestion":null,"confidence":0.9,
  "kind":null,"trifecta_components":null,"evidence":null}]}`

var wantReview = Review{
	Verdict: VerdictRequestChanges,
	Summary: "One real bug.",
	Score:   55,
	Findings: []Finding{{
		ID: "f1", Severity: SeverityCritical, Category: CategorySecurity, Title: "Live Stripe key",
		File: "src/config.ts", StartLine: 11, EndLine: 11, Rationale: "Secret in code.", Confidence: 0.9,
	}},
}

func TestParseReviewAccepts(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"pure JSON", validReview},
		{"JSON in a ```json fence", "```json\n" + validReview + "\n```"},
		{"JSON wrapped in prose", "Here is my review: " + validReview + " Hope this helps."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseReview(tt.text)
			if err != nil {
				t.Fatalf("parseReview: %v", err)
			}
			if !reviewsEqual(got, wantReview) {
				t.Errorf("got %+v\nwant %+v", got, wantReview)
			}
		})
	}

	// reviewer-core parsed a fenced answer by cutting at the first ``` pair,
	// so a code block inside a string value broke it.
	t.Run("fence inside a string value", func(t *testing.T) {
		text := "```json\n" + `{"verdict":"comment","summary":"Use:\n` + "```go\\nx := 1\\n```" + `","score":80,"findings":[]}` + "\n```"
		got, err := parseReview(text)
		if err != nil {
			t.Fatalf("parseReview: %v", err)
		}
		if !strings.Contains(got.Summary, "```go") {
			t.Errorf("Summary = %q, want the code block kept", got.Summary)
		}
	})
}

func TestParseReviewRejects(t *testing.T) {
	// withFinding returns validReview with one field of its finding replaced.
	withFinding := func(old, new string) string { return strings.Replace(validReview, old, new, 1) }

	tests := []struct {
		name string
		text string
		want string // part of the error, which the model will read
	}{
		{"no JSON at all", "I could not review this.", "no JSON object"},
		{"broken JSON", `{"verdict": "approve",`, "not valid JSON"},
		{"wrong type", strings.Replace(validReview, `"score":55`, `"score":"high"`, 1), "Review.score"},
		{"fractional line number", withFinding(`"start_line":11`, `"start_line":11.5`), "start_line"},
		{"unknown verdict", strings.Replace(validReview, "request_changes", "reject", 1), `verdict: got "reject"`},
		{"missing verdict", `{"summary":"s","score":90,"findings":[]}`, `verdict: got ""`},
		{"score above 100", strings.Replace(validReview, `"score":55`, `"score":101`, 1), "score: got 101"},
		{"unknown severity", withFinding(`"CRITICAL"`, `"HIGH"`), `findings.0.severity: got "HIGH"`},
		{"unknown category", withFinding(`"security"`, `"docs"`), `findings.0.category: got "docs"`},
		{"unknown kind", withFinding(`"kind":null`, `"kind":"guess"`), `findings.0.kind: got "guess"`},
		{"confidence above 1", withFinding(`"confidence":0.9`, `"confidence":1.5`), "findings.0.confidence: got 1.5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseReview(tt.text)
			if err == nil {
				t.Fatal("parseReview returned no error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}

	t.Run("lists every problem at once", func(t *testing.T) {
		text := strings.NewReplacer(`"request_changes"`, `"reject"`, `"CRITICAL"`, `"HIGH"`).Replace(validReview)
		_, err := parseReview(text)
		if err == nil || strings.Count(err.Error(), "\n- ") != 2 {
			t.Errorf("want two listed problems, got: %v", err)
		}
	})
}

func TestAskForReview(t *testing.T) {
	prompt := Prompt{System: "You are a reviewer."}.Assemble("D")

	t.Run("valid first answer", func(t *testing.T) {
		llm := &fakeLLM{answers: []string{validReview}}

		ans, err := model{llm: llm, name: "gpt-test", maxRetries: 2}.askForReview(context.Background(), prompt)
		if err != nil {
			t.Fatalf("askForReview: %v", err)
		}
		if ans.Attempts != 1 || ans.TokensIn != 100 || ans.TokensOut != 10 || ans.Raw != validReview {
			t.Errorf("got attempts=%d tokens=%d/%d raw=%q", ans.Attempts, ans.TokensIn, ans.TokensOut, ans.Raw)
		}
		if !reviewsEqual(ans.Review, wantReview) {
			t.Errorf("Review = %+v", ans.Review)
		}

		req := llm.reqs[0]
		if req.Model != "gpt-test" || req.System != prompt.System || req.SchemaName != "Review" {
			t.Errorf("request = model %q, schema %q, system %q", req.Model, req.SchemaName, req.System)
		}
		want := []Message{{Role: RoleUser, Content: prompt.User}}
		if !slices.Equal(req.Messages, want) {
			t.Errorf("Messages = %+v, want %+v", req.Messages, want)
		}
		if !json.Valid(req.Schema) {
			t.Error("request schema is not valid JSON")
		}
	})

	t.Run("invalid answer, then fixed", func(t *testing.T) {
		bad := strings.Replace(validReview, `"CRITICAL"`, `"HIGH"`, 1)
		llm := &fakeLLM{answers: []string{bad, validReview}}

		ans, err := model{llm: llm, name: "m", maxRetries: 2}.askForReview(context.Background(), prompt)
		if err != nil {
			t.Fatalf("askForReview: %v", err)
		}
		if ans.Attempts != 2 || ans.TokensIn != 200 || ans.Raw != validReview {
			t.Errorf("got attempts=%d tokensIn=%d, want 2 and 200, and the fixed answer as Raw", ans.Attempts, ans.TokensIn)
		}

		// The retry shows the model its bad answer and what was wrong with it.
		retry := llm.reqs[1].Messages
		if len(retry) != 3 || retry[1] != (Message{Role: RoleAssistant, Content: bad}) {
			t.Fatalf("retry messages = %+v", retry)
		}
		if fix := retry[2]; fix.Role != RoleUser || !strings.Contains(fix.Content, `findings.0.severity: got "HIGH"`) {
			t.Errorf("repair message = %+v", fix)
		}
	})

	t.Run("never valid", func(t *testing.T) {
		llm := &fakeLLM{answers: []string{"no", "still no", "nope"}}

		ans, err := model{llm: llm, name: "m", maxRetries: 2}.askForReview(context.Background(), prompt)
		if !errors.Is(err, ErrInvalidReview) {
			t.Fatalf("err = %v, want ErrInvalidReview", err)
		}
		if len(llm.reqs) != 3 {
			t.Errorf("made %d calls, want 3 (1 + 2 retries)", len(llm.reqs))
		}
		// The three answers were paid for.
		if ans.TokensIn != 300 || ans.TokensOut != 30 {
			t.Errorf("Usage = %d/%d tokens, want 300/30", ans.TokensIn, ans.TokensOut)
		}
	})

	t.Run("provider error is returned, not retried", func(t *testing.T) {
		boom := errors.New("rate limited")
		llm := &fakeLLM{err: boom}

		_, err := model{llm: llm, name: "m", maxRetries: 2}.askForReview(context.Background(), prompt)
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want it to wrap %v", err, boom)
		}
		if len(llm.reqs) != 1 {
			t.Errorf("made %d calls, want 1", len(llm.reqs))
		}
	})
}

// property is the part of a JSON Schema property the drift test reads.
type property struct {
	Enum  []string   `json:"enum"`
	AnyOf []property `json:"anyOf"`
	Items *struct {
		Properties map[string]property `json:"properties"`
		Required   []string            `json:"required"`
	} `json:"items"`
}

// TestReviewSchemaMatchesTypes guards against the embedded schema and the Go
// types drifting apart: every field and allowed value must appear in both.
func TestReviewSchemaMatchesTypes(t *testing.T) {
	var schema struct {
		Properties map[string]property `json:"properties"`
		Required   []string            `json:"required"`
	}
	if err := json.Unmarshal(reviewSchema, &schema); err != nil {
		t.Fatalf("review.schema.json: %v", err)
	}
	finding := schema.Properties["findings"].Items

	assertFields(t, "Review", Review{}, schema.Properties, schema.Required)
	assertFields(t, "Finding", Finding{}, finding.Properties, finding.Required)

	enums := []struct {
		name   string
		schema []string
		goVals []string
	}{
		{"verdict", schema.Properties["verdict"].Enum, strs(VerdictRequestChanges, VerdictApprove, VerdictComment)},
		{"severity", finding.Properties["severity"].Enum, strs(SeverityCritical, SeverityWarning, SeveritySuggestion)},
		{"category", finding.Properties["category"].Enum, strs(CategoryBug, CategorySecurity, CategoryPerf, CategoryStyle, CategoryTest)},
		{"kind", finding.Properties["kind"].AnyOf[0].Enum, strs(KindFinding, KindSecretLeak, KindLethalTrifecta, KindPhantom, KindHook)},
	}
	for _, e := range enums {
		if !slices.Equal(e.schema, e.goVals) {
			t.Errorf("%s: schema allows %v, Go constants are %v", e.name, e.schema, e.goVals)
		}
	}
}

// assertFields checks that every JSON field of the Go struct v is a required
// property in the schema. The schema may have more properties: the Go types
// ignore the lethal-trifecta fields until a later lesson needs them.
func assertFields(t *testing.T, name string, v any, props map[string]property, required []string) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	// Marshal leaves out omitempty fields that are empty; add them back.
	for _, f := range []string{"suggestion", "kind"} {
		if name == "Finding" {
			fields[f] = nil
		}
	}
	for f := range fields {
		if _, ok := props[f]; !ok {
			t.Errorf("%s field %q is not in the schema", name, f)
		}
		if !slices.Contains(required, f) {
			t.Errorf("%s field %q is not required by the schema", name, f)
		}
	}
}

func strs[T ~string](vs ...T) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = string(v)
	}
	return out
}

func reviewsEqual(a, b Review) bool {
	return a.Verdict == b.Verdict && a.Summary == b.Summary && a.Score == b.Score &&
		slices.Equal(a.Findings, b.Findings)
}
