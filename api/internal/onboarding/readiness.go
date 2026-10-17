package onboarding

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DimaMaimesko/dev-digest/api/internal/git"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

// readiness is what Start and Get need to know about a repository's index:
// whether it has a completed index to generate a tour from, or one is in
// progress.
type readiness struct {
	ready bool
	// indexing is set only when !ready: a clone or index job is active
	// for this repository (Approach -> "Indexing in progress").
	indexing bool

	// The following are set only when ready.
	dir            string // the clone's directory
	commitSha      string
	indexStatus    string // "full" or "partial"
	filesIndexed   int32
	codeIndexEmpty bool
}

// checkReadiness reports repoID's (scoped to workspace) index readiness.
//
// A "completed index" (Approach -> "What counts as a completed index",
// AC-3/AC-4) is all of:
//   - a repo_index_state row with status "full" or "partial" and a
//     non-empty last_indexed_sha
//   - repos.clone_path set, and that directory has a .git
//   - git.HasCommit(last_indexed_sha) is true in that clone
//
// A degraded or failed index, or no index at all, answers like "not
// indexed".
func (s *Store) checkReadiness(ctx context.Context, workspace, repoID uuid.UUID) (readiness, error) {
	row, err := s.q.OnboardingRepoState(ctx, postgres.OnboardingRepoStateParams{WorkspaceID: workspace, ID: repoID})
	if errors.Is(err, pgx.ErrNoRows) {
		return readiness{}, ErrNotFound
	}
	if err != nil {
		return readiness{}, err
	}

	var r readiness
	if row.ClonePath != nil && row.LastIndexedSha != nil && *row.LastIndexedSha != "" &&
		row.IndexStatus != nil && (*row.IndexStatus == "full" || *row.IndexStatus == "partial") {
		if _, statErr := os.Stat(filepath.Join(*row.ClonePath, ".git")); statErr == nil {
			if git.HasCommit(ctx, *row.ClonePath, *row.LastIndexedSha) {
				r.ready = true
				r.dir = *row.ClonePath
				r.commitSha = *row.LastIndexedSha
				r.indexStatus = *row.IndexStatus
				if row.FilesIndexed != nil {
					r.filesIndexed = *row.FilesIndexed
				}
				// Q5: a Go-only repository indexes with files_indexed = 0,
				// status partial; the plan treats that as a completed
				// index, with the header noting the code index is empty
				// instead of a generic partial-index note.
				r.codeIndexEmpty = r.filesIndexed == 0
			}
		}
	}
	if !r.ready {
		n, err := s.q.CountActiveIndexJobs(ctx, repoID.String())
		if err != nil {
			return readiness{}, err
		}
		r.indexing = n > 0
	}
	return r, nil
}

// indexReadiness renders r as the wire IndexReadiness.
func indexReadiness(r readiness) IndexReadiness {
	switch {
	case r.ready:
		sha := r.commitSha
		return IndexReadiness{
			State: "ready", CommitSha: &sha, FilesIndexed: r.filesIndexed,
			Partial: r.indexStatus == "partial", CodeIndexEmpty: r.codeIndexEmpty,
		}
	case r.indexing:
		return IndexReadiness{State: "indexing"}
	default:
		return IndexReadiness{State: "not_indexed"}
	}
}
