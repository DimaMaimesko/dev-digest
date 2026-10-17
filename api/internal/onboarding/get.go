package onboarding

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

// Get answers a repository's index readiness, its stored tour (if any) and
// its latest generation (if any).
func (s *Store) Get(ctx context.Context, workspace, repoID uuid.UUID) (View, error) {
	r, err := s.checkReadiness(ctx, workspace, repoID)
	if err != nil {
		return View{}, err
	}
	view := View{Index: indexReadiness(r)}

	tourRow, err := s.q.GetOnboardingTour(ctx, postgres.GetOnboardingTourParams{WorkspaceID: workspace, RepoID: repoID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// No tour generated yet.
	case err != nil:
		return View{}, err
	default:
		t, err := tourFromRow(tourRow)
		if err != nil {
			return View{}, err
		}
		view.Tour = &t
	}

	runRow, err := s.q.LatestOnboardingRun(ctx, repoID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// No generation has ever run.
	case err != nil:
		return View{}, err
	default:
		g, err := generationFromRow(runRow)
		if err != nil {
			return View{}, err
		}
		view.Generation = &g
	}

	return view, nil
}
