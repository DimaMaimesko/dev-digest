// Package review turns a pull request's diff into review findings that point
// at lines the diff really shows.
package review

// Severity says how serious a finding is.
type Severity string

// Severities, from most to least serious.
const (
	SeverityCritical   Severity = "CRITICAL"
	SeverityWarning    Severity = "WARNING"
	SeveritySuggestion Severity = "SUGGESTION"
)

// Category says what area of quality a finding is about.
type Category string

// Categories a finding can have.
const (
	CategoryBug      Category = "bug"
	CategorySecurity Category = "security"
	CategoryPerf     Category = "perf"
	CategoryStyle    Category = "style"
	CategoryTest     Category = "test"
)

// Kind says what produced a finding. The zero value, like KindFinding, means
// an ordinary finding from a reviewer agent, tied to changed lines.
type Kind string

// Kinds of findings. All kinds except KindFinding come from scanners that read
// whole files; later lessons add them.
const (
	KindFinding        Kind = "finding"
	KindSecretLeak     Kind = "secret_leak"
	KindLethalTrifecta Kind = "lethal_trifecta"
	KindPhantom        Kind = "phantom"
	KindHook           Kind = "hook"
)

// wholeFile reports whether findings of kind k come from a scanner that reads
// whole files rather than the diff.
func (k Kind) wholeFile() bool {
	switch k {
	case KindSecretLeak, KindLethalTrifecta, KindPhantom, KindHook:
		return true
	}
	return false
}

// Finding is one issue a reviewer reports about a range of lines in a file.
// The JSON field names match the API contract the web client uses.
type Finding struct {
	ID         string   `json:"id"`
	Severity   Severity `json:"severity"`
	Category   Category `json:"category"`
	Title      string   `json:"title"`
	File       string   `json:"file"`
	StartLine  int      `json:"start_line"`
	EndLine    int      `json:"end_line"`
	Rationale  string   `json:"rationale"`            // Markdown
	Suggestion string   `json:"suggestion,omitempty"` // Markdown
	Confidence float64  `json:"confidence"`           // from 0 to 1
	Kind       Kind     `json:"kind,omitempty"`
}
