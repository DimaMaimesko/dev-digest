package diff_test

import (
	"math"
	"slices"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/diff"
)

// sample is the fixture from server/test/grounding.test.ts. Its first hunk
// header announces one more line than it shows, like a diff cut short.
const sample = `diff --git a/src/config.ts b/src/config.ts
--- a/src/config.ts
+++ b/src/config.ts
@@ -10,3 +10,4 @@
   port: 3000,
+  stripeKey: "sk_live_xxx",
   redisUrl: x,
diff --git a/src/api/users.ts b/src/api/users.ts
--- a/src/api/users.ts
+++ b/src/api/users.ts
@@ -44,2 +44,6 @@
   const users = await db.users.findMany();
+  for (const u of users) {
+    const posts = await db.posts.findMany({ userId: u.id });
+    result.push({ ...u, posts });
+  }`

func TestParse(t *testing.T) {
	d, err := diff.Parse(sample)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := []struct {
		path      string
		additions int
		deletions int
		newLines  []int
	}{
		{"src/config.ts", 1, 0, []int{10, 11, 12}},
		{"src/api/users.ts", 4, 0, []int{44, 45, 46, 47, 48}},
	}
	if len(d.Files) != len(want) {
		t.Fatalf("got %d files, want %d", len(d.Files), len(want))
	}
	for i, w := range want {
		f := d.Files[i]
		if f.Path != w.path || f.Additions != w.additions || f.Deletions != w.deletions {
			t.Errorf("file %d = {%q +%d -%d}, want {%q +%d -%d}",
				i, f.Path, f.Additions, f.Deletions, w.path, w.additions, w.deletions)
		}
		if len(f.Hunks) != 1 {
			t.Fatalf("file %q: got %d hunks, want 1", f.Path, len(f.Hunks))
		}
		if got := f.Hunks[0].NewLines; !slices.Equal(got, w.newLines) {
			t.Errorf("file %q: NewLines = %v, want %v", f.Path, got, w.newLines)
		}
	}
}

// TestParseEdgeCases covers inputs the TypeScript parser got wrong.
func TestParseEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		paths    []string
		newLines []int // NewLines of the first hunk of the first file
		adds     int
		dels     int
	}{
		{
			name: "trailing newline adds no line",
			raw: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n" +
				"@@ -1,1 +1,2 @@\n x\n+y\n",
			paths:    []string{"a.go"},
			newLines: []int{1, 2},
			adds:     1,
		},
		{
			name: "no-newline marker is not a line",
			raw: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n" +
				"@@ -1 +1 @@\n-x\n\\ No newline at end of file\n+y\n\\ No newline at end of file",
			paths:    []string{"a.go"},
			newLines: []int{1},
			adds:     1,
			dels:     1,
		},
		{
			name: "removed line starting with -- is content, not a header",
			raw: "diff --git a/q.sql b/q.sql\n--- a/q.sql\n+++ b/q.sql\n" +
				"@@ -1,2 +1,1 @@\n--- old comment\n select 1;",
			paths:    []string{"q.sql"},
			newLines: []int{1},
			dels:     1,
		},
		{
			name: "added line starting with ++ is content, not a header",
			raw: "diff --git a/c.c b/c.c\n--- a/c.c\n+++ b/c.c\n" +
				"@@ -1 +1,2 @@\n i = 0;\n+++i;",
			paths:    []string{"c.c"},
			newLines: []int{1, 2},
			adds:     1,
		},
		{
			name: "added line starting with '++ ' does not rename the file",
			raw: "diff --git a/notes.md b/notes.md\n--- a/notes.md\n+++ b/notes.md\n" +
				"@@ -1 +1,2 @@\n title\n+++ bullet\n",
			paths:    []string{"notes.md"},
			newLines: []int{1, 2},
			adds:     1,
		},
		{
			name: "blank context line without its leading space",
			raw: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n" +
				"@@ -1,3 +1,4 @@\n a\n\n+b\n c",
			paths:    []string{"a.go"},
			newLines: []int{1, 2, 3, 4},
			adds:     1,
		},
		{
			name: "deleted file is left out",
			raw: "diff --git a/gone.go b/gone.go\ndeleted file mode 100644\n--- a/gone.go\n+++ /dev/null\n" +
				"@@ -1 +0,0 @@\n-x\n" +
				"diff --git a/kept.go b/kept.go\n--- a/kept.go\n+++ b/kept.go\n@@ -1 +1 @@\n-a\n+b",
			paths:    []string{"kept.go"},
			newLines: []int{1},
			adds:     1,
			dels:     1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := diff.Parse(tt.raw)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			var paths []string
			for _, f := range d.Files {
				paths = append(paths, f.Path)
			}
			if !slices.Equal(paths, tt.paths) {
				t.Fatalf("paths = %v, want %v", paths, tt.paths)
			}
			f := d.Files[0]
			if got := f.Hunks[0].NewLines; !slices.Equal(got, tt.newLines) {
				t.Errorf("NewLines = %v, want %v", got, tt.newLines)
			}
			if f.Additions != tt.adds || f.Deletions != tt.dels {
				t.Errorf("+%d -%d, want +%d -%d", f.Additions, f.Deletions, tt.adds, tt.dels)
			}
		})
	}
}

