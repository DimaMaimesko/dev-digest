package tour

import "testing"

func testTree() Tree {
	return NewTree([]Entry{
		{Path: "README.md", Dir: false},
		{Path: "a", Dir: true},
		{Path: "a/b.go", Dir: false},
		{Path: "dir", Dir: true},
		{Path: "dir/x.go", Dir: false},
		{Path: "dir/sub", Dir: true},
		{Path: "dir/sub/y.go", Dir: false},
	})
}

func TestGround(t *testing.T) {
	tree := testTree()

	tests := []struct {
		name     string
		path     string
		wantOK   bool
		wantPath string
		wantDir  bool
	}{
		{"leading ./", "./README.md", true, "README.md", false},
		{"backslash separators", `a\b.go`, true, "a/b.go", false},
		{"dir glob star", "dir/*", true, "dir", true},
		{"dir glob doublestar", "dir/**", true, "dir", true},
		{"glob on missing dir", "missing/*", false, "", false},
		{"parent traversal", "../x", false, "", false},
		{"parent traversal nested", "a/../x", false, "", false},
		{"absolute path", "/etc/passwd", false, "", false},
		{"scheme", "https://x", false, "", false},
		{"made up path", "nope/not/real.go", false, "", false},
		{"nested file", "dir/sub/y.go", true, "dir/sub/y.go", false},
		{"trailing slash on dir", "dir/", true, "dir", true},
		{"empty", "", false, "", false},
		{"NUL byte", "a/b.go\x00", false, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ref, ok := Ground(tree, tc.path)
			if ok != tc.wantOK {
				t.Fatalf("Ground(%q) ok = %v, want %v", tc.path, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if ref.Path != tc.wantPath || ref.IsDir != tc.wantDir {
				t.Fatalf("Ground(%q) = %+v, want {%q %v}", tc.path, ref, tc.wantPath, tc.wantDir)
			}
		})
	}
}
