package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/github"
	"github.com/DimaMaimesko/dev-digest/api/internal/httpapi"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
	"github.com/DimaMaimesko/dev-digest/api/internal/secrets"
)

// TestParityWithTypeScript compares the Go handler with a running TS server,
// both reading the same database, and requires the same status and JSON. It
// walks the real data: every repository, its index state and pull requests;
// each pull request's detail, reviews and runs, and each run's trace; every
// agent, its skills and each saved version. Lists are compared in any order, since the web app sorts
// them, and times as instants: right after a GitHub sync the TS server sends
// GitHub's format ("…41Z"), otherwise JavaScript's ("…41.000Z").
//
// Reading pull requests makes the TS server sync them from GitHub first, when
// it has a GitHub token; that is its normal behavior.
//
// It runs only when PARITY_TS_URL is set:
//
//	PARITY_TS_URL=http://localhost:3001 go test ./internal/httpapi -run Parity -v
//
// PARITY_DATABASE_URL defaults to the dev database from docker-compose.yml.
func TestParityWithTypeScript(t *testing.T) {
	tsURL := os.Getenv("PARITY_TS_URL")
	if tsURL == "" {
		t.Skip("set PARITY_TS_URL to the running TS server, e.g. http://localhost:3001")
	}
	dbURL := os.Getenv("PARITY_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://devdigest:devdigest@localhost:5433/devdigest"
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	workspace, err := postgres.New(db).WorkspaceByName(ctx, "default")
	if err != nil {
		t.Fatalf("find the default workspace: %v", err)
	}
	// Give the Go handler the settings the TS process has: its environment
	// plus server/.env, with relative paths resolved against server/.
	getenv := tsEnv(t)
	cloneDir := filepath.Join(serverDir(), "clones")
	if v := getenv("DEVDIGEST_CLONE_DIR"); filepath.IsAbs(v) {
		cloneDir = v
	} else if v != "" {
		cloneDir = filepath.Join(serverDir(), v)
	}
	home, _ := os.UserHomeDir()
	user, err := postgres.New(db).UserByEmail(ctx, "you@local")
	if err != nil {
		t.Fatalf("find the local user: %v", err)
	}
	// No GitHubAPI: the Go handler serves what the TS server has just synced
	// and never writes to the dev database itself. The Go GitHub client is
	// checked by TestGitHubClientWithTypeScript.
	goAPI := httptest.NewServer(httpapi.New(httpapi.Config{
		DB:        db,
		Workspace: workspace,
		User:      user,
		WebOrigin: webOrigin,
		CloneDir:  cloneDir,
		Secrets:   secrets.New(filepath.Join(home, ".devdigest", "secrets.json"), getenv),
		Log:       quiet,
	}).Handler())
	defer goAPI.Close()

	// compare checks path on both servers. Keys in ignore are TS-only fields
	// of the answer that Go doesn't send.
	compare := func(path string, ignore ...string) any {
		t.Helper()
		var tsBody any
		t.Run(path, func(t *testing.T) {
			tsStatus, ts := fetchJSON(t, tsURL+path)
			goStatus, gb := fetchJSON(t, goAPI.URL+path)
			tsBody = ts
			if m, ok := ts.(map[string]any); ok {
				for _, k := range ignore {
					delete(m, k)
				}
			}
			if tsStatus != goStatus {
				t.Errorf("status: TS %d, Go %d", tsStatus, goStatus)
			}
			if !reflect.DeepEqual(normalize(ts), normalize(gb)) {
				tj, _ := json.MarshalIndent(ts, "", "  ")
				gj, _ := json.MarshalIndent(gb, "", "  ")
				t.Errorf("bodies differ\nTS:\n%s\nGo:\n%s", tj, gj)
			}
		})
		return tsBody
	}

	const missing = "00000000-0000-0000-0000-000000000000"
	compare("/health")
	compare("/health/ready")
	compare("/settings")
	compare("/settings/secrets-status")
	compare("/workspace")
	compare("/repos/" + missing + "/pulls")
	compare("/pulls/" + missing)
	compare("/repos/" + missing + "/index-state")
	compare("/pulls/" + missing + "/reviews")
	compare("/pulls/" + missing + "/runs")
	compare("/pulls/" + missing + "/runs/active")
	compare("/runs/" + missing + "/trace")
	repos, _ := compare("/repos").([]any)
	for _, repo := range repos {
		compare("/repos/" + field(repo, "id") + "/index-state")
		pulls, _ := compare("/repos/" + field(repo, "id") + "/pulls").([]any)
		for _, pull := range pulls {
			id := field(pull, "id")
			compare("/pulls/"+id, "linked_issue")
			compare("/pulls/" + id + "/reviews")
			compare("/pulls/" + id + "/runs/active")
			runs, _ := compare("/pulls/" + id + "/runs").([]any)
			for _, run := range runs {
				compare("/runs/" + field(run, "run_id") + "/trace")
			}
		}
	}

	// Writes that change nothing: an empty settings update, invalid bodies,
	// and missing agents, findings, reviews and runs. For an error, the
	// status, code and message must match; the details are Zod's in TS and
	// simpler in Go.
	type write struct{ method, path, body string }
	writes := []write{
		{http.MethodPost, "/agents", `{}`},
		{http.MethodPost, "/agents", `{"name": null, "provider": "openai", "model": "m", "system_prompt": "p"}`},
		{http.MethodPut, "/agents/" + missing, `{}`},
		{http.MethodDelete, "/agents/" + missing, `{}`},
		{http.MethodPost, "/agents/" + missing + "/skills", `{"skill_ids": []}`},
		{http.MethodPost, "/findings/" + missing + "/accept", `{}`},
		{http.MethodPost, "/findings/" + missing + "/dismiss", `{}`},
		{http.MethodPost, "/findings/42/accept", `{}`},
		{http.MethodDelete, "/reviews/" + missing, `{}`},
		{http.MethodDelete, "/reviews/42", `{}`},
		{http.MethodDelete, "/runs/" + missing, `{}`},
		{http.MethodPost, "/repos/" + missing + "/poll", `{}`},
		{http.MethodPost, "/repos/42/poll", `{}`},
	}
	for _, body := range []string{`{}`, `{"theme": "blue"}`, `{"polling_interval_min": 0}`, `[1]`, `null`} {
		writes = append(writes, write{http.MethodPut, "/settings", body})
	}
	for _, wr := range writes {
		t.Run(wr.method+" "+wr.path+" "+wr.body, func(t *testing.T) {
			tsStatus, ts := sendJSON(t, wr.method, tsURL+wr.path, wr.body)
			goStatus, gb := sendJSON(t, wr.method, goAPI.URL+wr.path, wr.body)
			if tsStatus != goStatus {
				t.Errorf("status: TS %d, Go %d", tsStatus, goStatus)
			}
			if tsStatus >= 400 {
				ts, gb = errorWithoutDetails(ts), errorWithoutDetails(gb)
			}
			if !reflect.DeepEqual(normalize(ts), normalize(gb)) {
				t.Errorf("bodies differ\nTS: %v\nGo: %v", ts, gb)
			}
		})
	}

	for _, path := range []string{"", "/versions", "/versions/1", "/skills"} {
		compare("/agents/" + missing + path)
	}
	agents, _ := compare("/agents").([]any)
	for _, agent := range agents {
		base := "/agents/" + field(agent, "id")
		compare(base)
		compare(base + "/skills")
		versions, _ := compare(base + "/versions").([]any)
		for _, v := range versions {
			n, _ := v.(map[string]any)["version"].(float64)
			compare(fmt.Sprintf("%s/versions/%d", base, int(n)))
		}
		compare(base + "/versions/999")
	}
}

// TestGitHubClientWithTypeScript checks the Go GitHub client against the TS
// server's Octokit client: for up to 3 pull requests of each repository, the
// detail the TS server has just fetched from GitHub must equal what the Go
// client reads, and each pull request in GitHub's list must match the TS
// server's list. It only reads, from GitHub and from the TS server. It runs
// when PARITY_TS_URL is set and a GitHub token is configured.
func TestGitHubClientWithTypeScript(t *testing.T) {
	tsURL := os.Getenv("PARITY_TS_URL")
	if tsURL == "" {
		t.Skip("set PARITY_TS_URL to the running TS server, e.g. http://localhost:3001")
	}
	home, _ := os.UserHomeDir()
	token, err := secrets.New(filepath.Join(home, ".devdigest", "secrets.json"), tsEnv(t)).Get(secrets.GitHubToken)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Skip("no GitHub token")
	}
	gh := github.New(github.DefaultURL, token)
	ctx := context.Background()

	_, repos := fetchJSON(t, tsURL+"/repos")
	for _, repo := range repos.([]any) {
		owner, name := field(repo, "owner"), field(repo, "name")
		_, tsPulls := fetchJSON(t, tsURL+"/repos/"+field(repo, "id")+"/pulls")
		byNumber := map[float64]map[string]any{}
		for _, p := range tsPulls.([]any) {
			byNumber[p.(map[string]any)["number"].(float64)] = p.(map[string]any)
		}

		list, err := gh.Pulls(ctx, owner, name)
		var status *github.StatusError
		if errors.As(err, &status) && status.Code == http.StatusNotFound {
			t.Logf("%s/%s isn't on GitHub, as with the seed data; skipped", owner, name)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		t.Run(owner+"/"+name+" list", func(t *testing.T) {
			for _, p := range list {
				ts, ok := byNumber[float64(p.Number)]
				if !ok {
					t.Errorf("#%d isn't in the TS list", p.Number)
					continue
				}
				got := map[string]any{"title": p.Title, "author": p.Author, "branch": p.Branch, "base": p.Base, "head_sha": p.HeadSHA}
				if p.State != "open" { // the TS list shows the review status of open ones
					got["status"] = p.State
				}
				for k, v := range got {
					if ts[k] != v {
						t.Errorf("#%d %s: TS %v, Go %v", p.Number, k, ts[k], v)
					}
				}
			}
		})

		for i, p := range tsPulls.([]any) {
			if i == 3 {
				break
			}
			id, number := field(p, "id"), int(p.(map[string]any)["number"].(float64))
			t.Run(fmt.Sprintf("%s/%s#%d", owner, name, number), func(t *testing.T) {
				_, ts := fetchJSON(t, tsURL+"/pulls/"+id)
				tsDetail := ts.(map[string]any)
				delete(tsDetail, "id")
				delete(tsDetail, "linked_issue")
				goDetail := githubDetail(t, gh, owner, name, number)
				if !reflect.DeepEqual(normalize(tsDetail), normalize(goDetail)) {
					tj, _ := json.MarshalIndent(tsDetail, "", "  ")
					gj, _ := json.MarshalIndent(goDetail, "", "  ")
					t.Errorf("details differ\nTS:\n%s\nGo:\n%s", tj, gj)
				}
			})
		}
	}
}

// githubDetail reads a pull request with the Go client, as the JSON the TS
// server sends right after fetching it: a missing patch or commit date is
// left out, not null.
func githubDetail(t *testing.T, gh *github.Client, owner, name string, number int) map[string]any {
	t.Helper()
	ctx := context.Background()
	p, err := gh.Pull(ctx, owner, name, number)
	if err != nil {
		t.Fatal(err)
	}
	files, err := gh.PullFiles(ctx, owner, name, number)
	if err != nil {
		t.Fatal(err)
	}
	commits, err := gh.PullCommits(ctx, owner, name, number)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{
		"number": p.Number, "title": p.Title, "author": p.Author, "branch": p.Branch, "base": p.Base,
		"head_sha": p.HeadSHA, "additions": p.Additions, "deletions": p.Deletions, "files_count": p.ChangedFiles,
		"status": p.State, "opened_at": p.CreatedAt, "updated_at": p.UpdatedAt, "body": p.Body,
	}
	var fs, cs []map[string]any
	for _, f := range files {
		m := map[string]any{"path": f.Path, "additions": f.Additions, "deletions": f.Deletions}
		if f.Patch != nil {
			m["patch"] = *f.Patch
		}
		fs = append(fs, m)
	}
	for _, c := range commits {
		m := map[string]any{"sha": c.SHA, "message": c.Message, "author": c.Author}
		if c.CommittedAt != nil {
			m["committed_at"] = *c.CommittedAt
		}
		cs = append(cs, m)
	}
	out["files"], out["commits"] = fs, cs
	// Through JSON, so numbers and times compare like the TS answer's.
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// serverDir is the TS server's directory, its working directory when running.
func serverDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "server")
}

