package secrets_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/secrets"
)

func TestGet(t *testing.T) {
	tests := []struct {
		name string
		file string // contents of the secrets file; "" means no file
		env  map[string]string
		key  string
		want string
	}{
		{"no file, no env", "", nil, secrets.OpenAIKey, ""},
		{"from env", "", map[string]string{secrets.OpenAIKey: "sk-env"}, secrets.OpenAIKey, "sk-env"},
		{"file wins over env", `{"OPENAI_API_KEY": "sk-file"}`, map[string]string{secrets.OpenAIKey: "sk-env"}, secrets.OpenAIKey, "sk-file"},
		{"empty in file falls back to env", `{"OPENAI_API_KEY": ""}`, map[string]string{secrets.OpenAIKey: "sk-env"}, secrets.OpenAIKey, "sk-env"},
		{"other keys in file", `{"ANTHROPIC_API_KEY": "sk-ant"}`, nil, secrets.OpenAIKey, ""},
		{"GITHUB_PAT when GITHUB_TOKEN is unset", "", map[string]string{"GITHUB_PAT": "ghp_pat"}, secrets.GitHubToken, "ghp_pat"},
		// .env.example ships "GITHUB_TOKEN=" empty; the TS server then never
		// reached GITHUB_PAT.
		{"GITHUB_PAT when GITHUB_TOKEN is empty", "", map[string]string{"GITHUB_TOKEN": "", "GITHUB_PAT": "ghp_pat"}, secrets.GitHubToken, "ghp_pat"},
		{"GITHUB_TOKEN wins over GITHUB_PAT", "", map[string]string{"GITHUB_TOKEN": "ghp_tok", "GITHUB_PAT": "ghp_pat"}, secrets.GitHubToken, "ghp_tok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "secrets.json")
			if tt.file != "" {
				if err := os.WriteFile(path, []byte(tt.file), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			store := secrets.New(path, func(k string) string { return tt.env[k] })

			got, err := store.Get(tt.key)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got != tt.want {
				t.Errorf("Get(%s) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestGetReadsEditsWithoutRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	store := secrets.New(path, func(string) string { return "" })

	if got, _ := store.Get(secrets.OpenAIKey); got != "" {
		t.Fatalf("got %q before the file exists", got)
	}
	if err := os.WriteFile(path, []byte(`{"OPENAI_API_KEY": "sk-new"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Get(secrets.OpenAIKey); got != "sk-new" {
		t.Errorf("got %q after writing the file, want sk-new", got)
	}
}

func TestGetMalformedFile(t *testing.T) {
	for _, contents := range []string{`not json`, `["a"]`, `{"OPENAI_API_KEY": 42}`} {
		t.Run(contents, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "secrets.json")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := secrets.New(path, os.Getenv).Get(secrets.OpenAIKey)
			if err == nil || !strings.Contains(err.Error(), path) {
				t.Errorf("err = %v, want an error naming the file", err)
			}
		})
	}
}
