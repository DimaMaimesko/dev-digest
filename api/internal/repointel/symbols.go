package repointel

import (
	"path/filepath"
	"strings"
	"unicode/utf16"

	sitter "github.com/tree-sitter/go-tree-sitter"
	javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// Symbol is a top-level declaration in a TypeScript or JavaScript file.
type Symbol struct {
	Name string
	// Kind is function, class, method, interface, type or enum. A function
	// can be a declaration or a const, let or var holding a function.
	Kind     string
	Line     int // 1-based
	EndLine  int // the line of the declaration's last character
	Exported bool
	// Signature is the declaration's head on one line, up to its body, such
	// as "export async function load(id: string): Promise<Repo>".
	Signature string
}

// maxSignature is the longest signature kept, in UTF-16 code units as the TS
// server counts; a longer one is cut and ends with "…".
const maxSignature = 120

// language returns the grammar for a file, by its extension, or nil when
// the file isn't TypeScript or JavaScript. JSX is parsed as TSX.
func language(path string) *sitter.Language {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts":
		return sitter.NewLanguage(typescript.LanguageTypescript())
	case ".tsx", ".jsx":
		return sitter.NewLanguage(typescript.LanguageTSX())
	case ".js", ".cjs", ".mjs":
		return sitter.NewLanguage(javascript.Language())
	}
	return nil
}

// ParseSymbols returns the declarations at the top level of a TypeScript or
// JavaScript file, or none for any other file. A class's methods are listed
// twice, as "Class.method" and as "method", so a search for either finds
// them.
func ParseSymbols(path string, source []byte) []Symbol {
	lang := language(path)
	if lang == nil {
		return nil
	}
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(lang); err != nil {
		return nil
	}
	tree := parser.Parse(source, nil)
	defer tree.Close()
	root := tree.RootNode()

	p := symbolParser{src: source, declLines: map[string]int{}}
	for i := range root.ChildCount() {
		node, exported := unwrapExport(root.Child(i))
		p.decl(node, exported)
	}

	// `export { a, b as c }` makes earlier local declarations exported.
	for _, ex := range descendants(root, "export_statement") {
		clause := firstChild(ex, "export_clause")
		if clause == nil {
			continue
		}
		for _, spec := range children(clause, "export_specifier") {
			name := p.fieldText(spec, "name")
			if name == "" {
				continue
			}
			for i := range p.out {
				if s := &p.out[i]; s.Name == name && p.declLines[name] == s.Line {
					s.Exported = true
				}
			}
		}
	}
	return dedupe(p.out)
}

type symbolParser struct {
	src       []byte
	out       []Symbol
	declLines map[string]int // by name, the line of its last declaration
}

// keywords can't name a symbol.
var keywords = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "catch": true, "return": true,
	"function": true, "await": true, "typeof": true, "new": true, "delete": true, "void": true,
	"do": true, "else": true, "in": true, "of": true, "instanceof": true, "yield": true,
	"super": true, "constructor": true, "get": true, "set": true, "import": true, "export": true,
	"as": true, "from": true, "class": true, "extends": true,
}

func (p *symbolParser) add(name, kind string, n *sitter.Node, exported bool, signature string) {
	line := row(n)
	p.out = append(p.out, Symbol{
		Name: name, Kind: kind, Line: line, EndLine: int(n.EndPosition().Row) + 1,
		Exported: exported, Signature: signature,
	})
	p.declLines[name] = line
}

func (p *symbolParser) decl(n *sitter.Node, exported bool) {
	simple := map[string]string{
		"function_declaration":           "function",
		"generator_function_declaration": "function",
		"interface_declaration":          "interface",
		"type_alias_declaration":         "type",
		"enum_declaration":               "enum",
	}
	switch kind := n.Kind(); kind {
	case "function_declaration", "generator_function_declaration",
		"interface_declaration", "type_alias_declaration", "enum_declaration":
		if name := p.fieldText(n, "name"); name != "" && !keywords[name] {
			p.add(name, simple[kind], n, exported, p.headSignature(n))
		}

	case "class_declaration":
		name := p.fieldText(n, "name")
		if name == "" || keywords[name] {
			return
		}
		p.add(name, "class", n, exported, p.headSignature(n))
		body := n.ChildByFieldName("body")
		if body == nil {
			return
		}
		for _, m := range children(body, "method_definition") {
			method := p.fieldText(m, "name")
			if method == "" || keywords[method] {
				continue
			}
			sig := p.headSignature(m)
			p.add(name+"."+method, "method", m, exported, sig)
			p.add(method, "method", m, exported, sig)
		}

	case "lexical_declaration", "variable_declaration":
		// const f = () => …: a function; a plain value isn't a symbol.
		for _, d := range children(n, "variable_declarator") {
			name := p.fieldText(d, "name")
			if name == "" || keywords[name] || !functionLike(d.ChildByFieldName("value")) {
				continue
			}
			p.add(name, "function", d, exported, p.variableSignature(d))
		}
	}
}

