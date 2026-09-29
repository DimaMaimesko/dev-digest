package httpapi

import (
	"net/http"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

// repoJSON is a repository as the web app reads it (the Repo contract in
// server/src/vendor/shared/contracts/platform.ts). Nullable fields are
// pointers: the contract sends null, not an empty string.
type repoJSON struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	Owner         string  `json:"owner"`
	Name          string  `json:"name"`
	FullName      string  `json:"full_name"`
	DefaultBranch string  `json:"default_branch"`
	ClonePath     *string `json:"clone_path"`
	LastPolledAt  *string `json:"last_polled_at"`
	CreatedBy     *string `json:"created_by"`
}

func toRepoJSON(r postgres.Repo) repoJSON {
	out := repoJSON{
		ID:            r.ID.String(),
		WorkspaceID:   r.WorkspaceID.String(),
		Owner:         r.Owner,
		Name:          r.Name,
		FullName:      r.FullName,
		DefaultBranch: r.DefaultBranch,
		ClonePath:     r.ClonePath,
	}
	out.LastPolledAt = jsTimePtr(r.LastPolledAt)
	out.CreatedBy = uuidPtr(r.CreatedBy)
	return out
}

// listRepos answers GET /repos: the workspace's repositories, oldest first.
func (s *Server) listRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := s.queries.ListRepos(r.Context(), s.workspace)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]repoJSON, 0, len(repos)) // [] rather than null when empty
	for _, repo := range repos {
		out = append(out, toRepoJSON(repo))
	}
	writeJSON(w, http.StatusOK, out)
}
