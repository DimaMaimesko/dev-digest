package pgtest_test

import (
	"context"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/pgtest"
)

func TestNew(t *testing.T) {
	ctx := context.Background()
	a, b := pgtest.New(t), pgtest.New(t)

	// Every migration ran: the last one (0013) added onboarding_runs.
	var hasTable bool
	err := a.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables
		WHERE table_name = 'onboarding_runs')`).Scan(&hasTable)
	if err != nil {
		t.Fatalf("query schema: %v", err)
	}
	if !hasTable {
		t.Error("onboarding_runs is missing; migration 0013 should have added it")
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
