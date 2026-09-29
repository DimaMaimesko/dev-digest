package repointel

import (
	"slices"
	"testing"
)

// Beyond the TS golden cases: a string must close with the quote it opened
// with, as the TS regexp's backreference required.
func TestEndpointsQuotes(t *testing.T) {
	for line, want := range map[string][]string{
		`app.get("/mixed', h)`: nil,
		"app.get(`/tpl`, h)":   {"GET /tpl"},
		`app.get('/x"y')`:      nil,
	} {
		if got := Endpoints(line); !slices.Equal(got, want) {
			t.Errorf("%s: %q, want %q", line, got, want)
		}
	}
}

// Only TSX and JSX files have JSX references, as in TS, though the
// JavaScript grammar parses JSX too.
func TestJSXOnlyInTSXAndJSX(t *testing.T) {
	src := []byte("const v = <Foo />;\n")
	if refs := ParseReferences("a.js", src); len(refs) != 0 {
		t.Errorf("a.js: %v", refs)
	}
	if refs := ParseReferences("a.jsx", src); len(refs) != 1 {
		t.Errorf("a.jsx: %v", refs)
	}
}
