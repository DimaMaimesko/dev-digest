package httpapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// fakeGitHub serves GitHub's API for repository o/n: pull request 7, which
// is also listed, with a review comment, and pull request 8, which is new.
// Every answer is status when that is set.
type fakeGitHub struct {
	status int

	mu       sync.Mutex
	requests []string // "/path", or "POST /path body" for a POST
}

var githubAnswers = map[string]string{
	"/repos/o/n/pulls": `[
		{"number": 7, "title": "Rate limits", "state": "open", "user": {"login": "ann"},
		 "head": {"ref": "feat", "sha": "new"}, "base": {"ref": "main"}, "merged_at": null,
		 "created_at": "2026-09-01T08:00:00Z", "updated_at": "2026-09-03T00:00:00Z"},
		{"number": 8, "title": "New one", "state": "closed", "user": {"login": "bob"},
		 "head": {"ref": "fix", "sha": "abc"}, "base": {"ref": "main"}, "merged_at": "2026-09-04T00:00:00Z",
		 "created_at": "2026-09-02T00:00:00Z", "updated_at": "2026-09-04T00:00:00Z"}]`,
	"/repos/o/n/pulls/7": `{"number": 7, "title": "Rate limits", "state": "open", "body": "Adds limits.",
		"user": {"login": "ann"}, "head": {"ref": "feat", "sha": "new"}, "base": {"ref": "main"},
		"merged_at": null, "created_at": "2026-09-01T08:00:00Z", "updated_at": "2026-09-03T00:00:00Z",
		"additions": 40, "deletions": 4, "changed_files": 1}`,
	"/repos/o/n/pulls/8":       `{"number": 8, "state": "closed", "additions": 3, "deletions": 1, "changed_files": 2}`,
	"/repos/o/n/pulls/7/files": `[{"filename": "limit.go", "additions": 40, "deletions": 4, "patch": "@@ -1 +1 @@"}]`,
	"/repos/o/n/pulls/7/commits": `[{"sha": "new", "commit": {"message": "Add limits",
		"author": {"name": "Ann", "date": "2026-09-03T00:00:00Z"}}, "author": {"login": "ann"}}]`,
	"/repos/o/n/pulls/7/comments": `[
		{"id": 1, "path": "limit.go", "line": 11, "original_line": 11, "side": "RIGHT", "body": "Why?",
		 "user": {"login": "bob"}, "created_at": "2026-09-03T10:00:00Z", "html_url": "https://github.com/o/n/pull/7#discussion_r1"},
		{"id": 2, "path": "limit.go", "line": null, "original_line": 4, "side": "LEFT", "body": "Old",
		 "user": null, "created_at": "2026-09-03T11:00:00Z", "html_url": "https://github.com/o/n/pull/7#discussion_r2",
		 "in_reply_to_id": 1}]`,
	"/user":                            `{"login": "octocat"}`,
	"POST /repos/o/n/pulls/7/comments": postedComment,
	"POST /repos/o/n/pulls/7/comments/1234567890123/replies": postedComment,
}

const postedComment = `{"id": 3, "path": "limit.go", "line": 12, "original_line": 12, "side": "RIGHT",
	"body": "Use a constant.", "user": {"login": "me"}, "created_at": "2026-09-04T00:00:00Z",
	"html_url": "https://github.com/o/n/pull/7#discussion_r3"}`

func (g *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Path
	if r.Method != http.MethodGet {
		key = r.Method + " " + key
	}
	sent, _ := io.ReadAll(r.Body)
	g.mu.Lock()
	g.requests = append(g.requests, strings.TrimSpace(key+" "+string(sent)))
	g.mu.Unlock()
	if g.status != 0 {
		message := "Bad credentials"
		if g.status == http.StatusUnprocessableEntity {
			message = "Validation Failed"
		}
		http.Error(w, `{"message": "`+message+`"}`, g.status)
		return
	}
	body, ok := githubAnswers[key]
	if !ok {
		http.Error(w, `{"message": "Not Found"}`, http.StatusNotFound)
		return
	}
	w.Write([]byte(body))
}

