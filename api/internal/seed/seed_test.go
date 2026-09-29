package seed_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/pgtest"
	"github.com/DimaMaimesko/dev-digest/api/internal/seed"
)

// counts returns how many rows each seeded table has.
func counts(t *testing.T, db *pgxpool.Pool) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, table := range []string{"workspaces", "users", "workspace_members", "settings", "repos", "pull_requests",
		"pr_files", "pr_commits", "reviews", "findings", "agents"} {
		var n int
		if err := db.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[table] = n
	}
	return out
}

var seeded = map[string]int{"workspaces": 1, "users": 1, "workspace_members": 1, "settings": 4, "repos": 1,
	"pull_requests": 1, "pr_files": 4, "pr_commits": 1, "reviews": 1, "findings": 2, "agents": 3}

func TestRun(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	ws, user, err := seed.Run(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	for table, n := range counts(t, db) {
		if n != seeded[table] {
			t.Errorf("%s: %d rows, want %d", table, n, seeded[table])
		}
	}
	var role, theme, prompt, model string
	db.QueryRow(ctx, `SELECT role FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`, ws, user).Scan(&role)
	db.QueryRow(ctx, `SELECT value #>> '{}' FROM settings WHERE key = 'theme'`).Scan(&theme)
	db.QueryRow(ctx, `SELECT system_prompt, model FROM agents WHERE name = 'Security Reviewer'`).Scan(&prompt, &model)
	if role != "owner" || theme != "dark" || model != "deepseek/deepseek-v4-flash" || !strings.HasPrefix(prompt, "# Role") || strings.HasSuffix(prompt, "\n") {
		t.Errorf("role %q, theme %q, model %q, prompt %.20q…", role, theme, model, prompt)
	}

	// Again, after the user changed a setting: nothing added, nothing
	// overwritten.
	db.Exec(ctx, `UPDATE settings SET value = '"light"' WHERE key = 'theme'`)
	ws2, user2, err := seed.Run(ctx, db)
	if err != nil || ws2 != ws || user2 != user {
		t.Fatalf("second run: %v %v %v", ws2, user2, err)
	}
	for table, n := range counts(t, db) {
		if n != seeded[table] {
			t.Errorf("after a second run, %s: %d rows", table, n)
		}
	}
	db.QueryRow(ctx, `SELECT value #>> '{}' FROM settings WHERE key = 'theme'`).Scan(&theme)
	if theme != "light" {
		t.Errorf("the user's theme was overwritten: %q", theme)
	}
}

// Seeds at once make one workspace; the TS seed could make two.
func TestRunConcurrently(t *testing.T) {
	db := pgtest.New(t)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, _, err := seed.Run(context.Background(), db); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if c := counts(t, db); c["workspaces"] != 1 || c["agents"] != 3 {
		t.Errorf("counts %v", c)
	}
}

// The embedded prompts are docs/agent-prompts, for as long as both exist.
func TestPromptsMatchTheDocs(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	docs := filepath.Join(filepath.Dir(file), "..", "..", "..", "docs", "agent-prompts")
	for _, name := range []string{"general-reviewer", "security-reviewer", "performance-reviewer"} {
		want, err := os.ReadFile(filepath.Join(docs, name+".md"))
		if err != nil {
			t.Skip("no docs:", err)
		}
		got, _ := os.ReadFile(filepath.Join(filepath.Dir(file), "prompts", name+".md"))
		if string(got) != string(want) {
			t.Errorf("prompts/%s.md differs from docs/agent-prompts", name)
		}
	}
}
