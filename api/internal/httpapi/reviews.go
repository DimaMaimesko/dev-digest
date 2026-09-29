package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

// reviewJSON is one agent's saved review of a pull request, with its findings
// (ReviewDto in server/src/modules/reviews/helpers.ts).
type reviewJSON struct {
	ID        string        `json:"id"`
	PRID      string        `json:"pr_id"`
	AgentID   *string       `json:"agent_id"`
	RunID     *string       `json:"run_id"`
	AgentName *string       `json:"agent_name"` // null when the agent was deleted
	Kind      string        `json:"kind"`       // review or summary
	Verdict   *string       `json:"verdict"`
	Summary   *string       `json:"summary"`
	Score     *int32        `json:"score"`
	Model     *string       `json:"model"`
	CreatedAt string        `json:"created_at"`
	Findings  []findingJSON `json:"findings"`
}

// findingJSON is a saved finding (ReviewDtoFinding): the model's finding plus
// what the user did with it.
type findingJSON struct {
	ID                 string          `json:"id"`
	Severity           string          `json:"severity"`
	Category           string          `json:"category"`
	Title              string          `json:"title"`
	File               string          `json:"file"`
	StartLine          int32           `json:"start_line"`
	EndLine            int32           `json:"end_line"`
	Rationale          string          `json:"rationale"`
	Suggestion         *string         `json:"suggestion"`
	Confidence         float64         `json:"confidence"`
	Kind               string          `json:"kind"`
	TrifectaComponents json.RawMessage `json:"trifecta_components"` // stored JSON, or null
	Evidence           any             `json:"evidence"`            // not stored; always null, as in TS
	ReviewID           string          `json:"review_id"`
	AcceptedAt         *string         `json:"accepted_at"`
	DismissedAt        *string         `json:"dismissed_at"`
}

// listReviews answers GET /pulls/{id}/reviews: the pull request's reviews,
// newest first, each with its findings.
func (s *Server) listReviews(w http.ResponseWriter, r *http.Request) {
	prID, ok := pathID(w, r)
	if !ok {
		return
	}
	exists, err := s.queries.PullExists(r.Context(), postgres.PullExistsParams{WorkspaceID: s.workspace, ID: prID})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "not_found", "Pull request not found")
		return
	}
	reviews, err := s.queries.ListReviews(r.Context(), prID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	findings, err := s.queries.ListFindings(r.Context(), prID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	byReview := make(map[uuid.UUID][]findingJSON, len(reviews))
	for _, f := range findings {
		byReview[f.ReviewID] = append(byReview[f.ReviewID], toFindingJSON(f))
	}
	out := make([]reviewJSON, 0, len(reviews))
	for _, rv := range reviews {
		fs := byReview[rv.ID]
		if fs == nil {
			fs = []findingJSON{} // [] rather than null
		}
		out = append(out, reviewJSON{
			ID:        rv.ID.String(),
			PRID:      rv.PrID.String(),
			AgentID:   uuidPtr(rv.AgentID),
			RunID:     uuidPtr(rv.RunID),
			AgentName: rv.AgentName,
			Kind:      rv.Kind,
			Verdict:   rv.Verdict,
			Summary:   rv.Summary,
			Score:     rv.Score,
			Model:     rv.Model,
			CreatedAt: jsTime(rv.CreatedAt),
			Findings:  fs,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func toFindingJSON(f postgres.Finding) findingJSON {
	return findingJSON{
		ID:                 f.ID.String(),
		Severity:           f.Severity,
		Category:           f.Category,
		Title:              f.Title,
		File:               f.File,
		StartLine:          f.StartLine,
		EndLine:            f.EndLine,
		Rationale:          f.Rationale,
		Suggestion:         f.Suggestion,
		Confidence:         f.Confidence,
		Kind:               f.Kind,
		TrifectaComponents: f.TrifectaComponents,
		ReviewID:           f.ReviewID.String(),
		AcceptedAt:         jsTimePtr(f.AcceptedAt),
		DismissedAt:        jsTimePtr(f.DismissedAt),
	}
}

// runJSON is one review run: an agent reviewing a pull request once, whatever
// the outcome (RunSummary in server/src/vendor/shared/contracts/trace.ts).
type runJSON struct {
	RunID         string  `json:"run_id"`
	AgentID       *string `json:"agent_id"`
	AgentName     *string `json:"agent_name"`
	Provider      *string `json:"provider"`
	Model         *string `json:"model"`
	Status        *string `json:"status"` // running, done, failed or cancelled
	Error         *string `json:"error"`
	DurationMs    *int32  `json:"duration_ms"`
	TokensIn      *int32  `json:"tokens_in"`
	TokensOut     *int32  `json:"tokens_out"`
	FindingsCount *int32  `json:"findings_count"`
	Grounding     *string `json:"grounding"`
	RanAt         string  `json:"ran_at"`
	Score         *int32  `json:"score"`
	Blockers      *int32  `json:"blockers"`
}

// listRuns answers GET /pulls/{id}/runs: the run history, newest first,
// including failed and cancelled runs. An unknown pull request has none.
func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	prID, ok := pathID(w, r)
	if !ok {
		return
	}
	runs, err := s.queries.ListRuns(r.Context(), postgres.ListRunsParams{WorkspaceID: s.workspace, PrID: &prID})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]runJSON, 0, len(runs))
	for _, run := range runs {
		out = append(out, runJSON{
			RunID:         run.ID.String(),
			AgentID:       uuidPtr(run.AgentID),
			AgentName:     run.AgentName,
			Provider:      run.Provider,
			Model:         run.Model,
			Status:        run.Status,
			Error:         run.Error,
			DurationMs:    run.DurationMs,
			TokensIn:      run.TokensIn,
			TokensOut:     run.TokensOut,
			FindingsCount: run.FindingsCount,
			Grounding:     run.Grounding,
			RanAt:         jsTime(run.RanAt),
			Score:         run.Score,
			Blockers:      run.Blockers,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// activeRunJSON is a run still in progress.
type activeRunJSON struct {
	RunID     string  `json:"run_id"`
	AgentID   *string `json:"agent_id"`
	AgentName *string `json:"agent_name"`
	RanAt     string  `json:"ran_at"`
}

// listActiveRuns answers GET /pulls/{id}/runs/active: the runs still in
// progress, so the web app can show them again after a page reload.
func (s *Server) listActiveRuns(w http.ResponseWriter, r *http.Request) {
	prID, ok := pathID(w, r)
	if !ok {
		return
	}
	runs, err := s.queries.ListActiveRuns(r.Context(), postgres.ListActiveRunsParams{WorkspaceID: s.workspace, PrID: &prID})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]activeRunJSON, 0, len(runs))
	for _, run := range runs {
		out = append(out, activeRunJSON{
			RunID:     run.ID.String(),
			AgentID:   uuidPtr(run.AgentID),
			AgentName: run.AgentName,
			RanAt:     jsTime(run.RanAt),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// getRunTrace answers GET /runs/{id}/trace: the run's trace, a JSON document
// with its prompt, model calls, events and findings. It is sent as stored.
func (s *Server) getRunTrace(w http.ResponseWriter, r *http.Request) {
	runID, ok := pathID(w, r)
	if !ok {
		return
	}
	trace, err := s.queries.GetRunTrace(r.Context(), postgres.GetRunTraceParams{WorkspaceID: s.workspace, RunID: runID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "Run trace not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, json.RawMessage(trace))
}

// uuidPtr is the text of an ID that may be missing.
func uuidPtr(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}
