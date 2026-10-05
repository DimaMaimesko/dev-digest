package mcpserver_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/DimaMaimesko/dev-digest/api/internal/mcpserver"
)

// fakeAPI answers the DevDigest routes the tools call with canned JSON.
type fakeAPI struct {
	mu      sync.Mutex
	routes  map[string]string // "GET /agents" → JSON
	runs    []string          // successive answers of GET /pulls/pr-12/runs; the last repeats
	started string            // the answer of POST /pulls/pr-12/review
	posted  []map[string]any  // the review requests' bodies
	calls   map[string]int    // requests per route
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{
		routes: map[string]string{
			"GET /repos":                    `[{"id": "repo-1", "full_name": "acme/web"}, {"id": "repo-2", "full_name": "acme/api"}]`,
			"GET /repos/repo-1/pulls":       `[{"id": "pr-12", "number": 12}, {"id": "pr-13", "number": 13}]`,
			"GET /agents":                   agentsJSON,
			"GET /pulls/pr-12/reviews":      reviewsJSON,
			"GET /repos/repo-1/conventions": `[]`,
			"GET /skills":                   `[]`,
		},
		runs:    []string{runsJSON},
		started: `[{"runId": "run-new", "agentId": "agent-general", "agentName": "General Reviewer"}]`,
		calls:   map[string]int{},
	}
}

var agentsJSON = `[
	{"id": "agent-general", "name": "General Reviewer", "description": "Correctness, tests and clarity.", "provider": "openrouter", "model": "deepseek/v4", "system_prompt": "SECRET PROMPT", "enabled": true, "skill_count": 2},
	{"id": "agent-security", "name": "Security Reviewer", "description": "` + longText + `", "provider": "anthropic", "model": "claude-sonnet-5", "system_prompt": "x", "enabled": false, "skill_count": 0}
]`

var longText = strings.Repeat("a", 250)

// reviewsJSON is a PR's reviews, newest first: General has two, Security one.
const reviewsJSON = `[
	{"agent_id": "agent-general", "agent_name": "General Reviewer", "run_id": "run-2", "verdict": "comment", "score": 61, "findings": [
		{"severity": "SUGGESTION", "category": "style", "title": "Rename x", "file": "a.ts", "start_line": 1, "end_line": 1, "rationale": "r", "suggestion": null, "confidence": 0.5, "accepted_at": null, "dismissed_at": null},
		{"severity": "CRITICAL", "category": "bug", "title": "Null deref", "file": "b.ts", "start_line": 3, "end_line": 7, "rationale": "It crashes.", "suggestion": "Check it.", "confidence": 0.9, "accepted_at": "2026-10-01T00:00:00.000Z", "dismissed_at": null},
		{"severity": "WARNING", "category": "bug", "title": "Dismissed one", "file": "c.ts", "start_line": 2, "end_line": 2, "rationale": "r", "suggestion": null, "confidence": 0.7, "accepted_at": null, "dismissed_at": "2026-10-01T00:00:00.000Z"},
		{"severity": "WARNING", "category": "perf", "title": "Slow loop", "file": "d.ts", "start_line": 9, "end_line": 9, "rationale": "r", "suggestion": null, "confidence": 0.6, "accepted_at": null, "dismissed_at": null}
	]},
	{"agent_id": "agent-security", "agent_name": "Security Reviewer", "run_id": "run-sec", "verdict": "approve", "score": 100, "findings": []},
	{"agent_id": "agent-general", "agent_name": "General Reviewer", "run_id": "run-1", "verdict": "comment", "score": 88, "findings": [
		{"severity": "WARNING", "category": "bug", "title": "Old finding", "file": "a.ts", "start_line": 4, "end_line": 4, "rationale": "r", "suggestion": null, "confidence": 0.8, "accepted_at": null, "dismissed_at": null}
	]}
]`

const runsJSON = `[
	{"run_id": "run-running", "agent_name": "General Reviewer", "status": "running"},
	{"run_id": "run-failed", "agent_name": "General Reviewer", "status": "failed", "error": "model timed out"},
	{"run_id": "run-cancelled", "agent_name": "General Reviewer", "status": "cancelled"},
	{"run_id": "run-2", "agent_name": "General Reviewer", "status": "done", "score": 61, "blockers": 1},
	{"run_id": "run-sec", "agent_name": "Security Reviewer", "status": "done", "score": 100, "blockers": 0},
	{"run_id": "run-1", "agent_name": "General Reviewer", "status": "done", "score": 88, "blockers": 0}
]`

