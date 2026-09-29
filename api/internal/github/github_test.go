package github

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGitHub answers each path with the next of its answers ("200 {...}"),
// repeating the last, and records the requests it gets.
type fakeGitHub struct {
	mu       sync.Mutex
	answers  map[string][]string // by path and query
	requests []*http.Request
	bodies   []string
}

func newFake(t *testing.T, answers map[string][]string) (*Client, *fakeGitHub) {
	t.Helper()
	f := &fakeGitHub{answers: answers}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := New(srv.URL+"/", "ghp_test")
	c.retryDelay = time.Millisecond
	return c, f
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r)
	sent, _ := io.ReadAll(r.Body)
	f.bodies = append(f.bodies, string(sent))
	queue := f.answers[r.URL.RequestURI()]
	if len(queue) == 0 {
		http.Error(w, `{"message": "Not Found"}`, http.StatusNotFound)
		return
	}
	answer := queue[0]
	if len(queue) > 1 {
		f.answers[r.URL.RequestURI()] = queue[1:]
	}
	status, body, _ := strings.Cut(answer, " ")
	code, _ := strconv.Atoi(status)
	w.WriteHeader(code)
	w.Write([]byte(body))
}

func (f *fakeGitHub) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func date(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// A pull request as GitHub's list sends it, trimmed to the fields read.
func listed(number int, state, mergedAt, user string) string {
	return `{"number": ` + strconv.Itoa(number) + `, "title": "T", "state": "` + state + `",
		"user": ` + user + `, "head": {"ref": "feat", "sha": "abc"}, "base": {"ref": "main"},
		"merged_at": ` + mergedAt + `, "created_at": "2026-06-01T00:00:00Z", "updated_at": "2026-06-02T03:04:05Z"}`
}

func TestPulls(t *testing.T) {
	c, f := newFake(t, map[string][]string{
		"/repos/acme/w/pulls?state=all&sort=updated&direction=desc&per_page=50": {`200 [` +
			listed(1, "open", "null", `{"login": "ann"}`) + `,` +
			listed(2, "closed", `"2026-06-03T00:00:00Z"`, `{"login": "bob"}`) + `,` +
			listed(3, "closed", "null", "null") + `]`},
	})
	got, err := c.Pulls(context.Background(), "acme", "w")
	if err != nil {
		t.Fatal(err)
	}
	pull := func(number int, state, author string) Pull {
		return Pull{Number: number, Title: "T", Author: author, Branch: "feat", Base: "main", HeadSHA: "abc",
			State: state, CreatedAt: date("2026-06-01T00:00:00Z"), UpdatedAt: date("2026-06-02T03:04:05Z")}
	}
	want := []Pull{pull(1, "open", "ann"), pull(2, "merged", "bob"), pull(3, "closed", "unknown")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Pulls =\n%+v\nwant\n%+v", got, want)
	}

	req := f.requests[0]
	headers := map[string]string{
		"Authorization":        "Bearer ghp_test",
		"Accept":               "application/vnd.github+json",
		"User-Agent":           "devdigest",
		"X-Github-Api-Version": "2022-11-28",
	}
	for name, want := range headers {
		if got := req.Header.Get(name); got != want {
			t.Errorf("header %s = %q, want %q", name, got, want)
		}
	}
}

func TestPull(t *testing.T) {
	c, _ := newFake(t, map[string][]string{
		"/repos/acme/w/pulls/7": {`200 {"number": 7, "title": "T", "state": "open", "body": "Fixes it.",
			"user": {"login": "ann"}, "head": {"ref": "feat", "sha": "abc"}, "base": {"ref": "main"},
			"merged_at": null, "created_at": "2026-06-01T00:00:00Z", "updated_at": "2026-06-02T00:00:00Z",
			"additions": 247, "deletions": 38, "changed_files": 9}`},
		"/repos/acme/w/pulls/8": {`200 {"number": 8, "body": null, "state": "open", "user": null}`},
	})
	got, err := c.Pull(context.Background(), "acme", "w", 7)
	if err != nil {
		t.Fatal(err)
	}
	want := Pull{Number: 7, Title: "T", Author: "ann", Branch: "feat", Base: "main", HeadSHA: "abc", State: "open",
		CreatedAt: date("2026-06-01T00:00:00Z"), UpdatedAt: date("2026-06-02T00:00:00Z"),
		Body: new("Fixes it."), Additions: 247, Deletions: 38, ChangedFiles: 9}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Pull =\n%+v\nwant\n%+v", got, want)
	}
	if got, err := c.Pull(context.Background(), "acme", "w", 8); err != nil || got.Body != nil {
		t.Errorf("no description: Body = %v, err %v; want nil", got.Body, err)
	}
}

