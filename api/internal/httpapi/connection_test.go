package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/httpapi"
)

func TestTestConnection(t *testing.T) {
	apis, _ := modelAPIs(t, 0)
	f, _ := withGitHub(t, withModelKeys(newFixture(t), apis), "ghp_env", 0)
	tests := []struct{ body, want string }{
		{`{"provider": "openai"}`, `{"provider": "openai", "ok": true, "message": "OK — 2 models available"}`},
		{`{"provider": "anthropic"}`, `{"provider": "anthropic", "ok": true, "message": "OK — 2 models available"}`},
		{`{"provider": "openrouter"}`, `{"provider": "openrouter", "ok": true, "message": "OK — 4 models available"}`},
		{`{"provider": "github"}`, `{"provider": "github", "ok": true, "message": "Connected as @octocat"}`},
	}
	for _, tt := range tests {
		assertJSON(t, f.send(t, http.MethodPost, "/settings/test-connection", tt.body), http.StatusOK, tt.want)
	}
	if _, err := os.Stat(f.secretsFile); err == nil {
		t.Error("the secrets file was written without a key to save")
	}
}

// A key in the request is saved, then tested; it stays saved when the test
// fails, as in TS.
func TestTestConnectionSavesTheKey(t *testing.T) {
	// Like OpenRouter: the model list answers any key, /key doesn't.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/key" {
			http.Error(w, `{"error": {"message": "User not found."}}`, http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"data": [{"id": "a/b"}]}`))
	}))
	t.Cleanup(srv.Close)
	f := newFixture(t)
	f.modelAPIs = httpapi.ModelAPIs{OpenRouter: srv.URL}
	var res struct {
		Provider string
		OK       bool
		Message  string
	}
	decode(t, f.send(t, http.MethodPost, "/settings/test-connection", `{"provider": "openrouter", "key": "sk-or-bad"}`), http.StatusOK, &res)
	if res.OK || !strings.Contains(res.Message, "401") {
		t.Errorf("result %+v", res)
	}
	data, _ := os.ReadFile(f.secretsFile)
	var saved map[string]string
	json.Unmarshal(data, &saved)
	if saved["OPENROUTER_API_KEY"] != "sk-or-bad" {
		t.Errorf("secrets file: %s", data)
	}
}

func TestTestConnectionNotConfigured(t *testing.T) {
	apis, _ := modelAPIs(t, 0)
	f := newFixture(t)
	f.modelAPIs = apis
	for _, p := range []struct{ provider, name string }{
		{"openai", "OPENAI_API_KEY"}, {"anthropic", "ANTHROPIC_API_KEY"}, {"openrouter", "OPENROUTER_API_KEY"}, {"github", "GITHUB_TOKEN"},
	} {
		assertJSON(t, f.send(t, http.MethodPost, "/settings/test-connection", `{"provider": "`+p.provider+`"}`), http.StatusOK,
			`{"provider": "`+p.provider+`", "ok": false, "message": "`+p.name+` is not configured"}`)
	}
}

func TestTestConnectionInvalid(t *testing.T) {
	f := newFixture(t)
	for body, want := range map[string]string{
		`{}`:                                  "provider",
		`{"provider": "gemini"}`:              "provider",
		`{"provider": "openai", "key": ""}`:   "key",
		`{"provider": "openai", "key": null}`: "key",
	} {
		if paths := issuePaths(t, f.send(t, http.MethodPost, "/settings/test-connection", body)); strings.Join(paths, ",") != want {
			t.Errorf("%s: issues at %v", body, paths)
		}
	}
}
