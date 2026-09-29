package runner

import (
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

func TestCountBlockers(t *testing.T) {
	findings := []review.Finding{
		{Severity: review.SeverityCritical}, {Severity: review.SeverityWarning}, {Severity: review.SeverityWarning},
		{Severity: review.SeveritySuggestion},
	}
	for failOn, want := range map[string]int{"critical": 1, "warning": 3, "any": 4, "never": 0, "": 0} {
		if got := countBlockers(findings, failOn); got != want {
			t.Errorf("ci_fail_on %q: %d blockers, want %d", failOn, got, want)
		}
	}
}
