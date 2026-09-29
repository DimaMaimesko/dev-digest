package repointel_test

import (
	"context"
	"encoding/json"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/pgtest"
	"github.com/DimaMaimesko/dev-digest/api/internal/repointel"
)

// gitRepo is a git repository for tests, with helpers to change it.
type gitRepo struct {
	t   *testing.T
	dir string
}

func newGitRepo(t *testing.T, dir string) gitRepo {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	g := gitRepo{t, dir}
	g.git("init", "-q", "-b", "main")
	return g
}

func (g gitRepo) git(args ...string) string {
	g.t.Helper()
	cmd := osexec.Command("git", args...)
	cmd.Dir = g.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		g.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit writes files ("" deletes one) and commits them.
func (g gitRepo) commit(files map[string]string) string {
	g.t.Helper()
	for name, text := range files {
		p := filepath.Join(g.dir, name)
		if text == "" {
			os.Remove(p)
			continue
		}
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(text), 0o644)
	}
	g.git("add", "-A")
	g.git("commit", "-qm", "change", "--allow-empty")
	return g.git("rev-parse", "HEAD")
}

type indexFixture struct {
	db   *pgxpool.Pool
	repo uuid.UUID
	x    *repointel.Indexer
}

func newIndexFixture(t *testing.T, clonePath any) indexFixture {
	t.Helper()
	f := indexFixture{db: pgtest.New(t)}
	f.db.QueryRow(context.Background(), `WITH w AS (INSERT INTO workspaces (name) VALUES ('w') RETURNING id)
		INSERT INTO repos (workspace_id, owner, name, full_name, clone_path) SELECT id, 'o', 'n', 'o/n', $1 FROM w RETURNING id`,
		clonePath).Scan(&f.repo)
	f.x = repointel.NewIndexer(f.db, func() (string, error) { return "", nil })
	return f
}

