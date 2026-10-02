package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
	"github.com/DimaMaimesko/dev-digest/api/internal/runner"
)

// startedRunJSON is a run a review request started.
type startedRunJSON struct {
	RunID     string `json:"run_id"`
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
}

// startReview answers POST /pulls/{id}/review: {"agentId": "…"} reviews the
// pull request with one agent, {"all": true} with every agent that is on.
// It answers at once with the runs, which go on in the background.
func (s *Server) startReview(w http.ResponseWriter, r *http.Request) {
	prID, ok := pathID(w, r)
	if !ok {
		return
	}
	body := map[string]json.RawMessage{}
	// A request with no body at all asks for nothing, as in TS.
	if r.ContentLength != 0 || r.Header.Get("Content-Type") != "" {
		if !readJSON(w, r, &body) {
			return
		}
	}
	f := fields{body: body}
	agentID, all := f.str("agentId", false, nil), f.boolean("all")
	if len(f.issues) > 0 {
		invalid(w, f.issues)
		return
	}

	var agents []postgres.Agent
	switch {
	case all != nil && *all:
		var err error
		if agents, err = s.queries.EnabledAgents(r.Context(), s.workspace); err != nil {
			s.internalError(w, r, err)
			return
		}
	case agentID != nil && *agentID != "":
		id, err := uuid.Parse(*agentID)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "Agent not found")
			return
		}
		a, err := s.queries.GetAgent(r.Context(), postgres.GetAgentParams{WorkspaceID: s.workspace, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Agent not found")
			return
		}
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		agents = []postgres.Agent{a}
	default:
		writeError(w, http.StatusBadRequest, "invalid_run_request", "Provide agentId or all:true")
		return
	}

	pull, repo, ok := s.pullAndRepo(w, r)
	if !ok {
		return
	}
	started, err := s.runner.Start(r.Context(), pull, repo, agents)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	runs := make([]startedRunJSON, len(started))
	for i, st := range started {
		runs[i] = startedRunJSON{RunID: st.RunID.String(), AgentID: st.AgentID.String(), AgentName: st.AgentName}
	}
	writeJSON(w, http.StatusOK, struct {
		PRID    string           `json:"pr_id"`
		Runs    []startedRunJSON `json:"runs"`
		Reviews []any            `json:"reviews"` // always empty: the reviews come later
	}{prID.String(), runs, []any{}})
}

// runEvents answers GET /runs/{id}/events: a stream of server-sent events
// with the run's live log, the earlier events first. It ends when the run
// does, when the server shuts down (CloseStreams), or at once for a run this
// server doesn't know.
func (s *Server) runEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache,no-transform")
	h.Set("X-No-Compression", "1")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	io.WriteString(w, "retry: 3000\n\n") // how long a browser waits to reconnect, as in TS
	rc.Flush()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(s.streams, cancel)
	defer stop()

	s.runner.Follow(ctx, id, func(e runner.Event) error {
		data, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Kind, data); err != nil {
			return err
		}
		return rc.Flush()
	})
}

// cancelRun answers POST /runs/{id}/cancel: it stops the run if it is still
// running. It answers {"ok": true} either way, as in TS.
func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.runner.Cancel(r.Context(), s.workspace, id); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
