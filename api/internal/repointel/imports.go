package repointel

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// Edge is an import between two files of a repository: From imports To.
type Edge struct {
	From, To string
}

// importSpecifiers returns the modules a TypeScript or JavaScript file
// imports, as written: "./util.js", "react".
func importSpecifiers(file string, source []byte) []string {
	lang := language(file)
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

	used := valueNames(tree.RootNode(), source)
	var specs []string
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		switch n.Kind() {
		case "import_statement", "export_statement":
			s := n.ChildByFieldName("source")
			if s == nil || typeOnly(n) {
				break
			}
			// Compiling drops an import whose names are only used as types, or
			// not at all, as for export statements it keeps.
			if names := importedNames(n, source); n.Kind() == "import_statement" && names != nil && !slices.ContainsFunc(names, func(name string) bool { return used[name] }) {
				break
			}
			specs = append(specs, stringValue(s, source))
		case "call_expression":
			fn := n.ChildByFieldName("function")
			args := n.ChildByFieldName("arguments")
			if fn != nil && args != nil && (fn.Kind() == "import" || fn.Utf8Text(source) == "require") && args.NamedChildCount() > 0 {
				if a := args.NamedChild(0); a.Kind() == "string" {
					specs = append(specs, stringValue(a, source))
				}
			}
		}
		for i := range n.ChildCount() {
			walk(n.Child(i))
		}
	}
	walk(tree.RootNode())
	return specs
}

// typeOnly reports whether an import or re-export brings only types, which
// compiling to JavaScript removes: `import type {A}`, `import {type A}`,
// `export type {A} from`. dependency-cruiser reads the compiled imports.
func typeOnly(stmt *sitter.Node) bool {
	if firstChild(stmt, "type") != nil {
		return true
	}
	clause := firstChild(stmt, "import_clause")
	specKind := "import_specifier"
	if clause == nil {
		clause, specKind = firstChild(stmt, "export_clause"), "export_specifier"
	}
	if clause == nil {
		return false // a side-effect import, or export *
	}
	list := clause
	if n := firstChild(clause, "named_imports"); n != nil {
		list = n
	}
	if list != clause && clause.NamedChildCount() > 1 {
		return false // a default or namespace import besides the list
	}
	if clause.Kind() == "import_clause" && list == clause {
		return false // import D or import * as D
	}
	specs := children(list, specKind)
	if len(specs) == 0 {
		return false
	}
	for _, sp := range specs {
		if firstChild(sp, "type") == nil {
			return false
		}
	}
	return true
}

// importedNames returns the local names an import statement binds, leaving
// out type-only ones, or nil for a side-effect import (import "./x").
func importedNames(stmt *sitter.Node, source []byte) []string {
	clause := firstChild(stmt, "import_clause")
	if clause == nil {
		return nil
	}
	names := []string{}
	for i := range clause.NamedChildCount() {
		c := clause.NamedChild(i)
		switch c.Kind() {
		case "identifier": // import D
			names = append(names, c.Utf8Text(source))
		case "namespace_import": // import * as D
			if id := firstChild(c, "identifier"); id != nil {
				names = append(names, id.Utf8Text(source))
			}
		case "named_imports":
			for _, sp := range children(c, "import_specifier") {
				if firstChild(sp, "type") != nil {
					continue
				}
				local := sp.ChildByFieldName("alias")
				if local == nil {
					local = sp.ChildByFieldName("name")
				}
				if local != nil {
					names = append(names, local.Utf8Text(source))
				}
			}
		}
	}
	return names
}

// typeContexts are the nodes whose names are types, not values.
var typeContexts = map[string]bool{
	"type_annotation": true, "type_arguments": true, "type_parameters": true, "type_query": true,
	"type_alias_declaration": true, "interface_declaration": true, "implements_clause": true,
	"extends_type_clause": true, "type_predicate_annotation": true, "opting_type_annotation": true,
	"omitting_type_annotation": true, "asserts_annotation": true,
}

// valueNames returns the names a file uses as values: outside import
// statements and outside types.
func valueNames(root *sitter.Node, source []byte) map[string]bool {
	used := map[string]bool{}
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		k := n.Kind()
		if k == "import_statement" || typeContexts[k] {
			return
		}
		switch k {
		case "as_expression", "satisfies_expression":
			// Only the expression is a value; the type after it isn't.
			if e := n.NamedChild(0); e != nil {
				walk(e)
			}
			return
		case "identifier", "shorthand_property_identifier", "shorthand_property_identifier_pattern":
			used[n.Utf8Text(source)] = true
		}
		for i := range n.ChildCount() {
			walk(n.Child(i))
		}
	}
	walk(root)
	return used
}

// stringValue is the text of a string literal node, without its quotes.
func stringValue(n *sitter.Node, source []byte) string {
	t := n.Utf8Text(source)
	if len(t) >= 2 {
		return t[1 : len(t)-1]
	}
	return t
}

// resolveExts are the extensions tried for an import without one.
var resolveExts = []string{".js", ".cjs", ".mjs", ".jsx", ".ts", ".tsx", ".d.ts"}

// resolve finds the file a relative import from `from` names, among files,
// or returns "" when it names none.
func resolve(from, spec string, files map[string]bool) string {
	if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") && spec != "." && spec != ".." {
		return ""
	}
	base := path.Join(path.Dir(from), spec)
	candidates := []string{base}
	for _, ext := range resolveExts {
		candidates = append(candidates, base+ext)
	}
	// TypeScript's convention: "./util.js" names util.ts.
	for js, tsExts := range map[string][]string{".js": {".ts", ".tsx"}, ".jsx": {".tsx"}, ".mjs": {".mts"}, ".cjs": {".cts"}} {
		if strings.HasSuffix(base, js) {
			for _, ext := range tsExts {
				candidates = append(candidates, strings.TrimSuffix(base, js)+ext)
			}
		}
	}
	for _, ext := range resolveExts {
		candidates = append(candidates, base+"/index"+ext)
	}
	for _, c := range candidates {
		if files[c] {
			return c
		}
	}
	return ""
}

// ImportGraph returns the imports among files (paths relative to the clone
// at dir) that resolve to another of files, each once, in the order found.
func ImportGraph(dir string, files []string) []Edge {
	set := make(map[string]bool, len(files))
	for _, f := range files {
		set[f] = true
	}
	var edges []Edge
	seen := map[Edge]bool{}
	for _, from := range files {
		src, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(from)))
		if err != nil {
			continue
		}
		for _, spec := range importSpecifiers(from, src) {
			to := resolve(from, spec, set)
			e := Edge{from, to}
			if to == "" || to == from || seen[e] {
				continue
			}
			seen[e] = true
			edges = append(edges, e)
		}
	}
	return edges
}
