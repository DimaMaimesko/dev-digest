// Package mcpserver is devdigest-mcp: an MCP server that lets a coding agent
// list DevDigest's reviewers, run one on a pull request, and read the
// findings, a repository's conventions and its blast radius. It is a client
// of the HTTP API (see specs/mcp.md).
//
// Every word here is paid for in tokens, at the start of each chat (the
// instructions and the tool list) or on each call (the answers). Keep the
// descriptions short and the answers compact.
package mcpserver

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// instructions are sent to the client once and stay in every chat's context:
// what the server is and the order of calls, nothing a tool's own
// description says.
const instructions = `DevDigest: local AI pull-request review. A pull request is named by repo ("owner/name") and pr (its number); the repo must be imported in DevDigest. Flow: devdigest_list_agents, then devdigest_run_agent (wait=true blocks until done; runs cost money, start one only when asked), then devdigest_get_findings. devdigest_get_conventions gives a repo's coding rules.`

// Config is what the server needs.
type Config struct {
	// APIURL is the DevDigest API's base URL, such as DefaultAPIURL.
	APIURL string
	// HTTP calls the API. Nil means a client with a one-minute timeout:
	// listing pull requests may sync them from GitHub first.
	HTTP *http.Client
	// PollEvery and WaitAtMost bound devdigest_run_agent's wait; zero
	// means 2 seconds and 5 minutes.
	PollEvery, WaitAtMost time.Duration
}

// DefaultAPIURL is where scripts/dev.sh serves the API.
const DefaultAPIURL = "http://127.0.0.1:3001"

// New returns the MCP server with its tools. Run it with a transport, such
// as mcp.StdioTransport.
func New(cfg Config, version string) *mcp.Server {
	t := tools{
		api:        api{base: strings.TrimRight(cfg.APIURL, "/"), http: cfg.HTTP},
		pollEvery:  cfg.PollEvery,
		waitAtMost: cfg.WaitAtMost,
	}
	if t.api.http == nil {
		t.api.http = &http.Client{Timeout: time.Minute}
	}
	if t.pollEvery == 0 {
		t.pollEvery = 2 * time.Second
	}
	if t.waitAtMost == 0 {
		t.waitAtMost = 5 * time.Minute
	}

	s := mcp.NewServer(&mcp.Implementation{Name: "devdigest", Version: version},
		&mcp.ServerOptions{Instructions: instructions})
	t.register(s)
	return s
}

// answer is a tool's result: v as compact JSON text. There's no structured
// copy of it: that would double what the client receives.
func answer(v any) (*mcp.CallToolResult, any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
}

// cut shortens s to at most n characters, between characters.
func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func boolPtr(b bool) *bool { return &b }
