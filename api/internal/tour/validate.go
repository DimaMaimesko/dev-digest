package tour

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

// ErrNoEntries means a section's answer parsed and matched its schema, but
// grounding left nothing verifiable (AC-19): every path the model named was
// made up, or every run_locally step was unusable. Unlike a schema
// violation, this doesn't get a repair attempt: the model already named
// everything it had.
var ErrNoEntries = errors.New("no verifiable entries")

// Clipping limits applied to a model's answer, by kind (Approach -> Domain,
// per-kind validation table).
const (
	maxSummaryRunes = 1200
	maxReasonRunes  = 140
	maxCommandRunes = 300
	maxTitleRunes   = 100
	maxDescRunes    = 400
	maxDiagramNodes = 12
)

// parseAndValidate reads kind's model answer out of text, then validates,
// clips and grounds it against tree per the Approach -> Domain table. On
// success it returns the wire content (one of ArchitectureContent,
// PathListContent, RunLocallyContent, FirstTasksContent) and how many
// entries or references were removed. todos is only used by
// KindFirstTasks, to attach a TODO's scanned text (AC-41).
func parseAndValidate(kind Kind, tree Tree, todos []TodoRef, text string) (any, int, error) {
	obj, err := review.JSONObject(text)
	if err != nil {
		return nil, 0, err
	}
	switch kind {
	case KindArchitecture:
		return validateArchitecture(tree, obj)
	case KindCriticalPaths:
		return validatePathList(tree, obj, 10, 0)
	case KindReadingPath:
		return validatePathList(tree, obj, 12, 3)
	case KindRunLocally:
		return validateRunLocally(obj)
	case KindFirstTasks:
		return validateFirstTasks(tree, todos, obj)
	default:
		return nil, 0, fmt.Errorf("unknown kind %q", kind)
	}
}

// --- architecture ---

type archNode struct {
	ID    string  `json:"id"`
	Label string  `json:"label"`
	Kind  string  `json:"kind"`
	Path  *string `json:"path"`
}

type archEdge struct {
	From  string  `json:"from"`
	To    string  `json:"to"`
	Label *string `json:"label"`
}

type archAnswer struct {
	Summary string     `json:"summary"`
	Nodes   []archNode `json:"nodes"`
	Edges   []archEdge `json:"edges"`
}

func validateArchitecture(tree Tree, raw json.RawMessage) (ArchitectureContent, int, error) {
	var ans archAnswer
	if err := json.Unmarshal(raw, &ans); err != nil {
		return ArchitectureContent{}, 0, fmt.Errorf("it does not match the schema: %w", err)
	}

	summary, removed := buildSummary(tree, ans.Summary)
	diagram, diagRemoved := buildDiagram(tree, ans.Nodes, ans.Edges)
	removed += diagRemoved

	return ArchitectureContent{Summary: summary, Diagram: diagram}, removed, nil
}

// buildSummary clips text to maxSummaryRunes, then turns every backtick span
// that grounds into a PathSegment; a span that doesn't ground stays as plain
// text, without its backticks, and counts as removed (AC-17).
func buildSummary(tree Tree, text string) ([]SummarySegment, int) {
	text, _ = clipRunes(text, maxSummaryRunes)
	parts := strings.Split(text, "`")

	var segs []SummarySegment
	var buf strings.Builder
	removed := 0
	for i, part := range parts {
		if i%2 == 0 {
			buf.WriteString(part)
			continue
		}
		if ref, ok := Ground(tree, part); ok {
			if buf.Len() > 0 {
				segs = append(segs, TextSegment(buf.String()))
				buf.Reset()
			}
			segs = append(segs, PathSegment(ref))
			continue
		}
		buf.WriteString(part)
		removed++
	}
	if buf.Len() > 0 || len(segs) == 0 {
		segs = append(segs, TextSegment(buf.String()))
	}
	return segs, removed
}

