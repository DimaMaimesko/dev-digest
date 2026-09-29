// Package httpapi serves the DevDigest HTTP API that the web app calls. Its
// routes and JSON match the TypeScript server's, so the web app works
// against either.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

// Server handles the API's requests.
type Server struct {
	db      *pgxpool.Pool
	queries *postgres.Queries
	log     *slog.Logger
	// workspace is the one local workspace every request works in, like the
	// TS server's LocalNoAuthProvider. Real auth would resolve it per request.
	workspace uuid.UUID
	webOrigin string // the web app's origin, the only one CORS allows
}

// New returns a Server that works in workspace and allows the web app at
// webOrigin, such as "http://localhost:3000", to call it.
func New(db *pgxpool.Pool, workspace uuid.UUID, webOrigin string, log *slog.Logger) *Server {
	return &Server{
		db:        db,
		queries:   postgres.New(db),
		log:       log,
		workspace: workspace,
		webOrigin: webOrigin,
	}
}

// Handler returns the API's routes, wrapped in the middleware every request
// goes through.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /health/ready", s.ready)
	mux.HandleFunc("GET /repos", s.listRepos)
	mux.HandleFunc("/", notFound)

	return logRequests(s.log, recoverPanics(s.log, cors(s.webOrigin, securityHeaders(mux))))
}

// writeJSON sends v as the JSON body of a response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) // the status is sent; a write error can't be reported
}

// errorBody is the TS server's error envelope:
// {"error": {"code": "not_found", "message": "Repo not found"}}.
type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var body errorBody
	body.Error.Code = code
	body.Error.Message = message
	writeJSON(w, status, body)
}

// internalError logs err and sends a 500 without its details.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal_error", "Internal error")
}

func notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "Route "+r.Method+":"+r.URL.Path+" not found")
}

// jsTime formats t the way JavaScript's Date.toISOString does, as the web app
// expects: UTC with milliseconds, such as "2026-09-28T22:52:39.872Z".
func jsTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}
