package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/httpapi"
	"github.com/DimaMaimesko/dev-digest/api/internal/pgtest"
	"github.com/DimaMaimesko/dev-digest/api/internal/secrets"
)

const webOrigin = "http://localhost:3000"

var quiet = slog.New(slog.DiscardHandler)

// fixture is a database with the rows the tests read, and the server's
// secrets file and environment.
type fixture struct {
	db          *pgxpool.Pool
	workspace   uuid.UUID
	user        uuid.UUID
	secretsFile string // doesn't exist until a test writes it
	env         map[string]string
}

const cloneDir = "/work/clones"

func newFixture(t *testing.T) fixture {
	t.Helper()
	f := fixture{
		db:          pgtest.New(t),
		secretsFile: filepath.Join(t.TempDir(), "secrets.json"),
		env:         map[string]string{},
	}
	f.workspace = f.insertID(t, `INSERT INTO workspaces (name) VALUES ('default') RETURNING id`)
	f.user = f.insertID(t, `INSERT INTO users (email, name) VALUES ('you@local', 'You') RETURNING id`)
	return f
}

func (f fixture) insertID(t *testing.T, sql string, args ...any) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.db.QueryRow(context.Background(), sql, args...).Scan(&id); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return id
}

// get sends a GET request with the web app's Origin and returns the response.
func (f fixture) get(t *testing.T, path string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Origin", webOrigin)
	return f.serve(req)
}

func (f fixture) serve(req *http.Request) *http.Response {
	rec := httptest.NewRecorder()
	httpapi.New(httpapi.Config{
		DB:        f.db,
		Workspace: f.workspace,
		User:      f.user,
		WebOrigin: webOrigin,
		CloneDir:  cloneDir,
		Secrets:   secrets.New(f.secretsFile, func(k string) string { return f.env[k] }),
		Log:       quiet,
	}).Handler().ServeHTTP(rec, req)
	return rec.Result()
}

// assertJSON checks a response's status and that its body is the JSON want.
func assertJSON(t *testing.T, res *http.Response, status int, want string) {
	t.Helper()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != status {
		t.Errorf("status = %d, want %d; body: %s", res.StatusCode, status, body)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	var got, w any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body is not JSON: %v\n%s", err, body)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("bad want JSON: %v", err)
	}
	if !reflect.DeepEqual(got, w) {
		t.Errorf("body:\n%s\nwant:\n%s", body, want)
	}
}

// decode checks a response's status and decodes its JSON body into v.
func decode(t *testing.T, res *http.Response, status int, v any) {
	t.Helper()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != status {
		t.Fatalf("status = %d, want %d; body: %s", res.StatusCode, status, body)
	}
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decode body: %v\n%s", err, body)
	}
}

func TestListRepos(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	older := f.insertID(t, `INSERT INTO repos (workspace_id, owner, name, full_name, created_by, created_at)
		VALUES ($1, 'acme', 'payments-api', 'acme/payments-api', $2, '2026-09-01 10:00:00+00') RETURNING id`,
		f.workspace, f.user)
	// Postgres keeps microseconds; the web app gets milliseconds, in UTC.
	newer := f.insertID(t, `INSERT INTO repos (workspace_id, owner, name, full_name, clone_path, last_polled_at, created_at)
		VALUES ($1, 'me', 'greenlight', 'me/greenlight', '/clones/me/greenlight',
		        '2026-09-28 22:52:39.872999+00', '2026-09-02 10:00:00+00') RETURNING id`,
		f.workspace)
	other := f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`)
	if _, err := f.db.Exec(ctx, `INSERT INTO repos (workspace_id, owner, name, full_name)
		VALUES ($1, 'x', 'hidden', 'x/hidden')`, other); err != nil {
		t.Fatal(err)
	}

	assertJSON(t, f.get(t, "/repos"), http.StatusOK, `[
		{"id": "`+older.String()+`", "workspace_id": "`+f.workspace.String()+`",
		 "owner": "acme", "name": "payments-api", "full_name": "acme/payments-api",
		 "default_branch": "main", "clone_path": null, "last_polled_at": null,
		 "created_by": "`+f.user.String()+`"},
		{"id": "`+newer.String()+`", "workspace_id": "`+f.workspace.String()+`",
		 "owner": "me", "name": "greenlight", "full_name": "me/greenlight",
		 "default_branch": "main", "clone_path": "/clones/me/greenlight",
		 "last_polled_at": "2026-09-28T22:52:39.872Z", "created_by": null}
	]`)
}

func TestListReposEmpty(t *testing.T) {
	assertJSON(t, newFixture(t).get(t, "/repos"), http.StatusOK, `[]`)
}

func TestListReposDatabaseDown(t *testing.T) {
	f := newFixture(t)
	f.db.Close()

	res := f.get(t, "/repos")
	assertJSON(t, res, http.StatusInternalServerError, `{"error": {"code": "internal_error", "message": "Internal error"}}`)
}

func TestHealth(t *testing.T) {
	f := newFixture(t)
	assertJSON(t, f.get(t, "/health"), http.StatusOK, `{"status": "ok"}`)
	assertJSON(t, f.get(t, "/health/ready"), http.StatusOK, `{"ready": true}`)

	f.db.Close()
	assertJSON(t, f.get(t, "/health"), http.StatusOK, `{"status": "ok"}`)
	assertJSON(t, f.get(t, "/health/ready"), http.StatusServiceUnavailable, `{"ready": false}`)
}

func TestNotFound(t *testing.T) {
	f := newFixture(t)
	tests := []struct{ method, path, want string }{
		{http.MethodGet, "/nope", "Route GET:/nope not found"},
		// A known path with another method is also a 404, as in Fastify.
		{http.MethodDelete, "/health", "Route DELETE:/health not found"},
	}
	for _, tt := range tests {
		res := f.serve(httptest.NewRequest(tt.method, tt.path, nil))
		assertJSON(t, res, http.StatusNotFound, `{"error": {"code": "not_found", "message": "`+tt.want+`"}}`)
	}
}

func TestHeaders(t *testing.T) {
	f := newFixture(t)

	t.Run("security headers on every response", func(t *testing.T) {
		h := f.get(t, "/nope").Header
		for name, want := range map[string]string{
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":        "SAMEORIGIN",
			"Referrer-Policy":        "no-referrer",
		} {
			if got := h.Get(name); got != want {
				t.Errorf("%s = %q, want %q", name, got, want)
			}
		}
	})

	t.Run("CORS for the web app", func(t *testing.T) {
		h := f.get(t, "/health").Header
		if h.Get("Access-Control-Allow-Origin") != webOrigin || h.Get("Access-Control-Allow-Credentials") != "true" {
			t.Errorf("CORS headers = %v", h)
		}
	})

	t.Run("no CORS for other sites", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.Header.Set("Origin", "http://evil.example")
		if got := f.serve(req).Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin = %q, want none", got)
		}
	})

	t.Run("preflight", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/repos", nil)
		req.Header.Set("Origin", webOrigin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "content-type")
		res := f.serve(req)

		if res.StatusCode != http.StatusNoContent {
			t.Errorf("status = %d, want 204", res.StatusCode)
		}
		h := res.Header
		if h.Get("Access-Control-Allow-Methods") != "GET,HEAD,PUT,PATCH,POST,DELETE" ||
			h.Get("Access-Control-Allow-Headers") != "content-type" ||
			h.Get("Access-Control-Allow-Origin") != webOrigin ||
			strings.Join(h.Values("Vary"), ", ") != "Origin, Access-Control-Request-Headers" {
			t.Errorf("preflight headers = %v", h)
		}
	})
}
