package mcpserver

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// tools holds what the tool handlers share.
type tools struct {
	api                   api
	pollEvery, waitAtMost time.Duration
}

// Inputs. A description is one short line: it is sent with the tool list.

type pullInput struct {
	Repo string `json:"repo" jsonschema:"owner/name, as imported in DevDigest"`
	PR   int    `json:"pr" jsonschema:"pull request number"`
}

type runAgentInput struct {
	pullInput
	Agent string `json:"agent" jsonschema:"agent name or ID, or all for every enabled agent"`
	Wait  bool   `json:"wait,omitempty" jsonschema:"wait until the runs finish (at most 5 minutes)"`
}

type findingsInput struct {
	pullInput
	RunID       string `json:"run_id,omitempty" jsonschema:"one run; default: each agent's latest review"`
	MinSeverity string `json:"min_severity,omitempty"`
	Format      string `json:"format,omitempty"`
	Limit       int    `json:"limit,omitempty" jsonschema:"max findings, default 50, at most 200"`
}

type conventionsInput struct {
	Repo         string `json:"repo" jsonschema:"owner/name, as imported in DevDigest"`
	AcceptedOnly bool   `json:"accepted_only,omitempty" jsonschema:"only conventions a person accepted"`
}

func (t tools) register(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "devdigest_list_agents",
		Description: "List DevDigest's reviewer agents: id, name, enabled, model, linked skills, what each reviews.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(false)},
	}, t.listAgents)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "devdigest_run_agent",
		Description: "Start a review of a pull request by one agent, or all enabled ones. Calls a paid model. Answers the run IDs; with wait, each run's status, score and findings count.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(true)},
	}, t.runAgent)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "devdigest_get_findings",
		Description: "Findings of a pull request's reviews, most severe first; dismissed ones left out. A run still going answers its status.",
		InputSchema: schema[findingsInput](map[string]prop{
			"min_severity": {"lowest severity to include, default suggestion", []any{"suggestion", "warning", "critical"}},
			"format":       {"concise (default): severity, file:line, title; detailed adds rationale and suggestion", []any{"concise", "detailed"}},
		}),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(false)},
	}, t.getFindings)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "devdigest_get_conventions",
		Description: "A repository's coding conventions found by DevDigest, and the workspace's enabled convention skills.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(false)},
	}, t.getConventions)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "devdigest_get_blast_radius",
		Description: "The symbols a pull request changes and what depends on them. Not implemented yet: answers empty lists.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(false)},
	}, t.getBlastRadius)
}

// prop adds what struct tags can't say to a property of a tool's input.
type prop struct {
	description string
	enum        []any
}

// schema infers T's input schema, as mcp.AddTool would, and adds props.
func schema[T any](props map[string]prop) *jsonschema.Schema {
	s, err := jsonschema.For[T](nil)
	if err != nil {
		panic(err) // the input types are fixed: a failure is a bug, found by any test
	}
	for name, p := range props {
		ps, ok := s.Properties[name]
		if !ok {
			panic("schema: no property " + name)
		}
		ps.Description, ps.Enum = p.description, p.enum
	}
	return s
}

// devdigest_list_agents

type agentOut struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Model       string `json:"model"`
	Skills      int    `json:"skills,omitempty"`
	Description string `json:"description,omitempty"`
}

func (t tools) listAgents(ctx context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	agents, err := t.api.agents(ctx)
	if err != nil {
		return nil, nil, err
	}
	out := make([]agentOut, 0, len(agents))
	for _, a := range agents {
		out = append(out, agentOut{
			ID:          a.ID,
			Name:        a.Name,
			Enabled:     a.Enabled,
			Model:       a.Provider + "/" + a.Model,
			Skills:      a.SkillCount,
			Description: cut(a.Description, 200),
		})
	}
	return answer(map[string]any{"agents": out})
}

// devdigest_run_agent

type runOut struct {
	RunID    string         `json:"run_id"`
	Agent    string         `json:"agent"`
	Status   string         `json:"status,omitempty"`
	Score    *int           `json:"score,omitempty"`
	Blockers *int           `json:"blockers,omitempty"`
	Findings map[string]int `json:"findings,omitempty"` // by severity
	Error    string         `json:"error,omitempty"`
}

