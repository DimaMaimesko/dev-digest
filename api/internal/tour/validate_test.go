package tour

import (
	"errors"
	"strings"
	"testing"
)

func archTree() Tree {
	return NewTree([]Entry{
		{Path: "src", Dir: true},
		{Path: "src/api", Dir: true},
		{Path: "src/api/users.ts", Dir: false},
		{Path: "src/middleware", Dir: true},
		{Path: "src/middleware/ratelimit.ts", Dir: false},
		{Path: "README.md", Dir: false},
	})
}

func TestValidateArchitecture_SummaryGroundingAndClip(t *testing.T) {
	tree := archTree()
	raw := []byte(`{"summary":"Entry is ` + "`src/api/users.ts`" + ` and a made-up one is ` + "`src/nope.ts`" + `.","nodes":[],"edges":[]}`)
	content, removed, err := validateArchitecture(tree, raw)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	var sawPath, sawPlainNope bool
	for _, seg := range content.Summary {
		if seg.Type == "path" && seg.Path == "src/api/users.ts" {
			sawPath = true
		}
		if seg.Type == "text" && !containsBacktick(seg.Text) && stringsContains(seg.Text, "src/nope.ts") {
			sawPlainNope = true
		}
	}
	if !sawPath {
		t.Errorf("summary = %+v, want a grounded path segment for src/api/users.ts", content.Summary)
	}
	if !sawPlainNope {
		t.Errorf("summary = %+v, want the ungrounded span as plain text without backticks", content.Summary)
	}
}

func containsBacktick(s string) bool { return stringsContains(s, "`") }

func stringsContains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestValidateArchitecture_DiagramInvalidation(t *testing.T) {
	tree := archTree()

	t.Run("too many nodes", func(t *testing.T) {
		nodes := ""
		for i := 0; i < 13; i++ {
			if i > 0 {
				nodes += ","
			}
			nodes += `{"id":"n` + string(rune('a'+i)) + `","label":"x","kind":"module","path":null}`
		}
		raw := []byte(`{"summary":"x","nodes":[` + nodes + `],"edges":[]}`)
		content, _, err := validateArchitecture(tree, raw)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if content.Diagram != nil {
			t.Fatalf("diagram = %+v, want nil", content.Diagram)
		}
	})

	t.Run("edge to unknown node", func(t *testing.T) {
		raw := []byte(`{"summary":"x","nodes":[{"id":"a","label":"A","kind":"module","path":null}],"edges":[{"from":"a","to":"ghost","label":null}]}`)
		content, _, err := validateArchitecture(tree, raw)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if content.Diagram != nil {
			t.Fatalf("diagram = %+v, want nil", content.Diagram)
		}
	})

	t.Run("duplicate id", func(t *testing.T) {
		raw := []byte(`{"summary":"x","nodes":[{"id":"a","label":"A","kind":"module","path":null},{"id":"a","label":"B","kind":"module","path":null}],"edges":[]}`)
		content, _, err := validateArchitecture(tree, raw)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if content.Diagram != nil {
			t.Fatalf("diagram = %+v, want nil", content.Diagram)
		}
	})

	t.Run("unknown kind", func(t *testing.T) {
		raw := []byte(`{"summary":"x","nodes":[{"id":"a","label":"A","kind":"bogus","path":null}],"edges":[]}`)
		content, _, err := validateArchitecture(tree, raw)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if content.Diagram != nil {
			t.Fatalf("diagram = %+v, want nil", content.Diagram)
		}
	})

	t.Run("ungrounded node path becomes null and counts as removed", func(t *testing.T) {
		raw := []byte(`{"summary":"x","nodes":[{"id":"a","label":"A","kind":"module","path":"nope.ts"}],"edges":[]}`)
		content, removed, err := validateArchitecture(tree, raw)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if content.Diagram == nil {
			t.Fatalf("diagram = nil, want a diagram with a null path")
		}
		if content.Diagram.Nodes[0].Path != nil {
			t.Fatalf("node path = %v, want nil", *content.Diagram.Nodes[0].Path)
		}
		if removed != 1 {
			t.Fatalf("removed = %d, want 1", removed)
		}
	})

	t.Run("valid diagram grounds a path", func(t *testing.T) {
		raw := []byte(`{"summary":"x","nodes":[{"id":"a","label":"A","kind":"entry","path":"src/api/users.ts"}],"edges":[]}`)
		content, removed, err := validateArchitecture(tree, raw)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if content.Diagram == nil || content.Diagram.Nodes[0].Path == nil || *content.Diagram.Nodes[0].Path != "src/api/users.ts" {
			t.Fatalf("diagram = %+v, want a grounded path", content.Diagram)
		}
		if removed != 0 {
			t.Fatalf("removed = %d, want 0", removed)
		}
	})
}

