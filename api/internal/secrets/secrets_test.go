package secrets_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new-dir", "secrets.json")
	s := secrets.New(path, func(string) string { return "" })
	if err := s.Set(secrets.OpenAIKey, "sk-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(secrets.GitHubToken, "ghp-2"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(secrets.OpenAIKey, "sk-3"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "{\n  \"GITHUB_TOKEN\": \"ghp-2\",\n  \"OPENAI_API_KEY\": \"sk-3\"\n}\n" {
		t.Errorf("file:\n%s", data)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", info.Mode().Perm())
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("%d files next to it, want none", len(entries)-1)
	}
	if v, _ := s.Get(secrets.OpenAIKey); v != "sk-3" {
		t.Errorf("Get = %q", v)
	}
}

// Saves at once lose no key.
func TestSetConcurrently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	s := secrets.New(path, func(string) string { return "" })
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			if err := s.Set(fmt.Sprintf("KEY_%d", i), "v"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	for i := range 20 {
		if v, _ := s.Get(fmt.Sprintf("KEY_%d", i)); v != "v" {
			t.Errorf("KEY_%d lost", i)
		}
	}
}

func TestSetMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	os.WriteFile(path, []byte("not json"), 0o600)
	if err := secrets.New(path, os.Getenv).Set(secrets.OpenAIKey, "sk"); err == nil {
		t.Error("no error; the file would have been replaced")
	}
	if data, _ := os.ReadFile(path); string(data) != "not json" {
		t.Error("the file changed")
	}
}
