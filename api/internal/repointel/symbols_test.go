package repointel_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/repointel"
)

// symbolJSON is a symbol as the TS server's parseSymbols returns it.
type symbolJSON struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Line      int    `json:"line"`
	EndLine   int    `json:"endLine"`
	Exported  bool   `json:"exported"`
	Signature string `json:"signature"`
}

// compareSymbols parses each file under dir and compares the result with
// want, which maps a path relative to dir to the TS server's answer.
func compareSymbols(t *testing.T, dir string, want map[string][]symbolJSON) {
	t.Helper()
	for path, wantSyms := range want {
		t.Run(path, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join(dir, path))
			if err != nil {
				t.Fatal(err)
			}
			got := []symbolJSON{}
			for _, s := range repointel.ParseSymbols(path, src) {
				got = append(got, symbolJSON{s.Name, s.Kind, s.Line, s.EndLine, s.Exported, s.Signature})
			}
			if !reflect.DeepEqual(got, wantSyms) {
				t.Errorf("got:\n%+v\nTS:\n%+v", got, wantSyms)
			}
		})
	}
}

// The files in testdata/symbols were parsed by the TS server's parseSymbols
// (server/src/adapters/astgrep) to make testdata/symbols.golden.json.
func TestParseSymbols(t *testing.T) {
	data, err := os.ReadFile("testdata/symbols.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var want map[string][]symbolJSON
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	compareSymbols(t, "testdata/symbols", want)
}

// TestParseSymbolsLikeTS compares with the TS parser's answers for a whole
// directory, made by a script, when SYMBOLS_TS names that file and
// SYMBOLS_DIR the directory.
func TestParseSymbolsLikeTS(t *testing.T) {
	file, dir := os.Getenv("SYMBOLS_TS"), os.Getenv("SYMBOLS_DIR")
	if file == "" || dir == "" {
		t.Skip("set SYMBOLS_TS and SYMBOLS_DIR")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string][]symbolJSON
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	compareSymbols(t, dir, want)
}
