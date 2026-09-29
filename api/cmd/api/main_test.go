package main

import (
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
				"REPO_INTEL_ENABLED": "false"},
			want: config{databaseURL: "postgres://x/y", port: 3002, webOrigin: "http://localhost:4000", logLevel: slog.LevelDebug,
				cloneDir: "/srv/clones", secretsPath: "/home/ann/.devdigest/secrets.json", repoIntel: false},
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
		{name: "TS_API_URL without a scheme", env: map[string]string{"TS_API_URL": "localhost:3001"}, wantErr: `TS_API_URL "localhost:3001"`},
		{name: "TS_API_URL not http", env: map[string]string{"TS_API_URL": "ftp://localhost:3001"}, wantErr: `TS_API_URL "ftp://localhost:3001"`},
		{name: "TS_API_URL pointing at itself", env: map[string]string{"TS_API_URL": "http://localhost:3001"}, wantErr: "forward requests to itself"},
		{name: "TS_API_URL pointing at itself by IP", env: map[string]string{"API_PORT": "3002", "TS_API_URL": "http://127.0.0.1:3002"}, wantErr: "forward requests to itself"},
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

// A relative DEVDIGEST_CLONE_DIR is relative to the working directory, as in
// the TS server, whose .env sets ./clones.
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

func TestTSAPIURL(t *testing.T) {
	cfg, err := loadConfig(func(k string) string {
		return map[string]string{"HOME": "/h", "API_PORT": "3002", "TS_API_URL": "http://localhost:3001"}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.tsAPI == nil || cfg.tsAPI.String() != "http://localhost:3001" {
		t.Errorf("tsAPI = %v", cfg.tsAPI)
	}

	cfg, _ = loadConfig(func(k string) string { return map[string]string{"HOME": "/h"}[k] })
	if cfg.tsAPI != nil {
		t.Errorf("tsAPI = %v without TS_API_URL, want nil", cfg.tsAPI)
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
	model := reviewModel(store, httpapi.ModelAPIs{OpenAI: "http://openai.test", Anthropic: "http://anthropic.test"})
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
