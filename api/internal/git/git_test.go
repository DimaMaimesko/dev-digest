package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/git"
)

// repo makes a repository whose branch feat changes a.txt after main moved
// on, and returns its directory and feat's head commit.
func repo(t *testing.T) (dir, head string) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	write("a.txt", "one\n")
	run("add", ".")
	run("commit", "-qm", "base")
	run("checkout", "-qb", "feat")
	write("a.txt", "one\ntwo\n")
	run("commit", "-qam", "feat")
	head = run("rev-parse", "HEAD")
	run("checkout", "-q", "main")
	write("b.txt", "on main\n") // main moves on; the three-dot diff leaves it out
	run("add", ".")
	run("commit", "-qm", "main moves")
	return dir, head
}

func TestDiff(t *testing.T) {
	dir, head := repo(t)
	out, err := git.Diff(context.Background(), dir, "main", head)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "diff --git a/a.txt b/a.txt") || !strings.Contains(out, "+two") || strings.Contains(out, "b.txt") {
		t.Errorf("diff:\n%s", out)
	}
}

func TestDiffErrors(t *testing.T) {
	dir, head := repo(t)
	tests := []struct{ name, dir, base, head string }{
		{"unknown commit", dir, "main", "0123456789abcdef0123456789abcdef01234567"},
		{"a ref that looks like an option", dir, "--output=/tmp/x", head},
		{"not a repository", t.TempDir(), "main", head},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := git.Diff(context.Background(), tt.dir, tt.base, tt.head); err == nil {
				t.Error("no error")
			}
		})
	}
}
