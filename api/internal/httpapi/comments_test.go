package httpapi_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestListComments(t *testing.T) {
	f, _ := withGitHub(t, newFixture(t), "ghp_x", 0)
	pull := f.insertPull(t, f.insertRepo(t, f.workspace, "o/n"), 7, "open", "NULL", "NULL")

	assertJSON(t, f.get(t, "/pulls/"+pull.String()+"/comments"), http.StatusOK, `[
		{"id": 1, "path": "limit.go", "line": 11, "original_line": 11, "side": "RIGHT", "body": "Why?",
		 "user": "bob", "created_at": "2026-09-03T10:00:00.000Z",
		 "html_url": "https://github.com/o/n/pull/7#discussion_r1", "in_reply_to_id": null, "is_outdated": false},
		{"id": 2, "path": "limit.go", "line": null, "original_line": 4, "side": "LEFT", "body": "Old",
		 "user": "unknown", "created_at": "2026-09-03T11:00:00.000Z",
		 "html_url": "https://github.com/o/n/pull/7#discussion_r2", "in_reply_to_id": 1, "is_outdated": true}
	]`)
}

// Without a token, or when GitHub fails, there are no comments to show.
func TestListCommentsWithoutGitHub(t *testing.T) {
	for _, tt := range []struct {
		name, token string
		status      int
	}{{"no token", "", 0}, {"bad token", "ghp_bad", http.StatusUnauthorized}} {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := withGitHub(t, newFixture(t), tt.token, tt.status)
			pull := f.insertPull(t, f.insertRepo(t, f.workspace, "o/n"), 7, "open", "NULL", "NULL")
			assertJSON(t, f.get(t, "/pulls/"+pull.String()+"/comments"), http.StatusOK, `[]`)
		})
	}
}

func TestCreateComment(t *testing.T) {
	answer := `{"id": 3, "path": "limit.go", "line": 12, "original_line": 12, "side": "RIGHT",
		"body": "Use a constant.", "user": "me", "created_at": "2026-09-04T00:00:00.000Z",
		"html_url": "https://github.com/o/n/pull/7#discussion_r3", "in_reply_to_id": null, "is_outdated": false}`
	tests := []struct {
		name, body, sent string
	}{
		{"on the head commit, the new side by default",
			`{"path": "limit.go", "line": 12, "body": "Use a constant.", "extra": 1}`,
			`POST /repos/o/n/pulls/7/comments {"commit_id":"head","path":"limit.go","line":12,"side":"RIGHT","body":"Use a constant."}`},
		{"on the old side",
			`{"path": "limit.go", "line": 12, "side": "LEFT", "body": "Use a constant."}`,
			`POST /repos/o/n/pulls/7/comments {"commit_id":"head","path":"limit.go","line":12,"side":"LEFT","body":"Use a constant."}`},
		{"a reply, to an ID beyond 32 bits",
			`{"path": "limit.go", "line": 12, "body": "Use a constant.", "in_reply_to": 1234567890123}`,
			`POST /repos/o/n/pulls/7/comments/1234567890123/replies {"body":"Use a constant."}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, gh := withGitHub(t, newFixture(t), "ghp_x", 0)
			pull := f.insertPull(t, f.insertRepo(t, f.workspace, "o/n"), 7, "open", "NULL", "NULL")
			assertJSON(t, f.send(t, http.MethodPost, "/pulls/"+pull.String()+"/comments", tt.body), http.StatusOK, answer)
			if gh.calls() != tt.sent {
				t.Errorf("sent to GitHub:\n%s\nwant\n%s", gh.calls(), tt.sent)
			}
		})
	}
}

func TestCreateCommentInvalid(t *testing.T) {
	f, gh := withGitHub(t, newFixture(t), "ghp_x", 0)
	pull := f.insertPull(t, f.insertRepo(t, f.workspace, "o/n"), 7, "open", "NULL", "NULL")
	tests := []struct {
		body string
		want []string
	}{
		{`{}`, []string{"path", "line", "body"}},
		{`{"path": "", "line": 0, "body": ""}`, []string{"path", "line", "body"}},
		{`{"path": "a", "line": 1.5, "body": "b"}`, []string{"line"}},
		{`{"path": "a", "line": -3, "body": "b", "side": "UP"}`, []string{"line", "side"}},
		{`{"path": "a", "line": 1, "body": "b", "in_reply_to": "x"}`, []string{"in_reply_to"}},
		{`{"path": "a", "line": 1, "body": "b", "in_reply_to": 1e300}`, []string{"in_reply_to"}},
	}
	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			if got := issuePaths(t, f.send(t, http.MethodPost, "/pulls/"+pull.String()+"/comments", tt.body)); !slices.Equal(got, tt.want) {
				t.Errorf("issues at %v, want %v", got, tt.want)
			}
		})
	}
	if gh.calls() != "" {
		t.Errorf("GitHub called: %s", gh.calls())
	}
}

func TestCreateCommentFails(t *testing.T) {
	valid := `{"path": "limit.go", "line": 12, "body": "x"}`
	tests := []struct {
		name, token string
		status      int
		code        int
		want        string
	}{
		{"no token", "", 0, http.StatusBadRequest,
			`{"error": {"code": "github_unavailable", "message": "Connect a GitHub token to post comments."}}`},
		{"GitHub refuses", "ghp_x", http.StatusUnprocessableEntity, http.StatusBadRequest,
			`{"error": {"code": "github_comment_failed", "message": "GitHub returned 422 Unprocessable Entity: Validation Failed"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := withGitHub(t, newFixture(t), tt.token, tt.status)
			pull := f.insertPull(t, f.insertRepo(t, f.workspace, "o/n"), 7, "open", "NULL", "NULL")
			assertJSON(t, f.send(t, http.MethodPost, "/pulls/"+pull.String()+"/comments", valid), tt.code, tt.want)
		})
	}
}

func TestCommentsNotFound(t *testing.T) {
	f, gh := withGitHub(t, newFixture(t), "ghp_x", 0)
	other := f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`)
	otherPull := f.insertPull(t, f.insertRepo(t, other, "o/n"), 7, "open", "NULL", "NULL")
	notFound := `{"error": {"code": "not_found", "message": "Pull request not found"}}`
	for _, id := range []string{uuid.NewString(), otherPull.String()} {
		assertJSON(t, f.get(t, "/pulls/"+id+"/comments"), http.StatusNotFound, notFound)
		assertJSON(t, f.send(t, http.MethodPost, "/pulls/"+id+"/comments", `{"path": "a", "line": 1, "body": "b"}`),
			http.StatusNotFound, notFound)
	}
	// The body is checked first, as in TS, where Fastify validates it
	// before the handler runs.
	if paths := issuePaths(t, f.send(t, http.MethodPost, "/pulls/"+uuid.NewString()+"/comments", `{}`)); len(paths) != 3 {
		t.Errorf("missing pull request, empty body: issues at %v", paths)
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		if paths := issuePaths(t, f.send(t, method, "/pulls/42/comments", `{}`)); !slices.Equal(paths, []string{"id"}) {
			t.Errorf("%s: issues at %v", method, paths)
		}
	}
	if gh.calls() != "" {
		t.Errorf("GitHub called: %s", gh.calls())
	}
}
