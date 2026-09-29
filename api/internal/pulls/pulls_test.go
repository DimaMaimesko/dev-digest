package pulls_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/github"
	"github.com/DimaMaimesko/dev-digest/api/internal/pgtest"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
	"github.com/DimaMaimesko/dev-digest/api/internal/pulls"
)

// fakeGitHub answers from its fields and counts the pull requests asked for.
type fakeGitHub struct {
	list    []github.Pull
	pulls   map[int]github.Pull
	files   []github.File
	commits []github.Commit
	err     error        // returned by every call
	failFor map[int]bool // pull request numbers Pull fails for

	mu    sync.Mutex
	asked []int // by Pull
}

func (f *fakeGitHub) Pulls(context.Context, string, string) ([]github.Pull, error) {
	return f.list, f.err
}

func (f *fakeGitHub) Pull(_ context.Context, _, _ string, n int) (github.Pull, error) {
	f.mu.Lock()
	f.asked = append(f.asked, n)
	f.mu.Unlock()
	if f.failFor[n] {
		return github.Pull{}, errors.New("GitHub is down")
	}
	return f.pulls[n], f.err
}

func (f *fakeGitHub) PullFiles(context.Context, string, string, int) ([]github.File, error) {
	return f.files, f.err
}

func (f *fakeGitHub) PullCommits(context.Context, string, string, int) ([]github.Commit, error) {
	return f.commits, f.err
}

