package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
	"github.com/DimaMaimesko/dev-digest/api/internal/review"
	"github.com/DimaMaimesko/dev-digest/api/internal/tour"
)

// sectionWriteTimeout bounds one progress write (Approach -> Orchestration,
// "a detached 10s context"). It's detached from the job's own context, so a
// shutdown-triggered cancel doesn't also cancel the write that records it.
const sectionWriteTimeout = 10 * time.Second

// runJob is the onboarding-tour job's work: it marks run running, gathers
// its inputs, generates the tour and stores the outcome. ctx ends when the
// jobs.Runner's slot times out (2 minutes) or the runner closes.
func (s *Store) runJob(ctx context.Context, run postgres.OnboardingRun, dir string, llm review.LLM) error {
	n, err := s.q.StartOnboardingRun(ctx, run.ID)
	if err != nil {
		// jobs.Runner already logs a job function's error; logging it again
		// here would duplicate it. The run is still "queued" at this point
		// (the UPDATE never committed), so it's marked failed on a detached
		// context before returning, the same way an enqueue failure is
		// handled in Start — otherwise it would sit queued, blocking the
		// repository's onboarding-tour unique index until a restart's
		// FailStale sweep.
		s.failOrphanedRun(context.WithoutCancel(ctx), run.ID, err)
		return err
	}
	if n == 0 {
		// The run is no longer queued (it was deleted, or something else
		// already moved it past "queued"): nothing to generate.
		return nil
	}

	in, err := s.gather(ctx, dir, run.CommitSha, run.RepoID)
	var result tour.Result
	if err != nil {
		reason := err.Error()
		s.markAllFailed(ctx, run.ID, reason)
		result = allFailedResult(reason)
	} else {
		result = tour.Generate(ctx, llm, run.Model, in, tour.Options{SectionTimeout: s.sectionTimeout}, s.progressFunc(ctx, run.ID))
	}

	return s.finish(ctx, run, result)
}

// progressFunc returns the progress callback tour.Generate reports each
// section's status through, persisting it with SetOnboardingSection. ctx is
// the job's context; writes run detached from its cancellation, so a
// section's final status (in particular "failed" on a shutdown) is still
// recorded.
func (s *Store) progressFunc(ctx context.Context, runID uuid.UUID) func(tour.Kind, tour.Status, string) {
	return func(kind tour.Kind, status tour.Status, reason string) {
		idx := kindIndex(kind)
		if idx < 0 {
			return
		}
		val, err := json.Marshal(SectionStatus{Kind: kind, Status: status, Reason: orNull(reason)})
		if err != nil {
			s.log.Error("onboarding section status not encoded", "run", runID, "kind", kind, "err", err)
			return
		}

		wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sectionWriteTimeout)
		defer cancel()
		if err := s.q.SetOnboardingSection(wctx, postgres.SetOnboardingSectionParams{
			Idx: strconv.Itoa(idx), Val: val, ID: runID,
		}); err != nil {
			s.log.Error("onboarding section status not saved", "run", runID, "kind", kind, "err", err)
		}
	}
}

// markAllFailed records every section as failed with reason, for a run that
// never reached tour.Generate (its inputs couldn't be gathered).
func (s *Store) markAllFailed(ctx context.Context, runID uuid.UUID, reason string) {
	progress := s.progressFunc(ctx, runID)
	for _, k := range tour.Kinds {
		progress(k, tour.StatusFailed, reason)
	}
}

// allFailedResult is a tour.Result with every section failed with reason,
// and no usage: Generate was never called.
func allFailedResult(reason string) tour.Result {
	var result tour.Result
	for i, k := range tour.Kinds {
		result.Sections[i] = tour.SectionResult{Kind: k, Status: tour.StatusFailed, Reason: reason}
	}
	return result
}

// finish ends run with result, in one transaction (Approach -> Orchestration):
//  1. FinishOnboardingRun; no row means the run was deleted (the repository
//     was removed) or already finished — stop, store nothing (AC-37).
//  2. A cancelled job context (the server stopped) fails the run with the
//     fixed reason, leaving any previous tour untouched (AC-11). Hitting the
//     2-minute job cap (ctx.Err() == DeadlineExceeded) is not a server stop:
//     it falls through to the normal done/all-failed outcome below, so
//     sections that finished before the cap are still stored (AC-35, AC-47).
//  3. At least one section done replaces the tour (AC-12, AC-35).
//  4. All sections failed fails the run and keeps the previous tour (AC-36).
//  5. Either way, only the latest run for the repository is kept.
//
// The write uses context.WithoutCancel(ctx): a cancel from here on must not
// lose an outcome that was already paid for, the same rule jobs.Runner.finish
// and runner's intentFor follow.
func (s *Store) finish(ctx context.Context, run postgres.OnboardingRun, result tour.Result) error {
	stopped := errors.Is(ctx.Err(), context.Canceled)
	doneCount := 0
	for _, sr := range result.Sections {
		if sr.Status == tour.StatusDone {
			doneCount++
		}
	}

	status, errMsg := "failed", (*string)(nil)
	switch {
	case stopped:
		m := "the server stopped during generation"
		errMsg = &m
	case doneCount > 0:
		status = "done"
	default:
		m := "all sections failed"
		errMsg = &m
	}

	fctx := context.WithoutCancel(ctx)
	return pgx.BeginFunc(fctx, s.db, func(tx pgx.Tx) error {
		q := postgres.New(tx)

		repoID, err := q.FinishOnboardingRun(fctx, postgres.FinishOnboardingRunParams{
			Status: status, Error: errMsg,
			TokensIn: int32Ptr(result.Usage.TokensIn), TokensOut: int32Ptr(result.Usage.TokensOut),
			CostUsd: result.Usage.CostUSD, ID: run.ID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}

		if status == "done" {
			sections, err := sectionsToJSON(result.Sections)
			if err != nil {
				return err
			}
			if err := q.UpsertOnboardingTour(fctx, postgres.UpsertOnboardingTourParams{
				RepoID: repoID, Json: sections, CommitSha: &run.CommitSha, FilesIndexed: run.FilesIndexed,
				IndexStatus: &run.IndexStatus, CodeIndexEmpty: run.CodeIndexEmpty, Provider: &run.Provider, Model: &run.Model,
				TokensIn: int32Ptr(result.Usage.TokensIn), TokensOut: int32Ptr(result.Usage.TokensOut), CostUsd: result.Usage.CostUSD,
			}); err != nil {
				return err
			}
		}

		return q.DeleteOtherOnboardingRuns(fctx, repoID)
	})
}
