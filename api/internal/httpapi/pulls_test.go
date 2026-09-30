package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func (f fixture) insertRepo(t *testing.T, workspace uuid.UUID, fullName string) uuid.UUID {
	t.Helper()
	return f.insertID(t, `INSERT INTO repos (workspace_id, owner, name, full_name)
		VALUES ($1, 'o', 'n', $2) RETURNING id`, workspace, fullName)
}

// insertPull adds a pull request with head commit "head" and the given GitHub
// state, last reviewed commit and last update (SQL expressions).
func (f fixture) insertPull(t *testing.T, repo uuid.UUID, number int, state, lastReviewed, updatedAt string) uuid.UUID {
	t.Helper()
	return f.insertID(t, `INSERT INTO pull_requests
		(workspace_id, repo_id, number, title, author, branch, base, head_sha, last_reviewed_sha,
		 additions, deletions, files_count, status, opened_at, updated_at)
		VALUES ((SELECT workspace_id FROM repos WHERE id = $1), $1, $2, $3, 'ann', 'feat', 'main', 'head', `+
		lastReviewed+`, 10, 2, 3, $4, '2026-09-01 08:00:00+00', `+updatedAt+`) RETURNING id`,
		repo, number, fmt.Sprintf("PR %d", number), state)
}

func (f fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.db.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func TestListPulls(t *testing.T) {
	f := newFixture(t)
	repo := f.insertRepo(t, f.workspace, "o/n")
	never := f.insertPull(t, repo, 1, "open", "NULL", "now()")
	reviewed := f.insertPull(t, repo, 2, "open", "'head'", "now()")
	stale := f.insertPull(t, repo, 3, "open", "'head'", "now() - interval '30 days'")
	newCommits := f.insertPull(t, repo, 4, "open", "'older'", "now()")
	merged := f.insertPull(t, repo, 5, "merged", "NULL", "NULL")

	// Two reviews of PR 2: the list shows the latest one's score. A newer
	// summary isn't a review and doesn't count.
	for _, r := range []struct {
		kind  string
		score int
		at    string
	}{{"review", 40, "2026-09-10"}, {"review", 85, "2026-09-20"}, {"summary", 10, "2026-09-25"}} {
		f.exec(t, `INSERT INTO reviews (workspace_id, pr_id, kind, score, created_at) VALUES ($1, $2, $3, $4, $5)`,
			f.workspace, reviewed, r.kind, r.score, r.at)
	}
	// Another repository's pull request isn't listed.
	f.insertPull(t, f.insertRepo(t, f.workspace, "o/other"), 1, "open", "NULL", "now()")

	res := f.get(t, "/repos/"+repo.String()+"/pulls")

	var got []struct {
		ID     string `json:"id"`
		Number int    `json:"number"`
		Status string `json:"status"`
		Score  *int   `json:"score"`
	}
	decode(t, res, http.StatusOK, &got)
	want := []struct {
		id     uuid.UUID
		status string
		score  *int
	}{
		{merged, "merged", nil},
		{newCommits, "needs_review", nil},
		{stale, "stale", nil},
		{reviewed, "reviewed", ptr(85)},
		{never, "needs_review", nil},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d pull requests, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.ID != w.id.String() || g.Status != w.status || !equalPtr(g.Score, w.score) {
			t.Errorf("pull %d = {#%d %s score %v}, want {%s score %v}", i, g.Number, g.Status, deref(g.Score), w.status, deref(w.score))
		}
	}
}

func TestListPullsJSON(t *testing.T) {
	f := newFixture(t)
	repo := f.insertRepo(t, f.workspace, "o/n")
	pull := f.insertPull(t, repo, 7, "open", "NULL", "'2026-09-02 09:30:00.123456+00'")

	assertJSON(t, f.get(t, "/repos/"+repo.String()+"/pulls"), http.StatusOK, `[{
		"id": "`+pull.String()+`", "number": 7, "title": "PR 7", "author": "ann",
		"branch": "feat", "base": "main", "head_sha": "head",
		"additions": 10, "deletions": 2, "files_count": 3, "status": "needs_review",
		"opened_at": "2026-09-01T08:00:00.000Z", "updated_at": "2026-09-02T09:30:00.123Z",
		"score": null, "cost_usd": null
	}]`)
}

// A pull request's cost is the sum of its runs' known costs, whatever their
// status; it is null when no run's cost is known.
func TestListPullsCost(t *testing.T) {
	f := newFixture(t)
	repo := f.insertRepo(t, f.workspace, "o/n")
	priced := f.insertPull(t, repo, 1, "open", "NULL", "now()")
	unpriced := f.insertPull(t, repo, 2, "open", "NULL", "now()")
	f.insertPull(t, repo, 3, "open", "NULL", "now()") // never run
	free := f.insertPull(t, repo, 4, "open", "NULL", "now()")
	for _, r := range []struct {
		pull   uuid.UUID
		status string
		cost   string
	}{
		{priced, "done", "0.25"}, {priced, "failed", "0.5"}, {priced, "cancelled", "NULL"},
		{unpriced, "done", "NULL"},
		{free, "done", "0"},
	} {
		f.exec(t, `INSERT INTO agent_runs (workspace_id, pr_id, status, cost_usd) VALUES ($1, $2, $3, `+r.cost+`)`,
			f.workspace, r.pull, r.status)
	}

	var got []struct {
		Number  int      `json:"number"`
		CostUSD *float64 `json:"cost_usd"`
	}
	decode(t, f.get(t, "/repos/"+repo.String()+"/pulls"), http.StatusOK, &got)
	want := map[int]*float64{1: new(0.75), 2: nil, 3: nil, 4: new(0.0)}
	for _, g := range got {
		w := want[g.Number]
		if (g.CostUSD == nil) != (w == nil) || (w != nil && *g.CostUSD != *w) {
			t.Errorf("pull %d cost = %v, want %v", g.Number, costOf(g.CostUSD), costOf(w))
		}
	}
}

func TestGetPull(t *testing.T) {
	f := newFixture(t)
	repo := f.insertRepo(t, f.workspace, "o/n")
	pull := f.insertPull(t, repo, 7, "open", "NULL", "NULL")
	f.exec(t, `UPDATE pull_requests SET body = 'Adds rate limiting.' WHERE id = $1`, pull)
	f.exec(t, `INSERT INTO pr_files (pr_id, path, additions, deletions, patch) VALUES
		($1, 'src/z.ts', 1, 0, '@@ -1 +1 @@'), ($1, 'src/a.ts', 5, 1, NULL)`, pull)
	f.exec(t, `INSERT INTO pr_commits (pr_id, sha, message, author, committed_at) VALUES
		($1, 'ccc', 'no date', 'ann', NULL),
		($1, 'bbb', 'second', 'ann', '2026-09-02 10:00:00+00'),
		($1, 'aaa', 'first', 'ann', '2026-09-01 10:00:00+00')`, pull)

	assertJSON(t, f.get(t, "/pulls/"+pull.String()), http.StatusOK, `{
		"id": "`+pull.String()+`", "number": 7, "title": "PR 7", "author": "ann",
		"branch": "feat", "base": "main", "head_sha": "head",
		"additions": 10, "deletions": 2, "files_count": 3, "status": "open",
		"opened_at": "2026-09-01T08:00:00.000Z", "updated_at": null,
		"body": "Adds rate limiting.",
		"files": [
			{"path": "src/a.ts", "additions": 5, "deletions": 1, "patch": null},
			{"path": "src/z.ts", "additions": 1, "deletions": 0, "patch": "@@ -1 +1 @@"}
		],
		"commits": [
			{"sha": "aaa", "message": "first", "author": "ann", "committed_at": "2026-09-01T10:00:00.000Z"},
			{"sha": "bbb", "message": "second", "author": "ann", "committed_at": "2026-09-02T10:00:00.000Z"},
			{"sha": "ccc", "message": "no date", "author": "ann", "committed_at": null}
		]
	}`)
}

func TestGetPullWithoutFilesOrCommits(t *testing.T) {
	f := newFixture(t)
	pull := f.insertPull(t, f.insertRepo(t, f.workspace, "o/n"), 1, "open", "NULL", "NULL")

	var got struct {
		Body    *string `json:"body"`
		Files   []any   `json:"files"`
		Commits []any   `json:"commits"`
	}
	decode(t, f.get(t, "/pulls/"+pull.String()), http.StatusOK, &got)
	if got.Body != nil || got.Files == nil || got.Commits == nil {
		t.Errorf("want body null and files and commits [], got %+v", got)
	}
}

func TestPullsNotFound(t *testing.T) {
	f := newFixture(t)
	// Rows of another workspace look missing.
	other := f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`)
	otherRepo := f.insertRepo(t, other, "x/y")
	otherPull := f.insertPull(t, otherRepo, 1, "open", "NULL", "NULL")
	missing := uuid.New().String()

	tests := []struct{ path, want string }{
		{"/repos/" + missing + "/pulls", "Repo not found"},
		{"/repos/" + otherRepo.String() + "/pulls", "Repo not found"},
		{"/pulls/" + missing, "Pull request not found"},
		{"/pulls/" + otherPull.String(), "Pull request not found"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assertJSON(t, f.get(t, tt.path), http.StatusNotFound, `{"error": {"code": "not_found", "message": "`+tt.want+`"}}`)
		})
	}
}

func TestPullsInvalidID(t *testing.T) {
	f := newFixture(t)
	id := uuid.New()
	for _, bad := range []string{
		"not-a-uuid",
		"urn:uuid:" + id.String(), // uuid.Parse accepts these two forms;
		id.String()[:8] + id.String()[9:13] + id.String()[14:18] + id.String()[19:23] + id.String()[24:], // the TS server doesn't
	} {
		for _, path := range []string{"/pulls/" + bad, "/repos/" + bad + "/pulls"} {
			t.Run(path, func(t *testing.T) {
				assertJSON(t, f.get(t, path), http.StatusUnprocessableEntity, `{"error": {
					"code": "validation_error", "message": "Request validation failed",
					"details": [{"path": ["id"], "message": "Invalid uuid"}]}}`)
			})
		}
	}
}

func ptr(n int) *int { return &n }

// costOf shows a cost in a test message: its value, or nil.
func costOf(c *float64) any {
	if c == nil {
		return nil
	}
	return *c
}

func equalPtr(a, b *int) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
