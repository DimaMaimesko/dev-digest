package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// intentJSON is a PR's stored intent (PrIntentRecord in the client's
// contracts). in_scope, out_of_scope, sources and unresolved are stored as
// jsonb already in the shape the client expects, so they pass through
// unchanged rather than being decoded and re-encoded.
type intentJSON struct {
	PrID       string          `json:"pr_id"`
	Intent     string          `json:"intent"`
	InScope    json.RawMessage `json:"in_scope"`
	OutOfScope json.RawMessage `json:"out_of_scope"`
	Confidence string          `json:"confidence"`
	Sources    json.RawMessage `json:"sources"`
	Unresolved json.RawMessage `json:"unresolved"`
	HeadSha    *string         `json:"head_sha"`
	Provider   *string         `json:"provider"`
	Model      *string         `json:"model"`
	TokensIn   *int32          `json:"tokens_in"`
	TokensOut  *int32          `json:"tokens_out"`
	CostUsd    *float64        `json:"cost_usd"`
	DerivedAt  string          `json:"derived_at"`
}

// getPullIntent answers GET /pulls/{id}/intent: the PR's most recently
// derived intent, or null when none is stored.
func (s *Server) getPullIntent(w http.ResponseWriter, r *http.Request) {
	p, _, ok := s.pullAndRepo(w, r)
	if !ok {
		return
	}
	in, err := s.queries.GetPullIntent(r.Context(), p.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, intentJSON{
		PrID:       in.PrID.String(),
		Intent:     in.Intent,
		InScope:    in.InScope,
		OutOfScope: in.OutOfScope,
		Confidence: in.Confidence,
		Sources:    in.Sources,
		Unresolved: in.Unresolved,
		HeadSha:    in.HeadSha,
		Provider:   in.Provider,
		Model:      in.Model,
		TokensIn:   in.TokensIn,
		TokensOut:  in.TokensOut,
		CostUsd:    in.CostUsd,
		DerivedAt:  jsTime(in.DerivedAt),
	})
}
