package repointel

import (
	"strings"
	"testing"
)

func TestRenderMap(t *testing.T) {
	candidates := []MapSymbol{
		{"src/core.ts", "export function core()"},
		{"src/core.ts", "export function core()"}, // Class.method and method: once
		{"src/core.ts", ""},                       // no signature
		{"src/util.ts", "function util(a: string)"},
		{"src/core.ts", "class Core"},
	}
	text, tokens := RenderMap(candidates, 1500)
	want := mapHeader + "\nsrc/core.ts:\n  export function core()\n  class Core\nsrc/util.ts:\n  function util(a: string)\n"
	if text != want || tokens != countTokens(want) {
		t.Errorf("map (%d tokens):\n%s", tokens, text)
	}

	// Over the budget: the longest prefix that fits.
	var many []MapSymbol
	for range 200 {
		many = append(many, MapSymbol{"src/a.ts", "export function aVeryLongName" + strings.Repeat("x", len(many)) + "()"})
	}
	text, tokens = RenderMap(many, 300)
	if tokens > 300 || strings.Count(text, "\n") < 5 {
		t.Errorf("%d tokens, %d lines", tokens, strings.Count(text, "\n"))
	}
	// The symbols are all different, so the map shows the first lines-1.
	if more := renderMap(many[:strings.Count(text, "\n")-1]); countTokens(more) <= 300 {
		t.Error("one more symbol would have fit")
	}

	if text, _ := RenderMap(nil, 1500); text != mapHeader+"\n" {
		t.Errorf("empty map: %q", text)
	}
}
