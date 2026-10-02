package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// send sends a request with a JSON body.
func (f fixture) send(t *testing.T, method, path, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return f.serve(req)
}

// issuePaths returns the paths of a 422 answer's issues, joined with ".".
func issuePaths(t *testing.T, res *http.Response) []string {
	t.Helper()
	var body struct {
		Error struct {
			Code    string
			Details []struct{ Path []string }
		}
	}
	decode(t, res, http.StatusUnprocessableEntity, &body)
	if body.Error.Code != "validation_error" {
		t.Errorf("code = %q", body.Error.Code)
	}
	var paths []string
	for _, d := range body.Error.Details {
		paths = append(paths, strings.Join(d.Path, "."))
	}
	return paths
}

func TestCreateAgent(t *testing.T) {
	f := newFixture(t)
	res := f.send(t, http.MethodPost, "/agents", `{
		"name": "Perf", "provider": "openrouter", "model": "deepseek/deepseek-v4-flash",
		"system_prompt": "Find slow code.", "output_schema": {"type": "object"},
		"strategy": "auto", "unknown_field": "ignored"
	}`)
	var got map[string]any
	decode(t, res, http.StatusCreated, &got)
	id, _ := got["id"].(string)
	delete(got, "id")
	// Unset fields get the defaults; unknown fields are ignored.
	want := map[string]any{
		"name": "Perf", "description": "", "provider": "openrouter", "model": "deepseek/deepseek-v4-flash",
		"system_prompt": "Find slow code.", "output_schema": map[string]any{"type": "object"},
		"enabled": true, "version": 1.0, "strategy": "auto", "ci_fail_on": "critical", "repo_intel": true,
		"skill_count": 0.0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
	var versions []any
	decode(t, f.get(t, "/agents/"+id+"/versions"), http.StatusOK, &versions)
	if len(versions) != 1 {
		t.Errorf("got %d versions, want 1", len(versions))
	}
}

func TestCreateAgentInvalid(t *testing.T) {
	f := newFixture(t)
	tests := []struct {
		body string
		want []string
	}{
		{`{}`, []string{"name", "provider", "model", "system_prompt"}},
		{`{"name": null, "provider": "openai", "model": "m", "system_prompt": "p"}`, []string{"name"}},
		// description may be empty, so this fails only because null isn't a string.
		{`{"name": "n", "description": null, "provider": "openai", "model": "m", "system_prompt": "p"}`, []string{"description"}},
		{`{"name": "", "provider": "gemini", "model": "m", "system_prompt": "p", "strategy": "fast", "enabled": "yes"}`,
			[]string{"name", "provider", "strategy", "enabled"}},
	}
	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			if got := issuePaths(t, f.send(t, http.MethodPost, "/agents", tt.body)); strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("issues at %v, want %v", got, tt.want)
			}
		})
	}
	var agents []any
	decode(t, f.get(t, "/agents"), http.StatusOK, &agents)
	if len(agents) != 0 {
		t.Errorf("%d agents created by invalid requests", len(agents))
	}
}

func TestUpdateAgent(t *testing.T) {
	f := newFixture(t)
	var a struct{ ID string }
	decode(t, f.send(t, http.MethodPost, "/agents",
		`{"name": "G", "provider": "openai", "model": "gpt-4.1", "system_prompt": "p"}`), http.StatusCreated, &a)

	var got struct {
		Model   string
		Enabled bool
		Version int
	}
	decode(t, f.send(t, http.MethodPut, "/agents/"+a.ID, `{"model": "o3"}`), http.StatusOK, &got)
	if got.Model != "o3" || got.Version != 2 {
		t.Errorf("after a model change: %+v, want o3 at version 2", got)
	}
	decode(t, f.send(t, http.MethodPut, "/agents/"+a.ID, `{"enabled": false}`), http.StatusOK, &got)
	if got.Enabled || got.Version != 2 {
		t.Errorf("after turning it off: %+v, want version 2", got)
	}

	// The answer counts the agent's skills, as GET does.
	skill := f.insertID(t, `INSERT INTO skills (workspace_id, name, description, type, source, body)
		VALUES ($1, 'rubric', 'd', 'custom', 'manual', 'b') RETURNING id`, f.workspace)
	f.send(t, http.MethodPost, "/agents/"+a.ID+"/skills", `{"skill_id": "`+skill.String()+`"}`).Body.Close()
	var counted struct {
		SkillCount int `json:"skill_count"`
	}
	decode(t, f.send(t, http.MethodPut, "/agents/"+a.ID, `{"enabled": true}`), http.StatusOK, &counted)
	if counted.SkillCount != 1 {
		t.Errorf("skill_count = %d after linking one, want 1", counted.SkillCount)
	}
	decode(t, f.get(t, "/agents/"+a.ID), http.StatusOK, &counted)
	if counted.SkillCount != 1 {
		t.Errorf("GET: skill_count = %d, want 1", counted.SkillCount)
	}

	if paths := issuePaths(t, f.send(t, http.MethodPut, "/agents/"+a.ID, `{"model": "", "ci_fail_on": "sometimes"}`)); strings.Join(paths, ",") != "model,ci_fail_on" {
		t.Errorf("issues at %v", paths)
	}
	assertJSON(t, f.send(t, http.MethodPut, "/agents/"+uuid.NewString(), `{}`), http.StatusNotFound,
		`{"error": {"code": "not_found", "message": "Agent not found"}}`)
}

