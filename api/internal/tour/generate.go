package tour

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

// defaultSectionTimeout is how long one section may take, covering all of
// its attempts (Q9), when Options.SectionTimeout is zero.
const defaultSectionTimeout = 90 * time.Second

// maxAttempts is how many times a section may ask the model before giving
// up (AC-39).
const maxAttempts = 3

// Options configures Generate.
type Options struct {
	// SectionTimeout bounds one section's attempts, including repairs.
	// Zero means defaultSectionTimeout (90s); the "timed out after 90s"
	// failure reason is fixed text regardless of the value actually used,
	// matching the Approach's failure-reasons table.
	SectionTimeout time.Duration
}

func (o Options) sectionTimeout() time.Duration {
	if o.SectionTimeout > 0 {
		return o.SectionTimeout
	}
	return defaultSectionTimeout
}

// Generate runs the tour's five sections in parallel against llm, one
// goroutine per kind, each owned by this call and stopped by ctx (one
// section's error never stops the others, AC-7). progress, when non-nil, is
// called with StatusGenerating before a section's first attempt, and with
// StatusDone or StatusFailed once, when it has a final outcome; it may be
// called concurrently from more than one goroutine and must be safe for
// that.
func Generate(ctx context.Context, llm review.LLM, model string, in Inputs, opts Options, progress func(kind Kind, status Status, reason string)) Result {
	tree := NewTree(in.Tree)

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		result Result
	)
	for i, kind := range Kinds {
		wg.Add(1)
		go func(i int, kind Kind) {
			defer wg.Done()
			sr, usage := runSection(ctx, llm, model, kind, in, tree, opts, progress)

			mu.Lock()
			result.Sections[i] = sr
			result.Usage = result.Usage.Plus(usage)
			mu.Unlock()
		}(i, kind)
	}
	wg.Wait()
	return result
}

// runSection drives one kind's attempts to a final SectionResult.
func runSection(ctx context.Context, llm review.LLM, model string, kind Kind, in Inputs, tree Tree, opts Options, progress func(Kind, Status, string)) (SectionResult, review.Usage) {
	report(progress, kind, StatusGenerating, "")

	sctx, cancel := context.WithTimeout(ctx, opts.sectionTimeout())
	defer cancel()

	req := review.JSONRequest{
		Model:      model,
		System:     systemPrompt(kind),
		Messages:   []review.Message{{Role: review.RoleUser, Content: userMessage(kind, in, tree)}},
		SchemaName: schemaNames[kind],
		Schema:     schemaFor(kind),
	}

	var spent review.Usage
	var problem error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		res, err := llm.CompleteJSON(sctx, req)
		spent.Add(res) // counted even on error: a refusal is paid for too.
		if err != nil {
			// api/INSIGHTS.md 2026-10-05: a retry loop must report a
			// cancel as ctx.Err(), checked right after the failed
			// attempt, not the provider's own error text.
			reason := failureReason(ctx, sctx, err)
			return fail(progress, kind, reason, spent)
		}

		content, removed, verr := parseAndValidate(kind, tree, in.Todos, res.Text)
		if verr == nil {
			report(progress, kind, StatusDone, "")
			return SectionResult{Kind: kind, Status: StatusDone, RemovedRefs: removed, Content: content}, spent
		}
		if errors.Is(verr, ErrNoEntries) {
			return fail(progress, kind, "no verifiable entries", spent)
		}
		problem = verr

		if sctx.Err() != nil {
			return fail(progress, kind, failureReason(ctx, sctx, sctx.Err()), spent)
		}

		req.Messages = append(req.Messages,
			review.Message{Role: review.RoleAssistant, Content: res.Text},
			review.Message{Role: review.RoleUser, Content: repairMessage(problem)},
		)
	}
	return fail(progress, kind, "model gave no valid answer in 3 attempts", spent)
}

// failureReason prefers a context's own error over err's text: a cancel or
// deadline arriving around a failed attempt must be reported as such, not
// as whatever error the provider's call happened to return.
//
// parent is Generate's own ctx (the job's context, which also carries the
// 2-minute job-cap deadline); sctx is this section's own 90s timeout,
// derived from parent. parent's own deadline must be told apart from
// sctx's: hitting the job cap reads "timed out" (AC-47), never "the server
// stopped during generation" (that's reserved for an actual cancel, Canceled)
// and never "timed out after 90s" (that names the section's own timeout).
func failureReason(parent, sctx context.Context, err error) string {
	switch {
	case errors.Is(parent.Err(), context.Canceled):
		return "the server stopped during generation"
	case errors.Is(parent.Err(), context.DeadlineExceeded):
		return "timed out"
	case errors.Is(sctx.Err(), context.DeadlineExceeded):
		return "timed out after 90s"
	default:
		return err.Error()
	}
}

func fail(progress func(Kind, Status, string), kind Kind, reason string, spent review.Usage) (SectionResult, review.Usage) {
	report(progress, kind, StatusFailed, reason)
	return SectionResult{Kind: kind, Status: StatusFailed, Reason: reason}, spent
}

func report(progress func(Kind, Status, string), kind Kind, status Status, reason string) {
	if progress != nil {
		progress(kind, status, reason)
	}
}

// repairMessage tells the model what was wrong with its answer, the same
// pattern internal/intent and internal/review use.
func repairMessage(problem error) string {
	return "Your answer is not valid: " + problem.Error() +
		"\nReturn ONLY a single valid JSON object matching the schema, no prose."
}
