// Package github reads pull requests from GitHub's REST API and posts
// review comments on them.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultURL is the base URL of GitHub's REST API.
const DefaultURL = "https://api.github.com"

// Client calls GitHub's REST API with a personal access token.
type Client struct {
	baseURL    string
	token      string
	http       *http.Client
	timeout    time.Duration // for each request, including reading the answer
	retries    int           // extra attempts after a rate limit, server or network error
	retryDelay time.Duration // wait before the first retry; doubles after each one
}

// New returns a client for the API at baseURL, such as DefaultURL, that
// authenticates with token.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		token:      token,
		http:       &http.Client{},
		timeout:    30 * time.Second,
		retries:    3,
		retryDelay: 250 * time.Millisecond,
	}
}

// Pull is a pull request. The list leaves out the description and the diff
// stats; Pull fills them.
type Pull struct {
	Number    int
	Title     string
	Author    string // the login, or "unknown" for a deleted account
	Branch    string
	Base      string
	HeadSHA   string
	State     string // open, merged or closed
	CreatedAt time.Time
	UpdatedAt time.Time

	Body         *string // nil when there is no description
	Additions    int
	Deletions    int
	ChangedFiles int
}

// File is a file a pull request changes.
type File struct {
	Path      string
	Additions int
	Deletions int
	Patch     *string // nil for a binary file or a diff too large to show
}

// Commit is a commit of a pull request.
type Commit struct {
	SHA         string
	Message     string
	Author      string
	CommittedAt *time.Time
}

