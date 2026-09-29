package repos_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/jobs"
	"github.com/DimaMaimesko/dev-digest/api/internal/pgtest"
	"github.com/DimaMaimesko/dev-digest/api/internal/repointel"
	"github.com/DimaMaimesko/dev-digest/api/internal/repos"
)

func TestParseURL(t *testing.T) {
	tests := []struct {
		url   string
		want  repos.Ref
		valid bool
	}{
		{"https://github.com/acme/widgets", repos.Ref{Owner: "acme", Name: "widgets"}, true},
		{"https://github.com/acme/widgets.git", repos.Ref{Owner: "acme", Name: "widgets"}, true},
		{"https://github.com/acme/widgets/", repos.Ref{Owner: "acme", Name: "widgets"}, true},
		{"http://www.github.com/acme/widgets", repos.Ref{Owner: "acme", Name: "widgets"}, true},
		// Dots in names, which the TS server refused.
		{"https://github.com/vercel/next.js", repos.Ref{Owner: "vercel", Name: "next.js"}, true},
		{"https://github.com/socketio/socket.io.git", repos.Ref{Owner: "socketio", Name: "socket.io"}, true},
		{"https://github.com/acme/.github", repos.Ref{Owner: "acme", Name: ".github"}, true},
		// A path out of the clone directory, which the TS server accepted,
		// and deleted what was there.
		{"https://github.com/../victim", repos.Ref{}, false},
		{"https://github.com/acme/..", repos.Ref{}, false},
		{"https://github.com/acme/.", repos.Ref{}, false},
		{"https://github.com/-acme/widgets", repos.Ref{}, false},
		{"https://github.com/acme/wid gets", repos.Ref{}, false},
		{"https://github.com/acme", repos.Ref{}, false},
		{"https://github.com/acme/widgets/tree/main", repos.Ref{}, false},
		{"https://gitlab.com/acme/widgets", repos.Ref{}, false},
		{"https://evil.com/github.com/acme/widgets", repos.Ref{}, false},
		{"git@github.com:acme/widgets.git", repos.Ref{}, false},
		{"ftp://github.com/acme/widgets", repos.Ref{}, false},
	}
	for _, tt := range tests {
		got, err := repos.ParseURL(tt.url)
		if tt.valid && (err != nil || got != tt.want) || !tt.valid && !errors.Is(err, repos.ErrBadURL) {
			t.Errorf("ParseURL(%q) = %+v, %v", tt.url, got, err)
		}
	}
}

type fixture struct {
	db        *pgxpool.Pool
	workspace uuid.UUID
	user      uuid.UUID
	jobs      *jobs.Runner
	store     *repos.Store
	cloneDir  string
}

