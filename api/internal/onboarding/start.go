package onboarding

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

// Start starts a tour generation for repoID in workspace: it answers with
// the new generation, or with the one already queued or running for this
// repository (AC-6).
//
// Start:
//  1. checks the repository and its index (ErrNotFound, ErrNotIndexed).
//  2. resolves the onboarding feature's model and its client (*ConfigError),
//     before inserting anything (AC-33).
//  3. inserts a queued run, or finds the one already in progress.
//  4. enqueues the one background job that gathers the inputs, generates
//     the tour and stores the outcome.
//
// The insert and the enqueue run under a context detached from ctx
// (context.WithoutCancel): a request cancelled right after the insert must
// not leave the run queued forever, locking the repository until a
// restart's FailStale sweep. If the enqueue itself fails, the just-inserted
// run is marked failed before Start returns, for the same reason.
func (s *Store) Start(ctx context.Context, workspace, repoID uuid.UUID) (Generation, error) {
	r, err := s.checkReadiness(ctx, workspace, repoID)
	if err != nil {
		return Generation{}, err
	}
	if !r.ready {
		return Generation{}, ErrNotIndexed
	}

	provider, model := s.modelChoice(ctx, workspace)
	llm, err := s.llm(provider)
	if err != nil {
		return Generation{}, &ConfigError{Provider: provider, Err: err}
	}

	sections, err := json.Marshal(initialSections())
	if err != nil {
		return Generation{}, err
	}

	dctx := context.WithoutCancel(ctx)

	run, inserted, err := s.insertOrFindLive(dctx, repoID, postgres.InsertOnboardingRunParams{
		RepoID: repoID, WorkspaceID: workspace, CommitSha: r.commitSha, FilesIndexed: r.filesIndexed,
		IndexStatus: r.indexStatus, CodeIndexEmpty: r.codeIndexEmpty, Provider: provider, Model: model,
		Sections: sections,
	})
	if err != nil {
		return Generation{}, err
	}
	if !inserted {
		// A run was already queued or running for this repository (AC-6).
		return generationFromRow(run)
	}

	dir := r.dir
	if _, err := s.jobs.Enqueue(dctx, workspace, jobKind, map[string]string{"repoId": repoID.String()},
		func(jobCtx context.Context) error { return s.runJob(jobCtx, run, dir, llm) }); err != nil {
		s.failOrphanedRun(dctx, run.ID, err)
		return Generation{}, err
	}

	return generationFromRow(run)
}

// insertOrFindLive inserts a queued run for params.RepoID, or returns the
// one already queued or running for it (AC-6). The database's partial
// unique index refuses a concurrent insert with zero rows back
// (pgx.ErrNoRows): LiveOnboardingRun then finds the run that won the race.
// If that lookup itself finds nothing — the other run finished between the
// conflict and this lookup — the insert is retried once, since the repo is
// free again.
func (s *Store) insertOrFindLive(ctx context.Context, repoID uuid.UUID, params postgres.InsertOnboardingRunParams) (run postgres.OnboardingRun, inserted bool, err error) {
	run, err = s.q.InsertOnboardingRun(ctx, params)
	if err == nil {
		return run, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return postgres.OnboardingRun{}, false, err
	}

	live, err := s.q.LiveOnboardingRun(ctx, repoID)
	if err == nil {
		return live, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return postgres.OnboardingRun{}, false, err
	}

	run, err = s.q.InsertOnboardingRun(ctx, params)
	if err != nil {
		return postgres.OnboardingRun{}, false, err
	}
	return run, true, nil
}

// failOrphanedRun marks a just-inserted run failed when the enqueue itself
// failed (e.g. CreateJob couldn't write): otherwise the run stays queued,
// blocking the repository's onboarding-tour unique index until a restart's
// FailStale sweep. ctx is already detached from the request.
func (s *Store) failOrphanedRun(ctx context.Context, runID uuid.UUID, cause error) {
	msg := cause.Error()
	if _, err := s.q.FinishOnboardingRun(ctx, postgres.FinishOnboardingRunParams{Status: "failed", Error: &msg, ID: runID}); err != nil {
		s.log.Error("onboarding run not marked failed after an enqueue error", "run", runID, "err", err)
	}
}