// pullJSON is a pull request as GitHub sends it.
type pullJSON struct {
	Number int     `json:"number"`
	Title  string  `json:"title"`
	Body   *string `json:"body"`
	State  string  `json:"state"`
	User   *struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	MergedAt     *time.Time `json:"merged_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	Additions    int        `json:"additions"`
	Deletions    int        `json:"deletions"`
	ChangedFiles int        `json:"changed_files"`
}

func (p pullJSON) pull() Pull {
	author := "unknown"
	if p.User != nil {
		author = p.User.Login
	}
	state := "open"
	switch {
	case p.MergedAt != nil:
		state = "merged"
	case p.State == "closed":
		state = "closed"
	}
	return Pull{
		Number:       p.Number,
		Title:        p.Title,
		Author:       author,
		Branch:       p.Head.Ref,
		Base:         p.Base.Ref,
		HeadSHA:      p.Head.SHA,
		State:        state,
		CreatedAt:    p.CreatedAt,
		UpdatedAt:    p.UpdatedAt,
		Body:         p.Body,
		Additions:    p.Additions,
		Deletions:    p.Deletions,
		ChangedFiles: p.ChangedFiles,
	}
}

// Pulls returns the repository's 50 most recently updated pull requests,
// open or not.
func (c *Client) Pulls(ctx context.Context, owner, repo string) ([]Pull, error) {
	var page []pullJSON
	if err := c.get(ctx, repoPath(owner, repo)+"/pulls?state=all&sort=updated&direction=desc&per_page=50", &page); err != nil {
		return nil, err
	}
	pulls := make([]Pull, len(page))
	for i, p := range page {
		pulls[i] = p.pull()
	}
	return pulls, nil
}

// Pull returns one pull request, with its description and diff stats.
func (c *Client) Pull(ctx context.Context, owner, repo string, number int) (Pull, error) {
	var p pullJSON
	if err := c.get(ctx, fmt.Sprintf("%s/pulls/%d", repoPath(owner, repo), number), &p); err != nil {
		return Pull{}, err
	}
	return p.pull(), nil
}

// Login returns the login of the account the token belongs to.
func (c *Client) Login(ctx context.Context) (string, error) {
	var user struct {
		Login string `json:"login"`
	}
	err := c.get(ctx, "/user", &user)
	return user.Login, err
}

// PullFiles returns the first 100 files a pull request changes.
func (c *Client) PullFiles(ctx context.Context, owner, repo string, number int) ([]File, error) {
	var page []struct {
		Filename  string  `json:"filename"`
		Additions int     `json:"additions"`
		Deletions int     `json:"deletions"`
		Patch     *string `json:"patch"`
	}
	if err := c.get(ctx, fmt.Sprintf("%s/pulls/%d/files?per_page=100", repoPath(owner, repo), number), &page); err != nil {
		return nil, err
	}
	files := make([]File, len(page))
	for i, f := range page {
		files[i] = File{Path: f.Filename, Additions: f.Additions, Deletions: f.Deletions, Patch: f.Patch}
	}
	return files, nil
}

// PullCommits returns a pull request's first 100 commits. A commit's author
// is the name in the commit, else the GitHub login, else "unknown".
func (c *Client) PullCommits(ctx context.Context, owner, repo string, number int) ([]Commit, error) {
	var page []struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
			Author  *struct {
				Name string     `json:"name"`
				Date *time.Time `json:"date"`
			} `json:"author"`
		} `json:"commit"`
		Author *struct {
			Login string `json:"login"`
		} `json:"author"`
	}
	if err := c.get(ctx, fmt.Sprintf("%s/pulls/%d/commits?per_page=100", repoPath(owner, repo), number), &page); err != nil {
		return nil, err
	}
	commits := make([]Commit, len(page))
	for i, pc := range page {
		commit := Commit{SHA: pc.SHA, Message: pc.Commit.Message, Author: "unknown"}
		switch {
		case pc.Commit.Author != nil && pc.Commit.Author.Name != "":
			commit.Author = pc.Commit.Author.Name
		case pc.Author != nil:
			commit.Author = pc.Author.Login
		}
		if pc.Commit.Author != nil {
			commit.CommittedAt = pc.Commit.Author.Date
		}
		commits[i] = commit
	}
	return commits, nil
}

// Comment is a review comment on a line of a pull request's diff.
type Comment struct {
	ID           int64
	Path         string
	Line         *int // nil when the line is no longer in the diff
	OriginalLine *int
	Side         string // LEFT (the old file) or RIGHT (the new one)
	Body         string
	User         string // the login, or "unknown" for a deleted account
	CreatedAt    time.Time
	HTMLURL      string
	InReplyTo    *int64 // the thread's first comment, for a reply
}

type commentJSON struct {
	ID           int64   `json:"id"`
	Path         string  `json:"path"`
	Line         *int    `json:"line"`
	OriginalLine *int    `json:"original_line"`
	Side         *string `json:"side"`
	Body         string  `json:"body"`
	User         *struct {
		Login string `json:"login"`
	} `json:"user"`
	CreatedAt   time.Time `json:"created_at"`
	HTMLURL     string    `json:"html_url"`
	InReplyToID *int64    `json:"in_reply_to_id"`
}

func (c commentJSON) comment() Comment {
	out := Comment{
		ID: c.ID, Path: c.Path, Line: c.Line, OriginalLine: c.OriginalLine, Side: "RIGHT",
		Body: c.Body, User: "unknown", CreatedAt: c.CreatedAt, HTMLURL: c.HTMLURL, InReplyTo: c.InReplyToID,
	}
	if c.Side != nil && *c.Side == "LEFT" {
		out.Side = "LEFT"
	}
	if c.User != nil {
		out.User = c.User.Login
	}
	return out
}

// PullComments returns the first 100 review comments of a pull request.
func (c *Client) PullComments(ctx context.Context, owner, repo string, number int) ([]Comment, error) {
	var page []commentJSON
	if err := c.get(ctx, fmt.Sprintf("%s/pulls/%d/comments?per_page=100", repoPath(owner, repo), number), &page); err != nil {
		return nil, err
	}
	comments := make([]Comment, len(page))
	for i, cm := range page {
		comments[i] = cm.comment()
	}
	return comments, nil
}

// NewComment is a review comment to post on a line of a pull request's diff.
type NewComment struct {
	CommitSHA string // the commit whose diff the line is in
	Path      string
	Line      int
	Side      string // LEFT or RIGHT
	Body      string
}

// CreateComment posts a review comment that starts a thread.
func (c *Client) CreateComment(ctx context.Context, owner, repo string, number int, in NewComment) (Comment, error) {
	req := struct {
		CommitID string `json:"commit_id"`
		Path     string `json:"path"`
		Line     int    `json:"line"`
		Side     string `json:"side"`
		Body     string `json:"body"`
	}{in.CommitSHA, in.Path, in.Line, in.Side, in.Body}
	var out commentJSON
	if err := c.send(ctx, http.MethodPost, fmt.Sprintf("%s/pulls/%d/comments", repoPath(owner, repo), number), req, &out); err != nil {
		return Comment{}, err
	}
	return out.comment(), nil
}

// ReplyToComment posts body as a reply in the thread of comment to.
func (c *Client) ReplyToComment(ctx context.Context, owner, repo string, number int, to int64, body string) (Comment, error) {
	req := struct {
		Body string `json:"body"`
	}{body}
	var out commentJSON
	path := fmt.Sprintf("%s/pulls/%d/comments/%d/replies", repoPath(owner, repo), number, to)
	if err := c.send(ctx, http.MethodPost, path, req, &out); err != nil {
		return Comment{}, err
	}
	return out.comment(), nil
}

func repoPath(owner, repo string) string {
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
}

// get sends a GET request for path and decodes the JSON answer into out.
func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.send(ctx, http.MethodGet, path, nil, out)
}

// send sends a request with in, when not nil, as its JSON body, and decodes
// the JSON answer into out. It retries rate limits, waiting longer each
// time. A GET is also retried after a server or network error; a POST isn't,
// since GitHub may have done it already.
func (c *Client) send(ctx context.Context, method, path string, in, out any) error {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return err
		}
	}
	delay := c.retryDelay
	for attempt := 0; ; attempt++ {
		err := c.sendOnce(ctx, method, path, body, out)
		if err == nil || attempt == c.retries || !retryable(ctx, err, method) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
	}
}

func (c *Client) sendOnce(ctx context.Context, method, path string, body []byte, out any) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "devdigest") // GitHub rejects requests without one
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
	if err != nil {
		return fmt.Errorf("read GitHub's answer: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return &StatusError{Code: resp.StatusCode, Message: errorMessage(data)}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode GitHub's answer: %w", err)
	}
	return nil
}

// StatusError is an HTTP error from GitHub.
type StatusError struct {
	Code    int
	Message string // GitHub's error message, or the start of the response body
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("GitHub returned %d %s: %s", e.Code, http.StatusText(e.Code), e.Message)
}

// errorMessage returns the message of a GitHub error response.
func errorMessage(body []byte) string {
	var res struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &res) == nil && res.Message != "" {
		return res.Message
	}
	msg := strings.TrimSpace(string(body))
	if len(msg) > 200 {
		msg = msg[:200] + "…"
	}
	return msg
}

// retryable reports whether trying the same request again may succeed, and
// is safe: only a GET is repeated after an error that may come after GitHub
// did the work.
func retryable(ctx context.Context, err error, method string) bool {
	if ctx.Err() != nil {
		return false // cancelled, or out of time
	}
	var status *StatusError
	if errors.As(err, &status) {
		return status.Code == http.StatusTooManyRequests || (status.Code >= 500 && method == http.MethodGet)
	}
	var netErr net.Error
	return errors.As(err, &netErr) && method == http.MethodGet
}
