package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// insertAgent adds an agent with the given output schema (a SQL expression).
func (f fixture) insertAgent(t *testing.T, workspace uuid.UUID, name, outputSchema, createdAt string) uuid.UUID {
	t.Helper()
	return f.insertID(t, `INSERT INTO agents
		(workspace_id, name, provider, model, system_prompt, output_schema, strategy, ci_fail_on, repo_intel, version, created_at)
		VALUES ($1, $2, 'openrouter', 'deepseek/deepseek-v4-flash', 'You review code.', `+outputSchema+`,
		        'auto', 'warning', false, 3, $3) RETURNING id`, workspace, name, createdAt)
}

func TestListAgents(t *testing.T) {
	f := newFixture(t)
	second := f.insertAgent(t, f.workspace, "Security", "NULL", "2026-09-02")
	first := f.insertAgent(t, f.workspace, "General", `'{"type": "object", "required": ["a"]}'`, "2026-09-01")
	f.insertAgent(t, f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`), "Hidden", "NULL", "2026-09-01")

	assertJSON(t, f.get(t, "/agents"), http.StatusOK, `[
		{"id": "`+first.String()+`", "name": "General", "description": "", "provider": "openrouter",
		 "model": "deepseek/deepseek-v4-flash", "system_prompt": "You review code.",
		 "output_schema": {"type": "object", "required": ["a"]},
		 "enabled": true, "version": 3, "strategy": "auto", "ci_fail_on": "warning", "repo_intel": false},
		{"id": "`+second.String()+`", "name": "Security", "description": "", "provider": "openrouter",
		 "model": "deepseek/deepseek-v4-flash", "system_prompt": "You review code.",
		 "output_schema": null,
		 "enabled": true, "version": 3, "strategy": "auto", "ci_fail_on": "warning", "repo_intel": false}
	]`)
}

func TestListAgentsEmpty(t *testing.T) {
	assertJSON(t, newFixture(t).get(t, "/agents"), http.StatusOK, `[]`)
}

func TestGetAgent(t *testing.T) {
	f := newFixture(t)
	agent := f.insertAgent(t, f.workspace, "General", "NULL", "2026-09-01")

	var got struct{ ID, Name string }
	decode(t, f.get(t, "/agents/"+agent.String()), http.StatusOK, &got)
	if got.ID != agent.String() || got.Name != "General" {
		t.Errorf("got %+v", got)
	}
}

func TestAgentVersions(t *testing.T) {
	f := newFixture(t)
	agent := f.insertAgent(t, f.workspace, "General", "NULL", "2026-09-01")
	f.exec(t, `INSERT INTO agent_versions (agent_id, version, config_json, created_at) VALUES
		($1, 1, '{"provider": "openai", "model": "gpt-4.1", "system_prompt": "v1", "strategy": "single-pass",
		          "ci_fail_on": "critical", "repo_intel": true, "skills": [], "retired_field": 1}', '2026-09-01 10:00:00+00'),
		($1, 2, '{"provider": "openrouter", "model": "m", "system_prompt": "v2", "output_schema": null,
		          "strategy": "auto", "ci_fail_on": "never", "repo_intel": false, "skills": ["s1", "s2"]}', '2026-09-02 10:00:00+00')`,
		agent)

	v1 := `{"agent_id": "` + agent.String() + `", "version": 1, "created_at": "2026-09-01T10:00:00.000Z",
		"config": {"provider": "openai", "model": "gpt-4.1", "system_prompt": "v1", "strategy": "single-pass",
		           "ci_fail_on": "critical", "repo_intel": true, "skills": []}}`
	v2 := `{"agent_id": "` + agent.String() + `", "version": 2, "created_at": "2026-09-02T10:00:00.000Z",
		"config": {"provider": "openrouter", "model": "m", "system_prompt": "v2", "output_schema": null,
		           "strategy": "auto", "ci_fail_on": "never", "repo_intel": false, "skills": ["s1", "s2"]}}`

	// Newest first. Unknown keys ("retired_field") are dropped, as Zod does,
	// and a missing output_schema stays missing.
	assertJSON(t, f.get(t, "/agents/"+agent.String()+"/versions"), http.StatusOK, `[`+v2+`, `+v1+`]`)
	assertJSON(t, f.get(t, "/agents/"+agent.String()+"/versions/1"), http.StatusOK, v1)
}

func TestAgentVersionsEmpty(t *testing.T) {
	f := newFixture(t)
	agent := f.insertAgent(t, f.workspace, "General", "NULL", "2026-09-01")
	assertJSON(t, f.get(t, "/agents/"+agent.String()+"/versions"), http.StatusOK, `[]`)
}

