package intent_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DimaMaimesko/dev-digest/api/internal/intent"
)

func TestExtractDocuments(t *testing.T) {
	tests := []struct {
		name      string
		title     string
		body      string
		wantDocs  []string
		wantUnres []intent.Unresolved
	}{
		{
			name:     "relative path and dotted relative path dedupe",
			body:     "See [the plan](specs/x.md) and also ./specs/x.md for details.",
			wantDocs: []string{"specs/x.md"},
		},
		{
			name:     "blob URL at another ref dedupes with the path",
			body:     "See specs/x.md, also https://github.com/acme/payments-api/blob/main/specs/x.md and https://github.com/acme/payments-api/blob/deadbeef/specs/x.md",
			wantDocs: []string{"specs/x.md"},
		},
		{
			name: "AC-9: a non-GitHub host link is unresolved with zero reads, never fetched",
			body: "Background: http://169.254.169.254/ and the design at https://evil.example/spec.md",
			wantUnres: []intent.Unresolved{
				{Ref: "http://169.254.169.254/", Reason: "on another host"},
				{Ref: "https://evil.example/spec.md", Reason: "on another host"},
			},
		},
		{
			name:     "bare prose path with a doc extension and a slash counts",
			body:     "Implements specs/rate-limit.md as discussed.",
			wantDocs: []string{"specs/rate-limit.md"},
		},
		{
			name: "bare prose path without a doc extension does not count",
			body: "Changes src/config.ts to add a new field.",
		},
		{
			name: "blob URL of a different repository is unresolved",
			body: "See https://github.com/other/repo/blob/main/specs/x.md",
			wantUnres: []intent.Unresolved{
				{Ref: "https://github.com/other/repo/blob/main/specs/x.md", Reason: "other repository"},
			},
		},
		{
			name:     "an image target is never a reference",
			body:     "![diagram](specs/diagram.png)",
			wantDocs: nil,
		},
		{
			name:     "an in-page anchor link is not a document",
			body:     "See [Testing](#testing) below.",
			wantDocs: nil,
		},
		{
			name:     "a same-page relative link is not a document",
			body:     "Back to [top](./).",
			wantDocs: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs := intent.Extract(tt.title, tt.body, "acme", "payments-api", 482)
			if !slices.Equal(refs.Docs, tt.wantDocs) {
				t.Errorf("Docs = %v, want %v", refs.Docs, tt.wantDocs)
			}
			if !slices.Equal(refs.DocsUnresolved, tt.wantUnres) {
				t.Errorf("DocsUnresolved = %v, want %v", refs.DocsUnresolved, tt.wantUnres)
			}
		})
	}
}

func TestExtractIssues(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		self      int
		wantIssue []int
		wantUnres []intent.Unresolved
	}{
		{name: "bare hash", body: "Fixes a bug reported in #17", wantIssue: []int{17}},
		{name: "Fixes/Closes/Resolves keyword", body: "Closes #5. Also resolves #6.", wantIssue: []int{5, 6}},
		{name: "same-repo issue URL", body: "See https://github.com/acme/payments-api/issues/9", wantIssue: []int{9}},
		{
			name: "cross-repo shorthand is unresolved",
			body: "Related to other/repo#5",
			wantUnres: []intent.Unresolved{
				{Ref: "other/repo#5", Reason: "other repository"},
			},
		},
		{
			name: "cross-repo issue URL is unresolved",
			body: "See https://github.com/other/repo/issues/5",
			wantUnres: []intent.Unresolved{
				{Ref: "https://github.com/other/repo/issues/5", Reason: "other repository"},
			},
		},
		{
			name: "a self-reference is skipped silently",
			body: "This is PR #482 itself",
			self: 482,
		},
		{
			name:      "duplicate references dedupe",
			body:      "Fixes #5, see also #5 again",
			wantIssue: []int{5},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs := intent.Extract("", tt.body, "acme", "payments-api", tt.self)
			if !slices.Equal(refs.Issues, tt.wantIssue) {
				t.Errorf("Issues = %v, want %v", refs.Issues, tt.wantIssue)
			}
			if !slices.Equal(refs.IssuesUnresolved, tt.wantUnres) {
				t.Errorf("IssuesUnresolved = %v, want %v", refs.IssuesUnresolved, tt.wantUnres)
			}
		})
	}
}

// TestExtractScansLargeTextQuickly proves scan is bounded by maxRefLen per
// position, not by the text's full length. Before the fix (unanchored
// Find*Index calls scanning the rest of the text at every byte position), a
// 65 KB body measured 2m19s. Anchoring every pattern with \A fixed the
// common case, but a pattern that still matches at offset 0 of a long run
// of non-space bytes — barePathRe, crossIssueRe and mdLinkRe's "[^\]]*" —
// kept walking to the end of that run at every position inside it: a single
// long token (no spaces at all) stayed quadratic even after that fix. The
// bound here is generous (to stay reliable under -race, which slows
// everything down a lot) but still far below what the old quadratic scan
// took at this size.
func TestExtractScansLargeTextQuickly(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		adversarial bool // triggers the quadratic run in barePathRe, crossIssueRe or mdLinkRe
	}{
		{"plain prose", strings.Repeat("word ", 13_000), false},                        // ~65 KB
		{"many # and / characters", strings.Repeat("a/b#1 ", 11_000), false},           // ~65 KB
		{"one long token with no spaces", strings.Repeat("a", 16_000), true},           // barePathRe
		{"one long run of slashes", strings.Repeat("a/", 8_000), true},                 // barePathRe
		{"one long run of path-like segments", strings.Repeat("a/b.", 4_000), true},    // crossIssueRe / barePathRe
		{"one long run of unclosed Markdown links", strings.Repeat("[", 16_000), true}, // mdLinkRe
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if raceEnabled && tt.adversarial {
				t.Skip("skipped under -race: the race detector's instrumentation slows this adversarial case enough to flake on slower CI hardware")
			}
			bound := time.Second
			if raceEnabled {
				bound = 30 * time.Second
			}
			start := time.Now()
			intent.Extract("", tt.body, "acme", "payments-api", 482)
			if elapsed := time.Since(start); elapsed > bound {
				t.Errorf("Extract on a %d-byte body took %s, want well under %s", len(tt.body), elapsed, bound)
			}
		})
	}
}
