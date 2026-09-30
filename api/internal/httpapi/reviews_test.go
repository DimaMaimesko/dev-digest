package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestGetIndexState(t *testing.T) {
	f := newFixture(t)
	insertState := func(status, stats string) string {
		repo := f.insertRepo(t, f.workspace, "o/"+uuid.NewString())
		f.exec(t, `INSERT INTO repo_index_state
			(repo_id, last_indexed_sha, indexer_version, status, files_indexed, files_skipped, stats, updated_at)
			VALUES ($1, 'abc123', 2, $2, 312, 4, $3, '2026-09-28 21:59:33.286+00')`, repo, status, stats)
		return repo.String()
	}
	common := func(repo, status string) string {
		return `"repoId": "` + repo + `", "status": "` + status + `", "filesIndexed": 312, "filesSkipped": 4,
			"lastIndexedSha": "abc123", "indexerVersion": 2, "updatedAt": "2026-09-28T21:59:33.286Z"`
	}
	full := insertState("full", `{"durationMs": 1532}`)
	partial := insertState("partial", `{"durationMs": 35.5, "reason": "no_files"}`)
	failed := insertState("failed", `{}`)
	degraded := insertState("degraded", `{"degradedReason": "repo_too_large"}`)
	oddStats := insertState("full", `{"durationMs": "slow", "reason": 7}`)
	notObject := insertState("full", `[1, 2]`)

	tests := []struct{ name, repo, want string }{
		{"full", full, `{` + common(full, "full") + `, "durationMs": 1532}`},
		{"partial works, so it isn't flagged", partial, `{` + common(partial, "partial") + `, "durationMs": 35.5, "reason": "no_files"}`},
		{"failed defaults to index_failed", failed, `{` + common(failed, "failed") + `, "durationMs": 0, "degraded": true, "degradedReason": "index_failed"}`},
		{"degraded with its reason", degraded, `{` + common(degraded, "degraded") + `, "durationMs": 0, "degraded": true, "degradedReason": "repo_too_large"}`},
		{"stats of the wrong type are ignored", oddStats, `{` + common(oddStats, "full") + `, "durationMs": 0}`},
		{"stats that aren't an object", notObject, `{` + common(notObject, "full") + `, "durationMs": 0}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertJSON(t, f.get(t, "/repos/"+tt.repo+"/index-state"), http.StatusOK, tt.want)
		})
	}
}

// A repository with no index, unknown or in another workspace, gets the same
// "no data" answer, so the endpoint reveals nothing about other workspaces.
func TestGetIndexStateNoData(t *testing.T) {
	f := newFixture(t)
	notIndexed := f.insertRepo(t, f.workspace, "o/a").String()
	otherRepo := f.insertRepo(t, f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`), "x/y")
	f.exec(t, `INSERT INTO repo_index_state (repo_id, last_indexed_sha, indexer_version, status)
		VALUES ($1, 'sha', 2, 'full')`, otherRepo)

	for _, repo := range []string{notIndexed, otherRepo.String(), uuid.NewString()} {
		assertJSON(t, f.get(t, "/repos/"+repo+"/index-state"), http.StatusOK, `{
			"repoId": "`+repo+`", "status": "degraded", "filesIndexed": 0, "filesSkipped": 0,
			"durationMs": 0, "reason": "no_data", "lastIndexedSha": "", "indexerVersion": 2,
			"updatedAt": "1970-01-01T00:00:00.000Z", "degraded": true, "degradedReason": "no_data"}`)
	}
}

