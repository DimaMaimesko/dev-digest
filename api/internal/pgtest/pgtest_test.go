package pgtest_test

import (
	"context"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/pgtest"
)

func TestNew(t *testing.T) {
	ctx := context.Background()
	a, b := pgtest.New(t), pgtest.New(t)

	// Every migration ran: the last one (0009) dropped agent_runs.cost_usd.
	var hasCost bool
	err := a.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_name = 'agent_runs' AND column_name = 'cost_usd')`).Scan(&hasCost)
	if err != nil {
		t.Fatalf("query schema: %v", err)
	}
	if hasCost {
		t.Error("agent_runs.cost_usd exists; migration 0009 should have dropped it")
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
