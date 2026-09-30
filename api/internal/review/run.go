package review

import (
	"context"
	"fmt"
	"strings"

	"github.com/DimaMaimesko/dev-digest/api/internal/diff"
)

// Strategy says how Run splits a diff into model calls.
type Strategy string

// Strategies an agent can be configured with.
const (
	// StrategyAuto reviews the whole diff in one call, unless the diff changes
	// more than 400 lines across several files; then it reviews each file on
	// its own. The empty Strategy means StrategyAuto.
	StrategyAuto       Strategy = "auto"
	StrategySinglePass Strategy = "single-pass" // one call for the whole diff
	StrategyMapReduce  Strategy = "map-reduce"  // one call per file, if there are several
)

const (
	autoSplitLines = 400 // changed lines above which StrategyAuto reviews file by file
	maxRetries     = 2   // extra attempts after an invalid answer
)

// Input is a diff to review, and how to review it.
type Input struct {
	Model     string // model ID understood by the LLM, e.g. "deepseek/deepseek-v4-flash"
	Prompt    Prompt // the agent's system prompt and the context around the diff
	Diff      diff.Diff
	Strategy  Strategy
	SessionID string      // optional; see JSONRequest.SessionID
	OnEvent   func(Event) // optional; receives progress for the run's live log
}

// Event is a progress update from Run.
type Event struct {
	Kind    EventKind
	Message string
}

// EventKind says what an Event reports. The values match the run trace's
// live-log line kinds.
type EventKind string

// Kinds of events Run reports.
const (
	EventInfo   EventKind = "info"
	EventTool   EventKind = "tool" // a model call is starting
	EventResult EventKind = "result"
)

// Result is a finished review.
type Result struct {
	// Review has only the findings that passed grounding (Grounding.Kept) and
	// a score computed from them, not the score the model gave.
	Review    Review
	Grounding Grounding
	Strategy  Strategy // how the diff was split: StrategySinglePass or StrategyMapReduce
	Assembly  Assembly // the prompt for the whole diff, for the run trace
	Chunks    []string // what each model call reviewed: "all files" or a file's path
	Usage              // of every model call, retries included
	Raw       string   // the model's accepted answers, separated by "\n---\n"
}