// count is how many requests route had.
func (f *fakeAPI) count(route string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[route]
}

// reviewRequests are the bodies of the review requests so far.
func (f *fakeAPI) reviewRequests() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.posted)
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	route := r.Method + " " + r.URL.Path
	f.calls[route]++
	w.Header().Set("Content-Type", "application/json")
	switch route {
	case "POST /pulls/pr-12/review":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.posted = append(f.posted, body)
		io.WriteString(w, f.started)
		return
	case "GET /pulls/pr-12/runs":
		i := min(f.calls[route], len(f.runs)) - 1
		io.WriteString(w, f.runs[i])
		return
	}
	if body, ok := f.routes[route]; ok {
		io.WriteString(w, body)
		return
	}
	w.WriteHeader(http.StatusNotFound)
	io.WriteString(w, `{"error": {"code": "not_found", "message": "Route not found"}}`)
}

// connect serves the MCP server against api and returns a client session.
func connect(t *testing.T, api http.Handler, cfg mcpserver.Config) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	if cfg.APIURL == "" {
		ts := httptest.NewServer(api)
		t.Cleanup(ts.Close)
		cfg.APIURL = ts.URL
	}
	serverT, clientT := mcp.NewInMemoryTransports()
	ss, err := mcpserver.New(cfg, "test").Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// call calls a tool and returns its text, failing unless isError is wantErr.
func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any, wantErr bool) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("%s: %d content blocks, want 1", tool, len(res.Content))
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if res.IsError != wantErr {
		t.Fatalf("%s: isError = %v, want %v; text: %s", tool, res.IsError, wantErr, text)
	}
	if res.StructuredContent != nil {
		t.Errorf("%s: structuredContent is set: the answer would be sent twice", tool)
	}
	return text
}

// callJSON calls a tool that must succeed and decodes its answer.
func callJSON(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) map[string]any {
	t.Helper()
	text := call(t, cs, tool, args, false)
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("%s: answer isn't JSON: %v\n%s", tool, err, text)
	}
	if strings.ContainsAny(text, "\n\t") || strings.Contains(text, "null") {
		t.Errorf("%s: answer isn't compact: %s", tool, text)
	}
	return out
}

// equalJSON checks that got, decoded JSON, is the JSON want.
func equalJSON(t *testing.T, got any, want string) {
	t.Helper()
	var w any
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("bad want JSON: %v", err)
	}
	if !reflect.DeepEqual(got, w) {
		g, _ := json.Marshal(got)
		t.Errorf("got:\n%s\nwant:\n%s", g, want)
	}
}

func TestToolList(t *testing.T) {
	cs := connect(t, newFakeAPI(), mcpserver.Config{})

	// What every chat pays for: keep it small.
	if n := len(cs.InitializeResult().Instructions); n == 0 || n > 600 {
		t.Errorf("instructions are %d characters, want 1–600", n)
	}
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if n := len(tool.Description); n > 300 {
			t.Errorf("%s: description is %d characters, want at most 300", tool.Name, n)
		}
		if tool.OutputSchema != nil {
			t.Errorf("%s: has an output schema", tool.Name)
		}
		readOnly := tool.Name != "devdigest_run_agent"
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint != readOnly {
			t.Errorf("%s: readOnlyHint should be %v", tool.Name, readOnly)
		}
	}
	slices.Sort(names)
	want := []string{"devdigest_get_blast_radius", "devdigest_get_conventions", "devdigest_get_findings", "devdigest_list_agents", "devdigest_run_agent"}
	if !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v", names, want)
	}

	schemas := map[string]map[string]any{}
	for _, tool := range res.Tools {
		b, _ := json.Marshal(tool.InputSchema)
		var s map[string]any
		json.Unmarshal(b, &s)
		schemas[tool.Name] = s
	}
	required := map[string][]any{
		"devdigest_run_agent":        {"repo", "pr", "agent"},
		"devdigest_get_findings":     {"repo", "pr"},
		"devdigest_get_conventions":  {"repo"},
		"devdigest_get_blast_radius": {"repo", "pr"},
	}
	for tool, want := range required {
		got, _ := schemas[tool]["required"].([]any)
		if !slices.Equal(got, want) {
			t.Errorf("%s: required = %v, want %v", tool, got, want)
		}
	}
	props := schemas["devdigest_get_findings"]["properties"].(map[string]any)
	equalJSON(t, props["min_severity"].(map[string]any)["enum"], `["suggestion", "warning", "critical"]`)
	equalJSON(t, props["format"].(map[string]any)["enum"], `["concise", "detailed"]`)
}