func (t tools) runAgent(ctx context.Context, _ *mcp.CallToolRequest, in runAgentInput) (*mcp.CallToolResult, any, error) {
	body := map[string]any{"all": true}
	if !strings.EqualFold(strings.TrimSpace(in.Agent), "all") {
		agents, err := t.api.agents(ctx)
		if err != nil {
			return nil, nil, err
		}
		agent, err := findAgent(agents, in.Agent)
		if err != nil {
			return nil, nil, err
		}
		body = map[string]any{"agentId": agent.ID}
	}
	pr, err := t.api.findPull(ctx, in.Repo, in.PR)
	if err != nil {
		return nil, nil, err
	}
	var started []startedRunJSON
	if err := t.api.post(ctx, "/pulls/"+pr.id+"/review", body, &started); err != nil {
		return nil, nil, err
	}
	if len(started) == 0 {
		return nil, nil, errors.New("no agent is enabled: enable one in DevDigest, or name an agent")
	}

	runs := make([]runOut, 0, len(started))
	for _, s := range started {
		runs = append(runs, runOut{RunID: s.RunID, Agent: s.AgentName, Status: "running"})
	}
	if in.Wait {
		if err := t.wait(ctx, pr, runs); err != nil {
			return nil, nil, err
		}
	}
	res := map[string]any{"pr": pr.label, "runs": runs}
	if slices.ContainsFunc(runs, func(r runOut) bool { return r.Status == "running" }) {
		res["next"] = "devdigest_get_findings with a run_id once it's done; a run takes about a minute"
	}
	return answer(res)
}

