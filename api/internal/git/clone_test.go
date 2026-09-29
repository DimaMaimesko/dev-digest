package git

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// remote makes a repository to clone from, with one commit, and returns
// its file:// URL and a function that adds a commit.
func remote(t *testing.T) (string, func(msg string)) {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	commit := func(msg string) {
		os.WriteFile(filepath.Join(dir, msg+".txt"), []byte(msg), 0o644)
		git("add", ".")
		git("commit", "-qm", msg)
	}
	commit("first")
	return "file://" + dir, commit
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestClone(t *testing.T) {
	url, commit := remote(t)
	dir := filepath.Join(t.TempDir(), "o", "r")
	ctx := context.Background()

	if err := Clone(ctx, dir, url, "ghp_secret", 1); err != nil {
		t.Fatal(err)
	}
	if got := gitOut(t, dir, "log", "--format=%s"); got != "first" {
		t.Errorf("log = %q", got)
	}
	// The token isn't saved in the clone; the TS server's was, in the
	// remote's URL.
	config, _ := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if strings.Contains(string(config), "ghp_secret") || strings.Contains(string(config), base64.StdEncoding.EncodeToString([]byte("x-access-token:ghp_secret"))) {
		t.Errorf("the token is in .git/config:\n%s", config)
	}

	// Cloning again fetches: the clone, and a file in it, stay.
	commit("second")
	os.WriteFile(filepath.Join(dir, "kept"), []byte("x"), 0o644)
	if err := Clone(ctx, dir, url, "", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "kept")); err != nil {
		t.Error("the clone was made again instead of fetched")
	}
	if got := gitOut(t, dir, "log", "--format=%s", "origin/main"); !strings.HasPrefix(got, "second") {
		t.Errorf("origin/main after the fetch: %q", got)
	}
}

// A directory without a clone in it, which a clone cut short can leave, is
// replaced.
func TestCloneReplacesAPartialClone(t *testing.T) {
	url, _ := remote(t)
	dir := filepath.Join(t.TempDir(), "o", "r")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "leftover"), []byte("x"), 0o644)
	if err := Clone(context.Background(), dir, url, "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "leftover")); err == nil {
		t.Error("the leftover file is still there")
	}
}

func TestCloneFails(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "o", "r")
	err := Clone(context.Background(), dir, "file:///no/such/repo", "", 1)
	if err == nil || !strings.Contains(err.Error(), "git clone") {
		t.Errorf("err = %v", err)
	}
	if err := Clone(context.Background(), dir, "--upload-pack=touch /tmp/x", "", 1); err == nil {
		t.Error("an URL that looks like an option was run")
	}
}

func TestEnv(t *testing.T) {
	if got := env(""); !slices.Equal(got, []string{"GIT_TERMINAL_PROMPT=0"}) {
		t.Errorf("no token: %v", got)
	}
	want := []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
		"GIT_CONFIG_VALUE_0=Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:tok")),
	}
	if got := env("tok"); !slices.Equal(got, want) {
		t.Errorf("env = %v", got)
	}
}
