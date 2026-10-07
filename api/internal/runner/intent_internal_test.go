package runner

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/github"
	"github.com/DimaMaimesko/dev-digest/api/internal/intent"
)

// TestGitHubIssueReaderMapsStatusCodes checks that githubIssueReader.Issue
// maps GitHub's errors to intent's sentinel reasons: a 403 is "rate limited"
// only when GitHub's own message says so (a fine-grained token without
// Issues access, SAML enforcement or a blocked repo also answer 403, but
// aren't a rate limit), 404 is "not found", and 429 is always "rate limited".
func TestGitHubIssueReaderMapsStatusCodes(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		message string
		want    error // nil means "some other error", checked separately
	}{
		{"403 rate limit", http.StatusForbidden, "API rate limit exceeded for user", intent.ErrRateLimited},
		{"403 secondary rate limit", http.StatusForbidden, "You have exceeded a secondary rate limit", intent.ErrRateLimited},
		{"403 not a rate limit", http.StatusForbidden, "Resource not accessible by personal access token", nil},
		{"404 not found", http.StatusNotFound, "Not Found", intent.ErrIssueNotFound},
		{"429 too many requests", http.StatusTooManyRequests, "You have exceeded a rate limit", intent.ErrRateLimited},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"message": "` + tt.message + `"}`))
			}))
			defer srv.Close()

			reader := &githubIssueReader{client: github.New(srv.URL, "tok"), owner: "o", name: "n"}
			_, _, err := reader.Issue(context.Background(), 1)
			if err == nil {
				t.Fatal("want an error")
			}
			switch {
			case tt.want != nil && !errors.Is(err, tt.want):
				t.Errorf("err = %v, want %v", err, tt.want)
			case tt.want == nil && (errors.Is(err, intent.ErrRateLimited) || errors.Is(err, intent.ErrIssueNotFound)):
				t.Errorf("err = %v, want neither ErrRateLimited nor ErrIssueNotFound (a plain GitHub error)", err)
			}
		})
	}
}