// Run reviews a diff with one model.
//
// It asks the model for a Review of the whole diff, or of each file on its own
// (see Strategy), and merges the answers. Then it drops the findings that cite
// lines the diff doesn't show (see Ground) and scores the rest. When each file
// is reviewed on its own, a call's findings about any other file are dropped
// too: the model never saw that file. Before each model call, Run stops with
// ctx's error if ctx is done.
//
// When Run fails, the Result it returns holds only the Usage of the calls
// answered so far, which were paid for all the same.
func Run(ctx context.Context, llm LLM, in Input) (Result, error) {
	emit := func(kind EventKind, format string, args ...any) {
		if in.OnEvent != nil {
			in.OnEvent(Event{Kind: kind, Message: fmt.Sprintf(format, args...)})
		}
	}
	res := Result{
		Strategy: pickStrategy(in.Strategy, in.Diff),
		Assembly: in.Prompt.Assemble(in.Diff.Text),
	}

	// A chunk is what one model call reviews: one file, or the whole diff
	// when file is empty.
	type chunk struct{ label, file, diff string }
	var chunks []chunk
	if res.Strategy == StrategyMapReduce {
		emit(EventInfo, "Large diff → map-reduce over %d files", len(in.Diff.Files))
		for _, f := range in.Diff.Files {
			chunks = append(chunks, chunk{f.Path, f.Path, f.Text})
		}
	} else {
		emit(EventInfo, "Reviewing %d changed file(s) in one pass", len(in.Diff.Files))
		chunks = []chunk{{"all files", "", in.Diff.Text}}
	}

	m := model{llm: llm, name: in.Model, sessionID: in.SessionID, maxRetries: maxRetries}
	var (
		parts     []Review
		raws      []string
		offTarget []Dropped // findings about a file the call wasn't shown
	)
	for _, c := range chunks {
		if err := ctx.Err(); err != nil {
			return Result{Usage: res.Usage}, err
		}
		if res.Strategy == StrategyMapReduce {
			emit(EventTool, "map: reviewing %s", c.label)
		} else {
			emit(EventTool, "Reviewing %s in one pass", c.label)
		}
		ans, err := m.askForReview(ctx, in.Prompt.Assemble(c.diff))
		res.Usage = res.Usage.Plus(ans.Usage)
		if err != nil {
			return Result{Usage: res.Usage}, fmt.Errorf("review %s: %w", c.label, err)
		}
		emit(EventResult, "%s: %d candidate finding(s)", c.label, len(ans.Review.Findings))

		if c.file != "" {
			var own []Finding
			for _, f := range ans.Review.Findings {
				if f.File == c.file {
					own = append(own, f)
					continue
				}
				offTarget = append(offTarget, Dropped{
					Finding: f,
					Reason:  fmt.Sprintf("reported by the call for %q, which was not shown %q", c.file, f.File),
				})
			}
			ans.Review.Findings = own
		}
		parts = append(parts, ans.Review)
		raws = append(raws, ans.Raw)
		res.Chunks = append(res.Chunks, c.label)
	}

	res.Review = merge(parts)
	emit(EventResult, "Reduced to %d finding(s); verdict=%s", len(res.Review.Findings), res.Review.Verdict)

	res.Grounding = Ground(res.Review.Findings, in.Diff)
	res.Grounding.Dropped = append(offTarget, res.Grounding.Dropped...)
	for _, d := range res.Grounding.Dropped {
		emit(EventInfo, "grounding dropped %q: %s", d.Finding.Title, d.Reason)
	}
	emit(EventResult, "Citation grounding: %s", res.Grounding.Summary())

	res.Review.Findings = res.Grounding.Kept
	res.Review.Score = score(res.Review.Findings)
	res.Raw = strings.Join(raws, "\n---\n")
	return res, nil
}

// pickStrategy decides how to split d: s itself, except that splitting by file
// needs more than one file, and StrategyAuto splits only large diffs.
func pickStrategy(s Strategy, d diff.Diff) Strategy {
	severalFiles := len(d.Files) > 1
	switch s {
	case StrategySinglePass:
		return StrategySinglePass
	case StrategyMapReduce:
		if severalFiles {
			return StrategyMapReduce
		}
		return StrategySinglePass
	}
	changed := 0
	for _, f := range d.Files {
		changed += f.Additions + f.Deletions
	}
	if severalFiles && changed > autoSplitLines {
		return StrategyMapReduce
	}
	return StrategySinglePass
}

// merge combines the reviews of separate files into one: all their findings,
// the most serious verdict, and their summaries. It leaves the score to Run,
// which computes it after grounding.
func merge(parts []Review) Review {
	if len(parts) == 1 {
		return parts[0]
	}
	merged := Review{Verdict: VerdictApprove}
	var summaries []string
	for _, p := range parts {
		merged.Findings = append(merged.Findings, p.Findings...)
		if seriousness(p.Verdict) > seriousness(merged.Verdict) {
			merged.Verdict = p.Verdict
		}
		if p.Summary != "" {
			summaries = append(summaries, p.Summary)
		}
	}
	merged.Summary = strings.Join(summaries, " ")
	return merged
}

func seriousness(v Verdict) int {
	switch v {
	case VerdictRequestChanges:
		return 2
	case VerdictComment:
		return 1
	}
	return 0
}

// score rates a pull request from 0 to 100 by its findings: each one lowers a
// perfect 100 by an amount set by its severity. The score models report for
// themselves is ignored; it has no fixed meaning and varies a lot between
// models.
func score(findings []Finding) int {
	s := 100
	for _, f := range findings {
		switch f.Severity {
		case SeverityCritical:
			s -= 35
		case SeverityWarning:
			s -= 12
		case SeveritySuggestion:
			s -= 3
		}
	}
	return max(s, 0)
}
