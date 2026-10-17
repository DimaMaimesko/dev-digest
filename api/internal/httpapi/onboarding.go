package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/DimaMaimesko/dev-digest/api/internal/onboarding"
)

// generationJSON is an onboarding_runs row the way the web app reads it
// (the OnboardingGeneration contract in
// client/src/vendor/shared/contracts/onboarding.ts).
type generationJSON struct {
	ID         string                     `json:"id"`
	RepoID     string                     `json:"repo_id"`
	Status     string                     `json:"status"`
	Error      *string                    `json:"error"`
	CommitSha  string                     `json:"commit_sha"`
	CreatedAt  string                     `json:"created_at"`
	FinishedAt *string                    `json:"finished_at"`
	Sections   []onboarding.SectionStatus `json:"sections"`
}

func toGenerationJSON(g onboarding.Generation) generationJSON {
	return generationJSON{
		ID: g.ID.String(), RepoID: g.RepoID.String(), Status: g.Status, Error: g.Error,
		CommitSha: g.CommitSha, CreatedAt: jsTime(g.CreatedAt), FinishedAt: jsTimePtr(g.FinishedAt),
		Sections: g.Sections,
	}
}

// tourJSON is a repository's stored onboarding tour, the way the web app
// reads it (the OnboardingTour contract).
type tourJSON struct {
	RepoID         string                   `json:"repo_id"`
	CommitSha      string                   `json:"commit_sha"`
	FilesIndexed   int32                    `json:"files_indexed"`
	IndexStatus    string                   `json:"index_status"`
	CodeIndexEmpty bool                     `json:"code_index_empty"`
	GeneratedAt    string                   `json:"generated_at"`
	Provider       string                   `json:"provider"`
	Model          string                   `json:"model"`
	CostUsd        *float64                 `json:"cost_usd"`
	Sections       []onboarding.TourSection `json:"sections"`
}

func toTourJSON(t onboarding.Tour) tourJSON {
	return tourJSON{
		RepoID: t.RepoID.String(), CommitSha: t.CommitSha, FilesIndexed: t.FilesIndexed,
		IndexStatus: t.IndexStatus, CodeIndexEmpty: t.CodeIndexEmpty, GeneratedAt: jsTime(t.GeneratedAt),
		Provider: t.Provider, Model: t.Model, CostUsd: t.CostUsd, Sections: t.Sections,
	}
}

// onboardingTourResponseJSON is GET /repos/{id}/onboarding's answer (the
// OnboardingTourResponse contract).
type onboardingTourResponseJSON struct {
	Index      onboarding.IndexReadiness `json:"index"`
	Tour       *tourJSON                 `json:"tour"`
	Generation *generationJSON           `json:"generation"`
}

func toOnboardingTourResponseJSON(v onboarding.View) onboardingTourResponseJSON {
	out := onboardingTourResponseJSON{Index: v.Index}
	if v.Tour != nil {
		t := toTourJSON(*v.Tour)
		out.Tour = &t
	}
	if v.Generation != nil {
		g := toGenerationJSON(*v.Generation)
		out.Generation = &g
	}
	return out
}

// getOnboardingTour answers GET /repos/{id}/onboarding: the repository's
// index readiness, its stored tour (if any) and its latest generation (if
// any). An unknown repository, or one in another workspace, answers 404.
func (s *Server) getOnboardingTour(w http.ResponseWriter, r *http.Request) {
	repoID, ok := pathID(w, r)
	if !ok {
		return
	}
	view, err := s.onboarding.Get(r.Context(), s.workspace, repoID)
	if errors.Is(err, onboarding.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Repo not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toOnboardingTourResponseJSON(view))
}

// generateOnboardingTour answers POST /repos/{id}/onboarding (no body): it
// starts a tour generation in the background, or answers the one already
// queued or running for this repository.
func (s *Server) generateOnboardingTour(w http.ResponseWriter, r *http.Request) {
	repoID, ok := pathID(w, r)
	if !ok {
		return
	}
	gen, err := s.onboarding.Start(r.Context(), s.workspace, repoID)
	switch {
	case errors.Is(err, onboarding.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Repo not found")
		return
	case errors.Is(err, onboarding.ErrNotIndexed):
		writeError(w, http.StatusConflict, "repo_not_indexed", "Repository is not indexed")
		return
	}
	var cfgErr *onboarding.ConfigError
	if errors.As(err, &cfgErr) {
		writeError(w, http.StatusBadRequest, "config_error", fmt.Sprintf("%s for provider %s", cfgErr.Err.Error(), cfgErr.Provider))
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, toGenerationJSON(gen))
}
