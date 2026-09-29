package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

// indexerVersion is the TS indexer's version (INDEXER_VERSION in
// server/src/modules/repo-intel/constants.ts), reported when a repository has
// no index yet.
const indexerVersion = 2

// indexStateJSON is how far the code index of a repository got, which drives
// the "Indexed" badge (IndexState in server/src/modules/repo-intel/types.ts).
// Its keys are camelCase, as the TS server sends them.
type indexStateJSON struct {
	RepoID         string  `json:"repoId"`
	Status         string  `json:"status"` // full, partial, degraded or failed
	FilesIndexed   int32   `json:"filesIndexed"`
	FilesSkipped   int32   `json:"filesSkipped"`
	DurationMs     float64 `json:"durationMs"`
	Reason         *string `json:"reason,omitempty"`
	LastIndexedSha string  `json:"lastIndexedSha"`
	IndexerVersion int32   `json:"indexerVersion"`
	UpdatedAt      string  `json:"updatedAt"`
	// Degraded and DegradedReason are sent only for a degraded or failed index.
	Degraded       bool   `json:"degraded,omitempty"`
	DegradedReason string `json:"degradedReason,omitempty"`
}

// getIndexState answers GET /repos/{id}/index-state. It always answers 200:
// a repository with no index, or not in the workspace, gets a "degraded,
// no_data" state, as in the TS server.
func (s *Server) getIndexState(w http.ResponseWriter, r *http.Request) {
	repoID, ok := pathID(w, r)
	if !ok {
		return
	}
	row, err := s.queries.GetIndexState(r.Context(), postgres.GetIndexStateParams{WorkspaceID: s.workspace, RepoID: repoID})
	if errors.Is(err, pgx.ErrNoRows) {
		noData := "no_data"
		writeJSON(w, http.StatusOK, indexStateJSON{
			RepoID:         repoID.String(),
			Status:         "degraded",
			Reason:         &noData,
			IndexerVersion: indexerVersion,
			UpdatedAt:      jsTime(time.Unix(0, 0)),
			Degraded:       true,
			DegradedReason: noData,
		})
		return
	}
	if err != nil {
		// The TS server reported every error as "no_data", so a database
		// outage looked like a repository that was never indexed.
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toIndexStateJSON(row))
}

func toIndexStateJSON(row postgres.RepoIndexState) indexStateJSON {
	out := indexStateJSON{
		RepoID:         row.RepoID.String(),
		Status:         row.Status,
		FilesIndexed:   row.FilesIndexed,
		FilesSkipped:   row.FilesSkipped,
		LastIndexedSha: row.LastIndexedSha,
		IndexerVersion: row.IndexerVersion,
		UpdatedAt:      jsTime(row.UpdatedAt),
	}
	// stats is free-form JSON written by the indexer. Like the TS server, use
	// a value only when it has the expected type.
	var stats map[string]any
	_ = json.Unmarshal(row.Stats, &stats) // not an object: no stats
	if d, ok := stats["durationMs"].(float64); ok {
		out.DurationMs = d
	}
	if reason, ok := stats["reason"].(string); ok {
		out.Reason = &reason
	}
	// A partial index still works; only a degraded or failed one is flagged.
	if row.Status == "degraded" || row.Status == "failed" {
		out.Degraded = true
		out.DegradedReason = "index_failed"
		if reason, ok := stats["degradedReason"].(string); ok {
			out.DegradedReason = reason
		}
	}
	return out
}
