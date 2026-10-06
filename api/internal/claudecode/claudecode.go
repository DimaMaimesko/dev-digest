// Package claudecode asks Claude through the Claude Code CLI (`claude -p`)
// instead of the API, so reviews run on the signed-in Claude subscription
// and need no API key. It is meant for one person on their own machine.
package claudecode

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

// Client runs the claude executable once per request.
type Client struct {
	bin string   // the claude executable, a name on PATH or a path
	dir string   // the working directory of each run
	env []string // the environment of each run
}

// Client is an LLM for the review package.
var _ review.LLM = (*Client)(nil)

// New returns a client that runs bin with environ, minus ANTHROPIC_API_KEY:
// with that key set, the CLI bills the API instead of the subscription.
//
// Each run starts in the system's temporary directory, so the CLI doesn't
// pick up the CLAUDE.md of whichever project the API was started in.
func New(bin string, environ []string) *Client {
	var env []string
	for _, kv := range environ {
		if !strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			env = append(env, kv)
		}
	}
	return &Client{bin: bin, dir: os.TempDir(), env: env}
}

// Model is a model the CLI accepts.
type Model struct {
	ID   string // an alias the CLI resolves to the newest model of the family
	Name string // for people
}

// Models returns the model aliases the CLI accepts, for the agent editor.
// Full model IDs, such as "claude-sonnet-5", work as well.
func Models() []Model {
	return []Model{
		{ID: "sonnet", Name: "Claude Sonnet (latest, via Claude Code)"},
		{ID: "opus", Name: "Claude Opus (latest, via Claude Code)"},
		{ID: "haiku", Name: "Claude Haiku (latest, via Claude Code)"},
	}
}

// defaultSystem replaces the CLI's own system prompt when the request has
// none: that one describes a coding agent and costs about 20k tokens a call.
const defaultSystem = "Answer with JSON that matches the given schema."

// CompleteJSON runs `claude -p` with req's schema as --json-schema, with
// every tool, setting, skill and MCP server of the CLI turned off, so the run
// is a plain completion. It returns ctx.Err() when ctx ends first.
func (c *Client) CompleteJSON(ctx context.Context, req review.JSONRequest) (review.JSONResponse, error) {
	system := req.System
	if system == "" {
		system = defaultSystem
	}
	cmd := exec.CommandContext(ctx, c.bin,
		"-p",
		"--output-format", "json",
		"--model", req.Model,
		"--system-prompt", system,
		"--json-schema", string(req.Schema),
		"--tools", "",
		"--strict-mcp-config",
		"--setting-sources", "",
		"--disable-slash-commands",
		"--no-session-persistence",
	)
	cmd.Dir = c.dir
	cmd.Env = c.env
	cmd.Stdin = strings.NewReader(prompt(req.Messages))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// After a kill, don't wait forever for pipes a child process holds open.
	cmd.WaitDelay = 5 * time.Second

	runErr := cmd.Run()
	if err := ctx.Err(); err != nil {
		return review.JSONResponse{}, err
	}
	var out result
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		if runErr != nil {
			return review.JSONResponse{}, fmt.Errorf("run %s: %w: %s", c.bin, runErr, firstLine(stderr.String()))
		}
		return review.JSONResponse{}, fmt.Errorf("read the output of %s: %w", c.bin, err)
	}

	res := review.JSONResponse{
		TokensIn:  out.Usage.InputTokens + out.Usage.CacheCreationInputTokens + out.Usage.CacheReadInputTokens,
		TokensOut: out.Usage.OutputTokens,
		// CostUSD stays nil: total_cost_usd is what the call would cost on
		// the API, not what the subscription is charged.
	}
	if out.IsError || runErr != nil {
		msg := out.Result
		if msg == "" {
			msg = cmp.Or(firstLine(stderr.String()), fmt.Sprint(runErr))
		}
		return res, fmt.Errorf("claude: %s", msg)
	}
	res.Text = out.Result
	if len(out.StructuredOutput) > 0 && string(out.StructuredOutput) != "null" {
		res.Text = string(out.StructuredOutput)
	}
	return res, nil
}

// result is what `claude -p --output-format json` prints.
type result struct {
	IsError bool   `json:"is_error"`
	Result  string `json:"result"` // the answer, or the error message
	// StructuredOutput is the answer checked against --json-schema; absent
	// when the run failed.
	StructuredOutput json.RawMessage `json:"structured_output"`
	Usage            struct {
		InputTokens              int `json:"input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		OutputTokens             int `json:"output_tokens"`
	} `json:"usage"`
}

// prompt turns the conversation into the one prompt the CLI reads from
// stdin. A single user message is sent as it is. A longer conversation, such
// as a retry after an invalid answer, is written out turn by turn, since the
// CLI can't be handed earlier assistant turns.
func prompt(messages []review.Message) string {
	if len(messages) == 1 && messages[0].Role == review.RoleUser {
		return messages[0].Content
	}
	var b strings.Builder
	for i, m := range messages {
		if i > 0 {
			b.WriteString("\n\n")
		}
		if m.Role == review.RoleAssistant {
			b.WriteString("<your_earlier_answer>\n" + m.Content + "\n</your_earlier_answer>")
		} else {
			b.WriteString(m.Content)
		}
	}
	return b.String()
}

// firstLine returns the first non-empty line of s.
func firstLine(s string) string {
	for line := range strings.Lines(s) {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// ErrNotInstalled means the claude executable isn't on PATH.
var ErrNotInstalled = errors.New("the claude CLI is not installed or not on PATH")

// Find returns the path of the claude executable on PATH.
func Find() (string, error) {
	path, err := exec.LookPath("claude")
	if err != nil {
		return "", ErrNotInstalled
	}
	return path, nil
}
