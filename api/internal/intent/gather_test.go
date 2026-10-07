package intent_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/intent"
)

// fakeFiles answers Read from a fixed map, keyed by path, and records every
// path it was asked about.
type fakeFiles struct {
	entries map[string]fileEntry
	calls   []string
}

type fileEntry struct {
	data    []byte
	symlink bool
	err     error
}

func (f *fakeFiles) Read(_ context.Context, path string) ([]byte, bool, error) {
	f.calls = append(f.calls, path)
	e, ok := f.entries[path]
	if !ok {
		return nil, false, intent.ErrNotFound
	}
	return e.data, e.symlink, e.err
}

// fakeIssues answers Issue from a fixed map, keyed by number, and records
// every number it was asked about.
type fakeIssues struct {
	entries map[int]issueEntry
	calls   []int
}

type issueEntry struct {
	title, body string
	err         error
}

func (f *fakeIssues) Issue(_ context.Context, n int) (string, string, error) {
	f.calls = append(f.calls, n)
	e, ok := f.entries[n]
	if !ok {
		return "", "", intent.ErrIssueNotFound
	}
	return e.title, e.body, e.err
}

func label(sources intent.Sources, label string) (intent.Source, bool) {
	for _, s := range sources.Found {
		if s.Label == label {
			return s, true
		}
	}
	return intent.Source{}, false
}

func unresolvedReason(sources intent.Sources, ref string) (string, bool) {
	for _, u := range sources.Unresolved {
		if u.Ref == ref {
			return u.Reason, true
		}
	}
	return "", false
}

func TestGatherNoReaderCallsForUnresolvableLinks(t *testing.T) {
	in := intent.GatherInput{
		Title:       "Add rate limiting",
		Description: "See http://169.254.169.254/ and https://evil.example/spec.md for background.",
		Owner:       "acme", Name: "payments-api", Self: 482,
	}
	files := &fakeFiles{entries: map[string]fileEntry{}}
	issues := &fakeIssues{entries: map[int]issueEntry{}}

	got := intent.Gather(context.Background(), in, files, issues)

	if len(files.calls) != 0 {
		t.Errorf("FileReader.Read called %d times, want 0: %v", len(files.calls), files.calls)
	}
	if len(got.Unresolved) != 2 {
		t.Fatalf("Unresolved = %v, want 2 entries", got.Unresolved)
	}
	for _, ref := range []string{"http://169.254.169.254/", "https://evil.example/spec.md"} {
		if reason, ok := unresolvedReason(got, ref); !ok || reason != "on another host" {
			t.Errorf("unresolved reason for %s = %q, %v, want %q", ref, reason, ok, "on another host")
		}
	}
}

func TestGatherPathSafety(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"parent traversal", "../../etc/passwd"},
		{"absolute path", "/etc/passwd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := intent.GatherInput{
				Description: "See [it](" + tt.path + ")",
				Owner:       "acme", Name: "payments-api",
			}
			files := &fakeFiles{entries: map[string]fileEntry{}}
			got := intent.Gather(context.Background(), in, files, nil)
			if len(files.calls) != 0 {
				t.Errorf("FileReader.Read called for an unsafe path: %v", files.calls)
			}
			if reason, ok := unresolvedReason(got, tt.path); !ok || reason != "outside repository" {
				t.Errorf("unresolved reason = %q, %v, want %q", reason, ok, "outside repository")
			}
		})
	}
}

func TestGatherSymlinkEscape(t *testing.T) {
	in := intent.GatherInput{
		Description: "See [it](docs/link.md)",
		Owner:       "acme", Name: "payments-api",
	}
	files := &fakeFiles{entries: map[string]fileEntry{
		"docs/link.md": {symlink: true, data: []byte("../../etc/passwd")},
	}}
	got := intent.Gather(context.Background(), in, files, nil)
	if reason, ok := unresolvedReason(got, "docs/link.md"); !ok || reason != "outside repository" {
		t.Errorf("unresolved reason = %q, %v, want %q", reason, ok, "outside repository")
	}
}

func TestGatherSymlinkAbsoluteTargetEscape(t *testing.T) {
	in := intent.GatherInput{
		Description: "See [it](docs/link.md)",
		Owner:       "acme", Name: "payments-api",
	}
	files := &fakeFiles{entries: map[string]fileEntry{
		"docs/link.md": {symlink: true, data: []byte("/etc/passwd")},
	}}
	got := intent.Gather(context.Background(), in, files, nil)
	if reason, ok := unresolvedReason(got, "docs/link.md"); !ok || reason != "outside repository" {
		t.Errorf("unresolved reason = %q, %v, want %q", reason, ok, "outside repository")
	}
}

