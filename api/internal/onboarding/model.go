package onboarding

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// featureKey names the onboarding feature in settings.feature_models
// (client/src/vendor/shared/contracts/platform.ts FEATURE_MODELS).
const featureKey = "onboarding"

// defaultProvider and defaultModel are used when the workspace has no saved
// feature_models.onboarding choice: they mirror FEATURE_MODELS' onboarding
// entry.
const (
	defaultProvider = "anthropic"
	defaultModel    = "sonnet"
)

// modelChoice reads the workspace's saved feature_models.onboarding choice,
// the way runner.intentModelChoice does, falling back to the Go default
// when none is saved or it's malformed.
func (s *Store) modelChoice(ctx context.Context, workspace uuid.UUID) (provider, model string) {
	provider, model = defaultProvider, defaultModel
	rows, err := s.q.ListSettings(ctx, workspace)
	if err != nil {
		return provider, model
	}
	for _, row := range rows {
		if row.Key != "feature_models" {
			continue
		}
		var choices map[string]struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		}
		if json.Unmarshal(row.Value, &choices) != nil {
			continue
		}
		// Rows are ordered workspace-wide first, then per-user: a later,
		// matching row wins, the same fold settings() does.
		if c, ok := choices[featureKey]; ok && c.Provider != "" && c.Model != "" {
			provider, model = c.Provider, c.Model
		}
	}
	return provider, model
}
