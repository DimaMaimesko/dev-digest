// Package github reads pull requests from GitHub's REST API.
package github

import (
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

func repoPath(owner, repo string) string {
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
}

// get sends a GET request for path and decodes the JSON answer into out. It
// retries rate limits, server errors and network errors, waiting longer each
// time.
func (c *Client) get(ctx context.Context, path string, out any) error {
	delay := c.retryDelay
	for attempt := 0; ; attempt++ {
		err := c.getOnce(ctx, path, out)
		if err == nil || attempt == c.retries || !retryable(ctx, err) {
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

func (c *Client) getOnce(ctx context.Context, path string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
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
	if resp.StatusCode != http.StatusOK {
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

// retryable reports whether trying the same request again may succeed.
func retryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false // cancelled, or out of time
	}
	var status *StatusError
	if errors.As(err, &status) {
		return status.Code == http.StatusTooManyRequests || status.Code >= 500
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}