func TestValidatePathList(t *testing.T) {
	tree := archTree()

	t.Run("drops ungrounded entries", func(t *testing.T) {
		raw := []byte(`{"items":[{"path":"src/api/users.ts","reason":"entry point"},{"path":"nope.ts","reason":"made up"}]}`)
		content, removed, err := validatePathList(tree, raw, 10, 0)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(content.Items) != 1 || content.Items[0].Path != "src/api/users.ts" {
			t.Fatalf("items = %+v", content.Items)
		}
		if removed != 1 {
			t.Fatalf("removed = %d, want 1", removed)
		}
	})

	t.Run("all dropped gives ErrNoEntries", func(t *testing.T) {
		raw := []byte(`{"items":[{"path":"nope.ts","reason":"made up"}]}`)
		_, removed, err := validatePathList(tree, raw, 10, 0)
		if !errors.Is(err, ErrNoEntries) {
			t.Fatalf("err = %v, want ErrNoEntries", err)
		}
		if removed != 1 {
			t.Fatalf("removed = %d, want 1", removed)
		}
	})

	t.Run("fewer than minRawItems is a schema violation", func(t *testing.T) {
		raw := []byte(`{"items":[{"path":"src/api/users.ts","reason":"x"},{"path":"README.md","reason":"y"}]}`)
		_, _, err := validatePathList(tree, raw, 12, 3)
		if err == nil || errors.Is(err, ErrNoEntries) {
			t.Fatalf("err = %v, want a schema-violation error", err)
		}
	})

	t.Run("reason is clipped", func(t *testing.T) {
		long := make([]byte, 0, 200)
		for i := 0; i < 200; i++ {
			long = append(long, 'x')
		}
		raw := []byte(`{"items":[{"path":"README.md","reason":"` + string(long) + `"}]}`)
		content, _, err := validatePathList(tree, raw, 10, 0)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if runeLen(content.Items[0].Reason) != maxReasonRunes {
			t.Fatalf("reason length = %d, want %d", runeLen(content.Items[0].Reason), maxReasonRunes)
		}
	})
}

func TestValidateRunLocally(t *testing.T) {
	t.Run("drops multi-line and over-long commands, never clips", func(t *testing.T) {
		long := make([]byte, 0, 400)
		for i := 0; i < 400; i++ {
			long = append(long, 'x')
		}
		raw := []byte(`{"steps":[
			{"command":"pnpm install","comment":null},
			{"command":"line one\nline two","comment":null},
			{"command":"` + string(long) + `","comment":null}
		]}`)
		content, removed, err := validateRunLocally(raw)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(content.Steps) != 1 || content.Steps[0].Command != "pnpm install" {
			t.Fatalf("steps = %+v", content.Steps)
		}
		if removed != 2 {
			t.Fatalf("removed = %d, want 2", removed)
		}
	})

	t.Run("all dropped gives ErrNoEntries", func(t *testing.T) {
		raw := []byte(`{"steps":[{"command":"a\nb","comment":null}]}`)
		_, _, err := validateRunLocally(raw)
		if !errors.Is(err, ErrNoEntries) {
			t.Fatalf("err = %v, want ErrNoEntries", err)
		}
	})

	t.Run("drops commands with a control character or a Unicode line separator", func(t *testing.T) {
		raw := []byte(`{"steps":[
			{"command":"pnpm install","comment":null},
			{"command":"echo \u0007bell","comment":null},
			{"command":"echo tab\there","comment":null},
			{"command":"echo one\u2028two","comment":null},
			{"command":"echo one\u2029two","comment":null}
		]}`)
		content, removed, err := validateRunLocally(raw)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(content.Steps) != 1 || content.Steps[0].Command != "pnpm install" {
			t.Fatalf("steps = %+v", content.Steps)
		}
		if removed != 4 {
			t.Fatalf("removed = %d, want 4", removed)
		}
	})
}

