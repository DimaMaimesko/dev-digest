package pgtest_test

import (
	"context"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/pgtest"
)

func TestNew(t *testing.T) {
	ctx := context.Background()
	a, b := pgtest.New(t), pgtest.New(t)

	// Every migration ran: the last one (0012) added pr_intent.fingerprint.
	var hasColumn bool
	err := a.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_name = 'pr_intent' AND column_name = 'fingerprint')`).Scan(&hasColumn)
	if err != nil {
		t.Fatalf("query schema: %v", err)
	}
	if !hasColumn {
		t.Error("pr_intent.fingerprint is missing; migration 0012 should have added it")
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
