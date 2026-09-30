package runner

import (
	"encoding/json"
	"math"
	"strconv"
	"time"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

// traceJSON is the document saved for each run, which the trace view shows
// (RunTrace in client/src/vendor/shared/contracts/trace.ts).
type traceJSON struct {
	Config struct {
		Agent    string `json:"agent"`
		Version  string `json:"version"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		PR       int32  `json:"pr"`
		Source   string `json:"source"`
	} `json:"config"`
	Stats struct {
		DurationMs int64    `json:"duration_ms"`
		TokensIn   int      `json:"tokens_in"`
		TokensOut  int      `json:"tokens_out"`
		CostUSD    *float64 `json:"cost_usd"` // null when the provider doesn't say
		Findings   int      `json:"findings"`
		Grounding  string   `json:"grounding"`
	} `json:"stats"`
	PromptAssembly assemblyJSON   `json:"prompt_assembly"`
	ToolCalls      []toolCallJSON `json:"tool_calls"`
	RawOutput      string         `json:"raw_output"`
	MemoryPulled   []any          `json:"memory_pulled"`
	SpecsRead      []any          `json:"specs_read"`
	Log            []logLineJSON  `json:"log"`
}

// assemblyJSON is the prompt as sent; a section left out is null.
type assemblyJSON struct {
	System        string  `json:"system"`
	Skills        *string `json:"skills"`
	Memory        *string `json:"memory"`
	Specs         *string `json:"specs"`
	Callers       *string `json:"callers"`
	RepoMap       *string `json:"repo_map"`
	PRDescription *string `json:"pr_description"`
	User          string  `json:"user"`
}

type toolCallJSON struct {
	Tool string `json:"tool"`
	Args string `json:"args"`
	Meta string `json:"meta"`
	Ms   int64  `json:"ms"`
}

type logLineJSON struct {
	T    string `json:"t"`
	Kind string `json:"kind"`
	Msg  string `json:"msg"`
}

func (r *Runner) newTrace(j job, pull postgres.PullRequest, took time.Duration) traceJSON {
	var t traceJSON
	t.Config.Agent, t.Config.Version = j.agent.Name, strconv.Itoa(int(j.agent.Version))
	t.Config.Provider, t.Config.Model = j.agent.Provider, j.agent.Model
	t.Config.PR, t.Config.Source = pull.Number, "local"
	t.Stats.DurationMs = took.Milliseconds()
	t.ToolCalls, t.MemoryPulled, t.SpecsRead = []toolCallJSON{}, []any{}, []any{}
	t.Log = []logLineJSON{}
	for _, e := range r.bus.log(j.run) {
		t.Log = append(t.Log, logLineJSON{T: e.T, Kind: e.Kind, Msg: e.Msg})
	}
	return t
}

// successTrace is the trace of a finished review.
func (r *Runner) successTrace(j job, pull postgres.PullRequest, res review.Result, took time.Duration) []byte {
	t := r.newTrace(j, pull, took)
	t.Stats.TokensIn, t.Stats.TokensOut, t.Stats.CostUSD = res.TokensIn, res.TokensOut, res.CostUSD
	t.Stats.Findings, t.Stats.Grounding = len(res.Review.Findings), res.Grounding.Summary()
	a := res.Assembly
	t.PromptAssembly = assemblyJSON{
		System: a.System, Skills: orNull(a.Skills), Memory: orNull(a.Memory), Specs: orNull(a.Specs),
		Callers: orNull(a.Callers), RepoMap: orNull(a.RepoMap), PRDescription: orNull(a.PRDescription), User: a.User,
	}
	// Each model call gets an equal share of the time.
	per := int64(math.Round(float64(took.Milliseconds()) / float64(max(len(res.Chunks), 1))))
	for _, c := range res.Chunks {
		t.ToolCalls = append(t.ToolCalls, toolCallJSON{Tool: "review_file", Args: c, Meta: string(res.Strategy), Ms: per})
	}
	t.RawOutput = res.Raw
	return mustJSON(t)
}

// failureTrace is the trace of a run that failed or was cancelled: its log
// says why, and its stats what the model calls made before took.
func (r *Runner) failureTrace(j job, pull postgres.PullRequest, took time.Duration, spent review.Usage) []byte {
	t := r.newTrace(j, pull, took)
	t.Stats.TokensIn, t.Stats.TokensOut, t.Stats.CostUSD = spent.TokensIn, spent.TokensOut, spent.CostUSD
	t.Stats.Grounding = "0/0 passed"
	t.PromptAssembly = assemblyJSON{System: j.agent.SystemPrompt}
	return mustJSON(t)
}

func orNull(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// mustJSON encodes v, which holds nothing that can fail to encode.
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