// buildDiagram grounds each node's path, dropping it (path = null, counted
// as removed) when it doesn't ground. The whole diagram is unavailable
// (nil, AC-23) when the answer has more than maxDiagramNodes nodes, an edge
// names a node id that doesn't exist, two nodes share an id, or a node's
// kind isn't one of the five allowed.
func buildDiagram(tree Tree, nodes []archNode, edges []archEdge) (*Diagram, int) {
	if len(nodes) == 0 {
		return nil, 0
	}
	if len(nodes) > maxDiagramNodes {
		return nil, 0
	}

	seen := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		if n.ID == "" || seen[n.ID] {
			return nil, 0
		}
		seen[n.ID] = true
		if !validNodeKind(n.Kind) {
			return nil, 0
		}
	}
	for _, e := range edges {
		if !seen[e.From] || !seen[e.To] {
			return nil, 0
		}
	}

	removed := 0
	outNodes := make([]DiagramNode, len(nodes))
	for i, n := range nodes {
		var path *string
		isDir := false
		if n.Path != nil {
			if ref, ok := Ground(tree, *n.Path); ok {
				p := ref.Path
				path = &p
				isDir = ref.IsDir
			} else {
				removed++
			}
		}
		outNodes[i] = DiagramNode{ID: n.ID, Label: n.Label, Kind: DiagramNodeKind(n.Kind), Path: path, IsDir: isDir}
	}
	outEdges := make([]DiagramEdge, len(edges))
	for i, e := range edges {
		outEdges[i] = DiagramEdge(e)
	}
	return &Diagram{Nodes: outNodes, Edges: outEdges}, removed
}

func validNodeKind(k string) bool {
	switch DiagramNodeKind(k) {
	case NodeEntry, NodeModule, NodeMiddleware, NodeDatastore, NodeExternal:
		return true
	default:
		return false
	}
}

// --- critical_paths / reading_path ---

type pathListAnswer struct {
	Items []struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	} `json:"items"`
}

// validatePathList validates a critical_paths or reading_path answer: at
// most maxItems entries, grounded against tree, with the reason clipped. An
// answer with fewer than minRawItems entries, before grounding, is a schema
// violation (repaired); grounding leaving zero entries is ErrNoEntries.
func validatePathList(tree Tree, raw json.RawMessage, maxItems, minRawItems int) (PathListContent, int, error) {
	var ans pathListAnswer
	if err := json.Unmarshal(raw, &ans); err != nil {
		return PathListContent{}, 0, fmt.Errorf("it does not match the schema: %w", err)
	}
	if len(ans.Items) < minRawItems {
		return PathListContent{}, 0, fmt.Errorf(
			"it does not match the schema: items has %d entries, want at least %d",
			len(ans.Items), minRawItems)
	}
	if len(ans.Items) > maxItems {
		ans.Items = ans.Items[:maxItems]
	}

	var items []PathItem
	removed := 0
	for _, it := range ans.Items {
		ref, ok := Ground(tree, it.Path)
		if !ok {
			removed++
			continue
		}
		reason, _ := clipRunes(it.Reason, maxReasonRunes)
		items = append(items, PathItem{Path: ref.Path, IsDir: ref.IsDir, Reason: reason})
	}
	if len(items) == 0 {
		return PathListContent{}, removed, ErrNoEntries
	}
	return PathListContent{Items: items}, removed, nil
}

// --- run_locally ---

type runLocallyAnswer struct {
	Steps []struct {
		Command string  `json:"command"`
		Comment *string `json:"comment"`
	} `json:"steps"`
}

// validateRunLocally trims each command and drops it when it spans more
// than one line, contains a control character (including U+2028/U+2029, the
// Unicode line separators) or is over maxCommandRunes: a truncated command
// is dangerous to copy, so run_locally steps are dropped whole, never
// clipped.
func validateRunLocally(raw json.RawMessage) (RunLocallyContent, int, error) {
	var ans runLocallyAnswer
	if err := json.Unmarshal(raw, &ans); err != nil {
		return RunLocallyContent{}, 0, fmt.Errorf("it does not match the schema: %w", err)
	}
	if len(ans.Steps) > 15 {
		ans.Steps = ans.Steps[:15]
	}

	var steps []RunStep
	removed := 0
	for _, s := range ans.Steps {
		cmd := strings.TrimSpace(s.Command)
		if cmd == "" || hasDangerousRunes(cmd) || runeLen(cmd) > maxCommandRunes {
			removed++
			continue
		}
		steps = append(steps, RunStep{Command: cmd, Comment: cleanComment(s.Comment)})
	}
	if len(steps) == 0 {
		return RunLocallyContent{}, removed, ErrNoEntries
	}
	return RunLocallyContent{Steps: steps}, removed, nil
}