func TestGatherSymlinkSecondHop(t *testing.T) {
	in := intent.GatherInput{
		Description: "See [it](docs/link.md)",
		Owner:       "acme", Name: "payments-api",
	}
	files := &fakeFiles{entries: map[string]fileEntry{
		"docs/link.md":       {symlink: true, data: []byte("other-link.md")},
		"docs/other-link.md": {symlink: true, data: []byte("final.md")},
	}}
	got := intent.Gather(context.Background(), in, files, nil)
	if reason, ok := unresolvedReason(got, "docs/link.md"); !ok || reason != "not a text file" {
		t.Errorf("unresolved reason = %q, %v, want %q", reason, ok, "not a text file")
	}
}

func TestGatherBinaryDocumentIsNotAText(t *testing.T) {
	in := intent.GatherInput{
		Description: "See [it](specs/image.bin)",
		Owner:       "acme", Name: "payments-api",
	}
	files := &fakeFiles{entries: map[string]fileEntry{
		"specs/image.bin": {data: []byte{0x50, 0x4e, 0x00, 0x47, 0x0d, 0x0a}},
	}}
	got := intent.Gather(context.Background(), in, files, nil)
	if reason, ok := unresolvedReason(got, "specs/image.bin"); !ok || reason != "not a text file" {
		t.Errorf("unresolved reason = %q, %v, want %q", reason, ok, "not a text file")
	}
}

// TestGatherLongMultibyteDocumentIsText proves isText's byte-length cap
// doesn't split a multi-byte rune: "a" followed by 4000 Cyrillic letters is
// over textCheckLength (8000) bytes, and cutting mid-rune would make
// utf8.Valid reject otherwise-valid text.
func TestGatherLongMultibyteDocumentIsText(t *testing.T) {
	big := "a" + strings.Repeat("ж", 4000) // 1 + 4000*2 = 8001 bytes
	in := intent.GatherInput{
		Description: "See [it](specs/cyrillic.md)",
		Owner:       "acme", Name: "payments-api",
	}
	files := &fakeFiles{entries: map[string]fileEntry{
		"specs/cyrillic.md": {data: []byte(big)},
	}}
	got := intent.Gather(context.Background(), in, files, nil)
	if reason, ok := unresolvedReason(got, "specs/cyrillic.md"); ok {
		t.Errorf("unresolved reason = %q, want the document to be read as text", reason)
	}
	if _, ok := label(got, "doc:specs/cyrillic.md"); !ok {
		t.Errorf("doc:specs/cyrillic.md not found in Found: %v", got.Found)
	}
}

func TestGatherDocumentTruncated(t *testing.T) {
	big := strings.Repeat("x", 25_000)
	in := intent.GatherInput{
		Description: "See [it](specs/big.md)",
		Owner:       "acme", Name: "payments-api",
	}
	files := &fakeFiles{entries: map[string]fileEntry{
		"specs/big.md": {data: []byte(big)},
	}}
	got := intent.Gather(context.Background(), in, files, nil)
	src, ok := label(got, "doc:specs/big.md")
	if !ok {
		t.Fatalf("doc:specs/big.md not found in Found: %v", got.Found)
	}
	if !src.Truncated {
		t.Error("Truncated = false, want true for a 25,000-rune document")
	}
	for _, c := range got.Content {
		if c.Label == "doc:specs/big.md" && len([]rune(c.Text)) != 20_000 {
			t.Errorf("content length = %d, want 20000", len([]rune(c.Text)))
		}
	}
}

func TestGatherDocumentAndIssueLimits(t *testing.T) {
	docPath := func(i int) string { return "specs/doc" + strconv.Itoa(i) + ".md" }

	var body strings.Builder
	files := &fakeFiles{entries: map[string]fileEntry{}}
	for i := 1; i <= 6; i++ {
		p := docPath(i)
		files.entries[p] = fileEntry{data: []byte("content")}
		body.WriteString("See [doc](" + p + "). ")
	}
	for i := 1; i <= 4; i++ {
		body.WriteString("Fixes #" + strconv.Itoa(i) + ". ")
	}
	issues := &fakeIssues{entries: map[int]issueEntry{
		1: {title: "one", body: "b1"}, 2: {title: "two", body: "b2"},
		3: {title: "three", body: "b3"}, 4: {title: "four", body: "b4"},
	}}

	in := intent.GatherInput{Description: body.String(), Owner: "acme", Name: "payments-api"}
	got := intent.Gather(context.Background(), in, files, issues)

	if reason, ok := unresolvedReason(got, docPath(6)); !ok || reason != "limit reached" {
		t.Errorf("6th document unresolved reason = %q, %v, want %q", reason, ok, "limit reached")
	}
	if reason, ok := unresolvedReason(got, "#4"); !ok || reason != "limit reached" {
		t.Errorf("4th issue unresolved reason = %q, %v, want %q", reason, ok, "limit reached")
	}
	docCount, issueCount := 0, 0
	for _, s := range got.Found {
		switch s.Kind {
		case intent.KindDocument:
			docCount++
		case intent.KindIssue:
			issueCount++
		}
	}
	if docCount != 5 {
		t.Errorf("documents read = %d, want 5", docCount)
	}
	if issueCount != 3 {
		t.Errorf("issues read = %d, want 3", issueCount)
	}
}

