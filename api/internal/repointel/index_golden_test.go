package repointel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// testdata/index.golden.json holds what the TS server's functions return
// for the same inputs: parseReferences on testdata/symbols, extractEndpoints
// and extractCrons, computeFileRank, and js-tiktoken's counts.
type indexGolden struct {
	References map[string][]Reference
	Facts      []struct {
		Source           string
		Endpoints, Crons []string
	}
	Rank struct {
		Files []string
		Edges [][2]string
		Ranks []struct {
			Path       string
			PageRank   float64
			Percentile int
		}
	}
	Tokens []struct {
		Text   string
		Tokens int
	}
}

func readGolden(t *testing.T) indexGolden {
	t.Helper()
	data, err := os.ReadFile("testdata/index.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g indexGolden
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func TestParseReferencesLikeTS(t *testing.T) {
	for file, want := range readGolden(t).References {
		src, err := os.ReadFile(filepath.Join("testdata/symbols", file))
		if err != nil {
			t.Fatal(err)
		}
		got := ParseReferences(file, src)
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\ngot %v\nTS  %v", file, got, want)
		}
	}
}

func TestFactsLikeTS(t *testing.T) {
	for _, f := range readGolden(t).Facts {
		if e, c := Endpoints(f.Source), Crons(f.Source); !slices.Equal(e, f.Endpoints) || !slices.Equal(c, f.Crons) {
			t.Errorf("%q: endpoints %q crons %q; TS %q %q", f.Source, e, c, f.Endpoints, f.Crons)
		}
	}
}

// The scores are compared exactly: the port follows graphology's
// arithmetic, in its order.
func TestRankFilesLikeTS(t *testing.T) {
	g := readGolden(t).Rank
	var edges []Edge
	for _, e := range g.Edges {
		edges = append(edges, Edge{e[0], e[1]})
	}
	got := RankFiles(g.Files, edges)
	for i, want := range g.Ranks {
		if got[i].Path != want.Path || got[i].PageRank != want.PageRank || got[i].Percentile != want.Percentile {
			t.Errorf("%d: got %+v, TS %+v", i, got[i], want)
		}
	}
	if RankFiles(nil, nil) != nil {
		t.Error("no files: want no ranks")
	}
}

func TestCountTokensLikeTS(t *testing.T) {
	for _, tt := range readGolden(t).Tokens {
		if got := countTokens(tt.Text); got != tt.Tokens {
			t.Errorf("%q: %d tokens, js-tiktoken %d", tt.Text, got, tt.Tokens)
		}
	}
}