func TestListReviews(t *testing.T) {
	f := newFixture(t)
	pull := f.insertPull(t, f.insertRepo(t, f.workspace, "o/n"), 1, "open", "NULL", "NULL")
	agent := f.insertAgent(t, f.workspace, "Security", "NULL", "2026-09-01")
	gone := uuid.New() // an agent that was deleted

	older := f.insertID(t, `INSERT INTO reviews (workspace_id, pr_id, agent_id, run_id, kind, verdict, summary, score, model, created_at)
		VALUES ($1, $2, $3, NULL, 'review', 'request_changes', 'A live key.', 65, 'm', '2026-09-01 10:00:00+00') RETURNING id`,
		f.workspace, pull, agent)
	newer := f.insertID(t, `INSERT INTO reviews (workspace_id, pr_id, agent_id, kind, created_at)
		VALUES ($1, $2, $3, 'summary', '2026-09-02 10:00:00+00') RETURNING id`, f.workspace, pull, gone)
	finding := f.insertID(t, `INSERT INTO findings
		(review_id, file, start_line, end_line, severity, category, title, rationale, suggestion, confidence, trifecta_components, accepted_at)
		VALUES ($1, 'src/config.ts', 11, 12, 'CRITICAL', 'security', 'Live key', 'Secret.', 'Use env.', 0.9,
		        '["private_data_access"]', '2026-09-03 10:00:00+00') RETURNING id`, older)
	// Another pull request's review isn't included.
	other := f.insertPull(t, f.insertRepo(t, f.workspace, "o/m"), 2, "open", "NULL", "NULL")
	f.exec(t, `INSERT INTO reviews (workspace_id, pr_id, kind) VALUES ($1, $2, 'review')`, f.workspace, other)

	assertJSON(t, f.get(t, "/pulls/"+pull.String()+"/reviews"), http.StatusOK, `[
		{"id": "`+newer.String()+`", "pr_id": "`+pull.String()+`", "agent_id": "`+gone.String()+`",
		 "run_id": null, "agent_name": null, "kind": "summary", "verdict": null, "summary": null,
		 "score": null, "model": null, "created_at": "2026-09-02T10:00:00.000Z", "findings": []},
		{"id": "`+older.String()+`", "pr_id": "`+pull.String()+`", "agent_id": "`+agent.String()+`",
		 "run_id": null, "agent_name": "Security", "kind": "review", "verdict": "request_changes",
		 "summary": "A live key.", "score": 65, "model": "m", "created_at": "2026-09-01T10:00:00.000Z",
		 "findings": [{
			"id": "`+finding.String()+`", "severity": "CRITICAL", "category": "security", "title": "Live key",
			"file": "src/config.ts", "start_line": 11, "end_line": 12, "rationale": "Secret.",
			"suggestion": "Use env.", "confidence": 0.9, "kind": "finding",
			"trifecta_components": ["private_data_access"], "evidence": null,
			"review_id": "`+older.String()+`", "accepted_at": "2026-09-03T10:00:00.000Z", "dismissed_at": null}]}
	]`)
}

// insertRun adds a review run of pull by agent with the given status.
func (f fixture) insertRun(t *testing.T, workspace, pull uuid.UUID, agent any, status, ranAt string) uuid.UUID {
	t.Helper()
	return f.insertID(t, `INSERT INTO agent_runs (workspace_id, pr_id, agent_id, status, ran_at)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`, workspace, pull, agent, status, ranAt)
}