func (f indexFixture) rows(t *testing.T, sql string) []string {
	t.Helper()
	rows, err := f.db.Query(context.Background(), sql, f.repo)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

type state struct {
	Sha, Status      string
	Indexed, Skipped int
	Stats            map[string]any
}

func (f indexFixture) state(t *testing.T) state {
	t.Helper()
	var s state
	var stats []byte
	err := f.db.QueryRow(context.Background(), `SELECT last_indexed_sha, status, files_indexed, files_skipped, stats
		FROM repo_index_state WHERE repo_id = $1`, f.repo).Scan(&s.Sha, &s.Status, &s.Indexed, &s.Skipped, &stats)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(stats, &s.Stats)
	return s
}

var project = map[string]string{
	"src/core.ts": "export function core(n: number) {\n  return n;\n}\n",
	"src/util.ts": "import { core } from './core.js';\nexport function twice(n: number) {\n  return core(n) * 2;\n}\n",
	"src/app.ts":  "import { twice } from './util.js';\nimport { core } from './core.js';\napp.get('/twice', () => twice(core(1)));\n",
}

func TestFullIndex(t *testing.T) {
	g := newGitRepo(t, t.TempDir())
	head := g.commit(project)
	f := newIndexFixture(t, g.dir)
	res, err := f.x.Full(context.Background(), f.repo)
	if err != nil || res.Status != "full" || res.FilesIndexed != 3 || res.FilesSkipped != 0 {
		t.Fatalf("Full = %+v, %v", res, err)
	}
	st := f.state(t)
	if st.Sha != head || st.Status != "full" || st.Indexed != 3 || st.Stats["edgesWritten"] != 3.0 || st.Stats["symbolsWritten"] != 2.0 {
		t.Errorf("state = %+v", st)
	}
	edges := strings.Join(f.rows(t, `SELECT from_file || '>' || to_file FROM file_edges WHERE repo_id = $1 ORDER BY 1`), " ")
	if edges != "src/app.ts>src/core.ts src/app.ts>src/util.ts src/util.ts>src/core.ts" {
		t.Errorf("edges: %s", edges)
	}
	// core.ts, which both others import, ranks highest.
	if top := f.rows(t, `SELECT file_path FROM file_rank WHERE repo_id = $1 ORDER BY rank DESC LIMIT 1`); top[0] != "src/core.ts" || f.rows(t, `SELECT percentile::text FROM file_rank WHERE repo_id = $1 AND file_path = 'src/core.ts'`)[0] != "100" {
		t.Errorf("top file %v", top)
	}
	// References resolve through the imports.
	refs := strings.Join(f.rows(t, `SELECT from_path || ':' || to_symbol || '=' || coalesce(decl_file, '?') FROM "references" WHERE repo_id = $1 ORDER BY 1`), " ")
	if !strings.Contains(refs, "src/app.ts:twice=src/util.ts") || !strings.Contains(refs, "src/util.ts:core=src/core.ts") {
		t.Errorf("references: %s", refs)
	}
	maps := f.rows(t, `SELECT commit_sha || E'\n' || map_text FROM repo_map_cache WHERE repo_id = $1`)
	if len(maps) != 1 || !strings.HasPrefix(maps[0], head+"\n# Repo skeleton") || !strings.Contains(maps[0], "src/core.ts:\n  function core(n: number)") {
		t.Errorf("maps: %q", maps)
	}
	if facts := f.rows(t, `SELECT file_path || ' ' || endpoints::text FROM file_facts WHERE repo_id = $1`); len(facts) != 1 || facts[0] != `src/app.ts ["GET /twice"]` {
		t.Errorf("facts: %v", facts)
	}

	// Indexing again gives the same index, not twice as much.
	if _, err := f.x.Full(context.Background(), f.repo); err != nil {
		t.Fatal(err)
	}
	if n := len(f.rows(t, `SELECT name FROM symbols WHERE repo_id = $1`)); n != 2 {
		t.Errorf("%d symbols after a second index", n)
	}
}

func TestFullIndexDegraded(t *testing.T) {
	t.Run("no clone yet", func(t *testing.T) {
		f := newIndexFixture(t, nil)
		res, err := f.x.Full(context.Background(), f.repo)
		if err != nil || res.Status != "degraded" || f.state(t).Stats["degradedReason"] != "no_data" {
			t.Errorf("Full = %+v, %v; state %+v", res, err, f.state(t))
		}
	})
	t.Run("no code", func(t *testing.T) {
		g := newGitRepo(t, t.TempDir())
		g.commit(map[string]string{"README.md": "hi"})
		f := newIndexFixture(t, g.dir)
		res, err := f.x.Full(context.Background(), f.repo)
		if err != nil || res.Status != "partial" || res.Reason != "no_files" || f.state(t).Status != "partial" {
			t.Errorf("Full = %+v, %v", res, err)
		}
	})
	t.Run("repository deleted", func(t *testing.T) {
		f := newIndexFixture(t, nil)
		if res, err := f.x.Full(context.Background(), uuid.New()); err != nil || res.Reason != "repo_not_found" {
			t.Errorf("Full = %+v, %v", res, err)
		}
	})
}

func TestRefresh(t *testing.T) {
	g := newGitRepo(t, t.TempDir())
	g.commit(project)
	f := newIndexFixture(t, g.dir)
	ctx := context.Background()

	// No index yet: a full one.
	if res, err := f.x.Refresh(ctx, f.repo); err != nil || res.Status != "full" || res.FilesIndexed != 3 {
		t.Fatalf("first Refresh = %+v, %v", res, err)
	}
	// Nothing new.
	if res, _ := f.x.Refresh(ctx, f.repo); res.Reason != "sha_unchanged" {
		t.Errorf("unchanged: %+v", res)
	}
	// Only a non-code file changed: the new commit is recorded.
	head := g.commit(map[string]string{"README.md": "hi"})
	if res, _ := f.x.Refresh(ctx, f.repo); res.Reason != "no_supported_changes" || f.state(t).Sha != head {
		t.Errorf("no code change: %+v, state %+v", res, f.state(t))
	}
	// A file changes and one is added: only they are parsed; the rest stays.
	head = g.commit(map[string]string{
		"src/util.ts": "import { core } from './core.js';\nexport function thrice(n: number) {\n  return core(n) * 3;\n}\n",
		"src/new.ts":  "import { thrice } from './util.js';\nexport const run = () => thrice(1);\n",
	})
	res, err := f.x.Refresh(ctx, f.repo)
	if err != nil || res.Reason != "incremental" || res.FilesIndexed != 2 {
		t.Fatalf("incremental = %+v, %v", res, err)
	}
	syms := strings.Join(f.rows(t, `SELECT path || ':' || name FROM symbols WHERE repo_id = $1 ORDER BY 1`), " ")
	if syms != "src/core.ts:core src/new.ts:run src/util.ts:thrice" {
		t.Errorf("symbols: %s", syms)
	}
	st := f.state(t)
	if st.Sha != head || st.Status != "full" || st.Indexed != 5 || st.Stats["incremental"] != true || st.Stats["changedFiles"] != 2.0 {
		t.Errorf("state = %+v", st)
	}
	if n := len(f.rows(t, `SELECT to_file FROM file_edges WHERE repo_id = $1`)); n != 4 {
		t.Errorf("%d edges, want 4", n)
	}
	// A deleted file makes the index partial, as in TS.
	g.commit(map[string]string{"src/new.ts": ""})
	if res, _ := f.x.Refresh(ctx, f.repo); res.Status != "partial" || len(f.rows(t, `SELECT name FROM symbols WHERE repo_id = $1 AND path = 'src/new.ts'`)) != 0 {
		t.Errorf("after a delete: %+v", res)
	}
}

// Resync fetches the default branch's new commits into the clone first.
func TestResync(t *testing.T) {
	origin := newGitRepo(t, t.TempDir())
	origin.commit(project)
	clone := filepath.Join(t.TempDir(), "clone")
	if out, err := osexec.Command("git", "clone", "-q", "file://"+origin.dir, clone).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	f := newIndexFixture(t, clone)
	ctx := context.Background()
	if _, err := f.x.Full(ctx, f.repo); err != nil {
		t.Fatal(err)
	}
	head := origin.commit(map[string]string{"src/extra.ts": "export function extra() {}\n"})
	res, err := f.x.Resync(ctx, f.repo)
	if err != nil || res.Reason != "incremental" || f.state(t).Sha != head {
		t.Errorf("Resync = %+v, %v; state %+v", res, err, f.state(t))
	}
	if n := len(f.rows(t, `SELECT name FROM symbols WHERE repo_id = $1 AND name = 'extra'`)); n != 1 {
		t.Error("the new commit's file isn't indexed")
	}
	if res, _ := newIndexFixture(t, nil).x.Resync(ctx, uuid.New()); res.Reason != "no_clone" {
		t.Errorf("no clone: %+v", res)
	}
}

// Two index runs of one repository at once take turns.
func TestIndexConcurrently(t *testing.T) {
	g := newGitRepo(t, t.TempDir())
	g.commit(project)
	f := newIndexFixture(t, g.dir)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if _, err := f.x.Full(context.Background(), f.repo); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if n := len(f.rows(t, `SELECT name FROM symbols WHERE repo_id = $1`)); n != 2 {
		t.Errorf("%d symbols", n)
	}
}
