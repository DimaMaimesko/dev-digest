package tour

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoExecOrHTTPImports proves AC-27: the tour package can't run anything.
// It parses every non-test .go file in this package and fails if any of
// them imports os/exec, net/http or os.
func TestNoExecOrHTTPImports(t *testing.T) {
	banned := map[string]bool{
		`"os/exec"`:  true,
		`"net/http"`: true,
		`"os"`:       true,
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range file.Imports {
			if banned[imp.Path.Value] {
				t.Errorf("%s imports %s: internal/tour must never run a command, read a file or make a request itself", name, imp.Path.Value)
			}
		}
	}
}