func TestGatherIssueErrorReasons(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"no token", nil, "no token"}, // issues reader is nil
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := intent.GatherInput{Description: "Fixes #5", Owner: "acme", Name: "payments-api"}
			got := intent.Gather(context.Background(), in, &fakeFiles{entries: map[string]fileEntry{}}, nil)
			if reason, ok := unresolvedReason(got, "#5"); !ok || reason != tt.want {
				t.Errorf("unresolved reason = %q, %v, want %q", reason, ok, tt.want)
			}
		})
	}

	t.Run("not found", func(t *testing.T) {
		issues := &fakeIssues{entries: map[int]issueEntry{}} // Issue returns ErrIssueNotFound
		in := intent.GatherInput{Description: "Fixes #5", Owner: "acme", Name: "payments-api"}
		got := intent.Gather(context.Background(), in, &fakeFiles{entries: map[string]fileEntry{}}, issues)
		if reason, ok := unresolvedReason(got, "#5"); !ok || reason != "not found" {
			t.Errorf("unresolved reason = %q, %v, want %q", reason, ok, "not found")
		}
	})

	t.Run("rate limited", func(t *testing.T) {
		issues := &fakeIssues{entries: map[int]issueEntry{5: {err: intent.ErrRateLimited}}}
		in := intent.GatherInput{Description: "Fixes #5", Owner: "acme", Name: "payments-api"}
		got := intent.Gather(context.Background(), in, &fakeFiles{entries: map[string]fileEntry{}}, issues)
		if reason, ok := unresolvedReason(got, "#5"); !ok || reason != "rate limited" {
			t.Errorf("unresolved reason = %q, %v, want %q", reason, ok, "rate limited")
		}
	})

	t.Run("other GitHub error", func(t *testing.T) {
		issues := &fakeIssues{entries: map[int]issueEntry{5: {err: errors.New("boom")}}}
		in := intent.GatherInput{Description: "Fixes #5", Owner: "acme", Name: "payments-api"}
		got := intent.Gather(context.Background(), in, &fakeFiles{entries: map[string]fileEntry{}}, issues)
		if reason, ok := unresolvedReason(got, "#5"); !ok || reason != "GitHub error: boom" {
			t.Errorf("unresolved reason = %q, %v, want %q", reason, ok, "GitHub error: boom")
		}
	})
}

func TestGatherOtherRepositoryIssueIsUnresolved(t *testing.T) {
	in := intent.GatherInput{Description: "Related to other/repo#5", Owner: "acme", Name: "payments-api"}
	issues := &fakeIssues{entries: map[int]issueEntry{}}
	got := intent.Gather(context.Background(), in, &fakeFiles{entries: map[string]fileEntry{}}, issues)
	if len(issues.calls) != 0 {
		t.Errorf("Issue called for a cross-repository reference: %v", issues.calls)
	}
	if reason, ok := unresolvedReason(got, "other/repo#5"); !ok || reason != "other repository" {
		t.Errorf("unresolved reason = %q, %v, want %q", reason, ok, "other repository")
	}
}

func TestGatherConfidenceLevels(t *testing.T) {
	tests := []struct {
		name string
		in   intent.GatherInput
		want intent.Level
	}{
		{
			name: "high: a document was read, nothing unresolved",
			in: intent.GatherInput{
				Title:       "t",
				Description: "See [plan](specs/x.md) for the full write-up, this is a reasonably long description.",
				Owner:       "acme", Name: "payments-api",
			},
			want: intent.LevelHigh,
		},
		{
			name: "medium: non-trivial description, nothing read",
			in: intent.GatherInput{
				Title:       "t",
				Description: "This change adds per-client rate limiting to protect the public API from abuse by clients without a key.",
				Owner:       "acme", Name: "payments-api",
			},
			want: intent.LevelMedium,
		},
		{
			name: "low: no documented source beyond the title",
			in: intent.GatherInput{
				Title:  "Add rate limiting",
				Branch: "feature/rate-limit",
				Owner:  "acme", Name: "payments-api",
			},
			want: intent.LevelLow,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := &fakeFiles{entries: map[string]fileEntry{"specs/x.md": {data: []byte("the plan content")}}}
			got := intent.Gather(context.Background(), tt.in, files, nil)
			if got.Confidence != tt.want {
				t.Errorf("Confidence = %q, want %q", got.Confidence, tt.want)
			}
		})
	}
}
