package httpapi_test

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// createSkill creates a skill through the API and returns its ID.
func (f fixture) createSkill(t *testing.T, body string) string {
	t.Helper()
	var sk struct{ ID string }
	decode(t, f.send(t, http.MethodPost, "/skills", body), http.StatusCreated, &sk)
	return sk.ID
}

func TestCreateSkill(t *testing.T) {
	f := newFixture(t)
	res := f.send(t, http.MethodPost, "/skills", `{
		"name": "pr-quality-rubric", "type": "rubric", "body": "# Rubric",
		"message": "Initial rubric", "unknown_field": "ignored"
	}`)
	var got map[string]any
	decode(t, res, http.StatusCreated, &got)
	id, _ := got["id"].(string)
	if _, err := uuid.Parse(id); err != nil {
		t.Errorf("id = %q", id)
	}
	if c, _ := got["created_at"].(string); !strings.HasSuffix(c, "Z") {
		t.Errorf("created_at = %q, want a JavaScript time", c)
	}
	delete(got, "id")
	delete(got, "created_at")
	// Unset fields get the defaults; unknown fields are ignored.
	want := map[string]any{
		"name": "pr-quality-rubric", "description": "", "type": "rubric", "source": "manual",
		"body": "# Rubric", "enabled": true, "version": 1.0, "evidence_files": nil, "agent_count": 0.0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}

	var versions []map[string]any
	decode(t, f.get(t, "/skills/"+id+"/versions"), http.StatusOK, &versions)
	if len(versions) != 1 || versions[0]["body"] != "# Rubric" || versions[0]["message"] != "Initial rubric" {
		t.Errorf("versions = %v, want version 1", versions)
	}
}

func TestCreateSkillInvalid(t *testing.T) {
	f := newFixture(t)
	f.createSkill(t, `{"name": "taken", "type": "custom", "body": "b"}`)
	tests := []struct {
		body string
		want []string
	}{
		{`{}`, []string{"name", "type", "body"}},
		{`{"name": "", "type": "style", "body": "", "source": "github", "enabled": "yes"}`,
			[]string{"name", "type", "source", "body", "enabled"}},
		{`{"name": "n", "description": null, "type": "custom", "body": "b"}`, []string{"description"}},
		{`{"name": "taken", "type": "custom", "body": "b"}`, []string{"name"}},
	}
	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			if got := issuePaths(t, f.send(t, http.MethodPost, "/skills", tt.body)); strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("issues at %v, want %v", got, tt.want)
			}
		})
	}
	var skills []any
	decode(t, f.get(t, "/skills"), http.StatusOK, &skills)
	if len(skills) != 1 {
		t.Errorf("%d skills, want only the first", len(skills))
	}
}

