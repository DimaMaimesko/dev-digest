// Package repointel reads what the repo-intel index knows about a
// repository, to give a review more context than the diff: a map of the
// repository's code, how depended-on each file is, and the code that calls
// the symbols a pull request changes. Until phase 5 the TS server builds the
// index; this package only reads it.
package repointel

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DimaMaimesko/dev-digest/api/internal/postgres"
)

// MapTokenBudget is the size, in tokens, the indexer renders the repository
// map for. A map is cached per budget, and reviews use this one.
const MapTokenBudget = 1500

// Index reads a repository's index from the database.
type Index struct {
	q *postgres.Queries
}

// New returns an Index reading from db.
func New(db postgres.DBTX) *Index {
	return &Index{q: postgres.New(db)}
}

// Map is a repository map: the skeleton of a repository's code.
type Map struct {
	Text   string
	Tokens int
}

// Map returns the repository map for the last indexed commit, or a zero Map
// when the repository isn't indexed or has no map.
func (x *Index) Map(ctx context.Context, repo uuid.UUID) (Map, error) {
	row, err := x.q.RepoMap(ctx, postgres.RepoMapParams{RepoID: repo, TokenBudget: MapTokenBudget})
	if errors.Is(err, pgx.ErrNoRows) {
		return Map{}, nil
	}
	if err != nil {
		return Map{}, err
	}
	return Map{Text: row.MapText, Tokens: int(row.TokenCount)}, nil
}

// FileRanks returns how depended-on each of the files is, as a percentile
// from 0 to 100, by path. Files the index didn't rank are left out.
func (x *Index) FileRanks(ctx context.Context, repo uuid.UUID, paths []string) (map[string]int, error) {
	ranks := map[string]int{}
	if len(paths) == 0 {
		return ranks, nil
	}
	rows, err := x.q.FileRanks(ctx, postgres.FileRanksParams{RepoID: repo, Paths: paths})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		ranks[r.FilePath] = int(r.Percentile)
	}
	return ranks, nil
}
