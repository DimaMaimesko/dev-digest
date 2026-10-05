package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// api calls the DevDigest HTTP API. The server never opens the database:
// reviews run in the API's runner, and its rules stay in one place.
type api struct {
	base string // such as "http://127.0.0.1:3001", without a trailing slash
	http *http.Client
}

// get decodes the JSON answer of GET path into v.
func (a api) get(ctx context.Context, path string, v any) error {
	return a.do(ctx, http.MethodGet, path, nil, v)
}

// post sends body as JSON and decodes the answer into v.
func (a api) post(ctx context.Context, path string, body, v any) error {
	return a.do(ctx, http.MethodPost, path, body, v)
}

func (a api) do(ctx context.Context, method, path string, body, v any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, reqBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := a.http.Do(req)
	if err != nil {
		if urlErr := (*url.Error)(nil); errors.As(err, &urlErr) && ctx.Err() == nil {
			return fmt.Errorf("the DevDigest API isn't answering at %s: start it with ./scripts/dev.sh --no-client", a.base)
		}
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	if res.StatusCode >= 300 {
		return apiError(res.StatusCode, data)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s %s: unexpected answer: %w", method, path, err)
	}
	return nil
}

// apiError turns the API's error envelope into an error the agent can read.
func apiError(status int, body []byte) error {
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil && env.Error.Message != "" {
		return fmt.Errorf("DevDigest API: %s (HTTP %d %s)", env.Error.Message, status, env.Error.Code)
	}
	return fmt.Errorf("DevDigest API: HTTP %d: %s", status, strings.TrimSpace(string(body)))
}

// The API's JSON, only the fields the tools read.

type repoJSON struct {
	ID       string `json:"id"`
	FullName string `json:"full_name"`
}

type pullJSON struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
}

type agentJSON struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	Enabled     bool   `json:"enabled"`
	SkillCount  int    `json:"skill_count"`
}

type startedRunJSON struct {
	RunID     string `json:"runId"`
	AgentName string `json:"agentName"`
}

type runJSON struct {
	RunID     string  `json:"run_id"`
	AgentName *string `json:"agent_name"`
	Status    *string `json:"status"` // running, done, failed or cancelled
	Error     *string `json:"error"`
	Score     *int    `json:"score"`
	Blockers  *int    `json:"blockers"`
}

type reviewJSON struct {
	AgentID   *string       `json:"agent_id"`
	AgentName *string       `json:"agent_name"`
	RunID     *string       `json:"run_id"`
	Verdict   *string       `json:"verdict"`
	Score     *int          `json:"score"`
	Findings  []findingJSON `json:"findings"`
}

type findingJSON struct {
	Severity    string  `json:"severity"` // CRITICAL, WARNING or SUGGESTION
	Category    string  `json:"category"`
	Title       string  `json:"title"`
	File        string  `json:"file"`
	StartLine   int     `json:"start_line"`
	EndLine     int     `json:"end_line"`
	Rationale   string  `json:"rationale"`
	Suggestion  *string `json:"suggestion"`
	Confidence  float64 `json:"confidence"`
	AcceptedAt  *string `json:"accepted_at"`
	DismissedAt *string `json:"dismissed_at"`
}

type conventionJSON struct {
	Rule         string  `json:"rule"`
	EvidencePath string  `json:"evidence_path"`
	Confidence   float64 `json:"confidence"`
	Accepted     bool    `json:"accepted"`
}

type skillJSON struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Body        string `json:"body"`
	Enabled     bool   `json:"enabled"`
}
