package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testDiff = `diff --git a/src/config.ts b/src/config.ts
--- a/src/config.ts
+++ b/src/config.ts
@@ -10,3 +10,4 @@
   port: 3000,
+  stripeKey: "sk_live_xxx",
   redisUrl: x,
`

// modelReview is what the fake model answers: one real finding and one on a
// line the diff doesn't show.
const modelReview = `{"verdict":"request_changes","summary":"A live key is committed.","score":40,"findings":[
 {"id":"f1","severity":"CRITICAL","category":"security","title":"Live Stripe key in code","file":"src/config.ts",
  "start_line":11,"end_line":11,"rationale":"sk_live keys grant full account access.","suggestion":"Read it from the environment.",
  "confidence":0.95,"kind":null},
 {"id":"f2","severity":"WARNING","category":"bug","title":"Made-up issue","file":"src/config.ts",
  "start_line":300,"end_line":301,"rationale":"Not in the diff.","suggestion":null,"confidence":0.4,"kind":null}]}`

// fakeAPI serves an OpenAI-compatible chat completions API that always gives
// modelReview, and records the system prompt it was sent.
func fakeAPI(t *testing.T) (url string, system *string) {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("request is not JSON: %v", err)
		}
		got = req.Messages[0].Content
		content, _ := json.Marshal(modelReview)
		io.WriteString(w, `{"choices":[{"message":{"content":`+string(content)+`}}],"usage":{"prompt_tokens":900,"completion_tokens":150}}`)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &got
}

// promptFile writes an agent prompt to a temporary file and returns its path.
func promptFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.md")
	if err := os.WriteFile(path, []byte("You are a security reviewer."), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func noEnv(string) string { return "" }

func TestRunPrintsReview(t *testing.T) {
	url, system := fakeAPI(t)
	var stdout, stderr bytes.Buffer

	err := run(context.Background(),
		[]string{"-model", "local-model", "-prompt", promptFile(t), "-base-url", url},
		strings.NewReader(testDiff), &stdout, &stderr, noEnv)
	if err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, stderr.String())
	}

	if !strings.HasPrefix(*system, "You are a security reviewer.") {
		t.Errorf("system prompt sent = %q, want the prompt file's text first", *system)
	}
	out := stdout.String()
	for _, want := range []string{
		"request_changes · score 65 · grounding 1/2 passed · 900 → 150 tokens",
		"A live key is committed.",
		"CRITICAL   security src/config.ts:11",
		"  Live Stripe key in code",
		"    sk_live keys grant full account access.",
		"  Suggestion:\n    Read it from the environment.",
		`"Made-up issue": lines 300-301 of "src/config.ts" are not in any diff hunk`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q\noutput:\n%s", want, out)
		}
	}
	if !strings.Contains(stderr.String(), "▸ Citation grounding: 1/2 passed") {
		t.Errorf("progress is missing from stderr:\n%s", stderr.String())
	}
}

func TestRunJSON(t *testing.T) {
	url, _ := fakeAPI(t)
	var stdout, stderr bytes.Buffer

	err := run(context.Background(),
		[]string{"-model", "m", "-prompt", promptFile(t), "-base-url", url, "-json", "-quiet"},
		strings.NewReader(testDiff), &stdout, &stderr, noEnv)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("-quiet still printed progress: %q", stderr.String())
	}

	var got struct {
		Review struct {
			Score    int
			Findings []struct{ ID string }
		}
		Grounding string
		Dropped   []struct{ Reason string }
		Strategy  string
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, stdout.String())
	}
	if got.Review.Score != 65 || len(got.Review.Findings) != 1 || got.Review.Findings[0].ID != "f1" ||
		got.Grounding != "1/2 passed" || len(got.Dropped) != 1 || got.Strategy != "single-pass" {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestRunErrors(t *testing.T) {
	prompt := promptFile(t)
	tests := []struct {
		name  string
		args  []string
		stdin string
		env   map[string]string
		want  string
	}{
		{"no model", []string{"-prompt", prompt}, testDiff, nil, "-model and -prompt are required"},
		{"no prompt", []string{"-model", "m"}, testDiff, nil, "-model and -prompt are required"},
		{"missing prompt file", []string{"-model", "m", "-prompt", "nope.md", "-base-url", "http://x"}, testDiff, nil, "nope.md"},
		{"no OpenRouter key", []string{"-model", "m", "-prompt", prompt}, testDiff, nil, "set OPENROUTER_API_KEY"},
		{"no OpenAI key", []string{"-model", "m", "-prompt", prompt, "-provider", "openai"}, testDiff, nil, "set OPENAI_API_KEY"},
		{"unknown provider", []string{"-model", "m", "-prompt", prompt, "-provider", "gemini"}, testDiff, nil, `-provider "gemini"`},
		{"unknown strategy", []string{"-model", "m", "-prompt", prompt, "-strategy", "fast"}, testDiff, nil, `-strategy "fast"`},
		{"empty diff", []string{"-model", "m", "-prompt", prompt}, "", map[string]string{"OPENROUTER_API_KEY": "k"}, "changes no files"},
		{"malformed diff", []string{"-model", "m", "-prompt", prompt}, "diff --git a/x b/x\n+++ b/x\n@@ nonsense @@", map[string]string{"OPENROUTER_API_KEY": "k"}, "parse diff"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(k string) string { return tt.env[k] }
			err := run(context.Background(), tt.args, strings.NewReader(tt.stdin), io.Discard, io.Discard, getenv)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestRunHelp(t *testing.T) {
	var stderr bytes.Buffer
	if err := run(context.Background(), []string{"-h"}, strings.NewReader(""), io.Discard, &stderr, noEnv); err != nil {
		t.Errorf("-h returned %v, want no error", err)
	}
	if !strings.Contains(stderr.String(), "-model") {
		t.Errorf("-h printed no usage:\n%s", stderr.String())
	}
}
