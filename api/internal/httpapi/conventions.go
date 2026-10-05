package httpapi

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

// conventionJSON is a convention found in a repository (ConventionCandidate
// in the contracts). The contract has no nulls, so a missing evidence is ""
// and a missing confidence 0.
type conventionJSON struct {
	ID              string  `json:"id"`
	Rule            string  `json:"rule"`
	EvidencePath    string  `json:"evidence_path"`
	EvidenceSnippet string  `json:"evidence_snippet"`
	Confidence      float64 `json:"confidence"`
	Accepted        bool    `json:"accepted"`
}

// listConventions answers GET /repos/{id}/conventions: the repository's
// conventions, accepted first, then the most confident.
func (s *Server) listConventions(w http.ResponseWriter, r *http.Request) {
	repoID, ok := pathID(w, r)
	if !ok {
		return
	}
	_, err := s.queries.GetRepo(r.Context(), postgres.GetRepoParams{WorkspaceID: s.workspace, ID: repoID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "Repo not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	rows, err := s.queries.ListConventions(r.Context(), postgres.ListConventionsParams{WorkspaceID: s.workspace, RepoID: &repoID})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]conventionJSON, 0, len(rows))
	for _, c := range rows {
		out = append(out, conventionJSON{
			ID:              c.ID.String(),
			Rule:            c.Rule,
			EvidencePath:    deref(c.EvidencePath),
			EvidenceSnippet: deref(c.EvidenceSnippet),
			Confidence:      deref(c.Confidence),
			Accepted:        c.Accepted,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
