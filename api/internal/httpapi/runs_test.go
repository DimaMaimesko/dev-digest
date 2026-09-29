package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DimaMaimesko/dev-digest/api/internal/review"
	"github.com/DimaMaimesko/dev-digest/api/internal/runner"
)

// fakeModel answers every review with no findings, after gate closes when
// there is one.
type fakeModel struct{ gate chan struct{} }

func (m fakeModel) CompleteJSON(ctx context.Context, _ review.JSONRequest) (review.JSONResponse, error) {
	if m.gate != nil {
		select {
		case <-m.gate:
		case <-ctx.Done():
			return review.JSONResponse{}, ctx.Err()
		}
	}
	return review.JSONResponse{Text: `{"verdict": "approve", "summary": "Fine.", "score": 90, "findings": []}`}, nil
}

// withRunner gives the fixture a runner whose model is m, and a pull
// request #7 to review.
func withRunner(t *testing.T, m fakeModel) (fixture, uuid.UUID) {
	t.Helper()
	f := newFixture(t)
	f.runner = runner.New(runner.Config{
		DB:        f.db,
		LLM:       func(string) (review.LLM, error) { return m, nil },
		CloneDir:  t.TempDir(),
		RepoIntel: true,
		Log:       quiet,
	})
	t.Cleanup(f.runner.Close)
	pull := f.insertPull(t, f.insertRepo(t, f.workspace, "o/n"), 7, "open", "NULL", "NULL")
	f.exec(t, `INSERT INTO pr_files (pr_id, path, patch) VALUES ($1, 'a.go', '@@ -1 +1,2 @@
 a
+b')`, pull)
	return f, pull
}

func TestStartReview(t *testing.T) {
	f, pull := withRunner(t, fakeModel{})
	first := f.insertAgent(t, f.workspace, "First", "NULL", "2026-09-01")
	f.insertAgent(t, f.workspace, "Second", "NULL", "2026-09-02")
	off := f.insertAgent(t, f.workspace, "Off", "NULL", "2026-09-03")
	f.exec(t, `UPDATE agents SET enabled = false WHERE id = $1`, off)

	type started struct {
		PRID string `json:"pr_id"`
		Runs []struct {
			RunID     string `json:"run_id"`
			AgentID   string `json:"agent_id"`
			AgentName string `json:"agent_name"`
		}
		Reviews []any
	}
	path := "/pulls/" + pull.String() + "/review"
	tests := []struct {
		name, body string
		agents     []string
	}{
		{"one agent", `{"agentId": "` + first.String() + `"}`, []string{"First"}},
		{"one agent, even if it is off", `{"agentId": "` + off.String() + `", "all": false}`, []string{"Off"}},
		{"all the agents that are on", `{"all": true, "agentId": "` + off.String() + `"}`, []string{"First", "Second"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got started
			decode(t, f.send(t, http.MethodPost, path, tt.body), http.StatusOK, &got)
			if got.PRID != pull.String() || got.Reviews == nil || len(got.Reviews) != 0 || len(got.Runs) != len(tt.agents) {
				t.Fatalf("answer = %+v", got)
			}
			for i, name := range tt.agents {
				if got.Runs[i].AgentName != name {
					t.Errorf("run %d is %s's, want %s's", i, got.Runs[i].AgentName, name)
				}
			}
			f.runner.Wait()
			for _, r := range got.Runs {
				var status string
				var reviews int
				f.db.QueryRow(context.Background(), `SELECT status, (SELECT count(*) FROM reviews WHERE run_id = $1) FROM agent_runs WHERE id = $1`,
					r.RunID).Scan(&status, &reviews)
				if status != "done" || reviews != 1 {
					t.Errorf("run of %s: %s with %d review(s)", r.AgentName, status, reviews)
				}
			}
		})
	}
}

func TestStartReviewInvalid(t *testing.T) {
	f, pull := withRunner(t, fakeModel{})
	agent := f.insertAgent(t, f.workspace, "G", "NULL", "2026-09-01")
	other := f.insertAgent(t, f.insertID(t, `INSERT INTO workspaces (name) VALUES ('other') RETURNING id`), "G", "NULL", "2026-09-01")
	path := "/pulls/" + pull.String() + "/review"
	noAgent := `{"error": {"code": "not_found", "message": "Agent not found"}}`
	neither := `{"error": {"code": "invalid_run_request", "message": "Provide agentId or all:true"}}`
	tests := []struct {
		name, path, body string
		status           int
		want             string // the JSON answer, or the issue paths of a 422
	}{
		{"neither", path, `{}`, http.StatusBadRequest, neither},
		{"all false", path, `{"all": false}`, http.StatusBadRequest, neither},
		{"an empty agentId", path, `{"agentId": ""}`, http.StatusBadRequest, neither},
		{"not a UUID", path, `{"agentId": "x"}`, http.StatusNotFound, noAgent},
		{"an unknown agent", path, `{"agentId": "` + uuid.NewString() + `"}`, http.StatusNotFound, noAgent},
		{"another workspace's agent", path, `{"agentId": "` + other.String() + `"}`, http.StatusNotFound, noAgent},
		{"an unknown pull request", "/pulls/" + uuid.NewString() + "/review", `{"agentId": "` + agent.String() + `"}`, http.StatusNotFound,
			`{"error": {"code": "not_found", "message": "Pull request not found"}}`},
		{"null agentId", path, `{"agentId": null}`, http.StatusUnprocessableEntity, "agentId"},
		{"all not a boolean", path, `{"all": "yes"}`, http.StatusUnprocessableEntity, "all"},
		{"a bad pull request ID", "/pulls/42/review", `{}`, http.StatusUnprocessableEntity, "id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := f.send(t, http.MethodPost, tt.path, tt.body)
			if tt.status == http.StatusUnprocessableEntity {
				if paths := issuePaths(t, res); strings.Join(paths, ",") != tt.want {
					t.Errorf("issues at %v", paths)
				}
				return
			}
			assertJSON(t, res, tt.status, tt.want)
		})
	}

	// No body at all asks for nothing, as in TS.
	req := httptest.NewRequest(http.MethodPost, path, nil)
	assertJSON(t, f.serve(req), http.StatusBadRequest, neither)

	var runs int
	f.db.QueryRow(context.Background(), `SELECT count(*) FROM agent_runs`).Scan(&runs)
	if runs != 0 {
		t.Errorf("%d runs started", runs)
	}
}

