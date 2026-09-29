package repointel

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWalk(t *testing.T) {
	dir := t.TempDir()
	for name, size := range map[string]int{
		"b.ts": 1, "a/UPPER.TS": 1, "a/c.mjs": 1, "a/readme.md": 1, "big.js": maxIndexedSize + 1, "edge.js": maxIndexedSize,
		"node_modules/x/i.js": 1, "vendor/v.ts": 1, "out/o.ts": 1, "src/out/o.ts": 1, ".git/h.js": 1,
	} {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(strings.Repeat("x", size)), 0o644)
	}
	os.Symlink(filepath.Join(dir, "b.ts"), filepath.Join(dir, "link.ts"))
	files, stats := Walk(dir)
	if want := []string{"a/UPPER.TS", "a/c.mjs", "b.ts", "edge.js"}; !reflect.DeepEqual(files, want) {
		t.Errorf("files = %v, want %v", files, want)
	}
	if stats != (WalkStats{TotalCandidates: 5, SkippedTooLarge: 1}) {
		t.Errorf("stats = %+v", stats)
	}
}
