package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

// Until phase 3 adds a GitHub client, these handlers serve pull requests from
// the database. The TS server does the same when it has no GitHub token; with
// one, it first syncs from GitHub.

// pullJSON is the part of a pull request that the list and the detail share
// (PrMeta in server/src/vendor/shared/contracts/platform.ts).
type pullJSON struct {
	ID         string  `json:"id"`
	Number     int32   `json:"number"`
	Title      string  `json:"title"`
	Author     string  `json:"author"`
	Branch     string  `json:"branch"`
	Base       string  `json:"base"`
	HeadSHA    string  `json:"head_sha"`
	Additions  int32   `json:"additions"`
	Deletions  int32   `json:"deletions"`
	FilesCount int32   `json:"files_count"`
	Status     string  `json:"status"`
	OpenedAt   *string `json:"opened_at"`
	UpdatedAt  *string `json:"updated_at"`
}

// pullListItemJSON is a pull request in the list, with its latest review's
// score. Embedding pullJSON puts its fields at the same level in the JSON.
type pullListItemJSON struct {
	pullJSON
	Score *int32 `json:"score"`
}

// pullDetailJSON is one pull request with its description, files and commits
// (PrDetail in the contracts).
type pullDetailJSON struct {
	pullJSON
	Body    *string      `json:"body"`
	Files   []fileJSON   `json:"files"`
	Commits []commitJSON `json:"commits"`
}

type fileJSON struct {
	Path      string  `json:"path"`
	Additions int32   `json:"additions"`
	Deletions int32   `json:"deletions"`
	Patch     *string `json:"patch"`
}

type commitJSON struct {
	SHA         string  `json:"sha"`
	Message     string  `json:"message"`
	Author      string  `json:"author"`
	CommittedAt *string `json:"committed_at"`
}

// listPulls answers GET /repos/{id}/pulls: the repository's pull requests,
// each with where it stands for review and its latest review's score.
func (s *Server) listPulls(w http.ResponseWriter, r *http.Request) {
	repoID, ok := pathID(w, r)
	if !ok {
		return
	}
	exists, err := s.queries.RepoExists(r.Context(), postgres.RepoExistsParams{WorkspaceID: s.workspace, ID: repoID})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "not_found", "Repo not found")
		return
	}

	pulls, err := s.queries.ListPulls(r.Context(), repoID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	now := time.Now()
	out := make([]pullListItemJSON, 0, len(pulls))
	for _, p := range pulls {
		out = append(out, pullListItemJSON{
			pullJSON: pullJSON{
				ID:         p.ID.String(),
				Number:     p.Number,
				Title:      p.Title,
				Author:     p.Author,
				Branch:     p.Branch,
				Base:       p.Base,
				HeadSHA:    p.HeadSha,
				Additions:  p.Additions,
				Deletions:  p.Deletions,
				FilesCount: p.FilesCount,
				Status:     reviewStatus(p.Status, p.HeadSha, p.LastReviewedSha, p.UpdatedAt, now),
				OpenedAt:   jsTimePtr(p.OpenedAt),
				UpdatedAt:  jsTimePtr(p.UpdatedAt),
			},
			Score: p.LatestScore,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// getPull answers GET /pulls/{id}: one pull request with its description,
// changed files and commits.
func (s *Server) getPull(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.queries.GetPull(r.Context(), postgres.GetPullParams{WorkspaceID: s.workspace, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "Pull request not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	files, err := s.queries.ListPullFiles(r.Context(), p.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	commits, err := s.queries.ListPullCommits(r.Context(), p.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	out := pullDetailJSON{
		pullJSON: pullJSON{
			ID:         p.ID.String(),
			Number:     p.Number,
			Title:      p.Title,
			Author:     p.Author,
			Branch:     p.Branch,
			Base:       p.Base,
			HeadSHA:    p.HeadSha,
			Additions:  p.Additions,
			Deletions:  p.Deletions,
			FilesCount: p.FilesCount,
			// GitHub's state (open, merged, closed), as the TS server sends it
			// here; only the list works out the review status.
			Status:    p.Status,
			OpenedAt:  jsTimePtr(p.OpenedAt),
			UpdatedAt: jsTimePtr(p.UpdatedAt),
		},
		Body:    p.Body,
		Files:   make([]fileJSON, 0, len(files)),
		Commits: make([]commitJSON, 0, len(commits)),
	}
	for _, f := range files {
		out.Files = append(out.Files, fileJSON{Path: f.Path, Additions: f.Additions, Deletions: f.Deletions, Patch: f.Patch})
	}
	for _, c := range commits {
		out.Commits = append(out.Commits, commitJSON{SHA: c.Sha, Message: c.Message, Author: c.Author, CommittedAt: jsTimePtr(c.CommittedAt)})
	}
	writeJSON(w, http.StatusOK, out)
}

// staleAfter is how long a reviewed pull request can go without changes
// before the list calls it stale.
const staleAfter = 7 * 24 * time.Hour

// reviewStatus says where a pull request stands for review, from GitHub's
// state and the commit its latest review ran against. Merged and closed pull
// requests keep GitHub's state. An open one:
//   - needs_review: never reviewed, or new commits since the last review
//   - stale: its latest commit was reviewed, but it was last updated over a
//     week ago
//   - reviewed: its latest commit was reviewed recently
func reviewStatus(githubState, headSHA string, lastReviewedSHA *string, updatedAt *time.Time, now time.Time) string {
	if githubState == "merged" || githubState == "closed" {
		return githubState
	}
	if lastReviewedSHA == nil || *lastReviewedSHA != headSHA {
		return "needs_review"
	}
	if updatedAt != nil && now.Sub(*updatedAt) > staleAfter {
		return "stale"
	}
	return "reviewed"
}

// jsTimePtr is jsTime for a time that may be missing.
func jsTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := jsTime(*t)
	return &s
}