func (g *fakeGitHub) calls() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return strings.Join(g.requests, " ")
}

// withGitHub points the fixture at a fake GitHub and sets a token, unless
// token is empty.
func withGitHub(t *testing.T, f fixture, token string, status int) (fixture, *fakeGitHub) {
	t.Helper()
	gh := &fakeGitHub{status: status}
	srv := httptest.NewServer(gh)
	t.Cleanup(srv.Close)
	f.githubAPI = srv.URL
	f.env["GITHUB_TOKEN"] = token
	return f, gh
}

func TestListPullsSyncsFromGitHub(t *testing.T) {
	f, gh := withGitHub(t, newFixture(t), "ghp_x", 0)
	repo := f.insertRepo(t, f.workspace, "o/n")
	seven := f.insertPull(t, repo, 7, "open", "'head'", "NULL")
	f.exec(t, `UPDATE pull_requests SET additions = 0, deletions = 0, files_count = 0 WHERE id = $1`, seven)

	var list []map[string]any
	decode(t, f.get(t, "/repos/"+repo.String()+"/pulls"), http.StatusOK, &list)
	if len(list) != 2 {
		t.Fatalf("%d pull requests, want 2: %v", len(list), list)
	}
	// Newest number first. #8 is new, merged, and got its stats; #7 has a new
	// title and head, so its review is out of date, and got its stats.
	want := []map[string]any{
		{"number": 8.0, "title": "New one", "author": "bob", "head_sha": "abc", "status": "merged",
			"additions": 3.0, "deletions": 1.0, "files_count": 2.0,
			"opened_at": "2026-09-02T00:00:00.000Z", "updated_at": "2026-09-04T00:00:00.000Z"},
		{"number": 7.0, "title": "Rate limits", "author": "ann", "head_sha": "new", "status": "needs_review",
			"additions": 40.0, "deletions": 4.0, "files_count": 1.0,
			"opened_at": "2026-09-01T08:00:00.000Z", "updated_at": "2026-09-03T00:00:00.000Z"},
	}
	for i, fields := range want {
		for k, v := range fields {
			if list[i][k] != v {
				t.Errorf("pull request %d: %s = %v, want %v", i, k, list[i][k], v)
			}
		}
	}
	if calls := gh.calls(); calls != "/repos/o/n/pulls /repos/o/n/pulls/8 /repos/o/n/pulls/7" {
		t.Errorf("GitHub calls: %s", calls)
	}
}

// Without a token, or when GitHub fails, the saved pull requests are served.
func TestListPullsWithoutGitHub(t *testing.T) {
	tests := []struct {
		name   string
		token  string
		status int
		calls  bool
	}{
		{"no token", "", 0, false},
		{"bad token", "ghp_bad", http.StatusUnauthorized, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, gh := withGitHub(t, newFixture(t), tt.token, tt.status)
			repo := f.insertRepo(t, f.workspace, "o/n")
			f.insertPull(t, repo, 7, "open", "NULL", "NULL")

			var list []struct{ Title string }
			decode(t, f.get(t, "/repos/"+repo.String()+"/pulls"), http.StatusOK, &list)
			if len(list) != 1 || list[0].Title != "PR 7" {
				t.Errorf("list = %+v, want the saved PR 7", list)
			}
			if called := gh.calls() != ""; called != tt.calls {
				t.Errorf("GitHub called: %v, want %v", called, tt.calls)
			}
		})
	}
}

