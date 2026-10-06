package claudecode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DimaMaimesko/dev-digest/api/internal/review"
)

// fakeClaude writes a shell script that stands in for the claude CLI: it
// records its arguments, stdin and environment in dir, prints stdout, and
// exits with code. It returns the script's path.
func fakeClaude(t *testing.T, stdout string, code int) (bin, dir string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stdout"), []byte(stdout), 0o600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
printf '%s\0' "$@" > "` + dir + `/args"
cat > "` + dir + `/stdin"
env > "` + dir + `/env"
echo "some warning" >&2
cat "` + dir + `/stdout"
exit ` + string(rune('0'+code)) + "\n"
	bin = filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin, dir
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// flagValue returns the argument after flag in args.
func flagValue(args []string, flag string) (string, bool) {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return "", false
	}
	return args[i+1], true
}

var request = review.JSONRequest{
	Model:      "sonnet",
	System:     "You review code.\nBe brief.",
	Messages:   []review.Message{{Role: review.RoleUser, Content: "Review this diff"}},
	SchemaName: "Review",
	Schema:     []byte(`{"type":"object"}`),
}

func TestCompleteJSON(t *testing.T) {
	bin, dir := fakeClaude(t, `{"type":"result","is_error":false,"result":"{\"score\": 7}",
		"structured_output":{"score":7},
		"usage":{"input_tokens":10,"cache_creation_input_tokens":100,"cache_read_input_tokens":1000,"output_tokens":5}}`, 0)
	c := New(bin, []string{"PATH=" + os.Getenv("PATH"), "ANTHROPIC_API_KEY=sk-ant", "KEEP=1"})

	res, err := c.CompleteJSON(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != `{"score":7}` {
		t.Errorf("Text = %q, want the structured output", res.Text)
	}
	if res.TokensIn != 1110 || res.TokensOut != 5 {
		t.Errorf("tokens = %d in, %d out; want 1110 in, 5 out", res.TokensIn, res.TokensOut)
	}
	if res.CostUSD != nil {
		t.Errorf("CostUSD = %v, want nil: a subscription isn't billed per call", *res.CostUSD)
	}

	args := strings.Split(strings.TrimSuffix(readFile(t, filepath.Join(dir, "args")), "\x00"), "\x00")
	for flag, want := range map[string]string{
		"--model":         "sonnet",
		"--system-prompt": "You review code.\nBe brief.",
		"--json-schema":   `{"type":"object"}`,
		"--output-format": "json",
		"--tools":         "",
	} {
		if got, ok := flagValue(args, flag); !ok || got != want {
			t.Errorf("%s = %q (given: %t), want %q", flag, got, ok, want)
		}
	}
	if !slices.Contains(args, "-p") {
		t.Errorf("args %q lack -p", args)
	}
	if got := readFile(t, filepath.Join(dir, "stdin")); got != "Review this diff" {
		t.Errorf("stdin = %q, want the user message", got)
	}
	env := readFile(t, filepath.Join(dir, "env"))
	if strings.Contains(env, "ANTHROPIC_API_KEY") {
		t.Error("the CLI got ANTHROPIC_API_KEY; it would bill the API instead of the subscription")
	}
	if !strings.Contains(env, "KEEP=1") {
		t.Error("the CLI lost the rest of the environment")
	}
}

func TestCompleteJSONWithoutStructuredOutput(t *testing.T) {
	bin, _ := fakeClaude(t, `{"is_error":false,"result":"{\"score\": 7}","usage":{}}`, 0)
	res, err := New(bin, nil).CompleteJSON(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != `{"score": 7}` {
		t.Errorf("Text = %q, want the result", res.Text)
	}
}

func TestCompleteJSONDefaultSystemPrompt(t *testing.T) {
	bin, dir := fakeClaude(t, `{"is_error":false,"result":"{}","usage":{}}`, 0)
	req := request
	req.System = ""
	if _, err := New(bin, nil).CompleteJSON(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	args := strings.Split(readFile(t, filepath.Join(dir, "args")), "\x00")
	if got, _ := flagValue(args, "--system-prompt"); got != defaultSystem {
		t.Errorf("--system-prompt = %q, want %q, so the CLI's own prompt isn't used", got, defaultSystem)
	}
}

func TestCompleteJSONErrors(t *testing.T) {
	tests := []struct {
		name, stdout string
		code         int
		want         string // in the error
		tokensIn     int
	}{
		{
			name: "error result",
			stdout: `{"is_error":true,"result":"There's an issue with the selected model (nope).",
				"usage":{"input_tokens":0,"output_tokens":0}}`,
			code: 1,
			want: "issue with the selected model",
		},
		{
			name:     "error result with usage",
			stdout:   `{"is_error":true,"result":"You've hit your usage limit","usage":{"input_tokens":3,"output_tokens":4}}`,
			code:     0,
			want:     "usage limit",
			tokensIn: 3,
		},
		{name: "no JSON, failed", stdout: "", code: 2, want: "some warning"},
		{name: "no JSON, succeeded", stdout: "hello", code: 0, want: "read the output"},
		{name: "error without a message", stdout: `{"is_error":true,"usage":{}}`, code: 1, want: "some warning"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin, _ := fakeClaude(t, tt.stdout, tt.code)
			res, err := New(bin, nil).CompleteJSON(t.Context(), request)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, tt.want)
			}
			if res.TokensIn != tt.tokensIn {
				t.Errorf("TokensIn = %d, want %d", res.TokensIn, tt.tokensIn)
			}
		})
	}
}

func TestCompleteJSONStopsWhenCancelled(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := New(bin, nil).CompleteJSON(ctx, request)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("returned after %v; the process wasn't stopped", d)
	}
}

func TestCompleteJSONNotInstalled(t *testing.T) {
	_, err := New(filepath.Join(t.TempDir(), "claude"), nil).CompleteJSON(t.Context(), request)
	if err == nil {
		t.Fatal("no error for a missing executable")
	}
}

func TestPrompt(t *testing.T) {
	tests := []struct {
		name     string
		messages []review.Message
		want     string
	}{
		{
			name:     "one user message",
			messages: []review.Message{{Role: review.RoleUser, Content: "Review this"}},
			want:     "Review this",
		},
		{
			name: "a retry",
			messages: []review.Message{
				{Role: review.RoleUser, Content: "Review this"},
				{Role: review.RoleAssistant, Content: `{"bad": true}`},
				{Role: review.RoleUser, Content: "Your answer is not a valid Review"},
			},
			want: "Review this\n\n<your_earlier_answer>\n{\"bad\": true}\n</your_earlier_answer>\n\nYour answer is not a valid Review",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prompt(tt.messages); got != tt.want {
				t.Errorf("prompt() = %q, want %q", got, tt.want)
			}
		})
	}
}
