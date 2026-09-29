package repointel

import (
	"cmp"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/google/uuid"
)

// Caller is code that calls a symbol a change declares: the top-level
// function, class or method around the call.
type Caller struct {
	File      string // relative to the clone
	Symbol    string
	Signature string
	Rank      int // the file's rank percentile, or 0 when it isn't ranked
}

// maxFileSize is the largest file the reference search reads, in bytes.
const maxFileSize = 2_000_000

// searchedExt are the extensions the reference search reads (matched as
// written: ".TS" isn't one).
var searchedExt = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true}

// skippedDirs are directories the reference search doesn't enter, at any
// depth.
var skippedDirs = map[string]bool{".git": true, "node_modules": true, "dist": true, "build": true, ".next": true, "coverage": true}

// Callers finds up to limit callers of the functions, classes and methods
// declared in the changed TypeScript or JavaScript files of the clone at
// dir: each is the top-level symbol around a call in another file, with its
// signature. When the index ranks their files, the most depended-on come
// first. It finds none when dir is empty. Files it can't read are skipped.
func (x *Index) Callers(ctx context.Context, repo uuid.UUID, dir string, changed []string, limit int) ([]Caller, error) {
	if dir == "" || len(changed) == 0 {
		return nil, nil
	}

	// The callable symbols the change declares, each with its file.
	type decl struct{ name, file string }
	var decls []decl
	declared := map[string]bool{}
	for _, file := range changed {
		if language(file) == nil {
			continue
		}
		src, ok := readClone(dir, file)
		if !ok {
			continue
		}
		for _, s := range ParseSymbols(file, src) {
			// A qualified Class.method would find the same calls twice.
			if s.Kind != "function" && s.Kind != "method" && s.Kind != "class" || containsDot(s.Name) || declared[s.Name] {
				continue
			}
			declared[s.Name] = true
			decls = append(decls, decl{s.Name, file})
		}
	}
	if len(decls) == 0 {
		return nil, nil
	}

	files := codeFiles(dir)
	symbolsOf := map[string][]Symbol{} // each caller file parsed once
	seen := map[string]bool{}
	var out []Caller
	for _, d := range decls {
		if len(out) >= limit {
			break
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, file := range files {
			if len(out) >= limit {
				break
			}
			src, ok := readClone(dir, file)
			if !ok || file == d.file {
				continue
			}
			for _, line := range References(string(src), d.name) {
				if len(out) >= limit {
					break
				}
				syms, parsed := symbolsOf[file]
				if !parsed {
					syms = ParseSymbols(file, src)
					symbolsOf[file] = syms
				}
				enclosing, ok := enclosingSymbol(syms, line)
				if !ok || enclosing.Signature == "" {
					continue
				}
				key := file + "|" + enclosing.Name + "|" + d.name
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, Caller{File: file, Symbol: enclosing.Name, Signature: enclosing.Signature})
			}
		}
	}

	if len(out) > 0 {
		var paths []string
		for _, c := range out {
			if !slices.Contains(paths, c.File) {
				paths = append(paths, c.File)
			}
		}
		ranks, err := x.FileRanks(ctx, repo, paths)
		if err != nil {
			return nil, err
		}
		if len(ranks) > 0 {
			for i := range out {
				out[i].Rank = ranks[out[i].File]
			}
			slices.SortStableFunc(out, func(a, b Caller) int { return cmp.Compare(b.Rank, a.Rank) })
		}
	}
	return out, nil
}

// enclosingSymbol returns the last top-level symbol starting at or before
// line: the first of those on the same line.
func enclosingSymbol(syms []Symbol, line int) (Symbol, bool) {
	var best Symbol
	found := false
	for _, s := range syms {
		if s.Line <= line && !containsDot(s.Name) && (!found || s.Line > best.Line) {
			best, found = s, true
		}
	}
	return best, found
}

func containsDot(s string) bool { return strings.Contains(s, ".") }

// codeFiles lists the files the reference search reads in the clone at
// dir, relative to it, in lexical order. Symbolic links are skipped.
func codeFiles(dir string) []string {
	var files []string
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil // an unreadable directory is skipped
		case d.IsDir():
			if path != dir && skippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		case !d.Type().IsRegular() || !searchedExt[filepath.Ext(path)]:
			return nil
		}
		if info, err := d.Info(); err != nil || info.Size() >= maxFileSize {
			return nil
		}
		if rel, err := filepath.Rel(dir, path); err == nil {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	return files
}

// readClone reads a file of the clone at dir, by its path in the
// repository.
func readClone(dir, file string) ([]byte, bool) {
	if !filepath.IsLocal(filepath.FromSlash(file)) {
		return nil, false
	}
	src, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file)))
	return src, err == nil
}