// unwrapExport returns the declaration an export statement carries, and
// whether the node was exported.
func unwrapExport(top *sitter.Node) (*sitter.Node, bool) {
	if top.Kind() != "export_statement" {
		return top, false
	}
	for i := range top.ChildCount() {
		switch c := top.Child(i); c.Kind() {
		case "export", "default", "*", "from", ";", "string", "export_clause":
		default:
			return c, true
		}
	}
	return top, true
}

func functionLike(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	switch n.Kind() {
	case "arrow_function", "function_expression", "generator_function":
		return true
	}
	return false
}

// headSignature is n's text up to its body, on one line.
func (p *symbolParser) headSignature(n *sitter.Node) string {
	end := n.EndByte()
	if body := n.ChildByFieldName("body"); body != nil && body.StartByte() > n.StartByte() {
		end = body.StartByte()
	}
	return signature(string(p.src[n.StartByte():end]))
}

// variableSignature is the head of `const f = (x) =>`, keyword included.
func (p *symbolParser) variableSignature(d *sitter.Node) string {
	base := d
	if parent := d.Parent(); parent != nil && (parent.Kind() == "lexical_declaration" || parent.Kind() == "variable_declaration") {
		base = parent
	}
	if value := d.ChildByFieldName("value"); functionLike(value) {
		if body := value.ChildByFieldName("body"); body != nil {
			return signature(string(p.src[base.StartByte():body.StartByte()]))
		}
	}
	return p.headSignature(base)
}

// signature collapses head onto one line, drops the "=>", "{" or "=" that
// starts the body, and cuts it to maxSignature.
func signature(head string) string {
	head = jsTrim(strings.Join(strings.FieldsFunc(head, isJSSpace), " "))
	switch {
	case strings.HasSuffix(head, "=>"):
		head = head[:len(head)-2]
	case strings.HasSuffix(head, "{"), strings.HasSuffix(head, "="):
		head = head[:len(head)-1]
	}
	head = jsTrim(head)
	units := utf16.Encode([]rune(head))
	if len(units) <= maxSignature {
		return head
	}
	cut := units[:maxSignature-1]
	if utf16.IsSurrogate(rune(cut[len(cut)-1])) {
		cut = cut[:len(cut)-1] // don't split a character in two
	}
	return string(utf16.Decode(cut)) + "…"
}

// dedupe keeps one symbol per name, kind and line: the first, or a later
// one that is exported when the first isn't. The order is first-seen.
func dedupe(syms []Symbol) []Symbol {
	type key struct {
		name, kind string
		line       int
	}
	at := map[key]int{}
	var out []Symbol
	for _, s := range syms {
		k := key{s.Name, s.Kind, s.Line}
		i, ok := at[k]
		switch {
		case !ok:
			at[k] = len(out)
			out = append(out, s)
		case s.Exported && !out[i].Exported:
			out[i] = s
		}
	}
	return out
}

func (p *symbolParser) fieldText(n *sitter.Node, field string) string {
	if f := n.ChildByFieldName(field); f != nil {
		return f.Utf8Text(p.src)
	}
	return ""
}

func row(n *sitter.Node) int { return int(n.StartPosition().Row) + 1 }

// children returns n's children, named or not, of one kind.
func children(n *sitter.Node, kind string) []*sitter.Node {
	var out []*sitter.Node
	for i := range n.ChildCount() {
		if c := n.Child(i); c.Kind() == kind {
			out = append(out, c)
		}
	}
	return out
}

func firstChild(n *sitter.Node, kind string) *sitter.Node {
	if cs := children(n, kind); len(cs) > 0 {
		return cs[0]
	}
	return nil
}

// descendants returns the nodes of one kind below n, at any depth, in
// document order.
func descendants(n *sitter.Node, kind string) []*sitter.Node {
	var out []*sitter.Node
	for i := range n.ChildCount() {
		c := n.Child(i)
		if c.Kind() == kind {
			out = append(out, c)
		}
		out = append(out, descendants(c, kind)...)
	}
	return out
}

// isJSSpace reports whether r is whitespace or a line break to JavaScript's
// \s, which is wider than Go's.
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// jsTrim is JavaScript's String.prototype.trim.
func jsTrim(s string) string { return strings.TrimFunc(s, isJSSpace) }
