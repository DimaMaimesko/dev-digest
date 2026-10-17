package tour

import (
	"strconv"
	"strings"
	"testing"
)

func TestRenderTreeCollapsesWhenOverBudget(t *testing.T) {
	var entries []Entry
	for i := 0; i < 2000; i++ {
		entries = append(entries, Entry{Path: "pkg/deep/nested/file" + strconv.Itoa(i) + ".go"})
	}
	tree := NewTree(entries)

	rendered := renderTree(tree, 1000)
	if runeLen(rendered) > 1000 {
		t.Fatalf("rendered tree is %d runes, want <= 1000", runeLen(rendered))
	}
	if !strings.Contains(rendered, "files)") {
		t.Fatalf("rendered = %q, want a collapsed directory with a file count", rendered)
	}
}

// buildPkgDeepNestedEntries mirrors what git.ListTree returns: every
// intermediate directory is its own entry alongside the files under it, not
// just the files.
func buildPkgDeepNestedEntries(numFiles int) []Entry {
	entries := []Entry{
		{Path: "pkg", Dir: true},
		{Path: "pkg/deep", Dir: true},
		{Path: "pkg/deep/nested", Dir: true},
	}
	for i := 0; i < numFiles; i++ {
		entries = append(entries, Entry{Path: "pkg/deep/nested/file" + strconv.Itoa(i) + ".go"})
	}
	return entries
}

// TestRenderTreeCollapsedPutsCountOnKeptDirLine proves the fix for "every
// collapsed prefix is also a directory entry, so keptDirs skips every count
// line and the counts vanish": when a prefix is kept as its own directory
// line (because git.ListTree lists directories as entries, not just their
// files), its file count must appear on that same line, not be dropped.
func TestRenderTreeCollapsedPutsCountOnKeptDirLine(t *testing.T) {
	entries := buildPkgDeepNestedEntries(500)

	t.Run("depth 2", func(t *testing.T) {
		rendered := renderTreeCollapsed(entries, 2)
		if !strings.Contains(rendered, "pkg/deep/ (500 files)") {
			t.Fatalf("rendered = %q, want a %q line", rendered, "pkg/deep/ (500 files)")
		}
		if strings.Contains(rendered, "pkg/deep/nested") {
			t.Fatalf("rendered = %q, want pkg/deep/nested folded away, not listed", rendered)
		}
		if !strings.Contains(rendered, "pkg/\n") && !strings.HasSuffix(rendered, "pkg/") {
			t.Fatalf("rendered = %q, want the kept %q line with no count (no direct files under it)", rendered, "pkg/")
		}
	})

	t.Run("depth 1", func(t *testing.T) {
		rendered := renderTreeCollapsed(entries, 1)
		if !strings.Contains(rendered, "pkg/ (500 files)") {
			t.Fatalf("rendered = %q, want a %q line", rendered, "pkg/ (500 files)")
		}
		if strings.Contains(rendered, "pkg/deep") {
			t.Fatalf("rendered = %q, want pkg/deep folded away, not listed", rendered)
		}
	})
}

func TestRenderTreeFullWhenWithinBudget(t *testing.T) {
	tree := NewTree([]Entry{{Path: "README.md"}, {Path: "src", Dir: true}, {Path: "src/x.go"}})
	rendered := renderTree(tree, maxTreeRunes)
	if !strings.Contains(rendered, "README.md") || !strings.Contains(rendered, "src/x.go") {
		t.Fatalf("rendered = %q", rendered)
	}
}

func TestUserMessageStaysWithinCallBudget(t *testing.T) {
	in := testInputs()
	tree := NewTree(in.Tree)
	for _, kind := range Kinds {
		msg := userMessage(kind, in, tree)
		if runeLen(msg) > callBudgetRunes {
			t.Errorf("%s message is %d runes, want <= %d", kind, runeLen(msg), callBudgetRunes)
		}
	}
}

// untrustedOverhead upper-bounds the <untrusted source="...">...</untrusted>
// wrapping and the "\n\n" part separator a joined part costs beyond its own
// content, for the given source labels (Approach -> Domain, "Section
// inputs"). It mirrors internal/review's untrusted() shape without
// depending on it, so this test fails loudly if that shape ever grows.
func untrustedOverhead(sources ...string) int {
	const tagOverhead = len(`<untrusted source="">`) + len("\n") + len("\n</untrusted>")
	total := 0
	for i, s := range sources {
		total += tagOverhead + len(s)
		if i > 0 {
			total += len("\n\n")
		}
	}
	return total
}