func TestPullFilesAndCommits(t *testing.T) {
	c, _ := newFake(t, map[string][]string{
		"/repos/acme/w/pulls/7/files?per_page=100": {`200 [
			{"filename": "a.go", "additions": 4, "deletions": 1, "patch": "@@ -1 +1 @@"},
			{"filename": "logo.png", "additions": 0, "deletions": 0}]`},
		"/repos/acme/w/pulls/7/commits?per_page=100": {`200 [
			{"sha": "c1", "commit": {"message": "One", "author": {"name": "Ann Lee", "date": "2026-06-01T10:00:00Z"}}, "author": {"login": "ann"}},
			{"sha": "c2", "commit": {"message": "Two", "author": null}, "author": {"login": "bob"}},
			{"sha": "c3", "commit": {"message": "Three", "author": null}, "author": null}]`},
	})
	files, err := c.PullFiles(context.Background(), "acme", "w", 7)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := []File{{Path: "a.go", Additions: 4, Deletions: 1, Patch: new("@@ -1 +1 @@")}, {Path: "logo.png"}}
	if !reflect.DeepEqual(files, wantFiles) {
		t.Errorf("PullFiles = %+v, want %+v", files, wantFiles)
	}

	commits, err := c.PullCommits(context.Background(), "acme", "w", 7)
	if err != nil {
		t.Fatal(err)
	}
	wantCommits := []Commit{
		{SHA: "c1", Message: "One", Author: "Ann Lee", CommittedAt: new(date("2026-06-01T10:00:00Z"))},
		{SHA: "c2", Message: "Two", Author: "bob"},
		{SHA: "c3", Message: "Three", Author: "unknown"},
	}
	if !reflect.DeepEqual(commits, wantCommits) {
		t.Errorf("PullCommits = %+v, want %+v", commits, wantCommits)
	}
}

func TestRetries(t *testing.T) {
	const path = "/repos/acme/w/pulls/7"
	ok := `200 {"number": 7, "state": "open"}`
	tests := []struct {
		name     string
		answers  []string
		requests int
		code     int // of the StatusError; 0 for success
	}{
		{"success", []string{ok}, 1, 0},
		{"server error, then success", []string{`502 bad gateway`, ok}, 2, 0},
		{"rate limited, then success", []string{`429 {"message": "slow down"}`, ok}, 2, 0},
		{"not found isn't retried", []string{`404 {"message": "Not Found"}`}, 1, 404},
		{"bad token isn't retried", []string{`401 {"message": "Bad credentials"}`}, 1, 401},
		{"gives up after 3 retries", []string{`503 down`}, 4, 503},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, f := newFake(t, map[string][]string{path: tt.answers})
			_, err := c.Pull(context.Background(), "acme", "w", 7)
			if f.count() != tt.requests {
				t.Errorf("%d requests, want %d", f.count(), tt.requests)
			}
			var status *StatusError
			switch {
			case tt.code == 0 && err != nil:
				t.Errorf("err = %v", err)
			case tt.code != 0 && (!errors.As(err, &status) || status.Code != tt.code):
				t.Errorf("err = %v, want status %d", err, tt.code)
			}
		})
	}
}

func TestErrorMessage(t *testing.T) {
	c, _ := newFake(t, map[string][]string{"/repos/acme/w/pulls/7": {`401 {"message": "Bad credentials"}`}})
	_, err := c.Pull(context.Background(), "acme", "w", 7)
	if want := "GitHub returned 401 Unauthorized: Bad credentials"; err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}

func TestCancelWhileWaiting(t *testing.T) {
	c, _ := newFake(t, map[string][]string{"/repos/acme/w/pulls/7": {`503 down`}})
	c.retryDelay = time.Hour // the test would hang if the wait ignored ctx
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Pull(ctx, "acme", "w", 7); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want the context's error", err)
	}
}

