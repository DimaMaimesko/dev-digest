package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func (f fixture) insertConvention(t *testing.T, workspace, repo uuid.UUID, rule string, confidence any, accepted bool) uuid.UUID {
	t.Helper()
	return f.insertID(t, `INSERT INTO conventions (workspace_id, repo_id, rule, confidence, accepted)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`, workspace, repo, rule, confidence, accepted)
}

func TestListConventions(t *testing.T) {
	f := newFixture(t)
	repo := f.insertRepo(t, f.workspace, "acme/web")
	otherRepo := f.insertRepo(t, f.workspace, "acme/api")

	unknown := f.insertConvention(t, f.workspace, repo, "a: confidence unknown", nil, false)
	low := f.insertConvention(t, f.workspace, repo, "b: low", 0.4, false)
	high := f.insertConvention(t, f.workspace, repo, "c: high", 0.9, false)
	accepted := f.insertConvention(t, f.workspace, repo, "d: accepted", 0.1, true)
	f.insertConvention(t, f.workspace, otherRepo, "another repo's", 1.0, true)
	f.insertID(t, `INSERT INTO conventions (workspace_id, repo_id, rule, evidence_path, evidence_snippet)
		VALUES ($1, $2, 'with evidence', 'src/a.ts', 'await x') RETURNING id`, f.workspace, otherRepo)

	var got []struct{ ID string }
	decode(t, f.get(t, "/repos/"+repo.String()+"/conventions"), http.StatusOK, &got)
	want := []uuid.UUID{accepted, high, low, unknown}
	if len(got) != len(want) {
		t.Fatalf("got %d conventions, want %d: %+v", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].ID != id.String() {
			t.Errorf("conventions[%d] = %s, want %s (accepted, then by confidence, unknown last)", i, got[i].ID, id)
		}
	}

	// A missing evidence or confidence is "" or 0: the contract has no nulls.
	res := f.get(t, "/repos/"+repo.String()+"/conventions")
	var all []map[string]any
	decode(t, res, http.StatusOK, &all)
	last := all[len(all)-1]
	if last["evidence_path"] != "" || last["evidence_snippet"] != "" || last["confidence"] != 0.0 {
		t.Errorf("convention without evidence or confidence = %v, want \"\", \"\" and 0", last)
	}
}

func TestListConventionsUnknownRepo(t *testing.T) {
	f := newFixture(t)
	other := f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`)
	foreign := f.insertRepo(t, other, "acme/foreign")
	f.insertConvention(t, other, foreign, "foreign", 1.0, true)

	for name, path := range map[string]string{
		"unknown":         "/repos/" + uuid.NewString() + "/conventions",
		"other workspace": "/repos/" + foreign.String() + "/conventions",
	} {
		t.Run(name, func(t *testing.T) {
			assertJSON(t, f.get(t, path), http.StatusNotFound,
				`{"error": {"code": "not_found", "message": "Repo not found"}}`)
		})
	}
	res := f.get(t, "/repos/nope/conventions")
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("malformed ID: status = %d, want 422", res.StatusCode)
	}
}
