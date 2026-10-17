package tour

import (
	"fmt"
	"sort"
	"strings"

	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

// Doc is one allowlisted file's content (Readable && !Excluded), already
// read by internal/onboarding. tour never reads a file itself.
type Doc struct {
	Path string
	Text string
}

// Inputs is everything a tour's five sections are generated from, gathered
// once by internal/onboarding at the run's commit so a later refresh can't
// change what a tour describes mid-generation.
type Inputs struct {
	// Tree is every file and directory at the run's commit (git.ListTree's
	// shape). Generate builds the Tree every section grounds against from
	// this.
	Tree []Entry
	// RepoMap is internal/repointel's rendered skeleton of the code.
	RepoMap string
	// README is the root README's content, or "" when there isn't one.
	README string
	// Docs are the allowlisted root/top-level docs and manifests
	// (Readable && !Excluded), for architecture and run_locally.
	Docs []Doc
	// RankedFiles are file paths ordered most-important first
	// (file_rank), for critical_paths, reading_path and first_tasks.
	RankedFiles []string
	// RoutesCrons is a pre-rendered list of the files with HTTP routes or
	// cron schedules (file_facts), for architecture and critical_paths.
	RoutesCrons string
	// Todos are the TODO/FIXME matches from the commit's grep, already
	// filtered through Excluded. Text is the scanned line, the only
	// source first_tasks.Task.Todo.Text may use (AC-41).
	Todos []TodoRef
}

// maxTreeRunes caps the architecture and critical_paths/reading_path tree
// rendering (Approach -> Domain, Section inputs).
//
// Every kind's part caps are kept well under callBudgetRunes (48,000) once
// the <untrusted> tag overhead and the blank-line separators between parts
// are added: see TestPartCapsFitCallBudget. architecture is the tightest
// kind (tree + repo-map + docs + routes-crons), so its sum alone must stay
// comfortably below the budget.
const maxTreeRunes = 12000

// maxRankedFiles caps how many ranked files a section's input lists.
const maxRankedFiles = 40

// maxDocRunes caps a single doc's rendered length; maxDocsTotalRunes caps
// their sum, both for run_locally's docs-and-manifests input and
// architecture's docs part.
const (
	maxDocRunes       = 8000
	maxDocsTotalRunes = 20000
)

// maxTodos caps how many TODOs first_tasks's input lists; maxTodoRunes caps
// each one's rendered text.
const (
	maxTodos     = 60
	maxTodoRunes = 200
)

// maxRepoMapRunes, maxRoutesCronsRunes, maxReadmeRunes and
// maxRankedListRunes cap the parts whose own rendering has no built-in
// limit (RepoMap and RoutesCrons are repointel's own text; README has no
// cap in Inputs; a rendered ranked-files list is bounded in count, not
// runes). Each part is clipped to its own budget before it is wrapped in
// review.Untrusted, so every <untrusted> block stays closed — the final
// clip in userMessage is a safety net, not how a call normally fits its
// budget.
const (
	maxRepoMapRunes     = 8000
	maxRoutesCronsRunes = 4000
	maxReadmeRunes      = 8000
	maxRankedListRunes  = 6000
)

// userMessage builds the user message for kind from in, wrapping each
// section as review.Untrusted data. Every part is clipped to its own budget
// before wrapping, so each <untrusted> block is complete (opened and
// closed) on its own; joinBudgeted then keeps only as many whole parts as
// fit callBudgetRunes, dropping a trailing part entirely rather than
// cutting it in half. The final clipRunesEllipsis is a last-resort safety
// net: it should never actually cut, since joinBudgeted already respects
// the budget.
func userMessage(kind Kind, in Inputs, tree Tree) string {
	var parts []string
	switch kind {
	case KindArchitecture:
		// Ordered by priority: the tree grounds everything else, docs carry
		// the most project-level context, then the repo map and the
		// routes/crons list. On a large repo where not everything fits,
		// joinBudgeted keeps earlier parts and skips only the ones that
		// don't fit (see joinBudgeted).
		parts = []string{
			review.Untrusted("tree", renderTree(tree, maxTreeRunes)),
			review.Untrusted("docs", renderDocs(in.Docs, maxDocRunes, maxDocsTotalRunes)),
			review.Untrusted("repo-map", clipPart(in.RepoMap, maxRepoMapRunes)),
			review.Untrusted("routes-crons", clipPart(in.RoutesCrons, maxRoutesCronsRunes)),
		}
	case KindCriticalPaths:
		parts = []string{
			review.Untrusted("top-ranked-files", clipPart(renderRankedFiles(in.RankedFiles, maxRankedFiles), maxRankedListRunes)),
			review.Untrusted("routes-crons", clipPart(in.RoutesCrons, maxRoutesCronsRunes)),
			review.Untrusted("repo-map", clipPart(in.RepoMap, maxRepoMapRunes)),
			review.Untrusted("tree", renderTree(tree, maxTreeRunes)),
		}
	case KindRunLocally:
		parts = []string{
			review.Untrusted("docs", renderDocs(in.Docs, maxDocRunes, maxDocsTotalRunes)),
		}
	case KindReadingPath:
		parts = []string{
			review.Untrusted("top-ranked-files", clipPart(renderRankedFiles(in.RankedFiles, maxRankedFiles), maxRankedListRunes)),
			review.Untrusted("repo-map", clipPart(in.RepoMap, maxRepoMapRunes)),
			review.Untrusted("readme", clipPart(in.README, maxReadmeRunes)),
			review.Untrusted("tree", renderTree(tree, maxTreeRunes)),
		}
	case KindFirstTasks:
		parts = []string{
			review.Untrusted("todos", renderTodos(in.Todos, maxTodos, maxTodoRunes)),
			review.Untrusted("top-ranked-files", clipPart(renderRankedFiles(in.RankedFiles, maxRankedFiles), maxRankedListRunes)),
			review.Untrusted("tree", renderTree(tree, maxTreeRunes)),
		}
	}
	msg := joinBudgeted(parts, callBudgetRunes)
	return clipRunesEllipsis(msg, callBudgetRunes)
}

// clipPart clips s to n runes with an ellipsis, for a part whose own
// rendering doesn't already enforce a cap.
func clipPart(s string, n int) string {
	return clipRunesEllipsis(s, n)
}

// joinBudgeted joins parts (each already a complete, closed <untrusted>
// block) with a blank line between them, keeping only as many whole parts
// as fit within budget: a part that wouldn't fit is skipped (never cut
// mid-tag), but later, smaller parts are still tried — a single oversized
// part doesn't drop everything after it.
func joinBudgeted(parts []string, budget int) string {
	var b strings.Builder
	used := 0
	for _, p := range parts {
		sep := 0
		if b.Len() > 0 {
			sep = 2 // "\n\n"
		}
		if used+sep+runeLen(p) > budget {
			continue
		}
		if sep > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(p)
		used += sep + runeLen(p)
	}
	return b.String()
}

// renderRankedFiles lists up to max of files, one per line, most important
// first.
func renderRankedFiles(files []string, max int) string {
	if len(files) > max {
		files = files[:max]
	}
	return strings.Join(files, "\n")
}

// renderDocs renders docs as labelled sections, each clipped to perDoc
// runes, stopping once the running total would exceed total.
func renderDocs(docs []Doc, perDoc, total int) string {
	var b strings.Builder
	used := 0
	for _, d := range docs {
		text, _ := clipRunes(d.Text, perDoc)
		section := fmt.Sprintf("### %s\n%s", d.Path, text)
		if used+runeLen(section) > total {
			break
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(section)
		used += runeLen(section)
	}
	return b.String()
}

// renderTodos lists up to max TODOs as "path:line: text", each clipped to
// perTodo runes.
func renderTodos(todos []TodoRef, max, perTodo int) string {
	if len(todos) > max {
		todos = todos[:max]
	}
	lines := make([]string, len(todos))
	for i, t := range todos {
		text, _ := clipRunes(t.Text, perTodo)
		lines[i] = fmt.Sprintf("%s:%d: %s", t.Path, t.Line, text)
	}
	return strings.Join(lines, "\n")
}

// renderTree renders t's files and directories, one per line, dirs suffixed
// "/". When that exceeds maxRunes, it collapses directories deeper than
// depth 2 into a per-directory file count instead; if that's still over
// budget, it collapses again at depth 1; if even that doesn't fit, it keeps
// as many whole lines as fit and appends a "… N more" line, so the result
// never exceeds maxRunes and never cuts a line in half.
func renderTree(t Tree, maxRunes int) string {
	entries := t.Entries()
	full := renderTreeFull(entries)
	if runeLen(full) <= maxRunes {
		return full
	}
	for _, depth := range []int{2, 1} {
		collapsed := renderTreeCollapsed(entries, depth)
		if runeLen(collapsed) <= maxRunes {
			return collapsed
		}
	}
	return clipTreeLines(renderTreeCollapsed(entries, 1), maxRunes)
}

func renderTreeFull(entries []Entry) string {
	lines := make([]string, len(entries))
	for i, e := range entries {
		if e.Dir {
			lines[i] = e.Path + "/"
		} else {
			lines[i] = e.Path
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// renderTreeCollapsed keeps entries at or above depth directory segments as
// full lines, and folds anything deeper into a per-directory file count at
// that depth. A directory kept as a full line at exactly depth carries its
// own count on that same line ("dir/ (N files)") instead of a separate
// "dir/ (N files)" line, since git.ListTree always lists the directory
// itself alongside its files.
func renderTreeCollapsed(entries []Entry, depth int) string {
	type keptEntry struct {
		path string
		dir  bool
	}
	var kept []keptEntry
	keptDirs := make(map[string]bool)
	counts := make(map[string]int)
	var prefixes []string

	for _, e := range entries {
		segs := strings.Split(e.Path, "/")
		if len(segs) <= depth {
			kept = append(kept, keptEntry{path: e.Path, dir: e.Dir})
			if e.Dir {
				keptDirs[e.Path] = true
			}
			continue
		}
		if e.Dir {
			continue // only files count toward a collapsed directory's total
		}
		prefix := strings.Join(segs[:depth], "/")
		if _, ok := counts[prefix]; !ok {
			prefixes = append(prefixes, prefix)
		}
		counts[prefix]++
	}

	sort.Slice(kept, func(i, j int) bool { return kept[i].path < kept[j].path })
	sort.Strings(prefixes)
	lines := make([]string, 0, len(kept)+len(prefixes))
	for _, k := range kept {
		if !k.dir {
			lines = append(lines, k.path)
			continue
		}
		if n, ok := counts[k.path]; ok {
			lines = append(lines, fmt.Sprintf("%s/ (%d files)", k.path, n))
			continue
		}
		lines = append(lines, k.path+"/")
	}
	for _, p := range prefixes {
		if keptDirs[p] {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s/ (%d files)", p, counts[p]))
	}
	return strings.Join(lines, "\n")
}

// clipTreeLines keeps as many of s's whole lines as fit within maxRunes,
// then appends a "… N more" line naming how many were dropped, shrinking
// the kept lines further if needed to make room for that note. It never
// cuts a line (or the note) in half.
func clipTreeLines(s string, maxRunes int) string {
	lines := strings.Split(s, "\n")
	kept := 0
	used := 0
	for kept < len(lines) {
		add := runeLen(lines[kept])
		if kept > 0 {
			add++ // the newline before it
		}
		if used+add > maxRunes {
			break
		}
		used += add
		kept++
	}
	dropped := len(lines) - kept
	if dropped == 0 {
		return strings.Join(lines[:kept], "\n")
	}

	note := fmt.Sprintf("… %d more", dropped)
	for kept > 0 {
		candidate := strings.Join(lines[:kept], "\n") + "\n" + note
		if runeLen(candidate) <= maxRunes {
			return candidate
		}
		kept--
		dropped++
		note = fmt.Sprintf("… %d more", dropped)
	}
	return clipRunesEllipsis(note, maxRunes)
}