func TestCleanComment(t *testing.T) {
	ptr := func(s string) *string { return &s }

	t.Run("nil stays nil", func(t *testing.T) {
		if got := cleanComment(nil); got != nil {
			t.Fatalf("got = %v, want nil", *got)
		}
	})

	t.Run("multi-line becomes a single line of spaces", func(t *testing.T) {
		got := cleanComment(ptr("then fill in\nthe keys"))
		if got == nil || *got != "then fill in the keys" {
			t.Fatalf("got = %v, want %q", got, "then fill in the keys")
		}
	})

	t.Run("Unicode line separators become spaces", func(t *testing.T) {
		got := cleanComment(ptr("line one\u2028line two\u2029line three"))
		want := "line one line two line three"
		if got == nil || *got != want {
			t.Fatalf("got = %v, want %q", got, want)
		}
	})

	t.Run("a control character like ESC becomes a space", func(t *testing.T) {
		got := cleanComment(ptr("before\x1bafter"))
		want := "before after"
		if got == nil || *got != want {
			t.Fatalf("got = %v, want %q", got, want)
		}
	})

	t.Run("over-long comment is clipped to the limit", func(t *testing.T) {
		long := strings.Repeat("x", maxCommentRunes+50)
		got := cleanComment(&long)
		if got == nil {
			t.Fatalf("got = nil, want a clipped comment")
		}
		if runeLen(*got) != maxCommentRunes {
			t.Fatalf("length = %d, want %d", runeLen(*got), maxCommentRunes)
		}
	})

	t.Run("whitespace-only becomes nil", func(t *testing.T) {
		if got := cleanComment(ptr("   \t  \n  ")); got != nil {
			t.Fatalf("got = %v, want nil", *got)
		}
	})

	t.Run("empty string becomes nil", func(t *testing.T) {
		if got := cleanComment(ptr("")); got != nil {
			t.Fatalf("got = %v, want nil", *got)
		}
	})
}

func TestValidateFirstTasks_TodoFromScanOnly(t *testing.T) {
	tree := archTree()
	todos := []TodoRef{
		{Path: "src/middleware/ratelimit.ts", Line: 12, Text: "TODO: add Redis backend"},
	}
	raw := []byte(`{"tasks":[{
		"title":"Swap the rate limiter backend",
		"description":"Replace the in-memory limiter.",
		"size":"small",
		"paths":["src/middleware/ratelimit.ts"],
		"todo":{"path":"src/middleware/ratelimit.ts","line":12}
	}]}`)
	content, removed, err := validateFirstTasks(tree, todos, raw)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}
	task := content.Tasks[0]
	if task.Todo == nil || task.Todo.Text != "TODO: add Redis backend" {
		t.Fatalf("todo = %+v, want the scanned text", task.Todo)
	}

	t.Run("mismatched line never attaches a todo", func(t *testing.T) {
		raw := []byte(`{"tasks":[{
			"title":"x","description":"y","size":"small",
			"paths":["src/middleware/ratelimit.ts"],
			"todo":{"path":"src/middleware/ratelimit.ts","line":999}
		}]}`)
		content, _, err := validateFirstTasks(tree, todos, raw)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if content.Tasks[0].Todo != nil {
			t.Fatalf("todo = %+v, want nil", content.Tasks[0].Todo)
		}
	})

	t.Run("a task with no grounded paths is dropped", func(t *testing.T) {
		raw := []byte(`{"tasks":[{"title":"x","description":"y","size":"small","paths":["nope.ts"],"todo":null}]}`)
		_, removed, err := validateFirstTasks(tree, nil, raw)
		if !errors.Is(err, ErrNoEntries) {
			t.Fatalf("err = %v, want ErrNoEntries", err)
		}
		if removed != 2 {
			t.Fatalf("removed = %d, want 2 (the dropped path and the dropped task)", removed)
		}
	})

	t.Run("title and description are clipped", func(t *testing.T) {
		longTitle := make([]byte, 0, 150)
		for i := 0; i < 150; i++ {
			longTitle = append(longTitle, 'a')
		}
		raw := []byte(`{"tasks":[{"title":"` + string(longTitle) + `","description":"d","size":"small","paths":["README.md"],"todo":null}]}`)
		content, _, err := validateFirstTasks(tree, nil, raw)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if runeLen(content.Tasks[0].Title) != maxTitleRunes {
			t.Fatalf("title length = %d, want %d", runeLen(content.Tasks[0].Title), maxTitleRunes)
		}
	})
}