func TestListAgents(t *testing.T) {
	cs := connect(t, newFakeAPI(), mcpserver.Config{})
	got := callJSON(t, cs, "devdigest_list_agents", nil)
	equalJSON(t, got, `{"agents": [
		{"id": "agent-general", "name": "General Reviewer", "enabled": true, "model": "openrouter/deepseek/v4", "skills": 2, "description": "Correctness, tests and clarity."},
		{"id": "agent-security", "name": "Security Reviewer", "enabled": false, "model": "anthropic/claude-sonnet-5", "description": "`+strings.Repeat("a", 200)+`…"}
	]}`)
}

func TestRunAgent(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]any
		started  string // the API's answer; default: one run
		wantBody string // the review request; "" when none is sent
		wantErr  string // in the error text
	}{
		{name: "by name, any case", args: map[string]any{"repo": "ACME/web", "pr": 12, "agent": "general reviewer"},
			wantBody: `{"agentId": "agent-general"}`},
		{name: "by ID", args: map[string]any{"repo": "acme/web", "pr": 12, "agent": "agent-security"},
			wantBody: `{"agentId": "agent-security"}`},
		{name: "all", args: map[string]any{"repo": "acme/web", "pr": 12, "agent": "all"},
			wantBody: `{"all": true}`},
		{name: "no agent enabled", args: map[string]any{"repo": "acme/web", "pr": 12, "agent": "all"}, started: `[]`,
			wantBody: `{"all": true}`, wantErr: "no agent is enabled"},
		{name: "unknown agent", args: map[string]any{"repo": "acme/web", "pr": 12, "agent": "Perf"},
			wantErr: `no agent "Perf"; agents: General Reviewer, Security Reviewer`},
		{name: "unknown repo", args: map[string]any{"repo": "acme/mobile", "pr": 12, "agent": "all"},
			wantErr: `repository "acme/mobile" isn't imported in DevDigest; imported: acme/web, acme/api`},
		{name: "unknown PR", args: map[string]any{"repo": "acme/web", "pr": 99, "agent": "all"},
			wantErr: "acme/web#99 isn't in DevDigest"},
		{name: "not a PR number", args: map[string]any{"repo": "acme/web", "pr": 0, "agent": "all"},
			wantErr: "pr must be a pull request number"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeAPI()
			if tt.started != "" {
				api.started = tt.started
			}
			cs := connect(t, api, mcpserver.Config{})
			if tt.wantErr != "" {
				if text := call(t, cs, "devdigest_run_agent", tt.args, true); !strings.Contains(text, tt.wantErr) {
					t.Errorf("error = %q, want it to contain %q", text, tt.wantErr)
				}
			} else {
				got := callJSON(t, cs, "devdigest_run_agent", tt.args)
				equalJSON(t, got, `{"pr": "acme/web#12", "runs": [{"run_id": "run-new", "agent": "General Reviewer", "status": "running"}],
					"next": "devdigest_get_findings with a run_id once it's done; a run takes about a minute"}`)
			}
			posted := api.reviewRequests()
			switch {
			case tt.wantBody == "" && len(posted) > 0:
				t.Errorf("a review was started: %v", posted)
			case tt.wantBody != "":
				if len(posted) != 1 {
					t.Fatalf("%d review requests, want 1", len(posted))
				}
				equalJSON(t, posted[0], tt.wantBody)
			}
		})
	}
}

func TestRunAgentWait(t *testing.T) {
	api := newFakeAPI()
	api.started = `[{"runId": "run-2", "agentName": "General Reviewer"}, {"runId": "run-sec", "agentName": "Security Reviewer"}]`
	api.runs = []string{
		`[{"run_id": "run-2", "status": "running"}, {"run_id": "run-sec", "status": "running"}]`,
		`[{"run_id": "run-2", "status": "running"}, {"run_id": "run-sec", "status": "done", "score": 100, "blockers": 0}]`,
		`[{"run_id": "run-2", "status": "done", "score": 61, "blockers": 1}, {"run_id": "run-sec", "status": "done", "score": 100, "blockers": 0}]`,
	}
	cs := connect(t, api, mcpserver.Config{PollEvery: time.Millisecond})

	got := callJSON(t, cs, "devdigest_run_agent", map[string]any{"repo": "acme/web", "pr": 12, "agent": "all", "wait": true})
	// Findings are counted by severity, dismissed ones left out; no findings at all → no field.
	equalJSON(t, got, `{"pr": "acme/web#12", "runs": [
		{"run_id": "run-2", "agent": "General Reviewer", "status": "done", "score": 61, "blockers": 1, "findings": {"critical": 1, "warning": 1, "suggestion": 1}},
		{"run_id": "run-sec", "agent": "Security Reviewer", "status": "done", "score": 100, "blockers": 0}
	]}`)
	if n := api.count("GET /pulls/pr-12/runs"); n != 3 {
		t.Errorf("polled %d times, want 3", n)
	}
}

