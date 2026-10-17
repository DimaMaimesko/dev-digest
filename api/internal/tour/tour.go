// Package tour generates a repository's onboarding tour: five independent
// sections (architecture, critical paths, how to run locally, a guided
// reading path, first tasks) answered by a language model from a bounded,
// read-only view of the repository and grounded against its file tree so a
// model can't point a reader at a path that doesn't exist.
//
// It never imports HTTP, SQL or SDK packages, and never runs a command or
// opens a file itself: internal/onboarding gathers the inputs (through
// internal/git and internal/repointel) and passes them in as plain Go
// values. tour only talks to a model through review.LLM, the same interface
// internal/intent uses.
package tour

// Kind identifies one of the tour's five sections.
type Kind string

// The tour's sections, in the order they are generated and rendered.
const (
	KindArchitecture  Kind = "architecture"
	KindCriticalPaths Kind = "critical_paths"
	KindRunLocally    Kind = "run_locally"
	KindReadingPath   Kind = "reading_path"
	KindFirstTasks    Kind = "first_tasks"
)

// Kinds lists the tour's five sections, in their fixed order.
var Kinds = [5]Kind{
	KindArchitecture,
	KindCriticalPaths,
	KindRunLocally,
	KindReadingPath,
	KindFirstTasks,
}

// Status is a section's generation status.
type Status string

// Statuses a section goes through while Generate runs it.
const (
	StatusPending    Status = "pending"
	StatusGenerating Status = "generating"
	StatusDone       Status = "done"
	StatusFailed     Status = "failed"
)

// DiagramNodeKind is what role a node in an architecture diagram plays.
type DiagramNodeKind string

// Kinds a diagram node can have.
const (
	NodeEntry      DiagramNodeKind = "entry"
	NodeModule     DiagramNodeKind = "module"
	NodeMiddleware DiagramNodeKind = "middleware"
	NodeDatastore  DiagramNodeKind = "datastore"
	NodeExternal   DiagramNodeKind = "external"
)
