package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

// authorsPageSize is how many authors one page of GET /repos/{id}/authors
// holds.
const authorsPageSize = 20

type authorJSON struct {
	Author    string `json:"author"`
	Pulls     int    `json:"pulls"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	AvgSize   int    `json:"avg_size"`
}

// listAuthors answers GET /repos/{id}/authors?sort=pulls&page=1: the
// repository's pull request authors, largest first.
func (s *Server) listAuthors(w http.ResponseWriter, r *http.Request) {
	repoID, ok := pathID(w, r)
	if !ok {
		return
	}
	repo, err := s.queries.GetRepo(r.Context(), postgres.GetRepoParams{WorkspaceID: s.workspace, ID: repoID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "Repo not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "pulls"
	}
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, err = strconv.Atoi(raw)
		if err != nil || page < 1 {
			invalidParam(w, "page", "page must be a positive number")
			return
		}
	}

	stats, err := s.pulls.AuthorStats(r.Context(), repo.ID, sortBy, page, authorsPageSize)
	if err != nil {
		s.internalError(w, r, err)
	}
	authors := make([]authorJSON, 0, len(stats))
	for _, a := range stats {
		authors = append(authors, authorJSON{
			Author:    a.Author,
			Pulls:     a.Pulls,
			Additions: a.Additions,
			Deletions: a.Deletions,
			AvgSize:   a.AvgSize,
		})
	}
	writeJSON(w, http.StatusOK, authors)
}