func TestRunAgentWaitGivesUp(t *testing.T) {
	api := newFakeAPI()
	api.runs = []string{`[{"run_id": "run-new", "status": "running"}]`}
	cs := connect(t, api, mcpserver.Config{PollEvery: time.Millisecond, WaitAtMost: 20 * time.Millisecond})

	got := callJSON(t, cs, "devdigest_run_agent", map[string]any{"repo": "acme/web", "pr": 12, "agent": "General Reviewer", "wait": true})
	equalJSON(t, got, `{"pr": "acme/web#12", "runs": [{"run_id": "run-new", "agent": "General Reviewer", "status": "running"}],
		"next": "devdigest_get_findings with a run_id once it's done; a run takes about a minute"}`)
	if api.count("POST /runs/run-new/cancel") != 0 {
		t.Error("the run was cancelled")
	}
}

func TestGetFindings(t *testing.T) {
	pr := map[string]any{"repo": "acme/web", "pr": 12}
	with := func(kv ...any) map[string]any {
		args := map[string]any{}
		for k, v := range pr {
			args[k] = v
		}
		for i := 0; i < len(kv); i += 2 {
			args[kv[i].(string)] = kv[i+1]
		}
		return args
	}
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "each agent's latest review, most severe first, dismissed left out",
			args: pr,
			want: `{"pr": "acme/web#12", "reviews": [
				{"agent": "General Reviewer", "run_id": "run-2", "score": 61, "verdict": "comment", "findings": [
					{"severity": "CRITICAL", "at": "b.ts:3-7", "title": "Null deref"},
					{"severity": "WARNING", "at": "d.ts:9", "title": "Slow loop"},
					{"severity": "SUGGESTION", "at": "a.ts:1", "title": "Rename x"}]},
				{"agent": "Security Reviewer", "run_id": "run-sec", "score": 100, "verdict": "approve", "findings": []}]}`,
		},
		{
			name: "one run",
			args: with("run_id", "run-1"),
			want: `{"pr": "acme/web#12", "reviews": [{"agent": "General Reviewer", "run_id": "run-1", "score": 88, "verdict": "comment", "findings": [
				{"severity": "WARNING", "at": "a.ts:4", "title": "Old finding"}]}]}`,
		},
		{
			name: "min severity and detailed",
			args: with("min_severity", "critical", "format", "detailed", "run_id", "run-2"),
			want: `{"pr": "acme/web#12", "reviews": [{"agent": "General Reviewer", "run_id": "run-2", "score": 61, "verdict": "comment", "findings": [
				{"severity": "CRITICAL", "at": "b.ts:3-7", "title": "Null deref", "category": "bug", "rationale": "It crashes.", "suggestion": "Check it.", "confidence": 0.9, "accepted": true}]}]}`,
		},
		{
			name: "limit",
			args: with("limit", 1),
			want: `{"pr": "acme/web#12", "truncated": 2, "reviews": [
				{"agent": "General Reviewer", "run_id": "run-2", "score": 61, "verdict": "comment", "findings": [{"severity": "CRITICAL", "at": "b.ts:3-7", "title": "Null deref"}]},
				{"agent": "Security Reviewer", "run_id": "run-sec", "score": 100, "verdict": "approve", "findings": []}]}`,
		},
		{
			name: "a run still going",
			args: with("run_id", "run-running"),
			want: `{"pr": "acme/web#12", "run_id": "run-running", "status": "running", "next": "still running: call again in a minute"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := connect(t, newFakeAPI(), mcpserver.Config{})
			equalJSON(t, callJSON(t, cs, "devdigest_get_findings", tt.args), tt.want)
		})
	}

	errs := []struct {
		name string
		args map[string]any
		want string
	}{
		{"failed run", with("run_id", "run-failed"), "run run-failed failed: model timed out"},
		{"cancelled run", with("run_id", "run-cancelled"), "was cancelled"},
		{"another PR's run", with("run_id", "run-elsewhere"), "run run-elsewhere is not a run of acme/web#12"},
		{"unknown severity", with("min_severity", "fatal"), "min_severity"},
		{"limit too big", with("limit", 500), "limit must be between 1 and 200"},
	}
	for _, tt := range errs {
		t.Run(tt.name, func(t *testing.T) {
			cs := connect(t, newFakeAPI(), mcpserver.Config{})
			if text := call(t, cs, "devdigest_get_findings", tt.args, true); !strings.Contains(text, tt.want) {
				t.Errorf("error = %q, want it to contain %q", text, tt.want)
			}
		})
	}
}

func TestGetFindingsNoReview(t *testing.T) {
	api := newFakeAPI()
	api.routes["GET /pulls/pr-12/reviews"] = `[]`
	cs := connect(t, api, mcpserver.Config{})
	got := callJSON(t, cs, "devdigest_get_findings", map[string]any{"repo": "acme/web", "pr": 12})
	equalJSON(t, got, `{"pr": "acme/web#12", "reviews": [], "next": "no review yet: start one with devdigest_run_agent"}`)
}

func TestGetConventions(t *testing.T) {
	api := newFakeAPI()
	api.routes["GET /repos/repo-1/conventions"] = `[
		{"id": "c1", "rule": "Use async/await", "evidence_path": "src/a.ts", "evidence_snippet": "await x", "confidence": 0.9, "accepted": true},
		{"id": "c2", "rule": "Named exports", "evidence_path": "", "evidence_snippet": "", "confidence": 0, "accepted": false}
	]`
	api.routes["GET /skills"] = `[
		{"name": "no-then-chains", "description": "House rule", "type": "convention", "body": "` + strings.Repeat("b", 2100) + `", "enabled": true},
		{"name": "off", "description": "d", "type": "convention", "body": "x", "enabled": false},
		{"name": "blank", "description": "d", "type": "convention", "body": "  ", "enabled": true},
		{"name": "rubric", "description": "d", "type": "rubric", "body": "x", "enabled": true}
	]`
	cs := connect(t, api, mcpserver.Config{})

	skills := `[{"name": "no-then-chains", "description": "House rule", "body": "` + strings.Repeat("b", 2000) + `…"}]`
	equalJSON(t, callJSON(t, cs, "devdigest_get_conventions", map[string]any{"repo": "acme/web"}), `{
		"repo": "acme/web",
		"conventions": [
			{"rule": "Use async/await", "evidence": "src/a.ts", "confidence": 0.9, "accepted": true},
			{"rule": "Named exports", "accepted": false}],
		"skills": `+skills+`,
		"skills_scope": "workspace: they apply to every repository"}`)

	equalJSON(t, callJSON(t, cs, "devdigest_get_conventions", map[string]any{"repo": "acme/web", "accepted_only": true})["conventions"],
		`[{"rule": "Use async/await", "evidence": "src/a.ts", "confidence": 0.9, "accepted": true}]`)
}

func TestGetBlastRadius(t *testing.T) {
	cs := connect(t, newFakeAPI(), mcpserver.Config{})
	got := callJSON(t, cs, "devdigest_get_blast_radius", map[string]any{"repo": "acme/web", "pr": 12})
	equalJSON(t, got, `{"changed_symbols": [], "downstream": [],
		"summary": "Blast radius isn't implemented yet for acme/web#12: no symbols or callers to report."}`)

	if text := call(t, cs, "devdigest_get_blast_radius", map[string]any{"repo": "acme/web", "pr": 99}, true); !strings.Contains(text, "isn't in DevDigest") {
		t.Errorf("unknown PR: %q", text)
	}
}

func TestAPIErrors(t *testing.T) {
	t.Run("not answering", func(t *testing.T) {
		ts := httptest.NewServer(newFakeAPI())
		ts.Close() // nothing listens there any more
		cs := connect(t, nil, mcpserver.Config{APIURL: ts.URL})
		text := call(t, cs, "devdigest_list_agents", nil, true)
		if !strings.Contains(text, "isn't answering") || !strings.Contains(text, "./scripts/dev.sh") {
			t.Errorf("error = %q, want it to say how to start the API", text)
		}
	})
	t.Run("error envelope", func(t *testing.T) {
		api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"error": {"code": "internal_error", "message": "Something went wrong"}}`)
		})
		cs := connect(t, api, mcpserver.Config{})
		if text := call(t, cs, "devdigest_list_agents", nil, true); text != "DevDigest API: Something went wrong (HTTP 500 internal_error)" {
			t.Errorf("error = %q", text)
		}
	})
}