func TestListRuns(t *testing.T) {
	f := newFixture(t)
	pull := f.insertPull(t, f.insertRepo(t, f.workspace, "o/n"), 1, "open", "NULL", "NULL")
	agent := f.insertAgent(t, f.workspace, "General", "NULL", "2026-09-01")
	done := f.insertID(t, `INSERT INTO agent_runs
		(workspace_id, pr_id, agent_id, provider, model, status, duration_ms, tokens_in, tokens_out,
		 cost_usd, findings_count, grounding, score, blockers, ran_at)
		VALUES ($1, $2, $3, 'openrouter', 'm', 'done', 4200, 900, 150, 0.0123, 1, '1/2 passed', 65, 1,
		        '2026-09-01 10:00:00+00') RETURNING id`, f.workspace, pull, agent)
	failed := f.insertID(t, `INSERT INTO agent_runs (workspace_id, pr_id, agent_id, status, error, ran_at)
		VALUES ($1, $2, NULL, 'failed', 'rate limited', '2026-09-02 10:00:00+00') RETURNING id`, f.workspace, pull)
	running := f.insertRun(t, f.workspace, pull, agent, "running", "2026-09-03 10:00:00+00")
	// Runs of another pull request, or another workspace, aren't included.
	f.insertRun(t, f.workspace, f.insertPull(t, f.insertRepo(t, f.workspace, "o/m"), 2, "open", "NULL", "NULL"), nil, "running", "2026-09-03")
	other := f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`)
	f.insertRun(t, other, pull, nil, "running", "2026-09-03")

	nulls := `"provider": null, "model": null, "duration_ms": null, "tokens_in": null, "tokens_out": null,
		"cost_usd": null, "findings_count": null, "grounding": null, "score": null, "blockers": null`
	assertJSON(t, f.get(t, "/pulls/"+pull.String()+"/runs"), http.StatusOK, `[
		{"run_id": "`+running.String()+`", "agent_id": "`+agent.String()+`", "agent_name": "General",
		 "status": "running", "error": null, "ran_at": "2026-09-03T10:00:00.000Z", `+nulls+`},
		{"run_id": "`+failed.String()+`", "agent_id": null, "agent_name": null,
		 "status": "failed", "error": "rate limited", "ran_at": "2026-09-02T10:00:00.000Z", `+nulls+`},
		{"run_id": "`+done.String()+`", "agent_id": "`+agent.String()+`", "agent_name": "General",
		 "provider": "openrouter", "model": "m", "status": "done", "error": null, "duration_ms": 4200,
		 "tokens_in": 900, "tokens_out": 150, "cost_usd": 0.0123, "findings_count": 1, "grounding": "1/2 passed",
		 "ran_at": "2026-09-01T10:00:00.000Z", "score": 65, "blockers": 1}
	]`)

	assertJSON(t, f.get(t, "/pulls/"+pull.String()+"/runs/active"), http.StatusOK, `[
		{"run_id": "`+running.String()+`", "agent_id": "`+agent.String()+`", "agent_name": "General",
		 "ran_at": "2026-09-03T10:00:00.000Z"}
	]`)
}

func TestGetRunTrace(t *testing.T) {
	f := newFixture(t)
	pull := f.insertPull(t, f.insertRepo(t, f.workspace, "o/n"), 1, "open", "NULL", "NULL")
	run := f.insertRun(t, f.workspace, pull, nil, "done", "2026-09-01")
	trace := `{"run_id": "x", "prompt": {"system": "s", "user": "u"}, "log": [{"t": "00.31", "kind": "info", "msg": "Loading PR diff"}], "stats": {"tokens_in": 9}}`
	f.exec(t, `INSERT INTO run_traces (run_id, trace) VALUES ($1, $2)`, run, trace)
	other := f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`)
	otherRun := f.insertRun(t, other, pull, nil, "done", "2026-09-01")
	f.exec(t, `INSERT INTO run_traces (run_id, trace) VALUES ($1, '{}')`, otherRun)

	// Sent as stored.
	assertJSON(t, f.get(t, "/runs/"+run.String()+"/trace"), http.StatusOK, trace)

	notFound := `{"error": {"code": "not_found", "message": "Run trace not found"}}`
	// The TS server returned another workspace's trace.
	assertJSON(t, f.get(t, "/runs/"+otherRun.String()+"/trace"), http.StatusNotFound, notFound)
	assertJSON(t, f.get(t, "/runs/"+uuid.NewString()+"/trace"), http.StatusNotFound, notFound)
}

func TestReviewReadsNotFound(t *testing.T) {
	f := newFixture(t)
	missing := uuid.NewString()
	assertJSON(t, f.get(t, "/pulls/"+missing+"/reviews"), http.StatusNotFound,
		`{"error": {"code": "not_found", "message": "Pull request not found"}}`)
	// Runs of an unknown pull request are an empty list, as in TS.
	assertJSON(t, f.get(t, "/pulls/"+missing+"/runs"), http.StatusOK, `[]`)
	assertJSON(t, f.get(t, "/pulls/"+missing+"/runs/active"), http.StatusOK, `[]`)
}
