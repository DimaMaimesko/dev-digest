package review_test

import (
	"math"
	"strings"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/diff"
	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

// sample is the fixture from server/test/grounding.test.ts.
const sample = `diff --git a/src/config.ts b/src/config.ts
--- a/src/config.ts
+++ b/src/config.ts
@@ -10,3 +10,4 @@
   port: 3000,
+  stripeKey: "sk_live_xxx",
   redisUrl: x,
diff --git a/src/api/users.ts b/src/api/users.ts
--- a/src/api/users.ts
+++ b/src/api/users.ts
@@ -44,2 +44,6 @@
   const users = await db.users.findMany();
+  for (const u of users) {
+    const posts = await db.posts.findMany({ userId: u.id });
+    result.push({ ...u, posts });
+  }`

func parse(t *testing.T, raw string) diff.Diff {
	t.Helper()
	d, err := diff.Parse(raw)
	if err != nil {
		t.Fatalf("diff.Parse: %v", err)
	}
	return d
}

// finding returns a finding at file:start-end with the other fields filled in.
func finding(file string, start, end int) review.Finding {
	return review.Finding{
		ID:         "x",
		Severity:   review.SeverityWarning,
		Category:   review.CategoryBug,
		Title:      "t",
		File:       file,
		StartLine:  start,
		EndLine:    end,
		Rationale:  "r",
		Confidence: 0.8,
	}
}

func TestGround(t *testing.T) {
	d := parse(t, sample)

	secret := finding("src/config.ts", 1, 1)
	secret.Kind = review.KindSecretLeak

	tests := []struct {
		name       string
		finding    review.Finding
		wantKept   bool
		wantReason string // part of the drop reason; empty when kept
	}{
		{"line in a hunk", finding("src/config.ts", 12, 12), true, ""},
		{"line outside every hunk", finding("src/config.ts", 999, 999), false, "not in any diff hunk"},
		{"file not in the diff", finding("src/not-here.ts", 12, 12), false, "is not in the diff"},
		{"whole-file kind needs only the file", secret, true, ""},
		{"range overlapping added lines", finding("src/api/users.ts", 45, 52), true, ""},
		{"reversed range", finding("src/api/users.ts", 52, 45), true, ""},
		{"huge range overlapping the diff", finding("src/config.ts", 1, math.MaxInt), true, ""},
		// The TS gate looped over every number in the range: ~2 s for 10⁹ lines.
		{"huge range missing the diff", finding("src/config.ts", 100, math.MaxInt), false, "not in any diff hunk"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := review.Ground([]review.Finding{tt.finding}, d)

			if tt.wantKept {
				if len(g.Kept) != 1 || len(g.Dropped) != 0 {
					t.Fatalf("kept %d, dropped %d; want the finding kept: %+v",
						len(g.Kept), len(g.Dropped), g.Dropped)
				}
				if g.Kept[0] != tt.finding {
					t.Errorf("kept %+v, want %+v", g.Kept[0], tt.finding)
				}
				return
			}
			if len(g.Kept) != 0 || len(g.Dropped) != 1 {
				t.Fatalf("kept %d, dropped %d; want the finding dropped", len(g.Kept), len(g.Dropped))
			}
			if reason := g.Dropped[0].Reason; !strings.Contains(reason, tt.wantReason) {
				t.Errorf("reason = %q, want it to contain %q", reason, tt.wantReason)
			}
		})
	}
}

func TestGroundKeepsOrder(t *testing.T) {
	d := parse(t, sample)
	in := []review.Finding{
		finding("src/api/users.ts", 46, 46),
		finding("src/config.ts", 999, 999),
		finding("src/config.ts", 11, 11),
	}

	g := review.Ground(in, d)

	if len(g.Kept) != 2 || g.Kept[0] != in[0] || g.Kept[1] != in[2] {
		t.Errorf("Kept = %+v, want findings 0 and 2 in order", g.Kept)
	}
	if len(g.Dropped) != 1 || g.Dropped[0].Finding != in[1] {
		t.Errorf("Dropped = %+v, want finding 1", g.Dropped)
	}
}

func TestGroundingSummary(t *testing.T) {
	d := parse(t, sample)
	tests := []struct {
		name     string
		findings []review.Finding
		want     string
	}{
		{"no findings", nil, "0/0 passed"},
		{"one of two", []review.Finding{
			finding("src/config.ts", 12, 12),
			finding("src/config.ts", 999, 999),
		}, "1/2 passed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := review.Ground(tt.findings, d).Summary(); got != tt.want {
				t.Errorf("Summary() = %q, want %q", got, tt.want)
			}
		})
	}
}
