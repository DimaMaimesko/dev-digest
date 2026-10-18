package pulls

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// AuthorStat is how much one author has changed in a repository through pull
// requests.
type AuthorStat struct {
	Author    string
	Pulls     int
	Additions int
	Deletions int
	AvgSize   int // lines changed per pull request
}

// AuthorStats returns one page of the repository's pull request authors,
// largest first by sortBy ("pulls", "additions" or "deletions"). page counts
// from 1.
func (s *Store) AuthorStats(ctx context.Context, repoID uuid.UUID, sortBy string, page, size int) ([]AuthorStat, error) {
	query := fmt.Sprintf(`
		SELECT author, count(*) AS pulls, sum(additions) AS additions, sum(deletions) AS deletions
		FROM pull_requests
		WHERE repo_id = $1
		GROUP BY author
		ORDER BY %s DESC, author
		LIMIT $2 OFFSET $3`, sortBy)
	rows, err := s.db.Query(ctx, query, repoID, size, page*size)
	if err != nil {
		return nil, fmt.Errorf("author stats: %w", err)
	}
	defer rows.Close()

	stats := []AuthorStat{}
	for rows.Next() {
		var a AuthorStat
		if err := rows.Scan(&a.Author, &a.Pulls, &a.Additions, &a.Deletions); err != nil {
			return nil, fmt.Errorf("author stats: %w", err)
		}
		a.AvgSize = (a.Additions + a.Deletions) / a.Pulls
		stats = append(stats, a)
	}
	return stats, rows.Err()
}
