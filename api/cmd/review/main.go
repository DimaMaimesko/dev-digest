// Command review reviews a diff with a language model and prints the findings
// that point at lines the diff really changes.
//
// Usage:
//
//	git diff main | go run ./cmd/review -model MODEL -prompt FILE [flags]
//
// For example, with an agent prompt from this repository:
//
//	git diff main | go run ./cmd/review \
//	    -model deepseek/deepseek-v4-flash \
//	    -prompt ../docs/agent-prompts/general-reviewer.md
//
// By default it calls OpenRouter with the key in OPENROUTER_API_KEY. With
// -provider openai it calls OpenAI with the key in OPENAI_API_KEY, and with
// -provider anthropic, Anthropic with the key in ANTHROPIC_API_KEY. With
// -base-url it calls any other OpenAI-compatible API, such as Ollama at
// http://localhost:11434/v1, with the key in OPENAI_API_KEY if one is set.
//
// Progress goes to standard error; the review goes to standard output.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/DimaMaimesko/dev-digest/api/internal/anthropic"
	"github.com/DimaMaimesko/dev-digest/api/internal/diff"
	"github.com/DimaMaimesko/dev-digest/api/internal/openai"
	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, "review:", err)
		stop()
		os.Exit(1)
	}
}

// run is the whole command, with the process's environment passed in, so
// tests can call it.
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) error {
	flags := flag.NewFlagSet("review", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var (
		model      = flags.String("model", "", "model ID, such as deepseek/deepseek-v4-flash (required)")
		promptFile = flags.String("prompt", "", "file with the agent's system prompt (required)")
		diffFile   = flags.String("diff", "-", "file with the diff to review; - reads standard input")
		provider   = flags.String("provider", "openrouter", "openrouter, openai or anthropic")
		baseURL    = flags.String("base-url", "", "call another OpenAI-compatible API at this URL instead")
		strategy   = flags.String("strategy", "auto", "auto, single-pass or map-reduce")
		task       = flags.String("task", "", `one line framing the review, such as "Review PR #482"`)
		asJSON     = flags.Bool("json", false, "print the result as JSON")
		quiet      = flags.Bool("quiet", false, "don't print progress")
	)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *model == "" || *promptFile == "" {
		flags.Usage()
		return errors.New("-model and -prompt are required")
	}
	s := review.Strategy(*strategy)
	if s != review.StrategyAuto && s != review.StrategySinglePass && s != review.StrategyMapReduce {
		return fmt.Errorf("-strategy %q: want auto, single-pass or map-reduce", *strategy)
	}

	llm, err := newLLM(*provider, *baseURL, getenv)
	if err != nil {
		return err
	}
	system, err := os.ReadFile(*promptFile)
	if err != nil {
		return err
	}
	d, err := readDiff(*diffFile, stdin)
	if err != nil {
		return err
	}

	in := review.Input{
		Model:    *model,
		Prompt:   review.Prompt{System: string(system), Task: *task},
		Diff:     d,
		Strategy: s,
	}
	if !*quiet {
		in.OnEvent = func(e review.Event) { fmt.Fprintf(stderr, "▸ %s\n", e.Message) }
	}
	res, err := review.Run(ctx, llm, in)
	if err != nil {
		return err
	}

	if *asJSON {
		return printJSON(stdout, res)
	}
	printText(stdout, res)
	return nil
}

// newLLM returns the client for the API the flags name, with its key from the
// environment.
func newLLM(provider, baseURL string, getenv func(string) string) (review.LLM, error) {
	if baseURL != "" {
		return openai.NewCompatible(baseURL, getenv("OPENAI_API_KEY")), nil
	}
	switch provider {
	case "openrouter":
		key := getenv("OPENROUTER_API_KEY")
		if key == "" {
			return nil, errors.New("set OPENROUTER_API_KEY, or choose another -provider")
		}
		return openai.NewOpenRouter(key), nil
	case "openai":
		key := getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, errors.New("set OPENAI_API_KEY")
		}
		return openai.New(key), nil
	case "anthropic":
		key := getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, errors.New("set ANTHROPIC_API_KEY")
		}
		return anthropic.New(anthropic.DefaultURL, key), nil
	}
	return nil, fmt.Errorf("-provider %q: want openrouter, openai or anthropic", provider)
}

// readDiff reads and parses the diff in file, or in stdin when file is "-".
func readDiff(file string, stdin io.Reader) (diff.Diff, error) {
	var (
		raw []byte
		err error
	)
	if file == "-" {
		raw, err = io.ReadAll(stdin)
	} else {
		raw, err = os.ReadFile(file)
	}
	if err != nil {
		return diff.Diff{}, err
	}
	d, err := diff.Parse(string(raw))
	if err != nil {
		return diff.Diff{}, fmt.Errorf("parse diff: %w", err)
	}
	if len(d.Files) == 0 {
		return diff.Diff{}, errors.New("the diff changes no files; pipe in the output of git diff")
	}
	return d, nil
}

// printText prints a review for people to read.
func printText(w io.Writer, res review.Result) {
	r := res.Review
	fmt.Fprintf(w, "\n%s · score %d · grounding %s · %d → %d tokens\n",
		r.Verdict, r.Score, res.Grounding.Summary(), res.TokensIn, res.TokensOut)
	if r.Summary != "" {
		fmt.Fprintf(w, "\n%s\n", r.Summary)
	}
	for _, f := range r.Findings {
		fmt.Fprintf(w, "\n%-10s %-8s %s\n", f.Severity, f.Category, location(f))
		fmt.Fprintf(w, "  %s\n", f.Title)
		fmt.Fprintf(w, "%s\n", indent(f.Rationale))
		if f.Suggestion != "" {
			fmt.Fprintf(w, "  Suggestion:\n%s\n", indent(f.Suggestion))
		}
	}
	if len(res.Grounding.Dropped) > 0 {
		fmt.Fprintf(w, "\nDropped by grounding:\n")
		for _, d := range res.Grounding.Dropped {
			fmt.Fprintf(w, "  %q: %s\n", d.Finding.Title, d.Reason)
		}
	}
}

func location(f review.Finding) string {
	if f.StartLine == f.EndLine {
		return fmt.Sprintf("%s:%d", f.File, f.StartLine)
	}
	return fmt.Sprintf("%s:%d-%d", f.File, f.StartLine, f.EndLine)
}

func indent(text string) string {
	return "    " + strings.ReplaceAll(strings.TrimSpace(text), "\n", "\n    ")
}

// printJSON prints a review for programs to read.
func printJSON(w io.Writer, res review.Result) error {
	type dropped struct {
		Finding review.Finding `json:"finding"`
		Reason  string         `json:"reason"`
	}
	out := struct {
		Review    review.Review   `json:"review"`
		Grounding string          `json:"grounding"`
		Dropped   []dropped       `json:"dropped"`
		Strategy  review.Strategy `json:"strategy"`
		TokensIn  int             `json:"tokens_in"`
		TokensOut int             `json:"tokens_out"`
	}{
		Review:    res.Review,
		Grounding: res.Grounding.Summary(),
		Dropped:   []dropped{},
		Strategy:  res.Strategy,
		TokensIn:  res.TokensIn,
		TokensOut: res.TokensOut,
	}
	for _, d := range res.Grounding.Dropped {
		out.Dropped = append(out.Dropped, dropped{d.Finding, d.Reason})
	}
	if out.Review.Findings == nil {
		out.Review.Findings = []review.Finding{} // [] rather than null
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
