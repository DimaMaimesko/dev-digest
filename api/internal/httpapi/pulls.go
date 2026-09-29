package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/DimaMaimesko/dev-digest/api/internal/github"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
	"github.com/DimaMaimesko/dev-digest/api/internal/pulls"
	"github.com/DimaMaimesko/dev-digest/api/internal/secrets"
)

// With a GitHub token, reading pull requests first syncs them from GitHub.
// When GitHub can't be reached, the saved pull requests are served, so they
// stay readable offline, as in TS.

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
// each with where it stands for review and its latest review's score. With a
// GitHub token, it first saves GitHub's list and the diff stats of up to 10
// pull requests that have none.
func (s *Server) listPulls(w http.ResponseWriter, r *http.Request) {
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
	if gh := s.githubForRead(r); gh != nil {
		if _, err := s.pulls.SyncList(r.Context(), gh, repo); err != nil {
			s.log.Warn("GitHub sync skipped; serving saved pull requests", "repo", repo.FullName, "err", err)
		}
		if err := s.pulls.BackfillStats(r.Context(), gh, repo); err != nil {
			s.log.Warn("diff stats not fetched", "repo", repo.FullName, "err", err)
		}
	}

	saved, err := s.queries.ListPulls(r.Context(), repo.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	now := time.Now()
	out := make([]pullListItemJSON, 0, len(saved))
	for _, p := range saved {
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
// changed files and commits. With a GitHub token they are fetched again and
// saved first; the answer always comes from the database.
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
	repo, err := s.queries.GetRepo(r.Context(), postgres.GetRepoParams{WorkspaceID: s.workspace, ID: p.RepoID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "Repo not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if gh := s.githubForRead(r); gh != nil {
		if err := s.pulls.Refresh(r.Context(), gh, repo, p.Number); err != nil {
			s.log.Warn("GitHub refresh skipped; serving the saved pull request", "repo", repo.FullName, "number", p.Number, "err", err)
		} else if p, err = s.queries.GetPull(r.Context(), postgres.GetPullParams{WorkspaceID: s.workspace, ID: id}); err != nil {
			s.internalError(w, r, err)
			return
		}
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

// pollRepo answers POST /repos/{id}/poll: it saves GitHub's list of the
// repository's pull requests, like reading the list does, but fails when
// GitHub can't be reached. It never starts a review.
func (s *Server) pollRepo(w http.ResponseWriter, r *http.Request) {
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
	gh, err := s.github()
	if errors.Is(err, errNoGitHubToken) {
		writeError(w, http.StatusInternalServerError, "config_error", "GITHUB_TOKEN is not configured")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	synced, err := s.pulls.SyncList(r.Context(), gh, repo)
	var ghErr *pulls.GitHubError
	if errors.As(err, &ghErr) {
		writeError(w, http.StatusBadGateway, "github_error", ghErr.Error())
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.queries.MarkRepoPolled(r.Context(), repo.ID); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Synced          int  `json:"synced"`
		ReviewTriggered bool `json:"reviewTriggered"` // always false: reviews start only by hand
	}{Synced: synced})
}

// errNoGitHubToken means no GitHub token is set, or GitHub is turned off.
var errNoGitHubToken = errors.New("no GitHub token")

// github returns a GitHub client with the saved token.
func (s *Server) github() (*github.Client, error) {
	if s.githubAPI == "" {
		return nil, errNoGitHubToken
	}
	token, err := s.secrets.Get(secrets.GitHubToken)
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, errNoGitHubToken
	}
	return github.New(s.githubAPI, token), nil
}

// githubForRead returns a GitHub client for syncing before a read, or nil
// when there is no token. A read never fails for want of GitHub.
func (s *Server) githubForRead(r *http.Request) *github.Client {
	gh, err := s.github()
	if err != nil {
		if !errors.Is(err, errNoGitHubToken) {
			s.log.Warn("GitHub token unreadable; serving saved pull requests", "path", r.URL.Path, "err", err)
		}
		return nil
	}
	return gh
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
