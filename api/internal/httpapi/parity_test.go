package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/httpapi"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

// TestParityWithTypeScript compares the Go handler with a running TS server,
// both reading the same database, and requires the same status and JSON. It
// walks the real data: every repository, its pull requests, and each pull
// request's detail; every agent, its skills and each saved version. Lists are compared in any order, since the web app sorts
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
	goAPI := httptest.NewServer(httpapi.New(db, workspace, webOrigin, quiet).Handler())
	defer goAPI.Close()

	compare := func(path string) any {
		t.Helper()
		var tsBody any
		t.Run(path, func(t *testing.T) {
			tsStatus, ts := fetchJSON(t, tsURL+path)
			goStatus, gb := fetchJSON(t, goAPI.URL+path)
			tsBody = ts
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
	compare("/repos/" + missing + "/pulls")
	compare("/pulls/" + missing)
	repos, _ := compare("/repos").([]any)
	for _, repo := range repos {
		pulls, _ := compare("/repos/" + field(repo, "id") + "/pulls").([]any)
		for _, pull := range pulls {
			compare("/pulls/" + field(pull, "id"))
		}
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
