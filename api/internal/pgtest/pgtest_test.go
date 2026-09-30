package pgtest_test

import (
	"context"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/pgtest"
)

func TestNew(t *testing.T) {
	ctx := context.Background()
	a, b := pgtest.New(t), pgtest.New(t)

	// Every migration ran: the last one (0010) added agent_runs_pr_id_idx.
	var hasIndex bool
	err := a.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_indexes
		WHERE tablename = 'agent_runs' AND indexname = 'agent_runs_pr_id_idx')`).Scan(&hasIndex)
	if err != nil {
		t.Fatalf("query schema: %v", err)
	}
	if !hasIndex {
		t.Error("agent_runs_pr_id_idx is missing; migration 0010 should have added it")
	}

	// The two databases are separate.
	if _, err := a.Exec(ctx, `INSERT INTO workspaces (name) VALUES ('only-in-a')`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := b.QueryRow(ctx, `SELECT count(*) FROM workspaces`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("second database has %d workspaces, want 0", n)
	}
}