func TestGetPullRefreshesFromGitHub(t *testing.T) {
	f, _ := withGitHub(t, newFixture(t), "ghp_x", 0)
	pull := f.insertPull(t, f.insertRepo(t, f.workspace, "o/n"), 7, "open", "NULL", "NULL")
	f.exec(t, `INSERT INTO pr_files (pr_id, path) VALUES ($1, 'old.go')`, pull)

	// The answer is what was saved: GitHub's detail, in JavaScript's time
	// format, without linked_issue.
	assertJSON(t, f.get(t, "/pulls/"+pull.String()), http.StatusOK, `{
		"id": "`+pull.String()+`", "number": 7, "title": "Rate limits", "author": "ann",
		"branch": "feat", "base": "main", "head_sha": "new",
		"additions": 40, "deletions": 4, "files_count": 1, "status": "open",
		"opened_at": "2026-09-01T08:00:00.000Z", "updated_at": "2026-09-03T00:00:00.000Z",
		"body": "Adds limits.",
		"files": [{"path": "limit.go", "additions": 40, "deletions": 4, "patch": "@@ -1 +1 @@"}],
		"commits": [{"sha": "new", "message": "Add limits", "author": "Ann", "committed_at": "2026-09-03T00:00:00.000Z"}]
	}`)
}

func TestGetPullGitHubFails(t *testing.T) {
	f, gh := withGitHub(t, newFixture(t), "ghp_bad", http.StatusUnauthorized)
	pull := f.insertPull(t, f.insertRepo(t, f.workspace, "o/n"), 7, "open", "NULL", "NULL")
	f.exec(t, `INSERT INTO pr_files (pr_id, path) VALUES ($1, 'saved.go')`, pull)

	var got struct {
		Title string
		Files []struct{ Path string }
	}
	decode(t, f.get(t, "/pulls/"+pull.String()), http.StatusOK, &got)
	if got.Title != "PR 7" || len(got.Files) != 1 || got.Files[0].Path != "saved.go" {
		t.Errorf("got %+v, want the saved pull request", got)
	}
	if gh.calls() == "" {
		t.Error("GitHub wasn't asked")
	}
}

func TestPollRepo(t *testing.T) {
	tests := []struct {
		name   string
		token  string
		status int
		code   int
		want   string
	}{
		{"synced", "ghp_x", 0, http.StatusOK, `{"synced": 2, "reviewTriggered": false}`},
		{"no token", "", 0, http.StatusInternalServerError,
			`{"error": {"code": "config_error", "message": "GITHUB_TOKEN is not configured"}}`},
		{"GitHub fails", "ghp_bad", http.StatusUnauthorized, http.StatusBadGateway,
			`{"error": {"code": "github_error", "message": "GitHub returned 401 Unauthorized: Bad credentials"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := withGitHub(t, newFixture(t), tt.token, tt.status)
			repo := f.insertRepo(t, f.workspace, "o/n")
			assertJSON(t, f.send(t, http.MethodPost, "/repos/"+repo.String()+"/poll", ``), tt.code, tt.want)

			polled := f.count(t, `SELECT count(*) FROM repos WHERE id = $1 AND last_polled_at > now() - interval '1 minute'`, repo)
			pulls := f.count(t, `SELECT count(*) FROM pull_requests WHERE repo_id = $1 AND opened_at IS NOT NULL`, repo)
			if ok := tt.code == http.StatusOK; (polled == 1) != ok || (pulls == 2) != ok {
				t.Errorf("last_polled_at set: %v, pull requests with opened_at: %d", polled == 1, pulls)
			}
		})
	}
}

func TestPollRepoNotFound(t *testing.T) {
	f, gh := withGitHub(t, newFixture(t), "ghp_x", 0)
	other := f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`)
	for _, id := range []string{uuid.NewString(), f.insertRepo(t, other, "o/n").String()} {
		assertJSON(t, f.send(t, http.MethodPost, "/repos/"+id+"/poll", ``), http.StatusNotFound,
			`{"error": {"code": "not_found", "message": "Repo not found"}}`)
	}
	if paths := issuePaths(t, f.send(t, http.MethodPost, "/repos/42/poll", ``)); len(paths) != 1 || paths[0] != "id" {
		t.Errorf("issues at %v", paths)
	}
	if gh.calls() != "" {
		t.Errorf("GitHub called: %s", gh.calls())
	}
}
