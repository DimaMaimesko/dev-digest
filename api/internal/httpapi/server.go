// Package httpapi serves the DevDigest HTTP API that the web app calls. Its
// routes and JSON are those of the TypeScript server it replaced, which the
// web app was built against.
package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/agents"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
	"github.com/DimaMaimesko/dev-digest/api/internal/pulls"
	"github.com/DimaMaimesko/dev-digest/api/internal/repos"
	"github.com/DimaMaimesko/dev-digest/api/internal/runner"
	"github.com/DimaMaimesko/dev-digest/api/internal/secrets"
)

// Server handles the API's requests.
type Server struct {
	db        *pgxpool.Pool
	queries   *postgres.Queries
	log       *slog.Logger
	workspace uuid.UUID
	user      uuid.UUID
	webOrigin string
	cloneDir  string
	secrets   *secrets.Store
	agents    *agents.Store
	pulls     *pulls.Store
	githubAPI string
	modelAPIs ModelAPIs
	runner    *runner.Runner
	repos     *repos.Store
}

// Config is what a Server needs.
type Config struct {
	DB *pgxpool.Pool
	// Workspace is the one local workspace every request works in, like the
	// TS server's LocalNoAuthProvider. Real auth would resolve it per request.
	Workspace uuid.UUID
	// User is the one local user, whose preferences the API saves.
	User      uuid.UUID
	WebOrigin string // the web app's origin, such as "http://localhost:3000"; the only one CORS allows
	CloneDir  string // where repositories are cloned
	Secrets   *secrets.Store
	Log       *slog.Logger
	// GitHubAPI is GitHub's API base URL, such as github.DefaultURL. When it
	// is empty, pull requests are never synced from GitHub.
	GitHubAPI string
	// ModelAPIs are the model providers' APIs, for the model lists.
	ModelAPIs ModelAPIs
	// Runner runs reviews. Without one, the review, events and cancel
	// routes answer 404.
	Runner *runner.Runner
	// Repos adds, refreshes and removes repositories. Without it, those
	// routes answer 404.
	Repos *repos.Store
}

// New returns a Server.
func New(cfg Config) *Server {
	return &Server{
		db:        cfg.DB,
		queries:   postgres.New(cfg.DB),
		log:       cfg.Log,
		workspace: cfg.Workspace,
		user:      cfg.User,
		webOrigin: cfg.WebOrigin,
		cloneDir:  cfg.CloneDir,
		secrets:   cfg.Secrets,
		agents:    agents.NewStore(cfg.DB),
		pulls:     pulls.NewStore(cfg.DB),
		githubAPI: cfg.GitHubAPI,
		modelAPIs: cfg.ModelAPIs,
		runner:    cfg.Runner,
		repos:     cfg.Repos,
	}
}

