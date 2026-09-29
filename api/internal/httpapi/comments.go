package httpapi

import (
	"cmp"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/DimaMaimesko/dev-digest/api/internal/github"
)

// The review comments on a pull request's diff live on GitHub only: these
// handlers pass them through, as the TS server does, so the Files changed tab
// always shows GitHub's threads.

// commentJSON is a review comment (PrReviewComment in
// server/src/vendor/shared/contracts/platform.ts).
type commentJSON struct {
	ID           int64  `json:"id"`
	Path         string `json:"path"`
	Line         *int   `json:"line"`
	OriginalLine *int   `json:"original_line"`
	Side         string `json:"side"`
	Body         string `json:"body"`
	User         string `json:"user"`
	CreatedAt    string `json:"created_at"`
	HTMLURL      string `json:"html_url"`
	InReplyToID  *int64 `json:"in_reply_to_id"`
	// IsOutdated means GitHub can no longer place the comment on the diff.
	IsOutdated bool `json:"is_outdated"`
}

func toCommentJSON(c github.Comment) commentJSON {
	return commentJSON{
		ID:           c.ID,
		Path:         c.Path,
		Line:         c.Line,
		OriginalLine: c.OriginalLine,
		Side:         c.Side,
		Body:         c.Body,
		User:         c.User,
		CreatedAt:    jsTime(c.CreatedAt),
		HTMLURL:      c.HTMLURL,
		InReplyToID:  c.InReplyTo,
		IsOutdated:   c.Line == nil,
	}
}

// listComments answers GET /pulls/{id}/comments: the pull request's review
// comments on GitHub. Without a GitHub token, or when GitHub fails, it
// answers an empty list.
func (s *Server) listComments(w http.ResponseWriter, r *http.Request) {
	p, repo, ok := s.pullAndRepo(w, r)
	if !ok {
		return
	}
	out := []commentJSON{}
	gh, err := s.github()
	if err != nil {
		if !errors.Is(err, errNoGitHubToken) {
			s.log.Warn("GitHub token unreadable; no comments", "err", err)
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	comments, err := gh.PullComments(r.Context(), repo.Owner, repo.Name, int(p.Number))
	if err != nil {
		s.log.Warn("GitHub comments not fetched", "repo", repo.FullName, "number", p.Number, "err", err)
		writeJSON(w, http.StatusOK, out)
		return
	}
	for _, c := range comments {
		out = append(out, toCommentJSON(c))
	}
	writeJSON(w, http.StatusOK, out)
}

// createComment answers POST /pulls/{id}/comments: it posts a comment on a
// line of the pull request's diff at its head commit, or, with in_reply_to,
// a reply in that comment's thread.
func (s *Server) createComment(w http.ResponseWriter, r *http.Request) {
	if _, ok := pathID(w, r); !ok {
		return
	}
	var body map[string]json.RawMessage
	if !readJSON(w, r, &body) {
		return
	}
	f := fields{body: body}
	path := f.str("path", true, notEmpty)
	line := f.whole("line", true)
	if line != nil && *line <= 0 {
		f.bad("Expected a positive number", "line")
	}
	side := f.str("side", false, among("LEFT", "RIGHT"))
	text := f.str("body", true, notEmpty)
	inReplyTo := f.whole("in_reply_to", false)
	if len(f.issues) > 0 {
		invalid(w, f.issues)
		return
	}

	p, repo, ok := s.pullAndRepo(w, r)
	if !ok {
		return
	}
	gh, err := s.github()
	if errors.Is(err, errNoGitHubToken) {
		writeError(w, http.StatusBadRequest, "github_unavailable", "Connect a GitHub token to post comments.")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	var c github.Comment
	if inReplyTo != nil {
		c, err = gh.ReplyToComment(r.Context(), repo.Owner, repo.Name, int(p.Number), *inReplyTo, *text)
	} else {
		c, err = gh.CreateComment(r.Context(), repo.Owner, repo.Name, int(p.Number), github.NewComment{
			CommitSHA: p.HeadSha,
			Path:      *path,
			Line:      int(*line),
			Side:      cmp.Or(deref(side), "RIGHT"),
			Body:      *text,
		})
	}
	if err != nil {
		// GitHub refuses a line outside the diff, or a closed pull request.
		writeError(w, http.StatusBadRequest, "github_comment_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toCommentJSON(c))
}
