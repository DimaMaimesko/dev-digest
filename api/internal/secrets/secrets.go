// Package secrets reads the API keys and tokens DevDigest uses. Keys the user
// enters in the web app are stored in a JSON file readable only by them
// (~/.devdigest/secrets.json, shared with the TS server), never in the
// database; environment variables are the fallback.
package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// Names of the secrets DevDigest uses.
const (
	OpenAIKey     = "OPENAI_API_KEY"
	AnthropicKey  = "ANTHROPIC_API_KEY"
	OpenRouterKey = "OPENROUTER_API_KEY"
	GitHubToken   = "GITHUB_TOKEN"
)

// Store reads secrets from a file, then from the environment.
type Store struct {
	path   string
	getenv func(string) string
}

// New returns a Store that reads the file at path, which may not exist yet,
// and falls back to getenv.
func New(path string, getenv func(string) string) *Store {
	return &Store{path: path, getenv: getenv}
}

// Get returns the secret called name, or "" when it isn't set. A value in the
// file wins over the environment, so a key entered in the web app takes
// effect. For GitHubToken, GITHUB_PAT is read when GITHUB_TOKEN is empty.
//
// It reads the file on every call; it is small, and edits then apply without
// a restart. It returns an error when the file exists but isn't a JSON object
// of strings.
func (s *Store) Get(name string) (string, error) {
	stored, err := s.load()
	if err != nil {
		return "", err
	}
	if v := stored[name]; v != "" {
		return v, nil
	}
	v := s.getenv(name)
	if v == "" && name == GitHubToken {
		v = s.getenv("GITHUB_PAT")
	}
	return v, nil
}

func (s *Store) load() (map[string]string, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil // nothing entered in the web app yet
	}
	if err != nil {
		return nil, err
	}
	var stored map[string]string
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("secrets file %s is not a JSON object of strings: %w", s.path, err)
	}
	return stored, nil
}
