package tour

import "strings"

// Entry is one path at the commit a tour is generated from: a file or a
// directory. internal/onboarding builds a []Entry from git.ListTree; tour
// never imports internal/git, so it takes this shape of its own.
type Entry struct {
	Path string
	Dir  bool
}

// Ref is a path that has passed Ground: it names a real file or directory
// in the Tree it was grounded against. It's the same shape as PathRef
// (content.go), which carries the json tags content.go's wire types need;
// Ref is an alias rather than a second struct literal of the same two
// fields (staticcheck S1016).
type Ref = PathRef

// Tree is the set of files and directories a tour's claims are grounded
// against: every file and directory that exists at the commit the tour
// describes.
type Tree struct {
	files map[string]bool
	dirs  map[string]bool
}

// NewTree builds a Tree from entries, the shape internal/onboarding gets
// from git.ListTree.
func NewTree(entries []Entry) Tree {
	t := Tree{files: make(map[string]bool), dirs: make(map[string]bool)}
	// The root directory always exists, so "" and "." as a prefix for a
	// top-level "dir/*" pattern work without a matching Entry.
	t.dirs[""] = true
	for _, e := range entries {
		p := normalizePath(e.Path)
		if p == "" {
			continue
		}
		if e.Dir {
			t.dirs[p] = true
		} else {
			t.files[p] = true
		}
	}
	return t
}

// Entries returns every file and directory in t, for building a rendered
// tree. Order is unspecified.
func (t Tree) Entries() []Entry {
	out := make([]Entry, 0, len(t.files)+len(t.dirs))
	for p := range t.files {
		out = append(out, Entry{Path: p, Dir: false})
	}
	for p := range t.dirs {
		if p == "" {
			continue
		}
		out = append(out, Entry{Path: p, Dir: true})
	}
	return out
}

// Ground checks p against t and, when it names something real, returns the
// normalized Ref for it.
//
// The rules (AC-15):
//   - normalize: strip a leading "./", turn '\' into '/', strip a trailing
//     '/'
//   - reject an empty path, an absolute path, any ".." segment, anything
//     containing "://", and a NUL byte
//   - accept the path when it names a file or a directory in t
//   - accept "dir/*" and "dir/**" when dir exists in t, grounding to dir
//     itself (a directory reference)
func Ground(t Tree, p string) (Ref, bool) {
	if strings.Contains(p, "://") || strings.ContainsRune(p, 0) {
		return Ref{}, false
	}
	norm := normalizePath(p)
	if norm == "" {
		return Ref{}, false
	}
	if strings.HasPrefix(norm, "/") {
		return Ref{}, false
	}
	for _, seg := range strings.Split(norm, "/") {
		if seg == ".." {
			return Ref{}, false
		}
	}

	if base, ok := globBase(norm); ok {
		if t.dirs[base] {
			return Ref{Path: base, IsDir: true}, true
		}
		return Ref{}, false
	}

	if t.files[norm] {
		return Ref{Path: norm, IsDir: false}, true
	}
	if t.dirs[norm] {
		return Ref{Path: norm, IsDir: true}, true
	}
	return Ref{}, false
}

// globBase reports whether p is "dir/*" or "dir/**", returning dir.
func globBase(p string) (string, bool) {
	switch {
	case strings.HasSuffix(p, "/**"):
		return strings.TrimSuffix(p, "/**"), true
	case strings.HasSuffix(p, "/*"):
		return strings.TrimSuffix(p, "/*"), true
	default:
		return "", false
	}
}

// normalizePath strips a leading "./", turns '\' into '/', and strips a
// trailing '/'. It does not reject anything; Ground does that.
func normalizePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimSuffix(p, "/")
	return p
}
