package httpapi

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/DimaMaimesko/dev-digest/api/internal/anthropic"
	"github.com/DimaMaimesko/dev-digest/api/internal/github"
	"github.com/DimaMaimesko/dev-digest/api/internal/openai"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
	"github.com/DimaMaimesko/dev-digest/api/internal/secrets"
)

// ModelAPIs are the base URLs of the model providers' APIs, such as
// openai.OpenAIURL. An empty one leaves that provider's model list empty.
type ModelAPIs struct {
	OpenAI     string
	OpenRouter string
	Anthropic  string
}

// modelJSON is a model the agent editor offers (ModelInfo in
// server/src/vendor/shared/adapters.ts). Every provider fills different
// fields; the others are null.
type modelJSON struct {
	ID            string       `json:"id"`
	Provider      string       `json:"provider"`
	Label         *string      `json:"label"`
	Created       *int64       `json:"created"` // Unix seconds
	Pricing       *pricingJSON `json:"pricing"`
	ContextLength *int         `json:"contextLength"`
}

// pricingJSON is what a model costs, in US dollars per million tokens.
type pricingJSON struct {
	PromptPerM     float64 `json:"promptPerM"`
	CompletionPerM float64 `json:"completionPerM"`
}

// listProviderModels answers GET /providers/{id}/models: the models of
// provider {id} (openai, anthropic or openrouter).
func (s *Server) listProviderModels(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("id")
	if !slices.Contains(providers, provider) {
		invalidParam(w, "id", "Expected one of: "+strings.Join(providers, ", "))
		return
	}
	writeJSON(w, http.StatusOK, s.models(r, provider))
}

