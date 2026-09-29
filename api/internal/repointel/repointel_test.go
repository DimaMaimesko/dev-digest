package repointel_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/pgtest"
	"github.com/DimaMaimesko/dev-digest/api/internal/repointel"
)

func setup(t *testing.T) (*pgxpool.Pool, uuid.UUID) {
	t.Helper()
	db := pgtest.New(t)
	var repo uuid.UUID
	err := db.QueryRow(context.Background(), `WITH w AS (INSERT INTO workspaces (name) VALUES ('w') RETURNING id)
		INSERT INTO repos (workspace_id, owner, name, full_name) SELECT id, 'o', 'n', 'o/n' FROM w RETURNING id`).Scan(&repo)
	if err != nil {
		t.Fatal(err)
	}
	return db, repo
}

func exec(t *testing.T, db *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := db.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func TestMap(t *testing.T) {
	db, repo := setup(t)
	x := repointel.New(db)
	ctx := context.Background()

	// Not indexed yet.
	if m, err := x.Map(ctx, repo); err != nil || m != (repointel.Map{}) {
		t.Errorf("unindexed: %+v, %v", m, err)
	}

	exec(t, db, `INSERT INTO repo_index_state (repo_id, last_indexed_sha, indexer_version, status) VALUES ($1, 'new', 2, 'full')`, repo)
	for _, row := range []struct {
		sha    string
		budget int
		text   string
	}{
		{"new", 1500, "src/app.ts: main()"},
		{"new", 3000, "a bigger map"},
		{"old", 1500, "a map of an older commit"},
	} {
		exec(t, db, `INSERT INTO repo_map_cache (repo_id, commit_sha, token_budget, map_text, token_count)
			VALUES ($1, $2, $3, $4, 42)`, repo, row.sha, row.budget, row.text)
	}
	// The map of the last indexed commit, at the reviews' budget.
	want := repointel.Map{Text: "src/app.ts: main()", Tokens: 42}
	if m, err := x.Map(ctx, repo); err != nil || m != want {
		t.Errorf("Map = %+v, %v; want %+v", m, err, want)
	}

	// Indexed again, with no map for the new commit yet.
	exec(t, db, `UPDATE repo_index_state SET last_indexed_sha = 'newer' WHERE repo_id = $1`, repo)
	if m, err := x.Map(ctx, repo); err != nil || m != (repointel.Map{}) {
		t.Errorf("no map for the last commit: %+v, %v", m, err)
	}
}

func TestFileRanks(t *testing.T) {
	db, repo := setup(t)
	for _, r := range []struct {
		path       string
		percentile int
	}{{"src/core.ts", 99}, {"src/util.ts", 40}, {"src/unchanged.ts", 97}} {
		exec(t, db, `INSERT INTO file_rank (repo_id, file_path, pagerank, hotness, rank, percentile) VALUES ($1, $2, 0.1, 0.1, 0.1, $3)`,
			repo, r.path, r.percentile)
	}
	x := repointel.New(db)
	got, err := x.FileRanks(context.Background(), repo, []string{"src/core.ts", "src/util.ts", "src/new.ts"})
	if want := map[string]int{"src/core.ts": 99, "src/util.ts": 40}; err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("FileRanks = %v, %v; want %v", got, err, want)
	}
	if got, err := x.FileRanks(context.Background(), uuid.New(), []string{"src/core.ts"}); err != nil || len(got) != 0 {
		t.Errorf("another repository: %v, %v", got, err)
	}
}
