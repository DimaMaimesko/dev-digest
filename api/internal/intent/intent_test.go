package intent_test

import (
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/intent"
)

func TestFingerprintChangesWithAnyInput(t *testing.T) {
	base := intent.Fingerprint("abc123", "Title", "Body")
	tests := []struct {
		name             string
		sha, title, body string
	}{
		{"different sha", "def456", "Title", "Body"},
		{"different title", "abc123", "Other", "Body"},
		{"different body", "abc123", "Title", "Other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intent.Fingerprint(tt.sha, tt.title, tt.body)
			if got == base {
				t.Errorf("Fingerprint did not change for %s", tt.name)
			}
		})
	}
	if got := intent.Fingerprint("abc123", "Title", "Body"); got != base {
		t.Errorf("Fingerprint is not deterministic: got %q, want %q", got, base)
	}
}

func TestPromptText(t *testing.T) {
	tests := []struct {
		name string
		in   intent.Intent
		want string
	}{
		{
			name: "both lists filled",
			in: intent.Intent{
				Statement:  "Add rate limiting to protect public API endpoints from abusive clients.",
				InScope:    []string{"Token-bucket middleware for public endpoints", "Rate-limit configuration"},
				OutOfScope: []string{"Authenticated endpoints", "Changes to authentication"},
				Confidence: intent.LevelMedium,
			},
			want: "Confidence: medium\n" +
				"Intent: Add rate limiting to protect public API endpoints from abusive clients.\n" +
				"In scope:\n" +
				"- Token-bucket middleware for public endpoints\n" +
				"- Rate-limit configuration\n" +
				"Out of scope:\n" +
				"- Authenticated endpoints\n" +
				"- Changes to authentication",
		},
		{
			name: "empty lists say none stated",
			in: intent.Intent{
				Statement:  "Unclear change; inferred from file paths.",
				Confidence: intent.LevelLow,
			},
			want: "Confidence: low\n" +
				"Intent: Unclear change; inferred from file paths.\n" +
				"In scope:\n" +
				"- (none stated)\n" +
				"Out of scope:\n" +
				"- (none stated)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.PromptText(); got != tt.want {
				t.Errorf("PromptText() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestTrivial(t *testing.T) {
	tests := []struct {
		name string
		desc string
		want bool
	}{
		{"empty", "", true},
		{"only whitespace", "   \n\t  ", true},
		{"only an HTML comment", "<!-- TODO: fill this in -->", true},
		{"only a heading", "## Description", true},
		{"only an unchecked checkbox", "- [ ] Describe the change", true},
		{"heading plus unchecked template", "## Description\n- [ ] What does this PR do?\n## Testing\n- [ ] Added tests", true},
		{"short real text under 50 runes", "Fixes bug", true},
		{
			name: "real content over 50 runes",
			desc: "This change adds per-client rate limiting to protect the public API from abuse.",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := intent.Trivial(tt.desc); got != tt.want {
				t.Errorf("Trivial(%q) = %v, want %v", tt.desc, got, tt.want)
			}
		})
	}
}

func TestConfidenceRule(t *testing.T) {
	tests := []struct {
		name                string
		desc                string
		documentOrIssueRead bool
		documentUnresolved  bool
		want                intent.Level
	}{
		{"read and resolved -> high", "short", true, false, intent.LevelHigh},
		{"read but a doc unresolved -> not high", "This is a sufficiently long, non-trivial description of the change, well past fifty characters.", true, true, intent.LevelMedium},
		{"non-trivial description only -> medium", "This is a sufficiently long, non-trivial description of the change, well past fifty characters.", false, false, intent.LevelMedium},
		{"trivial and nothing read -> low", "short", false, false, intent.LevelLow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intent.Confidence(tt.desc, tt.documentOrIssueRead, tt.documentUnresolved)
			if got != tt.want {
				t.Errorf("Confidence() = %q, want %q", got, tt.want)
			}
		})
	}
}
