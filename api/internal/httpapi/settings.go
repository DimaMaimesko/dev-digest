package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/DimaMaimesko/dev-digest/api/internal/secrets"
)

// getSettings answers GET /settings: the workspace's preferences (theme,
// polling interval, models per feature, …) as one object. Values are JSON of
// any shape and pass through unchanged. API keys are never among them; see
// secretsStatus.
func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.queries.ListSettings(r.Context(), s.workspace)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make(map[string]json.RawMessage, len(rows))
	for _, row := range rows {
		out[row.Key] = row.Value // nil (SQL NULL) encodes as null
	}
	writeJSON(w, http.StatusOK, out)
}

// secretsStatusJSON says which API keys are set (SecretsStatus in the
// contracts). It holds only booleans, never the keys.
type secretsStatusJSON struct {
	OpenAI     bool `json:"openai"`
	Anthropic  bool `json:"anthropic"`
	OpenRouter bool `json:"openrouter"`
	GitHub     bool `json:"github"`
}

// secretsStatus answers GET /settings/secrets-status, for the
// "Configured / Not set" badges.
func (s *Server) secretsStatus(w http.ResponseWriter, r *http.Request) {
	var out secretsStatusJSON
	for secret, set := range map[string]*bool{
		secrets.OpenAIKey:     &out.OpenAI,
		secrets.AnthropicKey:  &out.Anthropic,
		secrets.OpenRouterKey: &out.OpenRouter,
		secrets.GitHubToken:   &out.GitHub,
	} {
		v, err := s.secrets.Get(secret)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		*set = v != ""
	}
	writeJSON(w, http.StatusOK, out)
}
