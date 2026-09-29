package migrate_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DimaMaimesko/dev-digest/api/internal/migrate"
	"github.com/DimaMaimesko/dev-digest/api/internal/pgtest"
)

// realDir is the TS server's migrations folder.
func realDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "server", "src", "db", "migrations")
}

type recorded struct {
	Hash string
	When int64
}

func records(t *testing.T, db *pgxpool.Pool) []recorded {
	t.Helper()
	rows, err := db.Query(context.Background(), `SELECT hash, created_at FROM drizzle.__drizzle_migrations ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []recorded
	for rows.Next() {
		var r recorded
		rows.Scan(&r.Hash, &r.When)
		out = append(out, r)
	}
	return out
}

func TestRunRealMigrations(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	applied, err := migrate.Run(ctx, db, realDir())
	if err != nil {
		t.Fatal(err)
	}
	var journal struct {
		Entries []struct {
			Tag  string
			When int64
		}
	}
	data, _ := os.ReadFile(filepath.Join(realDir(), "meta", "_journal.json"))
	json.Unmarshal(data, &journal)
	var tags []string
	var want []recorded
	for _, e := range journal.Entries {
		tags = append(tags, e.Tag)
		sql, _ := os.ReadFile(filepath.Join(realDir(), e.Tag+".sql"))
		sum := sha256.Sum256(sql)
		want = append(want, recorded{hex.EncodeToString(sum[:]), e.When})
	}
	if !slices.Equal(applied, tags) {
		t.Errorf("applied %v, want %v", applied, tags)
	}
	// What Drizzle records: the file's SHA-256 and the journal's time.
	if got := records(t, db); !slices.Equal(got, want) {
		t.Errorf("records = %v, want %v", got, want)
	}
	var tables int
	db.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public'`).Scan(&tables)
	if tables < 35 {
		t.Errorf("%d tables", tables)
	}
	// Again: nothing to do.
	if applied, err := migrate.Run(ctx, db, realDir()); err != nil || len(applied) != 0 {
		t.Errorf("second run: %v, %v", applied, err)
	}
}

// dir writes a migrations folder: one migration per SQL text, generated
// 1000 ms apart.
func dir(t *testing.T, sqls ...string) string {
	t.Helper()
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "meta"), 0o755)
	type entry struct {
		Idx         int    `json:"idx"`
		When        int64  `json:"when"`
		Tag         string `json:"tag"`
		Breakpoints bool   `json:"breakpoints"`
	}
	var entries []entry
	for i, sql := range sqls {
		tag := "000" + strconv.Itoa(i) + "_m"
		os.WriteFile(filepath.Join(d, tag+".sql"), []byte(sql), 0o644)
		entries = append(entries, entry{i, int64(1000 * (i + 1)), tag, true})
	}
	data, _ := json.Marshal(map[string]any{"version": "7", "dialect": "postgresql", "entries": entries})
	os.WriteFile(filepath.Join(d, "meta", "_journal.json"), data, 0o644)
	return d
}

func TestRunAppliesNewerOnly(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	three := dir(t,
		"CREATE TABLE a (x int);\n--> statement-breakpoint\nINSERT INTO a VALUES (1);",
		"INSERT INTO a VALUES (2);",
		"INSERT INTO a VALUES (3);")
	// A database that has the first two, as the TS server recorded them.
	if _, err := migrate.Run(ctx, db, dir(t, "CREATE TABLE a (x int);\n--> statement-breakpoint\nINSERT INTO a VALUES (1);", "INSERT INTO a VALUES (2);")); err != nil {
		t.Fatal(err)
	}
	applied, err := migrate.Run(ctx, db, three)
	if err != nil || !slices.Equal(applied, []string{"0002_m"}) {
		t.Fatalf("applied %v, %v", applied, err)
	}
	var sum int
	db.QueryRow(ctx, `SELECT sum(x) FROM a`).Scan(&sum)
	if sum != 6 {
		t.Errorf("sum = %d, want 6", sum)
	}
}

// A failing migration applies none: they run in one transaction.
func TestRunFails(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if _, err := migrate.Run(ctx, db, dir(t, "CREATE TABLE a (x int);", "INSERT INTO nowhere VALUES (1);")); err == nil {
		t.Fatal("no error")
	}
	var tables int
	db.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_name = 'a'`).Scan(&tables)
	if tables != 0 {
		t.Error("the first migration stayed")
	}
	for name, d := range map[string]string{
		"no journal":   t.TempDir(),
		"no .sql file": func() string { d := dir(t, "SELECT 1;"); os.Remove(filepath.Join(d, "0000_m.sql")); return d }(),
	} {
		if _, err := migrate.Run(ctx, db, d); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// Migrators running at once take turns; Drizzle's didn't.
func TestRunConcurrently(t *testing.T) {
	db := pgtest.NewEmpty(t)
	d := dir(t, "CREATE TABLE a (x int);", "INSERT INTO a VALUES (1);")
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0
	for range 4 {
		wg.Go(func() {
			applied, err := migrate.Run(context.Background(), db, d)
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			total += len(applied)
			mu.Unlock()
		})
	}
	wg.Wait()
	if total != 2 || len(records(t, db)) != 2 {
		t.Errorf("%d applied in all, %d records", total, len(records(t, db)))
	}
}