func date(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// fixture is a database with a repository, acme/w, in a workspace.
type fixture struct {
	db   *pgxpool.Pool
	repo postgres.Repo
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	f := fixture{db: pgtest.New(t)}
	ctx := context.Background()
	var workspace, repo uuid.UUID
	if err := f.db.QueryRow(ctx, `INSERT INTO workspaces (name) VALUES ('default') RETURNING id`).Scan(&workspace); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(ctx, `INSERT INTO repos (workspace_id, owner, name, full_name)
		VALUES ($1, 'acme', 'w', 'acme/w') RETURNING id`, workspace).Scan(&repo); err != nil {
		t.Fatal(err)
	}
	var err error
	f.repo, err = postgres.New(f.db).GetRepo(ctx, postgres.GetRepoParams{WorkspaceID: workspace, ID: repo})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.db.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// insertPull adds pull request number with diff stats, a review of an old
// commit, a description and no opening time, as a TS poll leaves it.
func (f fixture) insertPull(t *testing.T, number int, additions int) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := f.db.QueryRow(context.Background(), `INSERT INTO pull_requests
		(workspace_id, repo_id, number, title, author, branch, base, head_sha, last_reviewed_sha,
		 additions, deletions, files_count, status, body)
		VALUES ($1, $2, $3, 'Old', 'ann', 'feat', 'main', 'old', 'old', $4, $4, $4, 'open', 'Old body')
		RETURNING id`, f.repo.WorkspaceID, f.repo.ID, number, additions).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// pullRow is what a test reads back about a saved pull request.
type pullRow struct {
	Title, Author, Branch, Base, HeadSHA, LastReviewed, Status string
	Additions, Deletions, Files                                int32
	Body                                                       *string
	OpenedAt, UpdatedAt                                        *time.Time
}

func (f fixture) pull(t *testing.T, number int) pullRow {
	t.Helper()
	var p pullRow
	var lastReviewed *string
	err := f.db.QueryRow(context.Background(), `SELECT title, author, branch, base, head_sha, last_reviewed_sha,
		status, additions, deletions, files_count, body, opened_at, updated_at
		FROM pull_requests WHERE repo_id = $1 AND number = $2`, f.repo.ID, number).Scan(
		&p.Title, &p.Author, &p.Branch, &p.Base, &p.HeadSHA, &lastReviewed, &p.Status,
		&p.Additions, &p.Deletions, &p.Files, &p.Body, &p.OpenedAt, &p.UpdatedAt)
	if err != nil {
		t.Fatalf("pull request #%d: %v", number, err)
	}
	if lastReviewed != nil {
		p.LastReviewed = *lastReviewed
	}
	// In UTC, so reflect.DeepEqual compares the instants.
	for _, at := range []*time.Time{p.OpenedAt, p.UpdatedAt} {
		if at != nil {
			*at = at.UTC()
		}
	}
	return p
}

func (f fixture) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSyncList(t *testing.T) {
	f := newFixture(t)
	f.insertPull(t, 1, 5)
	// The same number in another repository isn't touched.
	var other uuid.UUID
	if err := f.db.QueryRow(context.Background(), `INSERT INTO repos (workspace_id, owner, name, full_name)
		VALUES ($1, 'acme', 'x', 'acme/x') RETURNING id`, f.repo.WorkspaceID).Scan(&other); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `INSERT INTO pull_requests (workspace_id, repo_id, number, title, author, branch, base, head_sha)
		VALUES ($1, $2, 1, 'Other', 'x', 'b', 'main', 'x')`, f.repo.WorkspaceID, other)

	gh := &fakeGitHub{list: []github.Pull{
		{Number: 1, Title: "New", Author: "someone", Branch: "other", Base: "dev", HeadSHA: "new", State: "merged",
			CreatedAt: date("2026-06-01T00:00:00Z"), UpdatedAt: date("2026-06-05T00:00:00Z")},
		{Number: 2, Title: "Second", Author: "bob", Branch: "fix", Base: "main", HeadSHA: "abc", State: "open",
			CreatedAt: date("2026-06-03T00:00:00Z"), UpdatedAt: date("2026-06-04T00:00:00Z")},
	}}
	n, err := pulls.NewStore(f.db).SyncList(context.Background(), gh, f.repo)
	if err != nil || n != 2 {
		t.Fatalf("SyncList = %d, %v; want 2", n, err)
	}

	// Title, head, state and times change; author, branches, stats, the
	// description and the review stay, as in TS.
	want1 := pullRow{Title: "New", Author: "ann", Branch: "feat", Base: "main", HeadSHA: "new", LastReviewed: "old",
		Status: "merged", Additions: 5, Deletions: 5, Files: 5, Body: new("Old body"),
		OpenedAt: new(date("2026-06-01T00:00:00Z")), UpdatedAt: new(date("2026-06-05T00:00:00Z"))}
	// A new one has no stats until they're backfilled.
	want2 := pullRow{Title: "Second", Author: "bob", Branch: "fix", Base: "main", HeadSHA: "abc", Status: "open",
		OpenedAt: new(date("2026-06-03T00:00:00Z")), UpdatedAt: new(date("2026-06-04T00:00:00Z"))}
	for number, want := range map[int]pullRow{1: want1, 2: want2} {
		if got := f.pull(t, number); !reflect.DeepEqual(got, want) {
			t.Errorf("#%d =\n%+v\nwant\n%+v", number, got, want)
		}
	}
	if n := f.count(t, `SELECT count(*) FROM pull_requests WHERE repo_id = $1 AND title = 'Other'`, other); n != 1 {
		t.Error("another repository's pull request changed")
	}
}

func TestSyncListGitHubFails(t *testing.T) {
	f := newFixture(t)
	gh := &fakeGitHub{list: []github.Pull{{Number: 1, Title: "T"}}, err: errors.New("GitHub is down")}
	if _, err := pulls.NewStore(f.db).SyncList(context.Background(), gh, f.repo); err == nil {
		t.Fatal("no error")
	}
	if n := f.count(t, `SELECT count(*) FROM pull_requests`); n != 0 {
		t.Errorf("%d pull requests saved", n)
	}
}

func TestBackfillStats(t *testing.T) {
	f := newFixture(t)
	gh := &fakeGitHub{pulls: map[int]github.Pull{}, failFor: map[int]bool{11: true}}
	for number := 1; number <= 12; number++ {
		f.insertPull(t, number, 0)
		gh.pulls[number] = github.Pull{Number: number, Additions: 100 + number, Deletions: 1, ChangedFiles: 2}
	}
	f.insertPull(t, 13, 7) // has stats already

	err := pulls.NewStore(f.db).BackfillStats(context.Background(), gh, f.repo)
	if err == nil || !strings.Contains(err.Error(), "#11") {
		t.Errorf("err = %v, want the failure for #11", err)
	}
	// The 10 newest without stats, in order.
	if want := []int{12, 11, 10, 9, 8, 7, 6, 5, 4, 3}; !reflect.DeepEqual(gh.asked, want) {
		t.Errorf("asked for %v, want %v", gh.asked, want)
	}
	for number := 1; number <= 13; number++ {
		p := f.pull(t, number)
		want := [3]int32{int32(100 + number), 1, 2}
		switch number {
		case 1, 2, 11: // not reached, or failed
			want = [3]int32{}
		case 13:
			want = [3]int32{7, 7, 7}
		}
		if got := [3]int32{p.Additions, p.Deletions, p.Files}; got != want {
			t.Errorf("#%d stats = %v, want %v", number, got, want)
		}
	}
}

func TestRefresh(t *testing.T) {
	f := newFixture(t)
	id := f.insertPull(t, 7, 5)
	f.exec(t, `INSERT INTO pr_files (pr_id, path) VALUES ($1, 'gone.go')`, id)
	f.exec(t, `INSERT INTO pr_commits (pr_id, sha, message, author) VALUES ($1, 'gone', 'm', 'a')`, id)
	gh := &fakeGitHub{
		pulls: map[int]github.Pull{7: {Number: 7, Title: "New", Author: "someone", Branch: "x", Base: "dev",
			HeadSHA: "new", State: "closed", CreatedAt: date("2026-06-01T00:00:00Z"), UpdatedAt: date("2026-06-05T00:00:00Z"),
			Body: nil, Additions: 247, Deletions: 38, ChangedFiles: 2}},
		files: []github.File{{Path: "a.go", Additions: 4, Deletions: 1, Patch: new("@@ -1 +1 @@")}, {Path: "logo.png"}},
		commits: []github.Commit{
			{SHA: "c1", Message: "One", Author: "Ann", CommittedAt: new(date("2026-06-01T10:00:00Z"))},
			{SHA: "c2", Message: "Two", Author: "unknown"},
		},
	}
	if err := pulls.NewStore(f.db).Refresh(context.Background(), gh, f.repo, 7); err != nil {
		t.Fatal(err)
	}

	want := pullRow{Title: "New", Author: "ann", Branch: "feat", Base: "main", HeadSHA: "new", LastReviewed: "old",
		Status: "closed", Additions: 247, Deletions: 38, Files: 2, Body: nil,
		OpenedAt: new(date("2026-06-01T00:00:00Z")), UpdatedAt: new(date("2026-06-05T00:00:00Z"))}
	if got := f.pull(t, 7); !reflect.DeepEqual(got, want) {
		t.Errorf("pull request =\n%+v\nwant\n%+v", got, want)
	}
	if got := f.rows(t, `SELECT path || ' ' || additions || ' ' || deletions || ' ' || coalesce(patch, 'NULL')
		FROM pr_files WHERE pr_id = $1 ORDER BY path`, id); !reflect.DeepEqual(got, []string{"a.go 4 1 @@ -1 +1 @@", "logo.png 0 0 NULL"}) {
		t.Errorf("files = %q", got)
	}
	if got := f.rows(t, `SELECT sha || ' ' || message || ' ' || author || ' ' || coalesce(committed_at::text, 'NULL')
		FROM pr_commits WHERE pr_id = $1 ORDER BY sha`, id); !reflect.DeepEqual(got, []string{"c1 One Ann 2026-06-01 10:00:00+00", "c2 Two unknown NULL"}) {
		t.Errorf("commits = %q", got)
	}
}

// The TS server left duplicate files and commits when two refreshes of the
// same pull request overlapped.
func TestRefreshConcurrently(t *testing.T) {
	f := newFixture(t)
	id := f.insertPull(t, 7, 0)
	gh := &fakeGitHub{
		pulls:   map[int]github.Pull{7: {Number: 7, Title: "T", HeadSHA: "h", State: "open"}},
		files:   []github.File{{Path: "a.go"}, {Path: "b.go"}},
		commits: []github.Commit{{SHA: "c1"}},
	}
	store := pulls.NewStore(f.db)
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for range 10 {
		wg.Go(func() { errs <- store.Refresh(context.Background(), gh, f.repo, 7) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := f.count(t, `SELECT count(*) FROM pr_files WHERE pr_id = $1`, id); n != 2 {
		t.Errorf("%d files, want 2", n)
	}
	if n := f.count(t, `SELECT count(*) FROM pr_commits WHERE pr_id = $1`, id); n != 1 {
		t.Errorf("%d commits, want 1", n)
	}
}

func TestRefreshFails(t *testing.T) {
	f := newFixture(t)
	id := f.insertPull(t, 7, 5)
	f.exec(t, `INSERT INTO pr_files (pr_id, path) VALUES ($1, 'kept.go')`, id)
	store := pulls.NewStore(f.db)

	down := &fakeGitHub{err: errors.New("GitHub is down")}
	if err := store.Refresh(context.Background(), down, f.repo, 7); err == nil {
		t.Error("GitHub down: no error")
	}
	if p := f.pull(t, 7); p.Title != "Old" || f.count(t, `SELECT count(*) FROM pr_files WHERE pr_id = $1`, id) != 1 {
		t.Error("GitHub down: the saved pull request changed")
	}

	gh := &fakeGitHub{pulls: map[int]github.Pull{8: {Number: 8}}}
	if err := store.Refresh(context.Background(), gh, f.repo, 8); err == nil {
		t.Error("unknown pull request: no error")
	}
}

func (f fixture) rows(t *testing.T, sql string, args ...any) []string {
	t.Helper()
	rows, err := f.db.Query(context.Background(), sql, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}
