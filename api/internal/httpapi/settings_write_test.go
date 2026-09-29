package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// put sends a PUT request with a JSON body.
func (f fixture) put(t *testing.T, path, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return f.serve(req)
}

// storedSettings returns the settings rows as key → JSON text.
func (f fixture) storedSettings(t *testing.T) map[string]string {
	t.Helper()
	rows, err := f.db.Query(context.Background(),
		`SELECT key, COALESCE(value::text, 'SQL NULL') FROM settings WHERE workspace_id = $1 AND user_id = $2`, f.workspace, f.user)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatal(err)
		}
		out[k] = v
	}
	return out
}

func TestPutSettings(t *testing.T) {
	f := newFixture(t)
	f.exec(t, `INSERT INTO settings (workspace_id, user_id, key, value) VALUES
		($1, $2, 'theme', '"dark"'), ($1, $2, 'density', '"regular"')`, f.workspace, f.user)

	res := f.put(t, "/settings", `{
		"theme": "light",
		"polling_interval_min": 10,
		"automatic_reviews": true,
		"feature_models": {"onboarding": {"provider": "openrouter", "model": "m", "extra": 1}},
		"my_extra": {"any": ["json"]},
		"cleared": null
	}`)

	// The answer is every setting: the saved ones and the untouched density.
	assertJSON(t, res, http.StatusOK, `{
		"theme": "light", "density": "regular", "polling_interval_min": 10, "automatic_reviews": true,
		"feature_models": {"onboarding": {"provider": "openrouter", "model": "m"}},
		"my_extra": {"any": ["json"]}, "cleared": null
	}`)
	stored := f.storedSettings(t)
	if stored["theme"] != `"light"` || stored["feature_models"] != `{"onboarding": {"model": "m", "provider": "openrouter"}}` {
		t.Errorf("stored = %v", stored)
	}
}

func TestPutSettingsEmpty(t *testing.T) {
	f := newFixture(t)
	f.exec(t, `INSERT INTO settings (workspace_id, user_id, key, value) VALUES ($1, $2, 'theme', '"dark"')`, f.workspace, f.user)
	assertJSON(t, f.put(t, "/settings", `{}`), http.StatusOK, `{"theme": "dark"}`)
}

func TestPutSettingsInvalid(t *testing.T) {
	tests := []struct {
		body string
		path string // of the issue, joined with "."
	}{
		{`{"theme": "blue"}`, "theme"},
		{`{"theme": null}`, "theme"},
		{`{"density": "tight"}`, "density"},
		{`{"polling_interval_min": 0}`, "polling_interval_min"},
		{`{"polling_interval_min": 2.5}`, "polling_interval_min"},
		{`{"polling_interval_min": "5"}`, "polling_interval_min"},
		{`{"polling_interval_min": null}`, "polling_interval_min"},
		{`{"sync_to_folder": "yes"}`, "sync_to_folder"},
		{`{"automatic_reviews": null}`, "automatic_reviews"},
		{`{"feature_models": []}`, "feature_models"},
		{`{"feature_models": {"nope": {"provider": "openai", "model": "m"}}}`, "feature_models.nope"},
		{`{"feature_models": {"onboarding": {"provider": "gemini", "model": "m"}}}`, "feature_models.onboarding.provider"},
		{`{"feature_models": {"onboarding": {"provider": "openai", "model": ""}}}`, "feature_models.onboarding.model"},
		// One bad key means nothing is saved, not even the good ones.
		{`{"density": "compact", "theme": "blue"}`, "theme"},
	}
	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			f := newFixture(t)
			var body struct {
				Error struct {
					Code    string
					Details []struct {
						Path    []string
						Message string
					}
				}
			}
			decode(t, f.put(t, "/settings", tt.body), http.StatusUnprocessableEntity, &body)
			if body.Error.Code != "validation_error" || len(body.Error.Details) != 1 ||
				strings.Join(body.Error.Details[0].Path, ".") != tt.path || body.Error.Details[0].Message == "" {
				t.Errorf("error = %+v, want one issue at %s", body.Error, tt.path)
			}
			if stored := f.storedSettings(t); len(stored) != 0 {
				t.Errorf("stored %v, want nothing", stored)
			}
		})
	}
}

func TestPutSettingsBadBody(t *testing.T) {
	f := newFixture(t)
	tests := []struct {
		name        string
		contentType string
		body        string
		status      int
		code        string
	}{
		{"not JSON content type", "text/plain", `{"theme": "light"}`, http.StatusUnsupportedMediaType, "unsupported_media_type"},
		{"empty", "application/json", ``, http.StatusBadRequest, "bad_request"},
		{"broken JSON", "application/json", `{"theme":`, http.StatusBadRequest, "invalid_json"},
		{"array", "application/json", `[1]`, http.StatusUnprocessableEntity, "validation_error"},
		{"null", "application/json", `null`, http.StatusUnprocessableEntity, "validation_error"},
		{"over 1 MB", "application/json", `{"big": "` + strings.Repeat("x", 1<<20) + `"}`, http.StatusRequestEntityTooLarge, "body_too_large"},
		{"JSON with charset", "application/json; charset=utf-8", `{}`, http.StatusOK, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			var body struct{ Error struct{ Code string } }
			decode(t, f.serve(req), tt.status, &body)
			if body.Error.Code != tt.code {
				t.Errorf("code = %q, want %q", body.Error.Code, tt.code)
			}
		})
	}
}
