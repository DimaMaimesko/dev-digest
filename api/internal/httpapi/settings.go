package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"net/http"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"

	"github.com/DimaMaimesko/dev-digest/api/internal/secrets"
)

// getSettings answers GET /settings: the workspace's preferences (theme,
// polling interval, models per feature, …) as one object. Values are JSON of
// any shape and pass through unchanged. API keys are never among them; see
// secretsStatus.
func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.settings(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// putSettings answers PUT /settings: it saves the preferences in the body,
// leaving the others as they are, and answers with all of them. The known
// preferences are checked; other keys may hold any JSON.
func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var update map[string]json.RawMessage
	if !readJSON(w, r, &update) {
		return
	}
	values, issues := parseSettings(update)
	if len(issues) > 0 {
		invalid(w, issues)
		return
	}

	// One transaction, so a failure saves nothing. Keys go in sorted order:
	// two saves locking the same rows in different orders could deadlock.
	err := pgx.BeginFunc(r.Context(), s.db, func(tx pgx.Tx) error {
		q := s.queries.WithTx(tx)
		for _, key := range slices.Sorted(maps.Keys(values)) {
			err := q.UpsertSetting(r.Context(), postgres.UpsertSettingParams{
				WorkspaceID: s.workspace, UserID: &s.user, Key: key, Value: values[key],
			})
			if err != nil {
				return fmt.Errorf("save setting %q: %w", key, err)
			}
		}
		return nil
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.getSettings(w, r)
}

// settings returns the workspace's preferences as one object.
func (s *Server) settings(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := s.queries.ListSettings(ctx, s.workspace)
	if err != nil {
		return nil, err
	}
	out := make(map[string]json.RawMessage, len(rows))
	for _, row := range rows {
		out[row.Key] = row.Value // nil (SQL NULL) encodes as null
	}
	return out, nil
}

// Allowed values of the known preferences (SettingsKnown and FeatureModelId in
// server/src/vendor/shared/contracts/platform.ts).
var (
	themes        = []string{"dark", "light"}
	densities     = []string{"regular", "compact"}
	providers     = []string{"openai", "anthropic", "openrouter"}
	featureModels = []string{"onboarding", "review_intent", "risk_brief", "conformance", "conventions"}
)

// parseSettings checks the known preferences in update, the way the TS
// server's Zod schema does, and returns the values to store: as sent, except
// that feature_models keeps only provider and model per feature. It also
// lists what is wrong; then the values must not be stored.
func parseSettings(update map[string]json.RawMessage) (map[string]json.RawMessage, []issue) {
	values := maps.Clone(update)
	var issues []issue
	bad := func(message string, path ...string) {
		issues = append(issues, issue{Path: path, Message: message})
	}
	for _, key := range slices.Sorted(maps.Keys(update)) {
		v := update[key]
		switch key {
		case "polling_interval_min":
			var n float64
			if isNull(v) || json.Unmarshal(v, &n) != nil || n != math.Trunc(n) || n < 1 {
				bad("Expected a whole number of at least 1", key)
			}
		case "theme":
			if !oneOf(v, themes) {
				bad("Expected one of: "+strings.Join(themes, ", "), key)
			}
		case "density":
			if !oneOf(v, densities) {
				bad("Expected one of: "+strings.Join(densities, ", "), key)
			}
		case "sync_to_folder", "automatic_reviews":
			var b bool
			if isNull(v) || json.Unmarshal(v, &b) != nil {
				bad("Expected true or false", key)
			}
		case "feature_models":
			var choices map[string]struct {
				Provider string `json:"provider"`
				Model    string `json:"model"`
			}
			if isNull(v) || json.Unmarshal(v, &choices) != nil {
				bad("Expected an object of provider and model per feature", key)
				continue
			}
			for _, feature := range slices.Sorted(maps.Keys(choices)) {
				c := choices[feature]
				if !slices.Contains(featureModels, feature) {
					bad("Unknown feature; expected one of: "+strings.Join(featureModels, ", "), key, feature)
				}
				if !slices.Contains(providers, c.Provider) {
					bad("Expected one of: "+strings.Join(providers, ", "), key, feature, "provider")
				}
				if c.Model == "" {
					bad("Expected a model name", key, feature, "model")
				}
			}
			// Only provider and model, dropping any other field, as Zod does.
			values[key], _ = json.Marshal(choices)
		}
	}
	return values, issues
}

func isNull(v json.RawMessage) bool { return string(v) == "null" }

// oneOf reports whether v is a JSON string equal to one of allowed.
func oneOf(v json.RawMessage, allowed []string) bool {
	var s string
	return json.Unmarshal(v, &s) == nil && slices.Contains(allowed, s)
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
