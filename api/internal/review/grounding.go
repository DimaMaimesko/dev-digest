package review

import (
	"fmt"

	"github.com/DimaMaimesko/dev-digest/api/internal/diff"
)

// Grounding is the result of checking findings against the diff they review.
type Grounding struct {
	Kept    []Finding
	Dropped []Dropped
}

// Dropped is a finding that Ground rejected, with the reason shown in the run
// trace.
type Dropped struct {
	Finding Finding
	Reason  string
}

// Ground keeps only the findings that point at something the diff shows. This
// removes locations a model made up.
//
// A finding is kept when its file is in the diff and its line range overlaps a
// line shown by one of that file's hunks, whatever its Kind.
func Ground(findings []Finding, d diff.Diff) Grounding {
	var g Grounding
	for _, f := range findings {
		file, ok := d.File(f.File)
		switch {
		case !ok:
			g.Dropped = append(g.Dropped, Dropped{
				Finding: f,
				Reason:  fmt.Sprintf("file %q is not in the diff", f.File),
			})
		case file.Covers(f.StartLine, f.EndLine):
			g.Kept = append(g.Kept, f)
		default:
			g.Dropped = append(g.Dropped, Dropped{
				Finding: f,
				Reason: fmt.Sprintf("lines %d-%d of %q are not in any diff hunk",
					f.StartLine, f.EndLine, f.File),
			})
		}
	}
	return g
}

// Summary says how many findings passed, such as "3/4 passed". The run trace
// shows it.
func (g Grounding) Summary() string {
	return fmt.Sprintf("%d/%d passed", len(g.Kept), len(g.Kept)+len(g.Dropped))
}
