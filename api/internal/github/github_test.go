package github

import (
	"context"
	"errors"
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
