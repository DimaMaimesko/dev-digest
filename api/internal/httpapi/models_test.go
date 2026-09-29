package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/DimaMaimesko/dev-digest/api/internal/httpapi"
)

// modelAPIs serves fake OpenAI, OpenRouter and Anthropic model lists, or
// answers status to everything when it is set, and counts the calls.
func modelAPIs(t *testing.T, status int) (httpapi.ModelAPIs, *atomic.Int32) {
	t.Helper()
	calls := new(atomic.Int32)
	answers := map[string]string{
		"/openai/models": `{"data": [
			{"id": "gpt-5", "created": 1754000000}, {"id": "o3-mini", "created": 1737000000},
			{"id": "o4-mini", "created": 1744000000}, {"id": "text-embedding-3-small", "created": 1705000000}]}`,
		"/openrouter/models": `{"data": [
			{"id": "a/pricey", "name": "Pricey", "context_length": 200000, "pricing": {"prompt": "0.000003", "completion": "0.000015"}},
			{"id": "openrouter/auto", "name": "Auto", "pricing": {"prompt": "-1", "completion": "-1"}},
			{"id": "b/cheap", "name": "Cheap", "context_length": 32768, "pricing": {"prompt": "0.0000001", "completion": "0.0000004"}},
			{"id": "c/unnamed"}]}`,
		"/openrouter/key": `{"data": {"label": "sk-or-…"}}`,
		"/v1/models": `{"data": [
			{"type": "model", "id": "claude-opus-5", "display_name": "Claude Opus 5", "created_at": "2026-05-01T00:00:00Z"},
			{"type": "model", "id": "claude-haiku-4-5", "display_name": "Claude Haiku 4.5", "created_at": "2025-10-01T00:00:00Z"}],
			"has_more": false, "first_id": "claude-opus-5", "last_id": "claude-haiku-4-5"}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if status != 0 {
			w.WriteHeader(status)
			w.Write([]byte(`{"error": {"message": "invalid key"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(answers[r.URL.Path]))
	}))
	t.Cleanup(srv.Close)
	return httpapi.ModelAPIs{OpenAI: srv.URL + "/openai", OpenRouter: srv.URL + "/openrouter", Anthropic: srv.URL}, calls
}

func withModelKeys(f fixture, apis httpapi.ModelAPIs) fixture {
	f.modelAPIs = apis
	f.env["OPENAI_API_KEY"] = "sk-openai"
	f.env["OPENROUTER_API_KEY"] = "sk-or"
	f.env["ANTHROPIC_API_KEY"] = "sk-ant"
	return f
}

func TestProviderModels(t *testing.T) {
	apis, _ := modelAPIs(t, 0)
	f := withModelKeys(newFixture(t), apis)
	tests := []struct{ provider, want string }{
		// GPT and o1/o3 models only, by the TS server's rule.
		{"openai", `[
			{"id": "gpt-5", "provider": "openai", "label": null, "created": 1754000000, "pricing": null, "contextLength": null},
			{"id": "o3-mini", "provider": "openai", "label": null, "created": 1737000000, "pricing": null, "contextLength": null}]`},
		// Cheapest first, prices per million tokens; unknown prices last.
		{"openrouter", `[
			{"id": "b/cheap", "provider": "openrouter", "label": "Cheap", "created": null,
			 "pricing": {"promptPerM": 0.09999999999999999, "completionPerM": 0.39999999999999997}, "contextLength": 32768},
			{"id": "a/pricey", "provider": "openrouter", "label": "Pricey", "created": null,
			 "pricing": {"promptPerM": 3, "completionPerM": 15}, "contextLength": 200000},
			{"id": "openrouter/auto", "provider": "openrouter", "label": "Auto", "created": null, "pricing": null, "contextLength": null},
			{"id": "c/unnamed", "provider": "openrouter", "label": null, "created": null, "pricing": null, "contextLength": null}]`},
		{"anthropic", `[
			{"id": "claude-opus-5", "provider": "anthropic", "label": "Claude Opus 5", "created": null, "pricing": null, "contextLength": null},
			{"id": "claude-haiku-4-5", "provider": "anthropic", "label": "Claude Haiku 4.5", "created": null, "pricing": null, "contextLength": null}]`},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			assertJSON(t, f.get(t, "/providers/"+tt.provider+"/models"), http.StatusOK, tt.want)
		})
	}
}

// Without a key, or when the provider fails, the list is empty, so the agent
// editor still works.
func TestProviderModelsUnavailable(t *testing.T) {
	failing, failingCalls := modelAPIs(t, http.StatusUnauthorized)
	working, workingCalls := modelAPIs(t, 0)
	noKeys := newFixture(t)
	noKeys.modelAPIs = working
	for _, provider := range []string{"openai", "openrouter", "anthropic"} {
		t.Run(provider, func(t *testing.T) {
			assertJSON(t, withModelKeys(newFixture(t), failing).get(t, "/providers/"+provider+"/models"), http.StatusOK, `[]`)
			assertJSON(t, noKeys.get(t, "/providers/"+provider+"/models"), http.StatusOK, `[]`)
		})
	}
	if failingCalls.Load() == 0 || workingCalls.Load() != 0 {
		t.Errorf("calls with keys: %d, without: %d", failingCalls.Load(), workingCalls.Load())
	}
}

func TestAgentModels(t *testing.T) {
	apis, _ := modelAPIs(t, 0)
	f := withModelKeys(newFixture(t), apis)
	agent := f.insertAgent(t, f.workspace, "G", "NULL", "2026-09-01")
	f.exec(t, `UPDATE agents SET provider = 'anthropic' WHERE id = $1`, agent)

	var models []struct{ ID string }
	decode(t, f.get(t, "/agents/"+agent.String()+"/models"), http.StatusOK, &models)
	if len(models) != 2 || models[0].ID != "claude-opus-5" {
		t.Errorf("models = %+v, want the Anthropic ones", models)
	}

	other := f.insertAgent(t, f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`), "G", "NULL", "2026-09-01")
	for _, id := range []uuid.UUID{uuid.New(), other} {
		assertJSON(t, f.get(t, "/agents/"+id.String()+"/models"), http.StatusNotFound,
			`{"error": {"code": "not_found", "message": "Agent not found"}}`)
	}
	for _, path := range []string{"/agents/42/models", "/providers/gemini/models"} {
		if paths := issuePaths(t, f.get(t, path)); !slices.Equal(paths, []string{"id"}) {
			t.Errorf("%s: issues at %v", path, paths)
		}
	}
}
