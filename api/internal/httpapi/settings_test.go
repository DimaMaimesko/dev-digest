package httpapi_test

import (
	"net/http"
	"os"
	"testing"
)

func TestGetSettings(t *testing.T) {
	f := newFixture(t)
	// Values are JSON of any shape.
	f.exec(t, `INSERT INTO settings (workspace_id, user_id, key, value) VALUES
		($1, $2, 'polling_interval_min', '5'),
		($1, $2, 'theme', '"dark"'),
		($1, $2, 'sync_to_folder', 'true'),
		($1, $2, 'feature_models', '{"onboarding": {"provider": "openrouter", "model": "m"}}'),
		($1, $2, 'cleared', NULL)`, f.workspace, f.user)
	// A user's own value wins over the workspace-wide one.
	f.exec(t, `INSERT INTO settings (workspace_id, user_id, key, value) VALUES ($1, NULL, 'theme', '"light"')`, f.workspace)
	// Another workspace's settings aren't included.
	other := f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`)
	f.exec(t, `INSERT INTO settings (workspace_id, key, value) VALUES ($1, 'density', '"compact"')`, other)

	assertJSON(t, f.get(t, "/settings"), http.StatusOK, `{
		"polling_interval_min": 5,
		"theme": "dark",
		"sync_to_folder": true,
		"feature_models": {"onboarding": {"provider": "openrouter", "model": "m"}},
		"cleared": null
	}`)
}

func TestGetSettingsEmpty(t *testing.T) {
	assertJSON(t, newFixture(t).get(t, "/settings"), http.StatusOK, `{}`)
}

func TestSecretsStatus(t *testing.T) {
	tests := []struct {
		name string
		file string // secrets file contents; "" for no file
		env  map[string]string
		want string
	}{
		{"nothing set", "", nil,
			`{"openai": false, "anthropic": false, "openrouter": false, "github": false}`},
		{"from the file and the environment", `{"OPENAI_API_KEY": "sk-1", "OPENROUTER_API_KEY": ""}`,
			map[string]string{"ANTHROPIC_API_KEY": "sk-ant", "GITHUB_PAT": "ghp"},
			`{"openai": true, "anthropic": true, "openrouter": false, "github": true}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.file != "" {
				if err := os.WriteFile(f.secretsFile, []byte(tt.file), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for k, v := range tt.env {
				f.env[k] = v
			}
			assertJSON(t, f.get(t, "/settings/secrets-status"), http.StatusOK, tt.want)
		})
	}
}

func TestSecretsStatusNeverSendsKeys(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(f.secretsFile, []byte(`{"OPENAI_API_KEY": "sk-very-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	res := f.get(t, "/settings/secrets-status")
	var body map[string]any
	decode(t, res, http.StatusOK, &body)
	for k, v := range body {
		if _, isBool := v.(bool); !isBool {
			t.Errorf("%s = %v, want only true or false", k, v)
		}
	}
}

// The TS server treated a malformed secrets file as empty, so every key
// looked unset with no hint why.
func TestSecretsStatusMalformedFile(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(f.secretsFile, []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	assertJSON(t, f.get(t, "/settings/secrets-status"), http.StatusInternalServerError,
		`{"error": {"code": "internal_error", "message": "Internal error"}}`)
}

func TestGetWorkspace(t *testing.T) {
	f := newFixture(t)
	cloned := f.insertID(t, `INSERT INTO repos (workspace_id, owner, name, full_name, clone_path, last_polled_at, created_at)
		VALUES ($1, 'o', 'a', 'o/a', '/work/clones/o/a', '2026-09-28 10:00:00+00', '2026-09-01') RETURNING id`, f.workspace)
	notCloned := f.insertID(t, `INSERT INTO repos (workspace_id, owner, name, full_name, clone_path, created_at)
		VALUES ($1, 'o', 'b', 'o/b', NULL, '2026-09-02') RETURNING id`, f.workspace)
	emptyPath := f.insertID(t, `INSERT INTO repos (workspace_id, owner, name, full_name, clone_path, created_at)
		VALUES ($1, 'o', 'c', 'o/c', '', '2026-09-03') RETURNING id`, f.workspace)
	other := f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`)
	f.insertRepo(t, other, "x/hidden")

	assertJSON(t, f.get(t, "/workspace"), http.StatusOK, `{
		"workspaceId": "`+f.workspace.String()+`",
		"cloneDir": "`+cloneDir+`",
		"repos": [
			{"id": "`+cloned.String()+`", "full_name": "o/a", "clone_path": "/work/clones/o/a",
			 "last_polled_at": "2026-09-28T10:00:00.000Z", "cloned": true},
			{"id": "`+notCloned.String()+`", "full_name": "o/b", "clone_path": null, "last_polled_at": null, "cloned": false},
			{"id": "`+emptyPath.String()+`", "full_name": "o/c", "clone_path": "", "last_polled_at": null, "cloned": false}
		]
	}`)
}

func TestGetWorkspaceEmpty(t *testing.T) {
	f := newFixture(t)
	assertJSON(t, f.get(t, "/workspace"), http.StatusOK,
		`{"workspaceId": "`+f.workspace.String()+`", "cloneDir": "`+cloneDir+`", "repos": []}`)
}