func TestTimeout(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(block); srv.Close() })
	c := New(srv.URL, "t")
	c.timeout = 20 * time.Millisecond
	c.retries = 1
	c.retryDelay = time.Millisecond
	start := time.Now()
	if _, err := c.Pull(context.Background(), "acme", "w", 7); err == nil {
		t.Fatal("no error from a server that never answers")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestPullComments(t *testing.T) {
	c, _ := newFake(t, map[string][]string{
		"/repos/acme/w/pulls/7/comments?per_page=100": {`200 [
			{"id": 1, "path": "a.go", "line": 11, "original_line": 10, "side": "RIGHT", "body": "Why?",
			 "user": {"login": "ann"}, "created_at": "2026-06-01T00:00:00Z", "html_url": "https://x/1"},
			{"id": 2, "path": "a.go", "line": null, "original_line": 3, "side": "LEFT", "body": "Old",
			 "user": null, "created_at": "2026-06-02T00:00:00Z", "html_url": "https://x/2", "in_reply_to_id": 1},
			{"id": 3, "path": "b.go", "body": "No side", "created_at": "2026-06-03T00:00:00Z", "html_url": "https://x/3"}]`},
	})
	got, err := c.PullComments(context.Background(), "acme", "w", 7)
	if err != nil {
		t.Fatal(err)
	}
	want := []Comment{
		{ID: 1, Path: "a.go", Line: new(11), OriginalLine: new(10), Side: "RIGHT", Body: "Why?", User: "ann",
			CreatedAt: date("2026-06-01T00:00:00Z"), HTMLURL: "https://x/1"},
		{ID: 2, Path: "a.go", OriginalLine: new(3), Side: "LEFT", Body: "Old", User: "unknown",
			CreatedAt: date("2026-06-02T00:00:00Z"), HTMLURL: "https://x/2", InReplyTo: new(int64(1))},
		{ID: 3, Path: "b.go", Side: "RIGHT", Body: "No side", User: "unknown",
			CreatedAt: date("2026-06-03T00:00:00Z"), HTMLURL: "https://x/3"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PullComments =\n%+v\nwant\n%+v", got, want)
	}
}

const created = `201 {"id": 9, "path": "a.go", "line": 11, "side": "RIGHT", "body": "Hi",
	"user": {"login": "me"}, "created_at": "2026-06-01T00:00:00Z", "html_url": "https://x/9"}`

func TestCreateComment(t *testing.T) {
	c, f := newFake(t, map[string][]string{"/repos/acme/w/pulls/7/comments": {created}})
	got, err := c.CreateComment(context.Background(), "acme", "w", 7,
		NewComment{CommitSHA: "abc", Path: "a.go", Line: 11, Side: "RIGHT", Body: "Hi"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 9 || got.User != "me" {
		t.Errorf("comment = %+v", got)
	}
	req := f.requests[0]
	if req.Method != http.MethodPost || req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("sent %s with Content-Type %q", req.Method, req.Header.Get("Content-Type"))
	}
	if want := `{"commit_id":"abc","path":"a.go","line":11,"side":"RIGHT","body":"Hi"}`; f.bodies[0] != want {
		t.Errorf("body = %s, want %s", f.bodies[0], want)
	}
}

func TestReplyToComment(t *testing.T) {
	c, f := newFake(t, map[string][]string{"/repos/acme/w/pulls/7/comments/1234567890123/replies": {created}})
	if _, err := c.ReplyToComment(context.Background(), "acme", "w", 7, 1234567890123, "Agreed"); err != nil {
		t.Fatal(err)
	}
	if f.requests[0].Method != http.MethodPost || f.bodies[0] != `{"body":"Agreed"}` {
		t.Errorf("sent %s %s", f.requests[0].Method, f.bodies[0])
	}
}

// A POST may have been done when GitHub fails, so only a rate limit, which
// means it wasn't, is retried.
func TestPostRetries(t *testing.T) {
	tests := []struct {
		name     string
		answers  []string
		requests int
		ok       bool
	}{
		{"rate limited, then created", []string{`429 {"message": "slow down"}`, created}, 2, true},
		{"server error isn't retried", []string{`502 bad gateway`, created}, 1, false},
		{"validation error", []string{`422 {"message": "Validation Failed"}`}, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, f := newFake(t, map[string][]string{"/repos/acme/w/pulls/7/comments": tt.answers})
			_, err := c.CreateComment(context.Background(), "acme", "w", 7, NewComment{Path: "a.go", Line: 1, Body: "x"})
			if f.count() != tt.requests || (err == nil) != tt.ok {
				t.Errorf("%d requests, err %v; want %d, ok %v", f.count(), err, tt.requests, tt.ok)
			}
		})
	}
}

func TestLogin(t *testing.T) {
	c, f := newFake(t, map[string][]string{"/user": {`200 {"login": "octocat", "id": 1}`}})
	if login, err := c.Login(context.Background()); err != nil || login != "octocat" || f.requests[0].Header.Get("Authorization") != "Bearer ghp_test" {
		t.Errorf("Login = %q, %v", login, err)
	}
}
