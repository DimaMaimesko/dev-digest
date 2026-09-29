package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeAPI serves two pages of models, the second after model "b".
func fakeAPI(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Api-Key") != "sk-ant-test" || r.Header.Get("Anthropic-Version") == "" || r.Header.Get("Authorization") != "" {
			t.Errorf("x-api-key %q, anthropic-version %q, Authorization %q",
				r.Header.Get("X-Api-Key"), r.Header.Get("Anthropic-Version"), r.Header.Get("Authorization"))
		}
		if status != 0 {
			w.WriteHeader(status)
			w.Write([]byte(`{"type": "error", "error": {"type": "authentication_error", "message": "invalid x-api-key"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after_id") == "b" {
			w.Write([]byte(`{"data": [{"type": "model", "id": "c", "display_name": "C", "created_at": "2025-01-01T00:00:00Z"}],
				"has_more": false, "first_id": "c", "last_id": "c"}`))
			return
		}
		w.Write([]byte(`{"data": [
			{"type": "model", "id": "a", "display_name": "A", "created_at": "2026-03-01T00:00:00Z"},
			{"type": "model", "id": "b", "display_name": "B", "created_at": "2026-02-01T00:00:00Z"}],
			"has_more": true, "first_id": "a", "last_id": "b"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func date(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// Every page is read: the TS server read only the first.
func TestModels(t *testing.T) {
	// The SDK's own credentials and base URL from the environment are
	// ignored: the key given to New is the only one sent. (An auth token
	// would go in an Authorization header next to it.)
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "token-env")
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:1")

	got, err := New(fakeAPI(t, 0).URL, "sk-ant-test").Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Model{
		{ID: "a", Name: "A", Released: date("2026-03-01T00:00:00Z")},
		{ID: "b", Name: "B", Released: date("2026-02-01T00:00:00Z")},
		{ID: "c", Name: "C", Released: date("2025-01-01T00:00:00Z")},
	}
	if len(got) != len(want) {
		t.Fatalf("Models = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].ID != want[i].ID || got[i].Name != want[i].Name || !got[i].Released.Equal(want[i].Released) {
			t.Errorf("model %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestModelsError(t *testing.T) {
	_, err := New(fakeAPI(t, http.StatusUnauthorized).URL, "sk-ant-test").Models(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %v, want the 401", err)
	}
}
