package repointel

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// The reference search is a line-based heuristic, ported from the TS
// server's extractReferences (server/src/adapters/codeindex/extract.ts). Its
// regular expressions use JavaScript features Go's RE2 lacks (lookbehind,
// backreferences) and JavaScript's wider \s and narrower ".", so those parts
// are written out by hand to match the same lines.

// jsSpace is JavaScript's \s as a character class.
const jsSpace = `[\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

var (
	lineComment = regexp.MustCompile(`^` + jsSpace + `*(//|\*|/\*)`)
	importLine  = regexp.MustCompile(`^` + jsSpace + `*import` + jsSpace + `|^` + jsSpace + `*export` + jsSpace +
		`+\{[^}]*\}` + jsSpace + `+from\b|^` + jsSpace + `*export` + jsSpace + `+\*` + jsSpace + `+from\b`)
)

// referenceMatcher finds the lines that use one symbol.
type referenceMatcher struct {
	name                     string
	memberCall, newExpr, jsx *regexp.Regexp
	decl                     *regexp.Regexp
}

func newReferenceMatcher(symbol string) *referenceMatcher {
	// Class.method is searched as method.
	name := symbol[strings.LastIndex(symbol, ".")+1:]
	q := regexp.QuoteMeta(name)
	return &referenceMatcher{
		name:       name,
		memberCall: regexp.MustCompile(`\.` + q + jsSpace + `*\(`),
		newExpr:    regexp.MustCompile(`new` + jsSpace + `+` + q + `\b`),
		jsx:        regexp.MustCompile(`<` + q + `[\s/>` + jsSpace[1:len(jsSpace)-1] + `]`),
		decl: regexp.MustCompile(`(?:function` + jsSpace + `*\*?` + jsSpace + `*|class` + jsSpace + `+|interface` + jsSpace +
			`+|type` + jsSpace + `+|enum` + jsSpace + `+|(?:const|let|var)` + jsSpace + `+)` + q + `\b`),
	}
}

// References returns the 1-based numbers of the lines of source that call,
// construct or render symbol: `sym(`, `.sym(`, `new Sym`, `<Sym`. Comment
// lines, import and re-export lines, and the symbol's own declaration are
// left out, and strings and // comments are ignored.
func References(source, symbol string) []int {
	m := newReferenceMatcher(symbol)
	var lines []int
	for i, raw := range strings.Split(source, "\n") {
		if lineComment.MatchString(raw) || importLine.MatchString(raw) {
			continue
		}
		line := blankStrings(stripLineComment(raw))
		if m.decl.MatchString(line) {
			continue
		}
		if m.call(line) || m.memberCall.MatchString(line) || m.newExpr.MatchString(line) || m.jsx.MatchString(line) {
			lines = append(lines, i+1)
		}
	}
	return lines
}

// call reports whether line calls the name directly: `name(`, with no
// word character, "$" or "." right before it.
func (m *referenceMatcher) call(line string) bool {
	for from := 0; ; {
		i := strings.Index(line[from:], m.name)
		if i < 0 {
			return false
		}
		i += from
		from = i + 1
		if i > 0 {
			if c := line[i-1]; isWordByte(c) || c == '$' || c == '.' {
				continue
			}
		}
		rest := strings.TrimLeftFunc(line[i+len(m.name):], isJSSpace)
		if strings.HasPrefix(rest, "(") {
			return true
		}
	}
}

func isWordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// isLineTerminator reports whether JavaScript's "." refuses to match r.
func isLineTerminator(r rune) bool {
	return r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029
}

// stripLineComment removes a // comment, as JavaScript's
// line.replace(/\/\/.*$/, ”) does: from the first "//" whose rest of the
// line has no line terminator.
func stripLineComment(line string) string {
	for i := 0; ; {
		j := strings.Index(line[i:], "//")
		if j < 0 {
			return line
		}
		j += i
		if strings.IndexFunc(line[j+2:], isLineTerminator) < 0 {
			return line[:j]
		}
		i = j + 1
	}
}

// blankStrings replaces each string literal with "", as JavaScript's
// line.replace(/(["'`])(?:\\.|(?!\1).)*\1/g, '""') does, backtracking
// included: an unclosed quote is left as it is.
func blankStrings(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); {
		if q := line[i]; q == '"' || q == '\'' || q == '`' {
			if end := stringEnd(line, i+1, q); end >= 0 {
				b.WriteString(`""`)
				i = end
				continue
			}
		}
		b.WriteByte(line[i])
		i++
	}
	return b.String()
}

// stringEnd returns the index just past the quote q that closes a string
// whose contents start at from, or -1 when none does. It explores the
// matches in the order JavaScript's backtracking regexp does: an escape
// (\ and any character) first, then any character but q, then the closing
// quote; the first way that works wins. Positions known to fail are
// remembered, which keeps it linear.
func stringEnd(s string, from int, q byte) int {
	failed := map[int]bool{}
	var try func(pos int) int
	try = func(pos int) int {
		if failed[pos] || pos >= len(s) {
			return -1
		}
		if s[pos] == '\\' && pos+1 < len(s) {
			if r, size := utf8.DecodeRuneInString(s[pos+1:]); !isLineTerminator(r) {
				if end := try(pos + 1 + size); end >= 0 {
					return end
				}
			}
		}
		if r, size := utf8.DecodeRuneInString(s[pos:]); s[pos] != q && !isLineTerminator(r) {
			if end := try(pos + size); end >= 0 {
				return end
			}
		}
		if s[pos] == q {
			return pos + 1
		}
		failed[pos] = true
		return -1
	}
	return try(from)
}
