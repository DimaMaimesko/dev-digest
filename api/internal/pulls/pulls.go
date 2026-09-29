// Package pulls keeps the pull requests in the database in step with GitHub.
package pulls

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/github"
	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

// GitHub is what syncing reads from GitHub. *github.Client implements it.
type GitHub interface {
	Pulls(ctx context.Context, owner, repo string) ([]github.Pull, error)
	Pull(ctx context.Context, owner, repo string, number int) (github.Pull, error)
	PullFiles(ctx context.Context, owner, repo string, number int) ([]github.File, error)
	PullCommits(ctx context.Context, owner, repo string, number int) ([]github.Commit, error)
}

// GitHubError is a failure to read from GitHub, as opposed to a database
// error.
type GitHubError struct {
	Err error
}

func (e *GitHubError) Error() string { return e.Err.Error() }

func (e *GitHubError) Unwrap() error { return e.Err }

// Store saves pull requests read from GitHub.
type Store struct {
	db *pgxpool.Pool
}

// NewStore returns a Store using db.
func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// SyncList saves the repository's pull requests from GitHub's list. New ones
// are added; known ones get their new title, head commit, state and update
// time. It returns how many it saved.
func (s *Store) SyncList(ctx context.Context, gh GitHub, repo postgres.Repo) (int, error) {
	pulls, err := gh.Pulls(ctx, repo.Owner, repo.Name)
	if err != nil {
		return 0, &GitHubError{err}
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := postgres.New(tx)
		for _, p := range pulls {
			err := q.UpsertPull(ctx, postgres.UpsertPullParams{
				WorkspaceID: repo.WorkspaceID,
				RepoID:      repo.ID,
				Number:      int32(p.Number),
				Title:       p.Title,
				Author:      p.Author,
				Branch:      p.Branch,
				Base:        p.Base,
				HeadSha:     p.HeadSHA,
				Status:      p.State,
				OpenedAt:    timePtr(p.CreatedAt),
				UpdatedAt:   timePtr(p.UpdatedAt),
			})
			if err != nil {
				return fmt.Errorf("save pull request #%d: %w", p.Number, err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(pulls), nil
}

// statsBatch is the most pull requests one BackfillStats call fetches, with
// one GitHub request each.
const statsBatch = 10

// BackfillStats fetches the diff stats that GitHub's list leaves out, for up
// to 10 of the repository's pull requests that have none, newest first. After
// a failure it goes on with the next one, and returns all the failures.
func (s *Store) BackfillStats(ctx context.Context, gh GitHub, repo postgres.Repo) error {
	q := postgres.New(s.db)
	numbers, err := q.PullsWithoutStats(ctx, postgres.PullsWithoutStatsParams{RepoID: repo.ID, Limit: statsBatch})
	if err != nil {
		return err
	}
	var errs []error
	for _, n := range numbers {
		if err := ctx.Err(); err != nil {
			return err
		}
		p, err := gh.Pull(ctx, repo.Owner, repo.Name, int(n))
		if err != nil {
			err = &GitHubError{err}
		} else {
			err = q.SetPullStats(ctx, postgres.SetPullStatsParams{
				RepoID:     repo.ID,
				Number:     n,
				Additions:  int32(p.Additions),
				Deletions:  int32(p.Deletions),
				FilesCount: int32(p.ChangedFiles),
			})
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("pull request #%d: %w", n, err))
		}
	}
	return errors.Join(errs...)
}

// Refresh fetches a pull request with its files and commits and saves them,
// replacing the files and commits saved before.
func (s *Store) Refresh(ctx context.Context, gh GitHub, repo postgres.Repo, number int32) error {
	p, err := gh.Pull(ctx, repo.Owner, repo.Name, int(number))
	if err != nil {
		return &GitHubError{err}
	}
	files, err := gh.PullFiles(ctx, repo.Owner, repo.Name, int(number))
	if err != nil {
		return &GitHubError{err}
	}
	commits, err := gh.PullCommits(ctx, repo.Owner, repo.Name, int(number))
	if err != nil {
		return &GitHubError{err}
	}

	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		q := postgres.New(tx)
		// First, so the row stays locked while the files and commits are
		// replaced.
		id, err := q.RefreshPull(ctx, postgres.RefreshPullParams{
			RepoID:     repo.ID,
			Number:     number,
			Title:      p.Title,
			HeadSha:    p.HeadSHA,
			Status:     p.State,
			UpdatedAt:  timePtr(p.UpdatedAt),
			OpenedAt:   timePtr(p.CreatedAt),
			Body:       p.Body,
			Additions:  int32(p.Additions),
			Deletions:  int32(p.Deletions),
			FilesCount: int32(p.ChangedFiles),
		})
		if err != nil {
			return fmt.Errorf("save pull request #%d: %w", number, err)
		}
		if err := q.DeletePullFiles(ctx, id); err != nil {
			return err
		}
		for _, f := range files {
			err := q.InsertPullFile(ctx, postgres.InsertPullFileParams{
				PrID: id, Path: f.Path, Additions: int32(f.Additions), Deletions: int32(f.Deletions), Patch: f.Patch,
			})
			if err != nil {
				return err
			}
		}
		if err := q.DeletePullCommits(ctx, id); err != nil {
			return err
		}
		for _, c := range commits {
			err := q.InsertPullCommit(ctx, postgres.InsertPullCommitParams{
				PrID: id, Sha: c.SHA, Message: c.Message, Author: c.Author, CommittedAt: c.CommittedAt,
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// timePtr is t, or nil when t is the zero time: GitHub didn't send it.
func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