// wait polls the pull request's runs until none of runs is running, or
// waitAtMost passes, and fills in how each ended. It never cancels a run.
func (t tools) wait(ctx context.Context, pr pull, runs []runOut) error {
	ctx, cancel := context.WithTimeout(ctx, t.waitAtMost)
	defer cancel()
	tick := time.NewTicker(t.pollEvery)
	defer tick.Stop()
	for {
		var all []runJSON
		err := t.api.get(ctx, "/pulls/"+pr.id+"/runs", &all)
		if err != nil && ctx.Err() == nil {
			return err
		}
		if err == nil && setStatus(runs, all) {
			return t.countFindings(ctx, pr, runs)
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil // the runs that are still going say "running"
			}
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// setStatus copies each run's status from all, and reports whether every
// one has finished.
func setStatus(runs []runOut, all []runJSON) (finished bool) {
	finished = true
	for i := range runs {
		r := &runs[i]
		for _, a := range all {
			if a.RunID != r.RunID {
				continue
			}
			r.Status = deref(a.Status)
			r.Score, r.Blockers, r.Error = a.Score, a.Blockers, deref(a.Error)
		}
		if r.Status == "running" {
			finished = false
		}
	}
	return finished
}

func (t tools) countFindings(ctx context.Context, pr pull, runs []runOut) error {
	var reviews []reviewJSON
	if err := t.api.get(ctx, "/pulls/"+pr.id+"/reviews", &reviews); err != nil {
		return err
	}
	for i := range runs {
		for _, rv := range reviews {
			if deref(rv.RunID) != runs[i].RunID {
				continue
			}
			for _, f := range rv.Findings {
				if f.DismissedAt != nil {
					continue
				}
				if runs[i].Findings == nil {
					runs[i].Findings = map[string]int{}
				}
				runs[i].Findings[strings.ToLower(f.Severity)]++
			}
		}
	}
	return nil
}

// devdigest_get_findings

// severities ranks the severities, the least severe first.
var severities = []string{"SUGGESTION", "WARNING", "CRITICAL"}

func rank(severity string) int { return slices.Index(severities, strings.ToUpper(severity)) }

type reviewOut struct {
	Agent    string       `json:"agent"`
	RunID    string       `json:"run_id,omitempty"`
	Score    *int         `json:"score,omitempty"`
	Verdict  string       `json:"verdict,omitempty"`
	Findings []findingOut `json:"findings"`
}

type findingOut struct {
	Severity   string  `json:"severity"`
	At         string  `json:"at"`
	Title      string  `json:"title"`
	Category   string  `json:"category,omitempty"`
	Rationale  string  `json:"rationale,omitempty"`
	Suggestion string  `json:"suggestion,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	Accepted   bool    `json:"accepted,omitempty"`
}

func (t tools) getFindings(ctx context.Context, _ *mcp.CallToolRequest, in findingsInput) (*mcp.CallToolResult, any, error) {
	limit := cmp.Or(in.Limit, 50)
	if limit < 1 || limit > 200 {
		return nil, nil, errors.New("limit must be between 1 and 200")
	}
	minRank := rank(cmp.Or(in.MinSeverity, "suggestion"))
	detailed := in.Format == "detailed"

	pr, err := t.api.findPull(ctx, in.Repo, in.PR)
	if err != nil {
		return nil, nil, err
	}
	if in.RunID != "" {
		if res, err := t.unfinishedRun(ctx, pr, in.RunID); res != nil || err != nil {
			return res, nil, err
		}
	}
	var reviews []reviewJSON
	if err := t.api.get(ctx, "/pulls/"+pr.id+"/reviews", &reviews); err != nil {
		return nil, nil, err
	}
	reviews = pick(reviews, in.RunID)
	if len(reviews) == 0 {
		if in.RunID != "" {
			return nil, nil, fmt.Errorf("run %s has no saved review: it may have been deleted", in.RunID)
		}
		return answer(map[string]any{"pr": pr.label, "reviews": []reviewOut{},
			"next": "no review yet: start one with devdigest_run_agent"})
	}

	out := make([]reviewOut, 0, len(reviews))
	left, truncated := limit, 0
	for _, rv := range reviews {
		ro := reviewOut{
			Agent:    cmp.Or(deref(rv.AgentName), "(deleted agent)"),
			RunID:    deref(rv.RunID),
			Score:    rv.Score,
			Verdict:  deref(rv.Verdict),
			Findings: []findingOut{},
		}
		fs := slices.DeleteFunc(slices.Clone(rv.Findings), func(f findingJSON) bool {
			return f.DismissedAt != nil || rank(f.Severity) < minRank
		})
		// The API sends them by location; keep that order within a severity.
		slices.SortStableFunc(fs, func(a, b findingJSON) int { return rank(b.Severity) - rank(a.Severity) })
		for _, f := range fs {
			if left == 0 {
				truncated++
				continue
			}
			left--
			ro.Findings = append(ro.Findings, toFindingOut(f, detailed))
		}
		out = append(out, ro)
	}
	res := map[string]any{"pr": pr.label, "reviews": out}
	if truncated > 0 {
		res["truncated"] = truncated
	}
	return answer(res)
}

// unfinishedRun answers for a run that has no review to read yet: one still
// running, failed or cancelled. For a finished one it returns nil, nil.
func (t tools) unfinishedRun(ctx context.Context, pr pull, runID string) (*mcp.CallToolResult, error) {
	var runs []runJSON
	if err := t.api.get(ctx, "/pulls/"+pr.id+"/runs", &runs); err != nil {
		return nil, err
	}
	i := slices.IndexFunc(runs, func(r runJSON) bool { return r.RunID == runID })
	if i < 0 {
		return nil, fmt.Errorf("run %s is not a run of %s", runID, pr.label)
	}
	run := runs[i]
	switch status := deref(run.Status); status {
	case "running":
		res, _, err := answer(map[string]any{"pr": pr.label, "run_id": runID, "status": status,
			"next": "still running: call again in a minute"})
		return res, err
	case "failed":
		return nil, fmt.Errorf("run %s failed: %s", runID, cmp.Or(deref(run.Error), "no reason saved"))
	case "cancelled":
		return nil, fmt.Errorf("run %s was cancelled: it has no findings", runID)
	}
	return nil, nil
}

// pick keeps the review of runID, or without one, the latest review of each
// agent. Reviews come newest first.
func pick(reviews []reviewJSON, runID string) []reviewJSON {
	var out []reviewJSON
	seen := map[string]bool{}
	for _, rv := range reviews {
		if runID != "" {
			if deref(rv.RunID) == runID {
				out = append(out, rv)
			}
			continue
		}
		agent := cmp.Or(deref(rv.AgentID), "name:"+deref(rv.AgentName))
		if !seen[agent] {
			seen[agent] = true
			out = append(out, rv)
		}
	}
	return out
}

func toFindingOut(f findingJSON, detailed bool) findingOut {
	at := f.File + ":" + strconv.Itoa(f.StartLine)
	if f.EndLine > f.StartLine {
		at += "-" + strconv.Itoa(f.EndLine)
	}
	out := findingOut{Severity: f.Severity, At: at, Title: f.Title}
	if detailed {
		out.Category = f.Category
		out.Rationale = f.Rationale
		out.Suggestion = deref(f.Suggestion)
		out.Confidence = f.Confidence
		out.Accepted = f.AcceptedAt != nil
	}
	return out
}

// devdigest_get_conventions

const (
	maxConventions = 50
	maxSkillBody   = 2000
)

type conventionOut struct {
	Rule       string  `json:"rule"`
	Evidence   string  `json:"evidence,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	Accepted   bool    `json:"accepted"`
}

type conventionSkillOut struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Body        string `json:"body"`
}

func (t tools) getConventions(ctx context.Context, _ *mcp.CallToolRequest, in conventionsInput) (*mcp.CallToolResult, any, error) {
	repo, err := t.api.findRepo(ctx, in.Repo)
	if err != nil {
		return nil, nil, err
	}
	var conventions []conventionJSON
	if err := t.api.get(ctx, "/repos/"+repo.ID+"/conventions", &conventions); err != nil {
		return nil, nil, err
	}
	var skills []skillJSON
	if err := t.api.get(ctx, "/skills", &skills); err != nil {
		return nil, nil, err
	}

	convOut := []conventionOut{}
	truncated := 0
	for _, c := range conventions { // accepted first
		if in.AcceptedOnly && !c.Accepted {
			continue
		}
		if len(convOut) == maxConventions {
			truncated++
			continue
		}
		convOut = append(convOut, conventionOut{Rule: c.Rule, Evidence: c.EvidencePath, Confidence: c.Confidence, Accepted: c.Accepted})
	}
	skillOut := []conventionSkillOut{}
	for _, s := range skills {
		if s.Type == "convention" && s.Enabled && strings.TrimSpace(s.Body) != "" {
			skillOut = append(skillOut, conventionSkillOut{Name: s.Name, Description: s.Description, Body: cut(s.Body, maxSkillBody)})
		}
	}
	res := map[string]any{
		"repo":         repo.FullName,
		"conventions":  convOut,
		"skills":       skillOut,
		"skills_scope": "workspace: they apply to every repository",
	}
	if truncated > 0 {
		res["truncated"] = truncated
	}
	return answer(res)
}

// devdigest_get_blast_radius

// blastRadius is the BlastRadius contract
// (client/src/vendor/shared/contracts/brief.ts).
type blastRadius struct {
	ChangedSymbols []json.RawMessage `json:"changed_symbols"`
	Downstream     []json.RawMessage `json:"downstream"`
	Summary        string            `json:"summary"`
}

// getBlastRadius is a stub that keeps the contract's shape, so an agent's
// workflow can call it already. The real one reads repo-intel.
func (t tools) getBlastRadius(ctx context.Context, _ *mcp.CallToolRequest, in pullInput) (*mcp.CallToolResult, any, error) {
	pr, err := t.api.findPull(ctx, in.Repo, in.PR)
	if err != nil {
		return nil, nil, err
	}
	return answer(blastRadius{
		ChangedSymbols: []json.RawMessage{},
		Downstream:     []json.RawMessage{},
		Summary:        "Blast radius isn't implemented yet for " + pr.label + ": no symbols or callers to report.",
	})
}

// deref returns *p, or the zero value for nil.
func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