// Handler returns the API's routes, wrapped in the middleware every request
// goes through.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /health/ready", s.ready)
	mux.HandleFunc("GET /repos", s.listRepos)
	mux.HandleFunc("GET /repos/{id}/pulls", s.listPulls)
	mux.HandleFunc("POST /repos/{id}/poll", s.pollRepo)
	mux.HandleFunc("GET /pulls/{id}", s.getPull)
	mux.HandleFunc("GET /pulls/{id}/comments", s.listComments)
	mux.HandleFunc("POST /pulls/{id}/comments", s.createComment)
	mux.HandleFunc("GET /agents", s.listAgents)
	mux.HandleFunc("GET /agents/{id}", s.getAgent)
	mux.HandleFunc("GET /agents/{id}/versions", s.listAgentVersions)
	mux.HandleFunc("GET /agents/{id}/versions/{version}", s.getAgentVersion)
	mux.HandleFunc("GET /agents/{id}/skills", s.listAgentSkills)
	mux.HandleFunc("POST /agents", s.createAgent)
	mux.HandleFunc("PUT /agents/{id}", s.updateAgent)
	mux.HandleFunc("DELETE /agents/{id}", s.deleteAgent)
	mux.HandleFunc("POST /agents/{id}/skills", s.changeAgentSkills)
	mux.HandleFunc("GET /agents/{id}/models", s.listAgentModels)
	mux.HandleFunc("GET /providers/{id}/models", s.listProviderModels)
	mux.HandleFunc("GET /settings", s.getSettings)
	mux.HandleFunc("PUT /settings", s.putSettings)
	mux.HandleFunc("POST /settings/test-connection", s.testConnection)
	mux.HandleFunc("GET /settings/secrets-status", s.secretsStatus)
	mux.HandleFunc("GET /workspace", s.getWorkspace)
	mux.HandleFunc("GET /repos/{id}/index-state", s.getIndexState)
	mux.HandleFunc("GET /pulls/{id}/reviews", s.listReviews)
	mux.HandleFunc("GET /pulls/{id}/runs", s.listRuns)
	mux.HandleFunc("GET /pulls/{id}/runs/active", s.listActiveRuns)
	mux.HandleFunc("GET /runs/{id}/trace", s.getRunTrace)
	if s.repos != nil {
		mux.HandleFunc("POST /repos", s.addRepo)
		mux.HandleFunc("POST /repos/{id}/refresh", s.refreshRepo)
		mux.HandleFunc("DELETE /repos/{id}", s.deleteRepo)
		mux.HandleFunc("POST /repos/{id}/resync", s.resyncRepo)
	}
	if s.runner != nil {
		mux.HandleFunc("POST /pulls/{id}/review", s.startReview)
		mux.HandleFunc("GET /runs/{id}/events", s.runEvents)
		mux.HandleFunc("POST /runs/{id}/cancel", s.cancelRun)
	}
	mux.HandleFunc("DELETE /reviews/{id}", s.deleteReview)
	mux.HandleFunc("DELETE /runs/{id}", s.deleteRun)
	mux.HandleFunc("POST /findings/{id}/accept", s.acceptFinding)
	mux.HandleFunc("POST /findings/{id}/dismiss", s.dismissFinding)
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
		Details any    `json:"details,omitempty"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeErrorDetails(w, status, code, message, nil)
}

func writeErrorDetails(w http.ResponseWriter, status int, code, message string, details any) {
	var body errorBody
	body.Error.Code = code
	body.Error.Message = message
	body.Error.Details = details
	writeJSON(w, status, body)
}

// pathID reads the {id} path value as a UUID in its standard 36-character
// form, which is all the TS server accepts. For anything else it answers 422
// and returns false.
func pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := r.PathValue("id")
	id, err := uuid.Parse(raw)
	if err != nil || len(raw) != 36 { // uuid.Parse also takes "urn:uuid:…" and no-hyphen forms
		invalidParam(w, "id", "Invalid uuid")
		return uuid.Nil, false
	}
	return id, true
}

// pathPositiveInt reads a path value as a whole number above zero. For
// anything else it answers 422 and returns false.
func pathPositiveInt(w http.ResponseWriter, r *http.Request, name string) (int32, bool) {
	n, err := strconv.ParseInt(r.PathValue(name), 10, 32)
	if err != nil || n <= 0 {
		invalidParam(w, name, "Expected a positive whole number")
		return 0, false
	}
	return int32(n), true
}

// issue is one problem with a request: where it is, such as ["theme"] or
// ["feature_models", "onboarding", "model"], and what is wrong. A 422
// response lists them as its details.
type issue struct {
	Path    []string `json:"path"`
	Message string   `json:"message"`
}

// invalid answers 422 with the issues found.
func invalid(w http.ResponseWriter, issues []issue) {
	writeErrorDetails(w, http.StatusUnprocessableEntity, "validation_error", "Request validation failed", issues)
}

// invalidParam answers 422 for a path value that isn't valid.
func invalidParam(w http.ResponseWriter, name, message string) {
	invalid(w, []issue{{Path: []string{name}, Message: message}})
}

// maxBody is the largest request body the API reads: 1 MB, as in the TS
// server.
const maxBody = 1 << 20

// readJSON decodes the request's JSON body into v, which must be a pointer to
// a map or struct: every request body is a JSON object. When the body isn't
// one, it answers with an error and returns false.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Send the body as JSON, with Content-Type: application/json")
		return false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "The request body is larger than 1 MB")
		return false
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "The request body couldn't be read")
		return false
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "The request body is empty")
		return false
	}
	var typeErr *json.UnmarshalTypeError
	switch err := json.Unmarshal(body, v); {
	case errors.As(err, &typeErr) || bytes.Equal(body, []byte("null")):
		// Valid JSON of the wrong shape, such as an array.
		invalid(w, []issue{{Path: []string{}, Message: "Expected a JSON object"}})
		return false
	case err != nil:
		writeError(w, http.StatusBadRequest, "invalid_json", "The request body isn't valid JSON")
		return false
	}
	return true
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