func TestRunEvents(t *testing.T) {
	f, pull := withRunner(t, fakeModel{})
	agent := f.insertAgent(t, f.workspace, "G", "NULL", "2026-09-01")
	var got struct {
		Runs []struct {
			RunID string `json:"run_id"`
		}
	}
	decode(t, f.send(t, http.MethodPost, "/pulls/"+pull.String()+"/review", `{"agentId": "`+agent.String()+`"}`), http.StatusOK, &got)
	run := got.Runs[0].RunID
	f.runner.Wait()

	// A finished run's log is replayed, then the stream ends.
	res := f.get(t, "/runs/"+run+"/events")
	body, _ := io.ReadAll(res.Body)
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream; charset=utf-8" || res.Header.Get("Cache-Control") != "no-cache,no-transform" {
		t.Errorf("headers: %v", res.Header)
	}
	frames := strings.Split(strings.TrimSuffix(string(body), "\n\n"), "\n\n")
	if frames[0] != "retry: 3000" || len(frames) < 5 {
		t.Fatalf("stream:\n%s", body)
	}
	for i, frame := range frames[1:] {
		lines := strings.Split(frame, "\n")
		var e runner.Event
		if len(lines) != 3 || lines[0] != "id: "+strconv.Itoa(i+1) || !strings.HasPrefix(lines[1], "event: ") ||
			json.Unmarshal([]byte(strings.TrimPrefix(lines[2], "data: ")), &e) != nil ||
			e.Seq != i+1 || e.RunID != run || "event: "+e.Kind != lines[1] {
			t.Errorf("frame %d:\n%s", i+1, frame)
		}
	}
	if !strings.Contains(frames[1], `"msg":"Loading PR diff…"`) || !strings.Contains(frames[len(frames)-1], "Run complete; trace persisted") {
		t.Errorf("first and last frames:\n%s\n%s", frames[1], frames[len(frames)-1])
	}

	// A run this server doesn't know ends at once; TS kept it open forever.
	body, _ = io.ReadAll(f.get(t, "/runs/"+uuid.NewString()+"/events").Body)
	if string(body) != "retry: 3000\n\n" {
		t.Errorf("unknown run: %q", body)
	}
	if paths := issuePaths(t, f.get(t, "/runs/42/events")); len(paths) != 1 {
		t.Errorf("issues at %v", paths)
	}
}

func TestCancelRunRoute(t *testing.T) {
	m := fakeModel{gate: make(chan struct{})}
	f, pull := withRunner(t, m)
	agent := f.insertAgent(t, f.workspace, "G", "NULL", "2026-09-01")
	var got struct {
		Runs []struct {
			RunID string `json:"run_id"`
		}
	}
	decode(t, f.send(t, http.MethodPost, "/pulls/"+pull.String()+"/review", `{"agentId": "`+agent.String()+`"}`), http.StatusOK, &got)
	run := got.Runs[0].RunID

	for _, id := range []string{run, uuid.NewString()} {
		assertJSON(t, f.send(t, http.MethodPost, "/runs/"+id+"/cancel", ``), http.StatusOK, `{"ok": true}`)
	}
	f.runner.Wait()
	var status string
	f.db.QueryRow(context.Background(), `SELECT status FROM agent_runs WHERE id = $1`, run).Scan(&status)
	if status != "cancelled" {
		t.Errorf("status = %s", status)
	}
	if paths := issuePaths(t, f.send(t, http.MethodPost, "/runs/42/cancel", ``)); len(paths) != 1 {
		t.Errorf("issues at %v", paths)
	}
}

// Without a runner the routes aren't served here: the TS server keeps them.
func TestReviewRoutesNeedARunner(t *testing.T) {
	f := newFixture(t)
	id := uuid.NewString()
	for _, r := range []struct{ method, path string }{
		{http.MethodPost, "/pulls/" + id + "/review"}, {http.MethodGet, "/runs/" + id + "/events"}, {http.MethodPost, "/runs/" + id + "/cancel"},
	} {
		if res := f.send(t, r.method, r.path, `{}`); res.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s: %d", r.method, r.path, res.StatusCode)
		}
	}
}