func TestListSkills(t *testing.T) {
	f := newFixture(t)
	first := f.createSkill(t, `{"name": "first", "type": "rubric", "body": "b"}`)
	second := f.createSkill(t, `{"name": "second", "type": "security", "body": "b"}`)
	other := f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`)
	f.insertID(t, `INSERT INTO skills (workspace_id, name, description, type, source, body)
		VALUES ($1, 'foreign', 'd', 'custom', 'manual', 'b') RETURNING id`, other)

	// Two agents use the first skill.
	for _, a := range []struct{ name, createdAt string }{{"A", "2026-09-01"}, {"B", "2026-09-02"}} {
		agent := f.insertAgent(t, f.workspace, a.name, "NULL", a.createdAt)
		f.send(t, http.MethodPost, "/agents/"+agent.String()+"/skills", `{"skill_id": "`+first+`"}`).Body.Close()
	}

	var got []struct {
		ID         string
		Name       string
		AgentCount int `json:"agent_count"`
	}
	decode(t, f.get(t, "/skills"), http.StatusOK, &got)
	if len(got) != 2 || got[0].ID != first || got[0].AgentCount != 2 || got[1].ID != second || got[1].AgentCount != 0 {
		t.Errorf("got %+v, want first (2 agents) then second (0), and not the other workspace's", got)
	}

	var one struct {
		AgentCount int `json:"agent_count"`
	}
	decode(t, f.get(t, "/skills/"+first), http.StatusOK, &one)
	if one.AgentCount != 2 {
		t.Errorf("GET /skills/{id}: agent_count = %d, want 2", one.AgentCount)
	}

	var agents []struct{ Name string }
	decode(t, f.get(t, "/skills/"+first+"/agents"), http.StatusOK, &agents)
	if len(agents) != 2 || agents[0].Name != "A" || agents[1].Name != "B" {
		t.Errorf("agents using the skill = %+v, want A, B", agents)
	}
}

func TestUpdateSkill(t *testing.T) {
	f := newFixture(t)
	id := f.createSkill(t, `{"name": "rubric", "type": "rubric", "body": "v1"}`)
	f.createSkill(t, `{"name": "taken", "type": "rubric", "body": "b"}`)

	var got struct {
		Name    string
		Body    string
		Enabled bool
		Version int
	}
	decode(t, f.send(t, http.MethodPut, "/skills/"+id, `{"body": "v2", "message": "Added tests"}`), http.StatusOK, &got)
	if got.Body != "v2" || got.Version != 2 {
		t.Errorf("after a new body: %+v, want v2 at version 2", got)
	}
	decode(t, f.send(t, http.MethodPut, "/skills/"+id, `{"name": "quality", "enabled": false}`), http.StatusOK, &got)
	if got.Name != "quality" || got.Enabled || got.Version != 2 {
		t.Errorf("after a rename: %+v, want version 2", got)
	}

	if paths := issuePaths(t, f.send(t, http.MethodPut, "/skills/"+id, `{"body": "", "type": "style"}`)); strings.Join(paths, ",") != "type,body" {
		t.Errorf("issues at %v", paths)
	}
	if paths := issuePaths(t, f.send(t, http.MethodPut, "/skills/"+id, `{"name": "taken"}`)); strings.Join(paths, ",") != "name" {
		t.Errorf("renaming to a taken name: issues at %v", paths)
	}
	assertJSON(t, f.send(t, http.MethodPut, "/skills/"+uuid.NewString(), `{}`), http.StatusNotFound,
		`{"error": {"code": "not_found", "message": "Skill not found"}}`)
}

func TestRestoreSkillVersion(t *testing.T) {
	f := newFixture(t)
	id := f.createSkill(t, `{"name": "rubric", "type": "rubric", "body": "v1"}`)
	f.send(t, http.MethodPut, "/skills/"+id, `{"body": "v2"}`).Body.Close()

	var got struct {
		Body    string
		Version int
	}
	decode(t, f.send(t, http.MethodPost, "/skills/"+id+"/versions/1/restore", ``), http.StatusOK, &got)
	if got.Body != "v1" || got.Version != 3 {
		t.Errorf("after restoring v1: %+v, want v1's body at version 3", got)
	}
	var versions []struct {
		Version int
		Message *string
	}
	decode(t, f.get(t, "/skills/"+id+"/versions"), http.StatusOK, &versions)
	if len(versions) != 3 || versions[0].Version != 3 || versions[0].Message == nil || *versions[0].Message != "Restored v1" ||
		versions[1].Message != nil {
		t.Errorf("versions = %+v, want 3, 2, 1, newest first", versions)
	}

	assertJSON(t, f.send(t, http.MethodPost, "/skills/"+id+"/versions/9/restore", ``), http.StatusNotFound,
		`{"error": {"code": "not_found", "message": "Skill version not found"}}`)
	assertJSON(t, f.send(t, http.MethodPost, "/skills/"+uuid.NewString()+"/versions/1/restore", ``), http.StatusNotFound,
		`{"error": {"code": "not_found", "message": "Skill not found"}}`)
	if res := f.send(t, http.MethodPost, "/skills/"+id+"/versions/0/restore", ``); res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("version 0: status %d, want 422", res.StatusCode)
	}
}

func TestDeleteSkill(t *testing.T) {
	f := newFixture(t)
	id := f.createSkill(t, `{"name": "rubric", "type": "rubric", "body": "b"}`)
	agent := f.insertAgent(t, f.workspace, "G", "NULL", "2026-09-01").String()
	f.send(t, http.MethodPost, "/agents/"+agent+"/skills", `{"skill_id": "`+id+`"}`).Body.Close()

	assertJSON(t, f.send(t, http.MethodDelete, "/skills/"+id, ``), http.StatusOK, `{"ok": true}`)
	// The agent stays, without the link.
	assertJSON(t, f.get(t, "/agents/"+agent+"/skills"), http.StatusOK, `[]`)
	assertJSON(t, f.send(t, http.MethodDelete, "/skills/"+id, ``), http.StatusNotFound,
		`{"error": {"code": "not_found", "message": "Skill not found"}}`)
}

func TestSkillNotFound(t *testing.T) {
	f := newFixture(t)
	other := f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`)
	foreign := f.insertID(t, `INSERT INTO skills (workspace_id, name, description, type, source, body)
		VALUES ($1, 'foreign', 'd', 'custom', 'manual', 'b') RETURNING id`, other).String()

	// Another workspace's skill answers exactly like an unknown one.
	for _, id := range []string{foreign, uuid.NewString()} {
		for _, path := range []string{"/skills/" + id, "/skills/" + id + "/versions", "/skills/" + id + "/agents"} {
			assertJSON(t, f.get(t, path), http.StatusNotFound,
				`{"error": {"code": "not_found", "message": "Skill not found"}}`)
		}
	}
	assertJSON(t, f.get(t, "/skills/not-a-uuid"), http.StatusUnprocessableEntity,
		`{"error": {"code": "validation_error", "message": "Request validation failed", "details": [{"path": ["id"], "message": "Invalid uuid"}]}}`)
}