// partCapsBySource lists, for each kind, the <untrusted> source label and
// the rune cap of the part behind it, in the order userMessage builds them.
// Keeping this table next to userMessage's switch is what makes
// TestPartCapsFitCallBudget catch a future kind whose caps no longer fit.
func partCapsBySource() map[Kind][]struct {
	source string
	cap    int
} {
	return map[Kind][]struct {
		source string
		cap    int
	}{
		KindArchitecture: {
			{"tree", maxTreeRunes},
			{"docs", maxDocsTotalRunes},
			{"repo-map", maxRepoMapRunes},
			{"routes-crons", maxRoutesCronsRunes},
		},
		KindCriticalPaths: {
			{"top-ranked-files", maxRankedListRunes},
			{"routes-crons", maxRoutesCronsRunes},
			{"repo-map", maxRepoMapRunes},
			{"tree", maxTreeRunes},
		},
		KindRunLocally: {
			{"docs", maxDocsTotalRunes},
		},
		KindReadingPath: {
			{"top-ranked-files", maxRankedListRunes},
			{"repo-map", maxRepoMapRunes},
			{"readme", maxReadmeRunes},
			{"tree", maxTreeRunes},
		},
		KindFirstTasks: {
			// renderTodos's cap is maxTodos entries each clipped to
			// maxTodoRunes; the "path:line: " prefix it also renders isn't
			// separately capped, so this is an approximation, not an exact
			// bound. It holds in practice: real paths are far shorter than
			// the headroom callBudgetRunes leaves below.
			{"todos", maxTodos * maxTodoRunes},
			{"top-ranked-files", maxRankedListRunes},
			{"tree", maxTreeRunes},
		},
	}
}

// TestPartCapsFitCallBudget proves the fix for "architecture's part caps
// add up to more than callBudgetRunes": for every kind, the sum of its
// parts' rune caps plus the <untrusted> tag overhead and separators must
// stay under callBudgetRunes, with room to spare, so joinBudgeted never has
// to drop a part that was meant to fit.
func TestPartCapsFitCallBudget(t *testing.T) {
	for kind, parts := range partCapsBySource() {
		sources := make([]string, len(parts))
		sum := 0
		for i, p := range parts {
			sources[i] = p.source
			sum += p.cap
		}
		total := sum + untrustedOverhead(sources...)
		if total >= callBudgetRunes {
			t.Errorf("%s: part caps sum to %d runes (incl. tag overhead), want < %d", kind, total, callBudgetRunes)
		}
	}
}

// TestUserMessageKeepsEveryPartAtCap proves every architecture part is
// still present when each one, independently, is exactly at its own cap:
// no part is dropped just because the others are large too, as long as the
// whole still fits (which TestPartCapsFitCallBudget guarantees it does).
func TestUserMessageKeepsEveryPartAtCap(t *testing.T) {
	var treeEntries []Entry
	for i := 0; i < 5000; i++ {
		treeEntries = append(treeEntries, Entry{Path: "pkg/deep/nested/file" + strconv.Itoa(i) + ".go"})
	}
	in := Inputs{
		Tree:    treeEntries,
		RepoMap: strings.Repeat("m", maxRepoMapRunes*2),
		Docs: []Doc{
			{Path: "README.md", Text: strings.Repeat("d", maxDocRunes*2)},
			{Path: "CONTRIBUTING.md", Text: strings.Repeat("c", maxDocRunes*2)},
			{Path: "package.json", Text: strings.Repeat("p", maxDocRunes*2)},
		},
		RoutesCrons: strings.Repeat("r", maxRoutesCronsRunes*2),
	}
	tree := NewTree(in.Tree)

	msg := userMessage(KindArchitecture, in, tree)
	if runeLen(msg) > callBudgetRunes {
		t.Fatalf("message is %d runes, want <= %d", runeLen(msg), callBudgetRunes)
	}
	for _, source := range []string{"tree", "docs", "repo-map", "routes-crons"} {
		tag := `<untrusted source="` + source + `">`
		if !strings.Contains(msg, tag) {
			t.Errorf("message dropped the %q part, want all parts present when each is at its own cap", source)
		}
	}
}

// TestUserMessageKeepsBalancedUntrustedTagsWhenOverBudget proves the fix
// for "over-long input breaks the untrusted wrapping": a huge tree, repo
// map and routes/crons text — individually and combined well past
// callBudgetRunes — must never leave an unbalanced <untrusted> block, and
// the parts that do fit (here, the tree, clipped to its own budget) must
// still be present, not silently dropped along with the oversized ones.
func TestUserMessageKeepsBalancedUntrustedTagsWhenOverBudget(t *testing.T) {
	in := testInputs()
	var entries []Entry
	for i := 0; i < 5000; i++ {
		entries = append(entries, Entry{Path: "pkg/deep/nested/file" + strconv.Itoa(i) + ".go"})
	}
	in.Tree = entries
	in.RepoMap = strings.Repeat("m", 100000)
	in.RoutesCrons = strings.Repeat("r", 100000)
	in.README = strings.Repeat("d", 100000)
	tree := NewTree(entries)

	for _, kind := range Kinds {
		msg := userMessage(kind, in, tree)
		if runeLen(msg) > callBudgetRunes {
			t.Fatalf("%s message is %d runes, want <= %d", kind, runeLen(msg), callBudgetRunes)
		}
		opens := strings.Count(msg, "<untrusted source=")
		closes := strings.Count(msg, "</untrusted>")
		if opens == 0 || opens != closes {
			t.Fatalf("%s message has %d opening tags and %d closing tags, want equal and > 0:\n%s", kind, opens, closes, msg)
		}
	}

	// architecture's tree part is well within its own budget even though
	// the repo map and routes/crons are oversized: it must still appear,
	// not be dropped along with (or cut apart by) the oversized parts.
	archMsg := userMessage(KindArchitecture, in, tree)
	if !strings.Contains(archMsg, `<untrusted source="tree">`) {
		t.Fatalf("architecture message dropped the tree part: %q", archMsg)
	}
}
