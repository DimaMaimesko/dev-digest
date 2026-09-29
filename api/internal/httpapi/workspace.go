package httpapi

import "net/http"

// workspaceJSON is the workspace overview: where repositories are cloned and
// which ones are. Its top-level keys are camelCase, unlike the rest of the
// API, because the TS server sends them that way.
type workspaceJSON struct {
	WorkspaceID string              `json:"workspaceId"`
	CloneDir    string              `json:"cloneDir"`
	Repos       []workspaceRepoJSON `json:"repos"`
}

type workspaceRepoJSON struct {
	ID           string  `json:"id"`
	FullName     string  `json:"full_name"`
	ClonePath    *string `json:"clone_path"`
	LastPolledAt *string `json:"last_polled_at"`
	Cloned       bool    `json:"cloned"`
}

// getWorkspace answers GET /workspace.
func (s *Server) getWorkspace(w http.ResponseWriter, r *http.Request) {
	repos, err := s.queries.ListRepos(r.Context(), s.workspace)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := workspaceJSON{
		WorkspaceID: s.workspace.String(),
		CloneDir:    s.cloneDir,
		Repos:       make([]workspaceRepoJSON, 0, len(repos)),
	}
	for _, repo := range repos {
		out.Repos = append(out.Repos, workspaceRepoJSON{
			ID:           repo.ID.String(),
			FullName:     repo.FullName,
			ClonePath:    repo.ClonePath,
			LastPolledAt: jsTimePtr(repo.LastPolledAt),
			Cloned:       repo.ClonePath != nil && *repo.ClonePath != "",
		})
	}
	writeJSON(w, http.StatusOK, out)
}
