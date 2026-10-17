package onboarding

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/pgtest"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
	"github.com/DimaMaimesko/dev-digest/api/internal/tour"
)

// jobFixture is a minimal Store, backed by a real database, for testing
// finish directly: it needs no clone, no LLM and no jobs.Runner.
type jobFixture struct {
	db        *pgxpool.Pool
	q         *postgres.Queries
	store     *Store
	workspace uuid.UUID
	repoID    uuid.UUID
}

func newJobFixture(t *testing.T) *jobFixture {
	t.Helper()
	db := pgtest.New(t)
	ctx := context.Background()

	var workspace uuid.UUID
	if err := db.QueryRow(ctx, `INSERT INTO workspaces (name) VALUES ('w') RETURNING id`).Scan(&workspace); err != nil {
		t.Fatal(err)
	}
	var repoID uuid.UUID
	if err := db.QueryRow(ctx, `INSERT INTO repos (workspace_id, owner, name, full_name)
		VALUES ($1, 'acme', 'demo', 'acme/demo') RETURNING id`, workspace).Scan(&repoID); err != nil {
		t.Fatal(err)
	}

	store := New(Config{DB: db, Log: slog.New(slog.DiscardHandler)})
	return &jobFixture{db: db, q: postgres.New(db), store: store, workspace: workspace, repoID: repoID}
}

// insertRunningRun inserts a running onboarding_runs row for the fixture's
// repository, so finish has a live row to end.
func (f *jobFixture) insertRunningRun(t *testing.T) postgres.OnboardingRun {
	t.Helper()
	ctx := context.Background()
	run, err := f.q.InsertOnboardingRun(ctx, postgres.InsertOnboardingRunParams{
		RepoID: f.repoID, WorkspaceID: f.workspace, CommitSha: "abc123", FilesIndexed: 3,
		IndexStatus: "full", CodeIndexEmpty: false, Provider: "openrouter", Model: "m",
		Sections: []byte(`[]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.q.StartOnboardingRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	return run
}

func (f *jobFixture) runStatus(t *testing.T, id uuid.UUID) (status string, errMsg *string) {
	t.Helper()
	if err := f.db.QueryRow(context.Background(), `SELECT status, error FROM onboarding_runs WHERE id = $1`, id).Scan(&status, &errMsg); err != nil {
		t.Fatal(err)
	}
	return status, errMsg
}

func (f *jobFixture) tourExists(t *testing.T) bool {
	t.Helper()
	var n int
	if err := f.db.QueryRow(context.Background(), `SELECT count(*) FROM onboarding WHERE repo_id = $1`, f.repoID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

// TestFinish_JobDeadlineExceededIsNotAServerStop proves the fix for the
// "job cap treated as server stop" finding: a ctx whose own 2-minute job
// slot has timed out (DeadlineExceeded) must still store the sections that
// finished before the cap, not be treated the same as a shutdown cancel
// (which discards everything and keeps only the previous tour, AC-11).
func TestFinish_JobDeadlineExceededIsNotAServerStop(t *testing.T) {
	f := newJobFixture(t)
	run := f.insertRunningRun(t)

	// A context whose deadline has already passed: ctx.Err() ==
	// DeadlineExceeded, the shape runJob sees when the 2-minute job slot
	// (jobs.Runner's timeout) ends a generation (AC-47).
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Millisecond))
	defer cancel()
	if ctx.Err() == nil {
		t.Fatal("test setup: ctx should already be past its deadline")
	}

	result := tour.Result{}
	result.Sections[0] = tour.SectionResult{Kind: tour.KindArchitecture, Status: tour.StatusDone, Content: tour.ArchitectureContent{}}
	for i, k := range tour.Kinds {
		if i == 0 {
			continue
		}
		result.Sections[i] = tour.SectionResult{Kind: k, Status: tour.StatusFailed, Reason: "timed out"}
	}

	if err := f.store.finish(ctx, run, result); err != nil {
		t.Fatalf("finish: %v", err)
	}

	status, errMsg := f.runStatus(t, run.ID)
	if status != "done" {
		t.Fatalf("status = %s, errMsg = %v, want done (a deadline-exceeded job cap is not a server stop)", status, errMsg)
	}
	if !f.tourExists(t) {
		t.Fatal("no tour row: the section that finished before the job cap was discarded")
	}
}

// TestFinish_CancelIsAServerStop is the control case: an actually cancelled
// parent context (a real shutdown) still fails the run with the fixed
// reason and stores no tour, even with a done section.
func TestFinish_CancelIsAServerStop(t *testing.T) {
	f := newJobFixture(t)
	run := f.insertRunningRun(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := tour.Result{}
	result.Sections[0] = tour.SectionResult{Kind: tour.KindArchitecture, Status: tour.StatusDone, Content: tour.ArchitectureContent{}}
	for i, k := range tour.Kinds {
		if i == 0 {
			continue
		}
		result.Sections[i] = tour.SectionResult{Kind: k, Status: tour.StatusFailed, Reason: "the server stopped during generation"}
	}

	if err := f.store.finish(ctx, run, result); err != nil {
		t.Fatalf("finish: %v", err)
	}

	status, errMsg := f.runStatus(t, run.ID)
	if status != "failed" || errMsg == nil || *errMsg != "the server stopped during generation" {
		t.Fatalf("status = %s, errMsg = %v, want failed, \"the server stopped during generation\"", status, errMsg)
	}
	if f.tourExists(t) {
		t.Fatal("a tour was stored for a cancelled (server-stopped) generation, want none")
	}
}