// tsEnv returns the environment the TS server sees: the process environment,
// then server/.env, which dotenv loads without overriding what is already set.
func tsEnv(t *testing.T) func(string) string {
	t.Helper()
	dotenv := map[string]string{}
	data, err := os.ReadFile(filepath.Join(serverDir(), ".env"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			dotenv[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return func(k string) string {
		if v, ok := os.LookupEnv(k); ok {
			return v
		}
		return dotenv[k]
	}
}

// field returns a string field of a decoded JSON object.
func field(obj any, name string) string {
	m, _ := obj.(map[string]any)
	s, _ := m[name].(string)
	return s
}

// normalize returns v with every array sorted and every time in one format,
// so that equal content compares equal.
func normalize(v any) any {
	switch v := v.(type) {
	case string:
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t.UTC().Format(time.RFC3339Nano)
		}
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[k] = normalize(e)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = normalize(e)
		}
		slices.SortFunc(out, func(a, b any) int {
			ja, _ := json.Marshal(a)
			jb, _ := json.Marshal(b)
			return strings.Compare(string(ja), string(jb))
		})
		return out
	}
	return v
}

// sendJSON sends a request with a JSON body and decodes the JSON answer.
func sendJSON(t *testing.T, method, url, body string) (int, any) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var v any
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		t.Fatalf("%s %s: answer is not JSON: %v", method, url, err)
	}
	return res.StatusCode, v
}

// errorWithoutDetails returns an error envelope without its details.
func errorWithoutDetails(v any) any {
	if m, ok := v.(map[string]any); ok {
		if e, ok := m["error"].(map[string]any); ok {
			delete(e, "details")
		}
	}
	return v
}

func fetchJSON(t *testing.T, url string) (int, any) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%s: body is not JSON: %v\n%s", url, err, b)
	}
	return res.StatusCode, v
}
