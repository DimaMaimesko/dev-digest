package httpapi_test

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DimaMaimesko/dev-digest/api/internal/jobs"
	"github.com/DimaMaimesko/dev-digest/api/internal/repos"
)

// withRepos gives the fixture a repository store that clones from local
// repositories acme/widgets and vercel/next.js, and its job runner.
func withRepos(t *testing.T) (fixture, *jobs.Runner, string) {
	t.Helper()
	f := newFixture(t)
	remotes := t.TempDir()
	for _, name := range []string{"acme/widgets.git", "vercel/next.js.git"} {
		dir := filepath.Join(remotes, name)
		os.MkdirAll(dir, 0o755)
		for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "first"}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
				"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
	}
	j := jobs.New(f.db, quiet)
	t.Cleanup(j.Close)
	clones := t.TempDir()
	f.repos = repos.NewStore(repos.Config{
		DB: f.db, Jobs: j, CloneDir: clones,
		Token:  func() (string, error) { return "", nil },
		Remote: "file://" + remotes + "/",
	})
	return f, j, clones
}

func TestAddRepo(t *testing.T) {
	f, j, clones := withRepos(t)
	var repo map[string]any
	decode(t, f.send(t, http.MethodPost, "/repos", `{"url": "https://github.com/acme/widgets.git"}`), http.StatusCreated, &repo)
	id, _ := repo["id"].(string)
	want := map[string]any{"id": id, "workspace_id": f.workspace.String(), "owner": "acme", "name": "widgets",
		"full_name": "acme/widgets", "default_branch": "main", "clone_path": nil, "last_polled_at": nil, "created_by": f.user.String()}
	for k, v := range want {
		if repo[k] != v {
			t.Errorf("%s = %v, want %v", k, repo[k], v)
		}
	}
	j.Wait()
	// Cloned: the list shows where.
	var list []map[string]any
	decode(t, f.get(t, "/repos"), http.StatusOK, &list)
	if len(list) != 1 || list[0]["clone_path"] != filepath.Join(clones, "acme", "widgets") || list[0]["last_polled_at"] == nil {
		t.Errorf("list = %v", list)
	}
	// Added again: 200 with the same repository.
	var again map[string]any
	decode(t, f.send(t, http.MethodPost, "/repos", `{"url": "https://github.com/acme/widgets"}`), http.StatusOK, &again)
	if again["id"] != id {
		t.Errorf("again: %v", again)
	}
	// A name with a dot, which the TS server refused.
	decode(t, f.send(t, http.MethodPost, "/repos", `{"url": "https://github.com/vercel/next.js"}`), http.StatusCreated, &repo)
	j.Wait()
	if _, err := os.Stat(filepath.Join(clones, "vercel", "next.js", ".git")); err != nil {
		t.Errorf("next.js not cloned: %v", err)
	}
}

func TestAddRepoInvalid(t *testing.T) {
	f, j, clones := withRepos(t)
	badURL := func(u string) string {
		return `{"error": {"code": "invalid_repo_url", "message": "Could not parse owner/repo from '` + u + `'"}}`
	}
	for _, u := range []string{"https://github.com/../victim", "https://gitlab.com/acme/widgets", "https://github.com/acme"} {
		assertJSON(t, f.send(t, http.MethodPost, "/repos", `{"url": "`+u+`"}`), http.StatusBadRequest, badURL(u))
	}
	for body, path := range map[string]string{`{}`: "url", `{"url": "acme/widgets"}`: "url", `{"url": "git@github.com:acme/widgets.git"}`: "url", `{"url": 7}`: "url"} {
		if paths := issuePaths(t, f.send(t, http.MethodPost, "/repos", body)); strings.Join(paths, ",") != path {
			t.Errorf("%s: issues at %v", body, paths)
		}
	}
	j.Wait()
	entries, _ := os.ReadDir(filepath.Dir(clones))
	var repos int
	f.db.QueryRow(context.Background(), `SELECT count(*) FROM repos`).Scan(&repos)
	if repos != 0 {
		t.Errorf("%d repositories added; %d entries next to the clones", repos, len(entries))
	}
}

func TestRefreshAndDeleteRepo(t *testing.T) {
	f, j, _ := withRepos(t)
	var repo struct{ ID string }
	decode(t, f.send(t, http.MethodPost, "/repos", `{"url": "https://github.com/acme/widgets"}`), http.StatusCreated, &repo)
	j.Wait()
	notFound := `{"error": {"code": "not_found", "message": "Repo not found"}}`
	missing := uuid.NewString()

	assertJSON(t, f.send(t, http.MethodPost, "/repos/"+repo.ID+"/refresh", ``), http.StatusOK, `{"status": "refreshing"}`)
	assertJSON(t, f.send(t, http.MethodPost, "/repos/"+missing+"/refresh", ``), http.StatusNotFound, notFound)
	j.Wait()

	pull := f.insertPull(t, uuid.MustParse(repo.ID), 1, "open", "NULL", "NULL")
	assertJSON(t, f.send(t, http.MethodDelete, "/repos/"+repo.ID, ``), http.StatusOK, `{"deleted": "`+repo.ID+`"}`)
	assertJSON(t, f.send(t, http.MethodDelete, "/repos/"+repo.ID, ``), http.StatusNotFound, notFound)
	var pulls int
	f.db.QueryRow(context.Background(), `SELECT count(*) FROM pull_requests WHERE id = $1`, pull).Scan(&pulls)
	if pulls != 0 {
		t.Error("the repository's pull request wasn't deleted with it")
	}
	for _, r := range []struct{ method, path string }{{http.MethodPost, "/repos/42/refresh"}, {http.MethodDelete, "/repos/42"}} {
		if paths := issuePaths(t, f.send(t, r.method, r.path, ``)); len(paths) != 1 {
			t.Errorf("%s %s: issues at %v", r.method, r.path, paths)
		}
	}
}
