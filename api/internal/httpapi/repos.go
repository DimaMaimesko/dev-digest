package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
	"github.com/DimaMaimesko/dev-digest/api/internal/repos"
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
	rows, err := s.queries.ListRepos(r.Context(), s.workspace)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]repoJSON, 0, len(rows)) // [] rather than null when empty
	for _, repo := range rows {
		out = append(out, toRepoJSON(repo))
	}
	writeJSON(w, http.StatusOK, out)
}

// addRepo answers POST /repos with {"url": "https://github.com/owner/name"}:
// it adds the repository and clones it in the background. It answers 201
// with the repository, or 200 when the workspace had it already.
func (s *Server) addRepo(w http.ResponseWriter, r *http.Request) {
	var body map[string]json.RawMessage
	if !readJSON(w, r, &body) {
		return
	}
	f := fields{body: body}
	raw := f.str("url", true, func(v string) string {
		if u, err := url.Parse(v); err != nil || u.Scheme == "" || u.Host == "" {
			return "Invalid url"
		}
		return ""
	})
	if len(f.issues) > 0 {
		invalid(w, f.issues)
		return
	}
	ref, err := repos.ParseURL(*raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_repo_url", fmt.Sprintf("Could not parse owner/repo from '%s'", *raw))
		return
	}
	repo, created, err := s.repos.Add(r.Context(), s.workspace, s.user, ref)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, toRepoJSON(repo))
}

// refreshRepo answers POST /repos/{id}/refresh: it fetches the latest
// commits into the repository's clone in the background.
func (s *Server) refreshRepo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.repos.Refresh(r.Context(), s.workspace, id)
	if errors.Is(err, repos.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Repo not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "refreshing"})
}

// deleteRepo answers DELETE /repos/{id}: it removes the repository, with its
// pull requests and reviews.
func (s *Server) deleteRepo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.repos.Remove(r.Context(), s.workspace, id)
	if errors.Is(err, repos.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Repo not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id.String()})
}

// resyncRepo answers POST /repos/{id}/resync: in the background, it moves
// the repository's clone to the latest commit of its default branch and
// indexes what changed. It answers 202 with the job, whose outcome shows in
// GET /repos/{id}/index-state.
func (s *Server) resyncRepo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	job, err := s.repos.Resync(r.Context(), s.workspace, id)
	if errors.Is(err, repos.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Repo not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted", "jobId": job.String()})
}
