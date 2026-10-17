package tour

import (
	"encoding/json"

	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

// PathRef names a file or directory from the repository's tree. Every
// PathRef in a tour's content has already passed Ground: it points at a real
// path at the commit the tour was generated from.
//
// The JSON field names match the TourPathRef contract
// (client/src/vendor/shared/contracts/onboarding.ts).
type PathRef struct {
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

// SummarySegment is one piece of an architecture section's summary: either
// plain text, or a grounded path rendered as a chip. It marshals to the
// TourSummarySegment discriminated union: a "text" segment carries only
// "text", a "path" segment carries only "path" and "is_dir", both tagged by
// "type".
type SummarySegment struct {
	Type  string // "text" or "path"
	Text  string // set when Type == "text"
	Path  string // set when Type == "path"
	IsDir bool   // set when Type == "path"
}

// TextSegment makes a plain-text SummarySegment.
func TextSegment(text string) SummarySegment {
	return SummarySegment{Type: "text", Text: text}
}

// PathSegment makes a grounded-path SummarySegment.
func PathSegment(ref Ref) SummarySegment {
	return SummarySegment{Type: "path", Path: ref.Path, IsDir: ref.IsDir}
}

// MarshalJSON renders s as the TourSummarySegment union: only the fields its
// Type uses.
func (s SummarySegment) MarshalJSON() ([]byte, error) {
	switch s.Type {
	case "path":
		return json.Marshal(struct {
			Type  string `json:"type"`
			Path  string `json:"path"`
			IsDir bool   `json:"is_dir"`
		}{"path", s.Path, s.IsDir})
	default:
		return json.Marshal(struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{"text", s.Text})
	}
}

// DiagramNode is one box in an architecture diagram.
type DiagramNode struct {
	ID    string          `json:"id"`
	Label string          `json:"label"`
	Kind  DiagramNodeKind `json:"kind"`
	Path  *string         `json:"path"`
	IsDir bool            `json:"is_dir"`
}

// DiagramEdge is one arrow in an architecture diagram, between two Node.ID
// values.
type DiagramEdge struct {
	From  string  `json:"from"`
	To    string  `json:"to"`
	Label *string `json:"label"`
}

// Diagram is an architecture section's node-and-edge diagram. A nil
// *Diagram in ArchitectureContent means "Diagram unavailable" (AC-23).
type Diagram struct {
	Nodes []DiagramNode `json:"nodes"`
	Edges []DiagramEdge `json:"edges"`
}

// ArchitectureContent is the architecture section's wire content.
type ArchitectureContent struct {
	Summary []SummarySegment `json:"summary"`
	Diagram *Diagram         `json:"diagram"`
}

// PathItem is one entry of a critical_paths or reading_path section.
type PathItem struct {
	Path   string `json:"path"`
	IsDir  bool   `json:"is_dir"`
	Reason string `json:"reason"`
}

// PathListContent is the wire content shared by the critical_paths and
// reading_path sections.
type PathListContent struct {
	Items []PathItem `json:"items"`
}

// RunStep is one command of the run_locally section.
type RunStep struct {
	Command string  `json:"command"`
	Comment *string `json:"comment"`
}

// RunLocallyContent is the run_locally section's wire content.
type RunLocallyContent struct {
	Steps []RunStep `json:"steps"`
}

// TodoRef is a TODO/FIXME a first_tasks task addresses. Text always comes
// from the repository's TODO scan, never from the model (AC-41).
type TodoRef struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Task is one entry of the first_tasks section.
type Task struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Size        string    `json:"size"` // "small" or "medium"
	Paths       []PathRef `json:"paths"`
	Todo        *TodoRef  `json:"todo"`
}

// Task sizes.
const (
	SizeSmall  = "small"
	SizeMedium = "medium"
)

// FirstTasksContent is the first_tasks section's wire content.
type FirstTasksContent struct {
	Tasks []Task `json:"tasks"`
}

// SectionResult is one section's outcome from Generate.
type SectionResult struct {
	Kind Kind
	// Status is StatusDone or StatusFailed once Generate has returned a
	// final SectionResult for this kind.
	Status Status
	// Reason is set only when Status is StatusFailed.
	Reason string
	// RemovedRefs counts entries or references Generate dropped because
	// they didn't ground against the tree, or otherwise failed validation
	// (a too-long run_locally command, an ungrounded diagram node path).
	RemovedRefs int
	// Content is one of ArchitectureContent, PathListContent,
	// RunLocallyContent or FirstTasksContent, matching Kind. It is nil when
	// Status is StatusFailed.
	Content any
}

// Result is Generate's answer: every section's outcome, plus the usage
// summed over every attempt of every section, failed sections included
// (AC-32).
type Result struct {
	Sections [5]SectionResult
	Usage    review.Usage
}
