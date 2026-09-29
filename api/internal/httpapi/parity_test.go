package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/httpapi"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

// parityPaths are the routes the Go server serves so far. Add each ported
// route here.
var parityPaths = []string{
	"/health",
	"/health/ready",
	"/repos",
}

// TestParityWithTypeScript compares the Go handler with a running TS server,
// both reading the same database, and requires the same status and JSON.
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

	for _, path := range parityPaths {
		t.Run(path, func(t *testing.T) {
			tsStatus, tsBody := fetchJSON(t, tsURL+path)
			goStatus, goBody := fetchJSON(t, goAPI.URL+path)
			if tsStatus != goStatus {
				t.Errorf("status: TS %d, Go %d", tsStatus, goStatus)
			}
			if !reflect.DeepEqual(tsBody, goBody) {
				ts, _ := json.MarshalIndent(tsBody, "", "  ")
				gb, _ := json.MarshalIndent(goBody, "", "  ")
				t.Errorf("bodies differ\nTS:\n%s\nGo:\n%s", ts, gb)
			}
		})
	}
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