// newFixture clones from remotes, a directory with a repository acme/widgets.
func newFixture(t *testing.T) fixture {
	t.Helper()
	f := fixture{db: pgtest.New(t), cloneDir: t.TempDir()}
	ctx := context.Background()
	f.db.QueryRow(ctx, `INSERT INTO workspaces (name) VALUES ('w') RETURNING id`).Scan(&f.workspace)
	f.db.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ('you@local', 'You') RETURNING id`).Scan(&f.user)

	remotes := t.TempDir()
	src := filepath.Join(remotes, "acme", "widgets.git")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "a.ts"), []byte("export function a() {}\n"), 0o644)
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "."}, {"commit", "-q", "-m", "first"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = src
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	f.jobs = jobs.New(f.db, slog.New(slog.DiscardHandler))
	t.Cleanup(f.jobs.Close)
	token := func() (string, error) { return "", nil }
	f.store = repos.NewStore(repos.Config{
		DB: f.db, Jobs: f.jobs, CloneDir: f.cloneDir, Token: token,
		Remote:  "file://" + remotes + "/",
		Indexer: repointel.NewIndexer(f.db, token),
	})
	return f
}

func (f fixture) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAdd(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ref := repos.Ref{Owner: "acme", Name: "widgets"}
	repo, created, err := f.store.Add(ctx, f.workspace, f.user, ref)
	if err != nil || !created || repo.FullName != "acme/widgets" || repo.CreatedBy == nil || *repo.CreatedBy != f.user {
		t.Fatalf("Add = %+v, %v, %v", repo, created, err)
	}
	f.jobs.Wait()

	dir := filepath.Join(f.cloneDir, "acme", "widgets")
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Errorf("not cloned: %v", err)
	}
	var clonePath *string
	var polled bool
	f.db.QueryRow(ctx, `SELECT clone_path, last_polled_at IS NOT NULL FROM repos WHERE id = $1`, repo.ID).Scan(&clonePath, &polled)
	if clonePath == nil || *clonePath != dir || !polled {
		t.Errorf("clone_path %v, last_polled_at set %v", clonePath, polled)
	}

	// Again: the same repository, not cloned again.
	again, created, err := f.store.Add(ctx, f.workspace, f.user, ref)
	if err != nil || created || again.ID != repo.ID {
		t.Errorf("second Add = %+v, %v, %v", again, created, err)
	}
	f.jobs.Wait()
	if n := f.count(t, `SELECT count(*) FROM jobs WHERE kind = 'clone' AND status = 'done'`); n != 1 {
		t.Errorf("%d clone jobs", n)
	}
}

// The TS server answered the second of two adds at once with a 500.
func TestAddConcurrently(t *testing.T) {
	f := newFixture(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	created := 0
	for range 5 {
		wg.Go(func() {
			_, c, err := f.store.Add(context.Background(), f.workspace, f.user, repos.Ref{Owner: "acme", Name: "widgets"})
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			if c {
				created++
			}
			mu.Unlock()
		})
	}
	wg.Wait()
	f.jobs.Wait()
	if created != 1 || f.count(t, `SELECT count(*) FROM repos`) != 1 {
		t.Errorf("%d created", created)
	}
}

func TestCloneFails(t *testing.T) {
	f := newFixture(t)
	repo, _, err := f.store.Add(context.Background(), f.workspace, f.user, repos.Ref{Owner: "acme", Name: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	f.jobs.Wait()
	if n := f.count(t, `SELECT count(*) FROM jobs WHERE status = 'failed' AND error LIKE '%clone acme/missing%'`); n != 1 {
		t.Error("no failed clone job")
	}
	if n := f.count(t, `SELECT count(*) FROM repos WHERE id = $1 AND clone_path IS NULL`, repo.ID); n != 1 {
		t.Error("clone_path set after a failed clone")
	}
}

func TestRefreshAndRemove(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	repo, _, _ := f.store.Add(ctx, f.workspace, f.user, repos.Ref{Owner: "acme", Name: "widgets"})
	f.jobs.Wait()
	if err := f.store.Refresh(ctx, f.workspace, repo.ID); err != nil {
		t.Fatal(err)
	}
	f.jobs.Wait()
	if n := f.count(t, `SELECT count(*) FROM jobs WHERE kind = 'clone' AND status = 'done'`); n != 2 {
		t.Errorf("%d clone jobs done, want 2", n)
	}

	var other uuid.UUID
	f.db.QueryRow(ctx, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`).Scan(&other)
	for _, ws := range []uuid.UUID{other, f.workspace} {
		if err := f.store.Refresh(ctx, ws, uuid.New()); !errors.Is(err, repos.ErrNotFound) {
			t.Errorf("Refresh of an unknown repository: %v", err)
		}
	}
	if err := f.store.Remove(ctx, other, repo.ID); !errors.Is(err, repos.ErrNotFound) {
		t.Errorf("Remove from another workspace: %v", err)
	}
	if err := f.store.Remove(ctx, f.workspace, repo.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Remove(ctx, f.workspace, repo.ID); !errors.Is(err, repos.ErrNotFound) {
		t.Errorf("second Remove: %v", err)
	}
	// The clone stays, as in TS.
	if _, err := os.Stat(filepath.Join(f.cloneDir, "acme", "widgets", ".git")); err != nil {
		t.Errorf("the clone was removed: %v", err)
	}
}

// A clone is indexed after it lands; refreshing and resyncing index again.
func TestIndexJobs(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	repo, _, _ := f.store.Add(ctx, f.workspace, f.user, repos.Ref{Owner: "acme", Name: "widgets"})
	f.jobs.Wait()
	if n := f.count(t, `SELECT count(*) FROM repo_index_state WHERE repo_id = $1 AND status = 'full' AND files_indexed = 1`, repo.ID); n != 1 {
		t.Error("not indexed after the clone")
	}
	if err := f.store.Refresh(ctx, f.workspace, repo.ID); err != nil {
		t.Fatal(err)
	}
	job, err := f.store.Resync(ctx, f.workspace, repo.ID)
	if err != nil || job == uuid.Nil {
		t.Fatalf("Resync = %v, %v", job, err)
	}
	f.jobs.Wait()
	// Each clone job indexes after it, the refresh's fetch too, as in TS.
	for kind, want := range map[string]int{"clone": 2, "repo-intel-index": 2, "repo-intel-refresh": 1, "repo-intel-resync": 1} {
		if n := f.count(t, `SELECT count(*) FROM jobs WHERE kind = $1 AND status = 'done'`, kind); n != want {
			t.Errorf("%d %s jobs done, want %d", n, kind, want)
		}
	}
	if _, err := f.store.Resync(ctx, f.workspace, uuid.New()); !errors.Is(err, repos.ErrNotFound) {
		t.Errorf("Resync of an unknown repository: %v", err)
	}
}
