// Package diff parses unified diffs, the output of git diff, and reports which
// lines of the new version of each file a diff shows.
package diff

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Diff is a parsed unified diff.
type Diff struct {
	Files []File
}

// File is one changed file in a diff.
type File struct {
	Path      string // path in the new version, without the "b/" prefix
	Additions int
	Deletions int
	Hunks     []Hunk
}

// Hunk is one "@@ -a,b +c,d @@" section of a file's diff.
type Hunk struct {
	OldStart, OldCount int
	NewStart, NewCount int
	// NewLines are the new-file line numbers the hunk shows, in order: added
	// lines and unchanged context lines. Removed lines have no new-file number.
	NewLines []int
}

// File returns the file in d with the given path.
func (d Diff) File(path string) (File, bool) {
	for _, f := range d.Files {
		if f.Path == path {
			return f, true
		}
	}
	return File{}, false
}

// Covers reports whether any hunk of f shows a new-file line between start and
// end, inclusive. The bounds may be given in either order.
func (f File) Covers(start, end int) bool {
	if start > end {
		start, end = end, start
	}
	for _, h := range f.Hunks {
		if h.covers(start, end) {
			return true
		}
	}
	return false
}

func (h Hunk) covers(start, end int) bool {
	if len(h.NewLines) == 0 {
		// A hunk that only removes lines shows no new-file lines. Treat the
		// line it points at as covered, so a finding about the removal can
		// still cite it.
		last := h.NewStart + max(h.NewCount, 1) - 1
		return start <= last && h.NewStart <= end
	}
	for _, n := range h.NewLines {
		if start <= n && n <= end {
			return true
		}
	}
	return false
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// Parse parses the output of git diff.
//
// Inside a hunk it counts the lines the hunk header announces, so a removed
// line that starts with "--" or an added line that starts with "++" is read as
// content, not as a file header. Lines outside hunks that it doesn't need
// (index, mode, rename, "Binary files ... differ") are skipped. Deleted files
// have no new-file path and are left out.
//
// Parse returns an error only for a malformed hunk header, or a hunk header
// that appears before any file header.
func Parse(raw string) (Diff, error) {
	var p parser
	for i, line := range strings.Split(raw, "\n") {
		if err := p.line(line); err != nil {
			return Diff{}, fmt.Errorf("line %d: %w", i+1, err)
		}
	}
	p.endFile()
	return Diff{Files: p.files}, nil
}

// parser holds the state of Parse between lines.
type parser struct {
	files []File
	file  *File // the file being read; nil before the first file header
	hunk  *Hunk // the hunk being read; nil outside a hunk body

	oldLeft, newLeft int // body lines the current hunk has yet to show
	next             int // new-file number of the next added or context line
}

func (p *parser) line(line string) error {
	if p.hunk != nil {
		if p.body(line) {
			return nil
		}
		p.endHunk()
	}

	switch {
	case strings.HasPrefix(line, "diff --git "):
		p.endFile()
		p.file = &File{}
	case strings.HasPrefix(line, "+++ "):
		if p.file == nil { // plain "diff -u" output has no "diff --git" line
			p.file = &File{}
		}
		if path := strings.TrimSpace(line[len("+++ "):]); path != "/dev/null" {
			p.file.Path = strings.TrimPrefix(path, "b/")
		}
	case strings.HasPrefix(line, "@@ "):
		return p.startHunk(line)
	}
	return nil
}

// body reads one line of the current hunk's body. It reports false when the
// line is not part of the body, which happens when the diff is cut short.
func (p *parser) body(line string) bool {
	switch {
	case line == "" || line[0] == ' ':
		// Context line. Some tools strip the leading space from blank lines.
		p.hunk.NewLines = append(p.hunk.NewLines, p.next)
		p.next++
		p.oldLeft--
		p.newLeft--
	case line[0] == '+':
		p.file.Additions++
		p.hunk.NewLines = append(p.hunk.NewLines, p.next)
		p.next++
		p.newLeft--
	case line[0] == '-':
		p.file.Deletions++
		p.oldLeft--
	case line[0] == '\\':
		// "\ No newline at end of file" describes the previous line.
	default:
		return false
	}
	if p.oldLeft <= 0 && p.newLeft <= 0 {
		p.endHunk()
	}
	return true
}

func (p *parser) startHunk(line string) error {
	if p.file == nil {
		return fmt.Errorf("hunk header before any file header: %q", line)
	}
	m := hunkHeader.FindStringSubmatch(line)
	if m == nil {
		return fmt.Errorf("malformed hunk header: %q", line)
	}
	var (
		h   Hunk
		err error
	)
	if h.OldStart, h.OldCount, err = startAndCount(m[1], m[2]); err != nil {
		return fmt.Errorf("hunk header %q: %w", line, err)
	}
	if h.NewStart, h.NewCount, err = startAndCount(m[3], m[4]); err != nil {
		return fmt.Errorf("hunk header %q: %w", line, err)
	}
	p.hunk = &h
	p.oldLeft, p.newLeft = h.OldCount, h.NewCount
	p.next = h.NewStart
	if p.oldLeft == 0 && p.newLeft == 0 {
		p.endHunk()
	}
	return nil
}

// startAndCount parses the two numbers of one side of a hunk header. A missing
// count means 1.
func startAndCount(start, count string) (int, int, error) {
	s, err := strconv.Atoi(start)
	if err != nil {
		return 0, 0, err
	}
	if count == "" {
		return s, 1, nil
	}
	c, err := strconv.Atoi(count)
	if err != nil {
		return 0, 0, err
	}
	return s, c, nil
}

func (p *parser) endHunk() {
	if p.hunk != nil {
		p.file.Hunks = append(p.file.Hunks, *p.hunk)
		p.hunk = nil
	}
}

func (p *parser) endFile() {
	p.endHunk()
	if p.file != nil && p.file.Path != "" {
		p.files = append(p.files, *p.file)
	}
	p.file = nil
}
