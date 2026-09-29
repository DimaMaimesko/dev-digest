package httpapi

import (
	"testing"
	"time"
)

func TestReviewStatus(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	daysAgo := func(d float64) *time.Time {
		t := now.Add(-time.Duration(d * 24 * float64(time.Hour)))
		return &t
	}
	head, older := "head", "older"

	tests := []struct {
		name         string
		state        string
		lastReviewed *string
		updatedAt    *time.Time
		want         string
	}{
		{"merged keeps GitHub's state", "merged", &head, daysAgo(1), "merged"},
		{"closed keeps GitHub's state", "closed", nil, nil, "closed"},
		{"never reviewed", "open", nil, daysAgo(1), "needs_review"},
		{"new commits since the review", "open", &older, daysAgo(1), "needs_review"},
		{"head reviewed recently", "open", &head, daysAgo(1), "reviewed"},
		{"head reviewed, no update time", "open", &head, nil, "reviewed"},
		{"exactly a week old is not stale yet", "open", &head, daysAgo(7), "reviewed"},
		{"over a week old", "open", &head, daysAgo(7.01), "stale"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reviewStatus(tt.state, head, tt.lastReviewed, tt.updatedAt, now); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