func TestDeleteAgent(t *testing.T) {
	f := newFixture(t)
	agent := f.insertAgent(t, f.workspace, "G", "NULL", "2026-09-01")
	assertJSON(t, f.send(t, http.MethodDelete, "/agents/"+agent.String(), ``), http.StatusOK, `{"ok": true}`)
	assertJSON(t, f.send(t, http.MethodDelete, "/agents/"+agent.String(), ``), http.StatusNotFound,
		`{"error": {"code": "not_found", "message": "Agent not found"}}`)
}

func TestChangeAgentSkills(t *testing.T) {
	f := newFixture(t)
	agent := f.insertAgent(t, f.workspace, "G", "NULL", "2026-09-01").String()
	skill := func(workspace uuid.UUID) string {
		return f.insertID(t, `INSERT INTO skills (workspace_id, name, description, type, source, body)
			VALUES ($1, gen_random_uuid()::text, 'd', 'custom', 'manual', 'b') RETURNING id`, workspace).String()
	}
	a, b := skill(f.workspace), skill(f.workspace)
	foreign := skill(f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`))
	path := "/agents/" + agent + "/skills"

	assertJSON(t, f.send(t, http.MethodPost, path, `{"skill_ids": ["`+b+`", "`+a+`"]}`), http.StatusOK, `[
		{"agent_id": "`+agent+`", "skill_id": "`+b+`", "order": 0},
		{"agent_id": "`+agent+`", "skill_id": "`+a+`", "order": 1}]`)
	assertJSON(t, f.send(t, http.MethodPost, path, `{"skill_ids": ["`+a+`"]}`), http.StatusOK,
		`[{"agent_id": "`+agent+`", "skill_id": "`+a+`", "order": 0}]`)
	assertJSON(t, f.send(t, http.MethodPost, path, `{"skill_id": "`+b+`"}`), http.StatusOK, `[
		{"agent_id": "`+agent+`", "skill_id": "`+a+`", "order": 0},
		{"agent_id": "`+agent+`", "skill_id": "`+b+`", "order": 1}]`)

	tests := []struct{ name, body, want string }{
		{"neither field", `{}`, ""},
		{"same skill twice", `{"skill_ids": ["` + a + `", "` + a + `"]}`, "skill_ids"},
		{"not a uuid", `{"skill_ids": ["x"]}`, "skill_ids.0"},
		// The TS server answered 500 with the database's foreign key error,
		// and linked another workspace's skill.
		{"missing skill", `{"skill_id": "` + uuid.NewString() + `"}`, "skill_id"},
		{"another workspace's skill", `{"skill_ids": ["` + foreign + `"]}`, "skill_ids"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := issuePaths(t, f.send(t, http.MethodPost, path, tt.body)); strings.Join(got, ",") != tt.want {
				t.Errorf("issues at %v, want %q", got, tt.want)
			}
		})
	}
	assertJSON(t, f.send(t, http.MethodPost, "/agents/"+uuid.NewString()+"/skills", `{"skill_ids": []}`), http.StatusNotFound,
		`{"error": {"code": "not_found", "message": "Agent not found"}}`)
}