// Snapshots are untyped JSON; one the web app can't read is a 500, as in TS.
func TestAgentVersionMalformed(t *testing.T) {
	valid := `"provider": "openai", "model": "m", "system_prompt": "p", "strategy": "auto", "ci_fail_on": "never", "repo_intel": true`
	tests := []struct{ name, config string }{
		{"missing provider", `{"model": "m", "system_prompt": "p", "strategy": "auto", "ci_fail_on": "never", "repo_intel": true, "skills": []}`},
		{"unknown provider", strings.Replace(`{`+valid+`, "skills": []}`, `"openai"`, `"gemini"`, 1)},
		{"unknown strategy", strings.Replace(`{`+valid+`, "skills": []}`, `"auto"`, `"fast"`, 1)},
		{"unknown ci_fail_on", strings.Replace(`{`+valid+`, "skills": []}`, `"never"`, `"sometimes"`, 1)},
		{"missing skills", `{` + valid + `}`},
		{"wrong type", strings.Replace(`{`+valid+`, "skills": []}`, `"repo_intel": true`, `"repo_intel": "yes"`, 1)},
	}
	internal := `{"error": {"code": "internal_error", "message": "Internal error"}}`
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			agent := f.insertAgent(t, f.workspace, "General", "NULL", "2026-09-01")
			f.exec(t, `INSERT INTO agent_versions (agent_id, version, config_json) VALUES ($1, 1, $2)`, agent, tt.config)

			assertJSON(t, f.get(t, "/agents/"+agent.String()+"/versions/1"), http.StatusInternalServerError, internal)
			assertJSON(t, f.get(t, "/agents/"+agent.String()+"/versions"), http.StatusInternalServerError, internal)
		})
	}
}

func TestAgentSkills(t *testing.T) {
	f := newFixture(t)
	agent := f.insertAgent(t, f.workspace, "General", "NULL", "2026-09-01")
	// Fixed IDs that sort the opposite way to "order", so sorting by anything
	// but "order" gives the wrong result every time.
	skills := []string{
		"ffffffff-0000-4000-8000-000000000000", // order 0
		"88888888-0000-4000-8000-000000000000", // order 1
		"11111111-0000-4000-8000-000000000000", // order 2
	}
	for i, id := range skills {
		f.exec(t, `INSERT INTO skills (id, workspace_id, name, description, type, source, body)
			VALUES ($1, $2, gen_random_uuid()::text, 'd', 'custom', 'manual', 'body')`, id, f.workspace)
		f.exec(t, `INSERT INTO agent_skills (agent_id, skill_id, "order") VALUES ($1, $2, $3)`, agent, id, i)
	}

	a := agent.String()
	assertJSON(t, f.get(t, "/agents/"+a+"/skills"), http.StatusOK, `[
		{"agent_id": "`+a+`", "skill_id": "`+skills[0]+`", "order": 0},
		{"agent_id": "`+a+`", "skill_id": "`+skills[1]+`", "order": 1},
		{"agent_id": "`+a+`", "skill_id": "`+skills[2]+`", "order": 2}
	]`)
}

func TestAgentsNotFound(t *testing.T) {
	f := newFixture(t)
	other := f.insertAgent(t, f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`), "Hidden", "NULL", "2026-09-01")
	f.exec(t, `INSERT INTO agent_versions (agent_id, version, config_json) VALUES
		($1, 1, '{"provider": "openai", "model": "m", "system_prompt": "p", "strategy": "auto", "ci_fail_on": "never", "repo_intel": true, "skills": []}')`, other)

	for _, id := range []string{uuid.NewString(), other.String()} {
		for path, want := range map[string]string{
			"/agents/" + id:                 "Agent not found",
			"/agents/" + id + "/versions":   "Agent not found",
			"/agents/" + id + "/skills":     "Agent not found",
			"/agents/" + id + "/versions/1": "Agent version not found",
		} {
			t.Run(path, func(t *testing.T) {
				assertJSON(t, f.get(t, path), http.StatusNotFound, `{"error": {"code": "not_found", "message": "`+want+`"}}`)
			})
		}
	}
}

func TestAgentVersionInvalid(t *testing.T) {
	f := newFixture(t)
	agent := f.insertAgent(t, f.workspace, "General", "NULL", "2026-09-01")
	for _, bad := range []string{"abc", "0", "-1", "1.5", "99999999999"} {
		t.Run(bad, func(t *testing.T) {
			assertJSON(t, f.get(t, "/agents/"+agent.String()+"/versions/"+bad), http.StatusUnprocessableEntity, `{"error": {
				"code": "validation_error", "message": "Request validation failed",
				"details": [{"path": ["version"], "message": "Expected a positive whole number"}]}}`)
		})
	}
}