func TestParseText(t *testing.T) {
	const x = "diff --git a/x.ts b/x.ts\n--- a/x.ts\n+++ b/x.ts\n@@ -1 +1 @@\n-a\n+b"
	// reviewer-core's sliceDiff matched "b/x.ts" as a substring, so this
	// file's lines ended up in the section for x.ts as well.
	const bak = "diff --git a/x.ts.bak b/x.ts.bak\n--- a/x.ts.bak\n+++ b/x.ts.bak\n@@ -1 +1 @@\n-old\n+new"
	raw := x + "\n" + bak

	d, err := diff.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if d.Text != raw {
		t.Errorf("Diff.Text is not the input")
	}
	if len(d.Files) != 2 {
		t.Fatalf("got %d files, want 2", len(d.Files))
	}
	if d.Files[0].Text != x {
		t.Errorf("x.ts Text = %q, want %q", d.Files[0].Text, x)
	}
	if d.Files[1].Text != bak {
		t.Errorf("x.ts.bak Text = %q, want %q", d.Files[1].Text, bak)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"malformed hunk header", "diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -x +1 @@\n+y"},
		{"hunk before file header", "@@ -1 +1 @@\n+y"},
		{"number too large", "diff --git a/a b/a\n+++ b/a\n@@ -1 +99999999999999999999 @@\n+y"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := diff.Parse(tt.raw); err == nil {
				t.Error("Parse returned no error")
			}
		})
	}
}

func TestFileCovers(t *testing.T) {
	file := diff.File{
		Path: "a.go",
		Hunks: []diff.Hunk{
			{NewStart: 10, NewCount: 3, NewLines: []int{10, 11, 12}},
			// Removes lines only; points at line 30 of the new file.
			{NewStart: 30, NewCount: 0},
		},
	}
	tests := []struct {
		name       string
		start, end int
		want       bool
	}{
		{"single shown line", 11, 11, true},
		{"range overlapping a hunk edge", 5, 10, true},
		{"range between hunks", 13, 29, false},
		{"reversed bounds", 12, 1, true},
		{"huge range overlapping a hunk", 1, math.MaxInt, true},
		{"huge range after every hunk", 100, math.MaxInt, false},
		{"line of a removal-only hunk", 30, 30, true},
		{"line after a removal-only hunk", 31, 31, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := file.Covers(tt.start, tt.end); got != tt.want {
				t.Errorf("Covers(%d, %d) = %v, want %v", tt.start, tt.end, got, tt.want)
			}
		})
	}
}
