package review

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Review is a reviewer model's verdict on a pull request. The JSON field names
// match the API contract the web client uses.
type Review struct {
	Verdict  Verdict   `json:"verdict"`
	Summary  string    `json:"summary"`
	Score    int       `json:"score"` // 0 to 100, higher is better
	Findings []Finding `json:"findings"`
}

// Verdict is what a reviewer recommends doing with a pull request.
type Verdict string

// Verdicts a review can have.
const (
	VerdictRequestChanges Verdict = "request_changes"
	VerdictApprove        Verdict = "approve"
	VerdictComment        Verdict = "comment"
)

// reviewSchema is the JSON Schema a model's answer must match. It is what
// reviewer-core generates from its Zod contract, minus an unused duplicate of
// the whole schema that the Zod converter adds.
//
//go:embed review.schema.json
var reviewSchema []byte

// ErrInvalidReview means a model kept answering with something that isn't a
// valid Review, even after being told what was wrong.
var ErrInvalidReview = errors.New("model gave no valid review")

// answer is a valid Review from a model, with what it took to get it.
type answer struct {
	Review    Review
	Raw       string // the model's accepted answer, for the run trace
	Attempts  int
	TokensIn  int // summed over all attempts
	TokensOut int
}

// askForReview sends the assembled prompt to llm and reads the answer as a
// Review. When the answer isn't a valid Review, it shows the model what was
// wrong and asks again, up to maxRetries more times.
func askForReview(ctx context.Context, llm LLM, model string, a Assembly, maxRetries int) (answer, error) {
	req := JSONRequest{
		Model:      model,
		System:     a.System,
		Messages:   []Message{{Role: RoleUser, Content: a.User}},
		SchemaName: "Review",
		Schema:     reviewSchema,
	}
	var (
		tokensIn, tokensOut int
		problem             error
	)
	for attempt := 1; attempt <= maxRetries+1; attempt++ {
		res, err := llm.CompleteJSON(ctx, req)
		if err != nil {
			return answer{}, fmt.Errorf("ask %s for a review: %w", model, err)
		}
		tokensIn += res.TokensIn
		tokensOut += res.TokensOut

		r, err := parseReview(res.Text)
		if err == nil {
			return answer{
				Review:    r,
				Raw:       res.Text,
				Attempts:  attempt,
				TokensIn:  tokensIn,
				TokensOut: tokensOut,
			}, nil
		}
		problem = err
		req.Messages = append(req.Messages,
			Message{Role: RoleAssistant, Content: res.Text},
			Message{Role: RoleUser, Content: repairRequest(problem)},
		)
	}
	return answer{}, fmt.Errorf("%w from %s in %d attempts; last problem: %v",
		ErrInvalidReview, model, maxRetries+1, problem)
}

// repairRequest tells the model what was wrong with its answer.
func repairRequest(problem error) string {
	return "Your answer is not a valid Review: " + problem.Error() +
		"\nReturn ONLY a single valid JSON object matching the schema, no prose."
}

// parseReview reads a Review from a model's answer.
func parseReview(text string) (Review, error) {
	obj, err := jsonObject(text)
	if err != nil {
		return Review{}, err
	}
	var r Review
	if err := json.Unmarshal(obj, &r); err != nil {
		// Valid JSON with a value of the wrong type, like a string for "score".
		return Review{}, fmt.Errorf("it does not match the schema: %w", err)
	}
	if issues := r.issues(); len(issues) > 0 {
		return Review{}, fmt.Errorf("it does not match the schema:\n- %s", strings.Join(issues, "\n- "))
	}
	return r, nil
}

// jsonObject returns the JSON object in a model's answer. The answer is
// usually pure JSON, but a model may wrap it in prose or a ```json fence, so
// jsonObject decodes one value starting at the first "{". The decoder stops
// where that object ends and understands strings, so braces or fences inside
// string values don't confuse it.
func jsonObject(text string) ([]byte, error) {
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return nil, errors.New("it contains no JSON object")
	}
	var obj json.RawMessage
	if err := json.NewDecoder(strings.NewReader(text[start:])).Decode(&obj); err != nil {
		return nil, fmt.Errorf("it is not valid JSON: %w", err)
	}
	return obj, nil
}

// issues lists the ways r breaks the schema's rules that decoding JSON into
// Go types doesn't check: allowed values and ranges. Missing fields decode to
// zero values; the zero values of required enums are caught here.
func (r Review) issues() []string {
	var issues []string
	add := func(format string, args ...any) {
		issues = append(issues, fmt.Sprintf(format, args...))
	}

	if !slices.Contains([]Verdict{VerdictRequestChanges, VerdictApprove, VerdictComment}, r.Verdict) {
		add("verdict: got %q, want request_changes, approve or comment", r.Verdict)
	}
	if r.Score < 0 || r.Score > 100 {
		add("score: got %d, want 0 to 100", r.Score)
	}
	for i, f := range r.Findings {
		if !slices.Contains([]Severity{SeverityCritical, SeverityWarning, SeveritySuggestion}, f.Severity) {
			add("findings.%d.severity: got %q, want CRITICAL, WARNING or SUGGESTION", i, f.Severity)
		}
		if !slices.Contains([]Category{CategoryBug, CategorySecurity, CategoryPerf, CategoryStyle, CategoryTest}, f.Category) {
			add("findings.%d.category: got %q, want bug, security, perf, style or test", i, f.Category)
		}
		if f.Kind != "" && !slices.Contains([]Kind{KindFinding, KindSecretLeak, KindLethalTrifecta, KindPhantom, KindHook}, f.Kind) {
			add("findings.%d.kind: got %q, want null, finding, secret_leak, lethal_trifecta, phantom or hook", i, f.Kind)
		}
		if f.Confidence < 0 || f.Confidence > 1 {
			add("findings.%d.confidence: got %g, want 0 to 1", i, f.Confidence)
		}
	}
	return issues
}
