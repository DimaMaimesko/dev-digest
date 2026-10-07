package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// insertIntent adds a stored intent for pull, with every field filled in, so
// the response shape can be checked in full.
func (f fixture) insertIntent(t *testing.T, pull uuid.UUID) {
	t.Helper()
	f.exec(t, `INSERT INTO pr_intent (pr_id, intent, in_scope, out_of_scope, confidence, sources, unresolved,
			head_sha, fingerprint, provider, model, tokens_in, tokens_out, cost_usd, derived_at)
		VALUES ($1, 'Add rate limiting to the public API.', '["Rate limiting middleware"]', '["Authenticated endpoints"]',
			'high', '[{"kind":"title","label":"PR title","ref":null,"truncated":false}]',
			'[{"ref":"other/repo#5","reason":"other repository"}]',
			'a1b2c3d4e5f6', 'fp1', 'anthropic', 'haiku', 1200, 340, 0.0042, '2026-09-28 22:52:39.872999+00')`, pull)
}

func TestGetPullIntent(t *testing.T) {
	f := newFixture(t)
	pull := f.insertPull(t, f.insertRepo(t, f.workspace, "o/n"), 7, "open", "NULL", "NULL")
	f.insertIntent(t, pull)

	assertJSON(t, f.get(t, "/pulls/"+pull.String()+"/intent"), http.StatusOK, `{
		"pr_id": "`+pull.String()+`",
		"intent": "Add rate limiting to the public API.",
		"in_scope": ["Rate limiting middleware"],
		"out_of_scope": ["Authenticated endpoints"],
		"confidence": "high",
		"sources": [{"kind": "title", "label": "PR title", "ref": null, "truncated": false}],
		"unresolved": [{"ref": "other/repo#5", "reason": "other repository"}],
		"head_sha": "a1b2c3d4e5f6",
		"provider": "anthropic",
		"model": "haiku",
		"tokens_in": 1200,
		"tokens_out": 340,
		"cost_usd": 0.0042,
		"derived_at": "2026-09-28T22:52:39.872Z"
	}`)
}

// A PR with no stored intent answers 200 with null, not an error.
func TestGetPullIntentNone(t *testing.T) {
	f := newFixture(t)
	pull := f.insertPull(t, f.insertRepo(t, f.workspace, "o/n"), 7, "open", "NULL", "NULL")

	assertJSON(t, f.get(t, "/pulls/"+pull.String()+"/intent"), http.StatusOK, `null`)
}

func TestGetPullIntentNotFound(t *testing.T) {
	f := newFixture(t)
	other := f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`)
	otherPull := f.insertPull(t, f.insertRepo(t, other, "x/y"), 1, "open", "NULL", "NULL")
	missing := uuid.New().String()

	for _, tt := range []struct{ name, path string }{
		{"unknown PR", "/pulls/" + missing + "/intent"},
		{"another workspace's PR", "/pulls/" + otherPull.String() + "/intent"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assertJSON(t, f.get(t, tt.path), http.StatusNotFound,
				`{"error": {"code": "not_found", "message": "Pull request not found"}}`)
		})
	}
}

func TestGetPullIntentInvalidID(t *testing.T) {
	f := newFixture(t)
	assertJSON(t, f.get(t, "/pulls/not-a-uuid/intent"), http.StatusUnprocessableEntity, `{"error": {
		"code": "validation_error", "message": "Request validation failed",
		"details": [{"path": ["id"], "message": "Invalid uuid"}]}}`)
}
