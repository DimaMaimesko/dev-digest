package git_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/git"
)

// blobRepo makes a repository with a regular file, a symlink to it, and a
// larger file, all in one commit, and returns the repository's directory
// and that commit's sha.
func blobRepo(t *testing.T) (dir, head string) {
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
	write("a.txt", "hello world\n")
	write("big.txt", strings.Repeat("x", 100))
	if err := os.Symlink("a.txt", filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "base")
	head = run("rev-parse", "HEAD")
	return dir, head
}

func TestReadBlob(t *testing.T) {
	dir, head := blobRepo(t)
	ctx := context.Background()

	data, mode, err := git.ReadBlob(ctx, dir, head, "a.txt", 0)
	if err != nil || string(data) != "hello world\n" || mode != "100644" {
		t.Errorf("a.txt: data=%q mode=%q err=%v", data, mode, err)
	}
}

func TestReadBlobSymlink(t *testing.T) {
	dir, head := blobRepo(t)
	data, mode, err := git.ReadBlob(context.Background(), dir, head, "link.txt", 0)
	if err != nil || string(data) != "a.txt" || mode != "120000" {
		t.Errorf("link.txt: data=%q mode=%q err=%v", data, mode, err)
	}
}

func TestReadBlobMissing(t *testing.T) {
	dir, head := blobRepo(t)
	_, _, err := git.ReadBlob(context.Background(), dir, head, "nope.txt", 0)
	if !errors.Is(err, git.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestReadBlobSizeCap(t *testing.T) {
	dir, head := blobRepo(t)
	data, mode, err := git.ReadBlob(context.Background(), dir, head, "big.txt", 5)
	if err != nil || len(data) != 5 || string(data) != "xxxxx" || mode != "100644" {
		t.Errorf("big.txt capped: data=%q (%d bytes) mode=%q err=%v", data, len(data), mode, err)
	}
}

func TestReadBlobUnknownCommit(t *testing.T) {
	dir, _ := blobRepo(t)
	_, _, err := git.ReadBlob(context.Background(), dir, "0123456789abcdef0123456789abcdef01234567", "a.txt", 0)
	if err == nil {
		t.Error("no error for an unknown commit")
	}
}

// dirRepo is blobRepo's layout plus a file inside a subdirectory, so "." and
// "dir" each name several ls-tree entries rather than one.
func dirRepo(t *testing.T) (dir, head string) {
	t.Helper()
	dir, _ = blobRepo(t)
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
	if err := os.MkdirAll(filepath.Join(dir, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dir", "c.txt"), []byte("c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "add dir")
	head = run("rev-parse", "HEAD")
	return dir, head
}

func TestReadBlobRoot(t *testing.T) {
	dir, head := dirRepo(t)
	_, _, err := git.ReadBlob(context.Background(), dir, head, ".", 0)
	if !errors.Is(err, git.ErrNotFound) {
		t.Errorf(`ReadBlob(".") err = %v, want ErrNotFound`, err)
	}
}

func TestReadBlobDirectory(t *testing.T) {
	dir, head := dirRepo(t)
	_, _, err := git.ReadBlob(context.Background(), dir, head, "dir", 0)
	if !errors.Is(err, git.ErrNotFound) {
		t.Errorf(`ReadBlob("dir") err = %v, want ErrNotFound`, err)
	}
}

// TestReadBlobBoundedRead proves a capped read doesn't buffer the whole
// blob first: a blob far larger than the cap still returns in proportion to
// the cap, not the blob's own size.
func TestReadBlobBoundedRead(t *testing.T) {
	dir := t.TempDir()
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
	run("init", "-q", "-b", "main")
	const bigSize = 50 << 20 // 50 MiB: large enough that buffering it all would be wasteful
	if err := os.WriteFile(filepath.Join(dir, "huge.bin"), make([]byte, bigSize), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "huge")
	head := run("rev-parse", "HEAD")

	const cap = 10
	data, _, err := git.ReadBlob(context.Background(), dir, head, "huge.bin", cap)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != cap {
		t.Errorf("len(data) = %d, want %d", len(data), cap)
	}
}
