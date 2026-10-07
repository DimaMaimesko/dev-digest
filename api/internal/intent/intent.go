// Package intent derives a pull request's purpose and scope — a short
// statement of why it exists, what is in scope and what is out of scope —
// from its title, description, linked issues, linked plan/spec documents and,
// when none of those say enough, indirect signals (branch, commits, changed
// files).
//
// It never imports HTTP, SQL or SDK packages: Gather reads files and issues
// through the small FileReader and IssueReader interfaces defined here, and
// Derive asks a model through review.LLM. Callers (internal/runner) wire the
// real adapters (internal/git, internal/github) to these interfaces.
package intent

// Kind labels what kind of source one piece of an intent came from.
type Kind string

// Kinds a Source can be.
const (
	KindTitle       Kind = "title"
	KindDescription Kind = "description"
	KindIssue       Kind = "issue"
	KindDocument    Kind = "document"
	KindBranch      Kind = "branch"
	KindCommits     Kind = "commits"
	KindFiles       Kind = "files"
)

// Level is an intent's confidence. It is set by rule (see Confidence), never
// by the model's own claim.
type Level string

// Levels a Level can be.
const (
	LevelHigh   Level = "high"
	LevelMedium Level = "medium"
	LevelLow    Level = "low"
)

// Source is one input an intent was derived from. It is kept with the
// intent so the PR page and the run trace can show where it came from.
type Source struct {
	Kind      Kind
	Label     string // e.g. "pr-title", "issue-#123", "doc:specs/x.md", "branch"
	Ref       string // the issue number, document path or branch name; "" when there isn't one
	Truncated bool   // the content was cut to a cap before use
}

// Unresolved is a reference the PR's text made that intent could not use,
// with why.
type Unresolved struct {
	Ref    string
	Reason string
}

// Intent is a pull request's derived purpose and scope.
type Intent struct {
	Statement  string
	InScope    []string
	OutOfScope []string
	Confidence Level
	Sources    []Source
	Unresolved []Unresolved
}
