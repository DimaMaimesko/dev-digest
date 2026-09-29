package httpapi_test

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

// insertReview adds a review of pull, produced by run (a uuid.UUID, or nil
// for none).
func (f fixture) insertReview(t *testing.T, workspace, pull uuid.UUID, run any) uuid.UUID {
	t.Helper()
	return f.insertID(t, `INSERT INTO reviews (workspace_id, pr_id, run_id, kind)
		VALUES ($1, $2, $3, 'review') RETURNING id`, workspace, pull, run)
}

// insertFinding adds a finding to review.
func (f fixture) insertFinding(t *testing.T, review uuid.UUID) uuid.UUID {
	t.Helper()
	return f.insertID(t, `INSERT INTO findings
		(review_id, file, start_line, end_line, severity, category, title, rationale, confidence)
		VALUES ($1, 'a.go', 3, 4, 'WARNING', 'bug', 'Off by one', 'The loop stops early.', 0.8) RETURNING id`, review)
}

// count returns the number sql selects.
func (f fixture) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func TestDecideFinding(t *testing.T) {
	f := newFixture(t)
	pull := f.insertPull(t, f.insertRepo(t, f.workspace, "o/n"), 1, "open", "NULL", "NULL")
	review := f.insertReview(t, f.workspace, pull, nil)
	finding := f.insertFinding(t, review)
	unchanged := map[string]any{
		"id": finding.String(), "review_id": review.String(), "file": "a.go", "start_line": 3.0, "end_line": 4.0,
		"severity": "WARNING", "category": "bug", "title": "Off by one", "rationale": "The loop stops early.",
		"suggestion": nil, "confidence": 0.8, "kind": "finding", "trifecta_components": nil, "evidence": nil,
	}

	// Each decision replaces the one before it.
	steps := []struct {
		action              string
		accepted, dismissed bool
	}{
		{"accept", true, false},
		{"accept", true, false},
		{"dismiss", false, true},
		{"dismiss", false, true},
		{"accept", true, false},
	}
	for i, step := range steps {
		before := time.Now().Truncate(time.Millisecond)
		var body struct{ Finding map[string]any }
		decode(t, f.send(t, http.MethodPost, "/findings/"+finding.String()+"/"+step.action, ``), http.StatusOK, &body)
		after := time.Now()

		checkTime := func(name string, want bool) {
			t.Helper()
			v := body.Finding[name]
			delete(body.Finding, name)
			if !want {
				if v != nil {
					t.Errorf("step %d (%s): %s = %v, want null", i, step.action, name, v)
				}
				return
			}
			s, _ := v.(string)
			at, err := time.Parse(time.RFC3339, s)
			if err != nil || at.Before(before) || at.After(after) || s != at.UTC().Format("2006-01-02T15:04:05.000Z") {
				t.Errorf("step %d (%s): %s = %v, want the time of the request as JavaScript writes it", i, step.action, name, v)
			}
		}
		checkTime("accepted_at", step.accepted)
		checkTime("dismissed_at", step.dismissed)
		if !reflect.DeepEqual(body.Finding, unchanged) {
			t.Errorf("step %d (%s): finding = %v, want %v", i, step.action, body.Finding, unchanged)
		}
	}
}

