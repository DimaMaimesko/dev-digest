package httpapi

import (
	"context"
	"net/http"
	"time"
)

// health answers GET /health: the process is up. It doesn't touch the
// database.
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ready answers GET /health/ready: the server can reach the database. It
// answers 503, not 500, so an orchestrator reads it as "not ready yet".
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		s.log.Warn("readiness check failed: database unreachable", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]bool{"ready": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ready": true})
}