// hasDangerousRunes reports whether cmd spans more than one line (plain
// '\n', or the Unicode line separators U+2028/U+2029, which render as a
// line break but don't match '\n') or contains any other control character.
// Any of these makes the command unsafe to show and copy as a single shell
// line, so it's dropped whole rather than clipped (Approach -> Domain,
// "Over-long model output").
func hasDangerousRunes(cmd string) bool {
	if strings.ContainsAny(cmd, "\u2028\u2029") {
		return true
	}
	return strings.ContainsFunc(cmd, unicode.IsControl)
}

// maxCommentRunes caps a cleaned run_locally comment, mirroring the
// command's own cap (Approach -> Domain, "run_locally"): a comment is
// optional context next to a command, not something that needs its own
// separate limit.
const maxCommentRunes = 300

// cleanComment trims comment, replaces any control character (including the
// Unicode line separators U+2028/U+2029, which don't match unicode.IsControl
// but render as a line break) with a space, clips the result to
// maxCommentRunes, and turns an all-whitespace or empty result into nil: a
// run_locally comment is optional, and an unchecked model-written comment
// could otherwise smuggle a fake line break or an over-long aside into text
// rendered next to a copyable command.
func cleanComment(comment *string) *string {
	if comment == nil {
		return nil
	}
	s := strings.TrimSpace(*comment)
	if s == "" {
		return nil
	}

	cleaned := strings.Map(func(r rune) rune {
		if r == '\u2028' || r == '\u2029' || unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	cleaned = strings.TrimSpace(cleaned)
	cleaned, _ = clipRunes(cleaned, maxCommentRunes)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

// --- first_tasks ---

type firstTasksAnswer struct {
	Tasks []struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Size        string   `json:"size"`
		Paths       []string `json:"paths"`
		Todo        *struct {
			Path string `json:"path"`
			Line int    `json:"line"`
		} `json:"todo"`
	} `json:"tasks"`
}

// validateFirstTasks grounds each task's paths, dropping the ones that
// don't ground; a task left with no grounded path is dropped entirely
// (counted as removed). A todo is attached only when (path, line) matches a
// scanned TodoRef; its Text always comes from the scan, never the model
// (AC-41), and its file is added to paths when missing.
func validateFirstTasks(tree Tree, todos []TodoRef, raw json.RawMessage) (FirstTasksContent, int, error) {
	var ans firstTasksAnswer
	if err := json.Unmarshal(raw, &ans); err != nil {
		return FirstTasksContent{}, 0, fmt.Errorf("it does not match the schema: %w", err)
	}
	if len(ans.Tasks) > 5 {
		ans.Tasks = ans.Tasks[:5]
	}

	var tasks []Task
	removed := 0
	for _, t := range ans.Tasks {
		var paths []PathRef
		for _, p := range t.Paths {
			ref, ok := Ground(tree, p)
			if !ok {
				removed++
				continue
			}
			if !slices.ContainsFunc(paths, func(p PathRef) bool { return p.Path == ref.Path }) {
				paths = append(paths, ref) // Ref is an alias for PathRef (ground.go)
			}
		}

		var todoRef *TodoRef
		if t.Todo != nil {
			if scanned := findTodo(todos, t.Todo.Path, t.Todo.Line); scanned != nil {
				todoRef = scanned
				if !slices.ContainsFunc(paths, func(p PathRef) bool { return p.Path == scanned.Path }) {
					paths = append(paths, PathRef{Path: scanned.Path, IsDir: false})
				}
			}
		}

		if len(paths) == 0 {
			removed++
			continue
		}

		size := t.Size
		if size != SizeSmall && size != SizeMedium {
			size = SizeSmall
		}
		title, _ := clipRunes(strings.TrimSpace(t.Title), maxTitleRunes)
		desc, _ := clipRunes(strings.TrimSpace(t.Description), maxDescRunes)
		tasks = append(tasks, Task{
			Title:       title,
			Description: desc,
			Size:        size,
			Paths:       paths,
			Todo:        todoRef,
		})
	}
	if len(tasks) == 0 {
		return FirstTasksContent{}, removed, ErrNoEntries
	}
	return FirstTasksContent{Tasks: tasks}, removed, nil
}

// findTodo returns the scanned TodoRef matching (path, line), normalizing
// path the same way Ground does, or nil when there's no match.
func findTodo(todos []TodoRef, path string, line int) *TodoRef {
	norm := normalizePath(path)
	for i := range todos {
		if todos[i].Line == line && normalizePath(todos[i].Path) == norm {
			t := todos[i]
			return &t
		}
	}
	return nil
}
