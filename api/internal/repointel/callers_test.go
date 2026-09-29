package repointel_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/repointel"
)

// clone writes files into a temporary clone and returns its directory.
func clone(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

var cloneFiles = map[string]string{
	"src/lib.ts": `export function helper(x: number) {
  return x;
}
export class Store {
  save() { return helper(0); }
}
export type Id = string;
`,
	"src/a.ts": `import { helper } from './lib';
export function useA() {
  return helper(1);
}
`,
	"src/b.ts": `export const useB = (n: number) => {
  helper(2);
  return new Store();
};
`,
	"src/c.ts": `function inner() {
  helper(3);
  helper(4);
}
`,
	"src/d.js": "helper(5); // no enclosing symbol\n",
	// Two symbols start on the line: the first is the enclosing one.
	"src/f.ts":                "const first = () => 1, second = () => helper(10);\n",
	"src/e.TS":                "function upper() { helper(6); }\n",
	"node_modules/x/index.ts": "export function nm() { helper(7); }\n",
	"dist/out.js":             "function built() { helper(8); }\n",
	"README.md":               "function readme() { helper(9); }\n",
}

func TestCallers(t *testing.T) {
	db, repo := setup(t)
	x := repointel.New(db)
	dir := clone(t, cloneFiles)
	ctx := context.Background()

	// Without ranks: in the order found, one per caller and symbol. The
	// changed file's own calls, node_modules, dist, other extensions (and
	// ".TS") and calls outside any symbol don't count.
	got, err := x.Callers(ctx, repo, dir, []string{"src/lib.ts", "notes.md"}, 10)
	want := []repointel.Caller{
		{File: "src/a.ts", Symbol: "useA", Signature: "function useA()"}, // without "export", as in TS
		{File: "src/b.ts", Symbol: "useB", Signature: "const useB = (n: number)"},
		{File: "src/c.ts", Symbol: "inner", Signature: "function inner()"},
		{File: "src/f.ts", Symbol: "first", Signature: "const first = ()"},
		{File: "src/b.ts", Symbol: "useB", Signature: "const useB = (n: number)"}, // calls Store too
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Callers =\n%+v, %v\nwant\n%+v", got, err, want)
	}

	// At most limit.
	if got, _ := x.Callers(ctx, repo, dir, []string{"src/lib.ts"}, 2); len(got) != 2 || got[1].File != "src/b.ts" {
		t.Errorf("limit 2: %+v", got)
	}

	// With ranks, the most depended-on files first; the order is kept
	// among equals.
	for path, percentile := range map[string]int{"src/c.ts": 99, "src/b.ts": 60} {
		exec(t, db, `INSERT INTO file_rank (repo_id, file_path, pagerank, hotness, rank, percentile) VALUES ($1, $2, 0, 0, 0, $3)`,
			repo, path, percentile)
	}
	got, _ = x.Callers(ctx, repo, dir, []string{"src/lib.ts"}, 10)
	var order []string
	for _, c := range got {
		order = append(order, c.Symbol+":"+strings.Repeat("*", c.Rank/30))
	}
	if want := []string{"inner:***", "useB:**", "useB:**", "useA:", "first:"}; !reflect.DeepEqual(order, want) {
		t.Errorf("ranked order = %v, want %v", order, want)
	}
}

func TestCallersNone(t *testing.T) {
	db, repo := setup(t)
	x := repointel.New(db)
	dir := clone(t, cloneFiles)
	// A file next to a clone, which a changed path must not reach.
	outside := clone(t, map[string]string{
		"secret.ts":     "export function secret() {}\n",
		"clone/call.ts": "function f() { secret(); }\n",
	})
	tests := []struct {
		name    string
		dir     string
		changed []string
	}{
		{"no clone", "", []string{"src/lib.ts"}},
		{"no changed files", dir, nil},
		{"only a type changed", clone(t, map[string]string{"t.ts": "export type T = number;\n", "u.ts": "function f() { T(1); }\n"}), []string{"t.ts"}},
		{"not TypeScript or JavaScript", dir, []string{"README.md"}},
		{"a file missing from the clone", dir, []string{"src/gone.ts"}},
		{"a path out of the clone", filepath.Join(outside, "clone"), []string{"../secret.ts"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := x.Callers(context.Background(), repo, tt.dir, tt.changed, 10); err != nil || len(got) != 0 {
				t.Errorf("Callers = %+v, %v", got, err)
			}
		})
	}
}
