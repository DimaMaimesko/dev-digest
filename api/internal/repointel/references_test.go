package repointel_test

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/repointel"
)

// testdata/references.golden.json holds lines of code and, for each symbol,
// the lines the TS server's extractReferences
// (server/src/adapters/codeindex/extract.ts) reports: calls, member calls,
// `new`, JSX, with comments, imports, strings and declarations left out.
func TestReferences(t *testing.T) {
	data, err := os.ReadFile("testdata/references.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Source string
		Cases  []struct {
			Symbol string
			Lines  []int
		}
	}
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(golden.Source, "\n")
	for _, c := range golden.Cases {
		t.Run(c.Symbol, func(t *testing.T) {
			got := repointel.References(golden.Source, c.Symbol)
			if !slices.Equal(got, c.Lines) {
				for _, n := range symmetricDiff(got, c.Lines) {
					t.Errorf("line %d %q: Go found it %v, TS %v", n, lines[n-1], slices.Contains(got, n), slices.Contains(c.Lines, n))
				}
			}
		})
	}
}

func symmetricDiff(a, b []int) []int {
	var out []int
	for _, n := range a {
		if !slices.Contains(b, n) {
			out = append(out, n)
		}
	}
	for _, n := range b {
		if !slices.Contains(a, n) {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}