func TestDecideFindingNotFound(t *testing.T) {
	f := newFixture(t)
	other := f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`)
	otherPull := f.insertPull(t, f.insertRepo(t, other, "x/y"), 1, "open", "NULL", "NULL")
	otherFinding := f.insertFinding(t, f.insertReview(t, other, otherPull, nil))

	for _, action := range []string{"accept", "dismiss"} {
		t.Run(action, func(t *testing.T) {
			notFound := `{"error": {"code": "not_found", "message": "Finding not found"}}`
			assertJSON(t, f.send(t, http.MethodPost, "/findings/"+uuid.NewString()+"/"+action, ``), http.StatusNotFound, notFound)
			assertJSON(t, f.send(t, http.MethodPost, "/findings/"+otherFinding.String()+"/"+action, ``), http.StatusNotFound, notFound)
			if paths := issuePaths(t, f.send(t, http.MethodPost, "/findings/42/"+action, ``)); !reflect.DeepEqual(paths, []string{"id"}) {
				t.Errorf("issues at %v", paths)
			}
		})
	}
	if n := f.count(t, `SELECT count(*) FROM findings WHERE id = $1 AND accepted_at IS NULL AND dismissed_at IS NULL`, otherFinding); n != 1 {
		t.Error("another workspace's finding was changed")
	}
}

func TestDeleteReview(t *testing.T) {
	f := newFixture(t)
	pull := f.insertPull(t, f.insertRepo(t, f.workspace, "o/n"), 1, "open", "NULL", "NULL")
	run := f.insertRun(t, f.workspace, pull, nil, "done", "2026-09-01")
	review := f.insertReview(t, f.workspace, pull, run)
	f.insertFinding(t, review)
	kept := f.insertReview(t, f.workspace, pull, nil)
	f.insertFinding(t, kept)
	other := f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`)
	otherReview := f.insertReview(t, other, f.insertPull(t, f.insertRepo(t, other, "x/y"), 1, "open", "NULL", "NULL"), nil)

	assertJSON(t, f.send(t, http.MethodDelete, "/reviews/"+review.String(), ``), http.StatusOK, `{"ok": true}`)

	notFound := `{"error": {"code": "not_found", "message": "Review not found"}}`
	for _, id := range []uuid.UUID{review, otherReview, uuid.New()} {
		assertJSON(t, f.send(t, http.MethodDelete, "/reviews/"+id.String(), ``), http.StatusNotFound, notFound)
	}
	tests := []struct {
		what string
		sql  string
		arg  uuid.UUID
		want int
	}{
		{"deleted review", `SELECT count(*) FROM reviews WHERE id = $1`, review, 0},
		{"its findings", `SELECT count(*) FROM findings WHERE review_id = $1`, review, 0},
		{"its run", `SELECT count(*) FROM agent_runs WHERE id = $1`, run, 1},
		{"another review's findings", `SELECT count(*) FROM findings WHERE review_id = $1`, kept, 1},
		{"another workspace's review", `SELECT count(*) FROM reviews WHERE id = $1`, otherReview, 1},
	}
	for _, tt := range tests {
		if n := f.count(t, tt.sql, tt.arg); n != tt.want {
			t.Errorf("%s: %d rows, want %d", tt.what, n, tt.want)
		}
	}
}

func TestDeleteRun(t *testing.T) {
	f := newFixture(t)
	pull := f.insertPull(t, f.insertRepo(t, f.workspace, "o/n"), 1, "open", "NULL", "NULL")
	run := f.insertRun(t, f.workspace, pull, nil, "done", "2026-09-01")
	f.exec(t, `INSERT INTO run_traces (run_id, trace) VALUES ($1, '{}')`, run)
	review := f.insertReview(t, f.workspace, pull, run)
	f.insertFinding(t, review)
	keptRun := f.insertRun(t, f.workspace, pull, nil, "done", "2026-09-02")
	keptReview := f.insertReview(t, f.workspace, pull, keptRun)
	// Another workspace's run, and a review in that workspace pointing at our
	// run's ID: neither is touched.
	other := f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`)
	otherPull := f.insertPull(t, f.insertRepo(t, other, "x/y"), 1, "open", "NULL", "NULL")
	otherRun := f.insertRun(t, other, otherPull, nil, "done", "2026-09-01")
	otherReview := f.insertReview(t, other, otherPull, run)

	assertJSON(t, f.send(t, http.MethodDelete, "/runs/"+run.String(), ``), http.StatusOK, `{"ok": true}`)
	// An unknown run is still 200, as in TS.
	for _, id := range []uuid.UUID{run, otherRun, uuid.New()} {
		assertJSON(t, f.send(t, http.MethodDelete, "/runs/"+id.String(), ``), http.StatusOK, `{"ok": false}`)
	}
	if paths := issuePaths(t, f.send(t, http.MethodDelete, "/runs/42", ``)); !reflect.DeepEqual(paths, []string{"id"}) {
		t.Errorf("issues at %v", paths)
	}

	tests := []struct {
		what string
		sql  string
		arg  uuid.UUID
		want int
	}{
		{"deleted run", `SELECT count(*) FROM agent_runs WHERE id = $1`, run, 0},
		{"its trace", `SELECT count(*) FROM run_traces WHERE run_id = $1`, run, 0},
		{"its review", `SELECT count(*) FROM reviews WHERE id = $1`, review, 0},
		{"its review's findings", `SELECT count(*) FROM findings WHERE review_id = $1`, review, 0},
		{"another run", `SELECT count(*) FROM agent_runs WHERE id = $1`, keptRun, 1},
		{"another run's review", `SELECT count(*) FROM reviews WHERE id = $1`, keptReview, 1},
		{"another workspace's run", `SELECT count(*) FROM agent_runs WHERE id = $1`, otherRun, 1},
		{"another workspace's review", `SELECT count(*) FROM reviews WHERE id = $1`, otherReview, 1},
	}
	for _, tt := range tests {
		if n := f.count(t, tt.sql, tt.arg); n != tt.want {
			t.Errorf("%s: %d rows, want %d", tt.what, n, tt.want)
		}
	}
}
