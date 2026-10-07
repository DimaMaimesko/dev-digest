package main

import (
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/claudecode"
	"github.com/DimaMaimesko/dev-digest/api/internal/httpapi"
	"github.com/DimaMaimesko/dev-digest/api/internal/secrets"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    config
		wantErr string
	}{
		{
			name: "defaults match the TS server",
			want: config{
				databaseURL: "postgres://devdigest:devdigest@localhost:5433/devdigest",
				port:        3001,
				webOrigin:   "http://localhost:3000",
				cloneDir:    "/home/ann/.devdigest/workspace",
				secretsPath: "/home/ann/.devdigest/secrets.json",
				logLevel:    slog.LevelInfo,
				repoIntel:   true,
			},
		},
		{
			name: "everything set",
			env: map[string]string{"DATABASE_URL": "postgres://x/y", "API_PORT": "3002", "WEB_PORT": "4000", "LOG_LEVEL": "debug", "DEVDIGEST_CLONE_DIR": "/srv/clones",
				"REPO_INTEL_ENABLED": "false", "ANTHROPIC_VIA_CLAUDE_CODE": "true"},
			want: config{databaseURL: "postgres://x/y", port: 3002, webOrigin: "http://localhost:4000", logLevel: slog.LevelDebug,
				cloneDir: "/srv/clones", secretsPath: "/home/ann/.devdigest/secrets.json", repoIntel: false, claudeCode: true},
		},
		{
			// .env.example ships LOG_LEVEL= empty.
			name: "empty LOG_LEVEL means info",
			env:  map[string]string{"LOG_LEVEL": ""},
			want: config{databaseURL: "postgres://devdigest:devdigest@localhost:5433/devdigest", port: 3001, webOrigin: "http://localhost:3000", logLevel: slog.LevelInfo,
				cloneDir: "/home/ann/.devdigest/workspace", secretsPath: "/home/ann/.devdigest/secrets.json", repoIntel: true},
		},
		{
			// Only "false" turns it off, as in TS.
			name: "REPO_INTEL_ENABLED=0 leaves it on",
			env:  map[string]string{"REPO_INTEL_ENABLED": "0"},
			want: config{databaseURL: "postgres://devdigest:devdigest@localhost:5433/devdigest", port: 3001, webOrigin: "http://localhost:3000", logLevel: slog.LevelInfo,
				cloneDir: "/home/ann/.devdigest/workspace", secretsPath: "/home/ann/.devdigest/secrets.json", repoIntel: true},
		},
		{name: "bad port", env: map[string]string{"API_PORT": "abc"}, wantErr: `API_PORT "abc"`},
		{name: "no HOME", env: map[string]string{"HOME": ""}, wantErr: "HOME is not set"},
		{name: "bad web port", env: map[string]string{"WEB_PORT": "x"}, wantErr: `WEB_PORT "x"`},
		{name: "bad log level", env: map[string]string{"LOG_LEVEL": "loud"}, wantErr: `LOG_LEVEL "loud"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{"HOME": "/home/ann"}
			maps.Copy(env, tt.env)
			got, err := loadConfig(func(k string) string { return env[k] })
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadConfig: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// A relative DEVDIGEST_CLONE_DIR is relative to the working directory, as
// .env.example's ./clones is.
func TestRelativeCloneDir(t *testing.T) {
	cfg, err := loadConfig(func(k string) string {
		return map[string]string{"HOME": "/home/ann", "DEVDIGEST_CLONE_DIR": "./clones"}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	if want := filepath.Join(wd, "clones"); cfg.cloneDir != want {
		t.Errorf("cloneDir = %q, want %q", cfg.cloneDir, want)
	}
}

func TestSilentLogLevel(t *testing.T) {
	cfg, err := loadConfig(func(k string) string { return map[string]string{"HOME": "/h", "LOG_LEVEL": "silent"}[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.logLevel <= slog.LevelError {
		t.Errorf("silent level %v would still log errors", cfg.logLevel)
	}
}

func TestReviewModel(t *testing.T) {
	env := map[string]string{"OPENAI_API_KEY": "sk-openai", "ANTHROPIC_API_KEY": "sk-ant"}
	store := secrets.New(filepath.Join(t.TempDir(), "secrets.json"), func(k string) string { return env[k] })
	model := reviewModel(store, httpapi.ModelAPIs{OpenAI: "http://openai.test", Anthropic: "http://anthropic.test"}, nil)
	tests := []struct {
		provider, wantErr string
	}{
		{"openai", ""},
		{"anthropic", ""},
		{"openrouter", "OPENROUTER_API_KEY is not configured"},
		{"gemini", `unknown provider "gemini"`},
	}
	for _, tt := range tests {
		llm, err := model(tt.provider)
		switch {
		case tt.wantErr == "" && (err != nil || llm == nil):
			t.Errorf("%s: %v, %v", tt.provider, llm, err)
		case tt.wantErr != "" && (err == nil || err.Error() != tt.wantErr):
			t.Errorf("%s: err = %v, want %q", tt.provider, err, tt.wantErr)
		}
	}
}

// With the claude CLI, the anthropic provider runs through it and needs no
// key; the other providers still need theirs.
func TestReviewModelClaudeCode(t *testing.T) {
	store := secrets.New(filepath.Join(t.TempDir(), "secrets.json"), func(string) string { return "" })
	claude := claudecode.New("claude", nil)
	model := reviewModel(store, httpapi.ModelAPIs{Anthropic: "http://anthropic.test"}, claude)

	llm, err := model("anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if llm != claude {
		t.Errorf("anthropic got %T, want the claude CLI client", llm)
	}
	if _, err := model("openai"); err == nil || err.Error() != "OPENAI_API_KEY is not configured" {
		t.Errorf("openai: err = %v, want the missing key", err)
	}
}

// AC-17a: without the claude CLI wired, picking a Claude Code alias (such as
// the intent feature's default, haiku) as the intent model fails with a
// message naming the setting to change, instead of trying the Anthropic API
// key reviewModel would otherwise fall through to.
func TestIntentModelNeedsClaudeCode(t *testing.T) {
	store := secrets.New(filepath.Join(t.TempDir(), "secrets.json"), func(string) string { return "" })
	model := intentModel(store, httpapi.ModelAPIs{Anthropic: "http://anthropic.test"}, nil)

	_, err := model("anthropic", "haiku")
	wantErr := "intent model `haiku` needs ANTHROPIC_VIA_CLAUDE_CODE=true; pick another intent model in Settings"
	if err == nil || err.Error() != wantErr {
		t.Errorf("err = %v, want %q", err, wantErr)
	}

	// Any other Claude Code alias is refused the same way.
	if _, err := model("anthropic", "opus"); err == nil || !strings.Contains(err.Error(), "needs ANTHROPIC_VIA_CLAUDE_CODE=true") {
		t.Errorf("opus: err = %v", err)
	}

	// A non-Claude-Code choice (or any provider once claude is wired) falls
	// through to reviewModel, unaffected.
	if _, err := model("openai", "gpt-test"); err == nil || err.Error() != "OPENAI_API_KEY is not configured" {
		t.Errorf("openai: err = %v, want the missing key", err)
	}
}

// With the claude CLI wired, an "anthropic" intent model choice, Claude Code
// alias or not, runs through it — the same as reviewModel.
func TestIntentModelWithClaudeCode(t *testing.T) {
	store := secrets.New(filepath.Join(t.TempDir(), "secrets.json"), func(string) string { return "" })
	claude := claudecode.New("claude", nil)
	model := intentModel(store, httpapi.ModelAPIs{Anthropic: "http://anthropic.test"}, claude)

	llm, err := model("anthropic", "haiku")
	if err != nil || llm != claude {
		t.Errorf("anthropic/haiku: %v, %v, want the claude CLI client", llm, err)
	}
}