// listAgentModels answers GET /agents/{id}/models: the models of the
// agent's provider.
func (s *Server) listAgentModels(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	a, err := s.queries.GetAgent(r.Context(), postgres.GetAgentParams{WorkspaceID: s.workspace, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "Agent not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.models(r, a.Provider))
}

// errNoModelKey means the provider's API key isn't set, or its API is
// turned off.
var errNoModelKey = errors.New("no API key")

// models lists the provider's models. Without its API key, or when its API
// fails, the list is empty, as in TS, so the agent editor still works.
func (s *Server) models(r *http.Request, provider string) []modelJSON {
	models, err := s.fetchModels(r.Context(), provider)
	if err != nil {
		if !errors.Is(err, errNoModelKey) {
			s.log.Warn("models not listed", "provider", provider, "err", err)
		}
		return []modelJSON{}
	}
	return models
}

func (s *Server) fetchModels(ctx context.Context, provider string) ([]modelJSON, error) {
	out := []modelJSON{}
	switch provider {
	case "anthropic":
		key, err := s.modelKey(secrets.AnthropicKey, s.modelAPIs.Anthropic)
		if err != nil {
			return nil, err
		}
		models, err := anthropic.New(s.modelAPIs.Anthropic, key).Models(ctx)
		if err != nil {
			return nil, err
		}
		for _, m := range models {
			out = append(out, modelJSON{ID: m.ID, Provider: provider, Label: &m.Name})
		}

	case "openai":
		key, err := s.modelKey(secrets.OpenAIKey, s.modelAPIs.OpenAI)
		if err != nil {
			return nil, err
		}
		models, err := openai.NewCompatible(s.modelAPIs.OpenAI, key).Models(ctx)
		if err != nil {
			return nil, err
		}
		for _, m := range models {
			if chatModel(m.ID) {
				out = append(out, modelJSON{ID: m.ID, Provider: provider, Created: &m.Created})
			}
		}

	case "openrouter":
		key, err := s.modelKey(secrets.OpenRouterKey, s.modelAPIs.OpenRouter)
		if err != nil {
			return nil, err
		}
		models, err := openai.NewCompatible(s.modelAPIs.OpenRouter, key).Models(ctx)
		if err != nil {
			return nil, err
		}
		for _, m := range models {
			mj := modelJSON{ID: m.ID, Provider: provider, Pricing: perMillion(m.Pricing)}
			if m.Name != "" {
				mj.Label = &m.Name
			}
			if m.ContextLength != 0 {
				mj.ContextLength = &m.ContextLength
			}
			out = append(out, mj)
		}
		// Cheapest first; models without a known price last.
		slices.SortStableFunc(out, func(a, b modelJSON) int {
			return cmp.Compare(completionPrice(a), completionPrice(b))
		})
	}
	return out, nil
}

// modelKey returns the API key called name, or errNoModelKey when it isn't
// set or the provider's API (at url) is turned off.
func (s *Server) modelKey(name, url string) (string, error) {
	if url == "" {
		return "", errNoModelKey
	}
	key, err := s.secrets.Get(name)
	if err != nil {
		return "", err
	}
	if key == "" {
		return "", errNoModelKey
	}
	return key, nil
}

// chatModel reports whether an OpenAI model is one the editor offers, by the
// TS server's rule: GPT models and the o1 and o3 families.
func chatModel(id string) bool {
	return strings.HasPrefix(id, "gpt") || strings.Contains(id, "o1") || strings.Contains(id, "o3")
}

// perMillion converts OpenRouter's prices per token into prices per million
// tokens. A price that isn't a number, or is negative (OpenRouter's "-1" for
// a price that varies), makes it unknown.
func perMillion(p *openai.Pricing) *pricingJSON {
	if p == nil {
		return nil
	}
	prompt, err1 := strconv.ParseFloat(p.Prompt, 64)
	completion, err2 := strconv.ParseFloat(p.Completion, 64)
	if err1 != nil || err2 != nil || prompt < 0 || completion < 0 {
		return nil
	}
	return &pricingJSON{PromptPerM: prompt * 1_000_000, CompletionPerM: completion * 1_000_000}
}

func completionPrice(m modelJSON) float64 {
	if m.Pricing == nil {
		return math.Inf(1)
	}
	return m.Pricing.CompletionPerM
}

// connTestJSON is the outcome of a connection test (ConnTestResult).
type connTestJSON struct {
	Provider string `json:"provider"`
	OK       bool   `json:"ok"`
	Message  string `json:"message"`
}

// secretNames are the secrets a connection test saves and tests, by
// provider.
var secretNames = map[string]string{
	"openai": secrets.OpenAIKey, "anthropic": secrets.AnthropicKey,
	"openrouter": secrets.OpenRouterKey, "github": secrets.GitHubToken,
}

// testConnection answers POST /settings/test-connection with {"provider":
// "openai", "key": "…"}: it saves the key, when there is one, then checks
// the saved key with a cheap call. A failed test is a 200 with "ok": false.
func (s *Server) testConnection(w http.ResponseWriter, r *http.Request) {
	var body map[string]json.RawMessage
	if !readJSON(w, r, &body) {
		return
	}
	f := fields{body: body}
	provider := f.str("provider", true, among("openai", "anthropic", "openrouter", "github"))
	key := f.str("key", false, notEmpty)
	if len(f.issues) > 0 {
		invalid(w, f.issues)
		return
	}
	res := connTestJSON{Provider: *provider}
	msg, err := s.connectionTest(r.Context(), *provider, key)
	res.OK, res.Message = err == nil, msg
	if err != nil {
		res.Message = err.Error()
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) connectionTest(ctx context.Context, provider string, key *string) (string, error) {
	name := secretNames[provider]
	if key != nil {
		if err := s.secrets.Set(name, *key); err != nil {
			return "", err
		}
	}
	if provider == "github" {
		token, err := s.secrets.Get(name)
		if err != nil {
			return "", err
		}
		if token == "" || s.githubAPI == "" {
			return "", errors.New(name + " is not configured")
		}
		login, err := github.New(s.githubAPI, token).Login(ctx)
		if err != nil {
			return "", err
		}
		return "Connected as @" + login, nil
	}
	if provider == "openrouter" {
		// Its model list answers any key: check the key first.
		key, err := s.modelKey(name, s.modelAPIs.OpenRouter)
		if errors.Is(err, errNoModelKey) {
			return "", errors.New(name + " is not configured")
		}
		if err != nil {
			return "", err
		}
		if err := openai.NewCompatible(s.modelAPIs.OpenRouter, key).VerifyKey(ctx); err != nil {
			return "", err
		}
	}
	models, err := s.fetchModels(ctx, provider)
	if errors.Is(err, errNoModelKey) {
		return "", errors.New(name + " is not configured")
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("OK — %d models available", len(models)), nil
}
