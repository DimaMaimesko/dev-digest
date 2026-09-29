package repointel

import (
	"math"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/tiktoken-go/tokenizer"
)

// mapHeader starts every repository map.
const mapHeader = "# Repo skeleton (top-ranked by import graph only, partial view)"

// MapSymbol is a symbol that may go in a repository map. The candidates
// come most depended-on file first.
type MapSymbol struct {
	Path      string
	Signature string
}

// cl100k is the tokenizer of the repository map's budget, loaded once: it
// is the TS server's (js-tiktoken's cl100k_base).
var cl100k = sync.OnceValues(func() (tokenizer.Codec, error) { return tokenizer.Get(tokenizer.Cl100kBase) })

// countTokens counts text's tokens, or estimates them, a token for every 4
// characters, if the tokenizer can't load, as the TS server did.
func countTokens(text string) int {
	if codec, err := cl100k(); err == nil {
		if n, err := codec.Count(text); err == nil {
			return n
		}
	}
	return int(math.Ceil(float64(len(utf16.Encode([]rune(text)))) / 4))
}

// RenderMap renders the largest prefix of the candidates that fits budget
// tokens: each signature under its file, files in the order they first
// come, after mapHeader. A signature repeated in a file (a method listed as
// Class.method and as method) goes in once. It returns the map and its
// token count.
func RenderMap(candidates []MapSymbol, budget int) (string, int) {
	var items []MapSymbol
	seen := map[MapSymbol]bool{}
	for _, c := range candidates {
		if c.Signature == "" || seen[c] {
			continue
		}
		seen[c] = true
		items = append(items, c)
	}
	best := mapHeader + "\n"
	bestTokens := countTokens(best)
	// The largest prefix that fits, by binary search, as the TS server did.
	for lo, hi := 0, len(items); lo <= hi; {
		mid := (lo + hi) / 2
		text := renderMap(items[:mid])
		if tokens := countTokens(text); tokens <= budget {
			best, bestTokens = text, tokens
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return best, bestTokens
}

func renderMap(items []MapSymbol) string {
	var files []string
	sigs := map[string][]string{}
	for _, it := range items {
		if _, ok := sigs[it.Path]; !ok {
			files = append(files, it.Path)
		}
		sigs[it.Path] = append(sigs[it.Path], "  "+it.Signature)
	}
	var b strings.Builder
	b.WriteString(mapHeader + "\n")
	for _, f := range files {
		b.WriteString(f + ":\n" + strings.Join(sigs[f], "\n") + "\n")
	}
	return b.String()
}
