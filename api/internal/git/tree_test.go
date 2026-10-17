package git_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/git"
)

// treeRun runs git in dir with a deterministic identity, for the tests in
// this file.
func treeRun(t *testing.T, dir string, args ...string) string {
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

// treeRepo makes a repository whose first commit has nested directories, a
// fake submodule entry (a tree entry of git's "commit" type, added with
// update-index --cacheinfo so no network or .gitmodules is involved), a
// binary file containing the literal text "TODO", and several text files
// with TODO/FIXME comments. A second commit then changes the tree, so head
// differs from the first commit the tests read.
func treeRepo(t *testing.T) (dir, first string) {
	t.Helper()
	dir = t.TempDir()
	write := func(name, text string) {
		t.Helper()
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	treeRun(t, dir, "init", "-q", "-b", "main")
	write("a.txt", "plain file\n")
	write("dir/sub/file.go", "package sub\n\n// TODO: refactor this\nfunc f() {}\n")
	write("dir/sub/other.txt", "// FIXME: one\n// FIXME: two\n// FIXME: three\nno marker here\n")
	write("dir/readme.md", "# nothing to do here\n")
	if err := os.WriteFile(filepath.Join(dir, "bin.dat"), []byte("TODO\x00binary\x00data"), 0o644); err != nil {
		t.Fatal(err)
	}
	treeRun(t, dir, "add", ".")
	// A fake submodule entry: a tree entry of type "commit", without ever
	// registering .gitmodules or talking to a remote.
	treeRun(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+strings.Repeat("a", 40)+",vendor/lib")
	treeRun(t, dir, "commit", "-qm", "first")
	first = treeRun(t, dir, "rev-parse", "HEAD")

	write("a.txt", "plain file, changed\n")
	write("new.txt", "added after first\n")
	treeRun(t, dir, "add", ".")
	treeRun(t, dir, "commit", "-qm", "second")
	return dir, first
}

func TestListTree(t *testing.T) {
	dir, first := treeRepo(t)
	entries, err := git.ListTree(context.Background(), dir, first)
	if err != nil {
		t.Fatal(err)
	}

	byPath := map[string]bool{}
	for _, e := range entries {
		byPath[e.Path] = e.Dir
	}

	for _, path := range []string{"a.txt", "dir/sub/file.go", "dir/sub/other.txt", "dir/readme.md"} {
		if d, ok := byPath[path]; !ok || d {
			t.Errorf("entries missing file %q (ok=%v dir=%v)", path, ok, d)
		}
	}
	for _, path := range []string{"dir", "dir/sub"} {
		if d, ok := byPath[path]; !ok || !d {
			t.Errorf("entries missing directory %q (ok=%v dir=%v)", path, ok, d)
		}
	}
	if _, ok := byPath["new.txt"]; ok {
		t.Error("ListTree at the first commit must not see a file added in the second")
	}
	if _, ok := byPath["vendor/lib"]; ok {
		t.Error("ListTree must skip a submodule (commit) entry")
	}

	if status := treeRun(t, dir, "status", "--porcelain"); status != "" {
		t.Errorf("work tree changed: %q", status)
	}
}

func TestListTreeUnknownCommit(t *testing.T) {
	dir, _ := treeRepo(t)
	if _, err := git.ListTree(context.Background(), dir, "0123456789abcdef0123456789abcdef01234567"); err == nil {
		t.Error("no error for an unknown commit")
	}
}

func TestGrepTodos(t *testing.T) {
	dir, first := treeRepo(t)
	todos, err := git.GrepTodos(context.Background(), dir, first, 10)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{
		"dir/sub/file.go:3":   true,
		"dir/sub/other.txt:1": true,
		"dir/sub/other.txt:2": true,
		"dir/sub/other.txt:3": true,
	}
	got := map[string]bool{}
	for _, td := range todos {
		got[fmt.Sprintf("%s:%d", td.Path, td.Line)] = true
		if td.Path == "bin.dat" {
			t.Error("GrepTodos must skip a binary file")
		}
	}
	for key := range want {
		if !got[key] {
			t.Errorf("missing TODO/FIXME match %q; got %+v", key, todos)
		}
	}
	if len(todos) != len(want) {
		t.Errorf("len(todos) = %d, want %d: %+v", len(todos), len(want), todos)
	}

	if status := treeRun(t, dir, "status", "--porcelain"); status != "" {
		t.Errorf("work tree changed: %q", status)
	}
}

func TestGrepTodosAtFirstCommit(t *testing.T) {
	// The second commit removes no TODOs, but renames a.txt's content; this
	// proves GrepTodos reads the commit it's given, not HEAD.
	dir, first := treeRepo(t)
	head := treeRun(t, dir, "rev-parse", "HEAD")
	if head == first {
		t.Fatal("test setup: head should differ from the first commit")
	}

	todos, err := git.GrepTodos(context.Background(), dir, first, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, td := range todos {
		if td.Path == "new.txt" {
			t.Error("GrepTodos at the first commit must not see a file added in the second")
		}
	}
}

func TestGrepTodosMax(t *testing.T) {
	dir, first := treeRepo(t)
	todos, err := git.GrepTodos(context.Background(), dir, first, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(todos) != 2 {
		t.Errorf("len(todos) = %d, want 2: %+v", len(todos), todos)
	}

	if status := treeRun(t, dir, "status", "--porcelain"); status != "" {
		t.Errorf("work tree changed: %q", status)
	}
}

// TestGrepTodosLongLine proves a single matching line far longer than
// bufio.Scanner's default max token size (64KiB, the cap this package used
// to pass to scanner.Buffer) doesn't fail the whole call, and that the
// stored Todo.Text is clipped rather than kept in full.
func TestGrepTodosLongLine(t *testing.T) {
	dir := t.TempDir()
	treeRun(t, dir, "init", "-q", "-b", "main")

	long := "x" + strings.Repeat("y", 2<<20) // > 2 MiB, well past 1 MiB
	content := "// TODO: " + long + "\n"
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	treeRun(t, dir, "add", ".")
	treeRun(t, dir, "commit", "-qm", "big line")
	head := treeRun(t, dir, "rev-parse", "HEAD")

	todos, err := git.GrepTodos(context.Background(), dir, head, 10)
	if err != nil {
		t.Fatalf("GrepTodos must not fail on an over-long line: %v", err)
	}
	if len(todos) != 1 {
		t.Fatalf("len(todos) = %d, want 1: %+v", len(todos), todos)
	}
	if todos[0].Path != "big.txt" || todos[0].Line != 1 {
		t.Errorf("todos[0] = %+v, want big.txt:1", todos[0])
	}
	if n := len([]rune(todos[0].Text)); n > 300 {
		t.Errorf("len(Text) = %d runes, want <= 300", n)
	}
}

// TestGrepTodosLongLineTodoPastPrefix proves a line whose TODO/FIXME marker
// sits after git grep -z's "commit:path\x00line\x00" prefix but past the
// first 4KiB of matched text is still returned: git grep itself matched the
// whole line, so GrepTodos reports it, with Text clipped from only the
// first ~4KiB of the line (the part it keeps in memory at all), not from
// wherever the marker happened to be. A caller wanting to see the marker
// itself in Text needs it within that prefix; that's an accepted limit, not
// a bug, since keeping the whole line defeats the point of bounding memory.
func TestGrepTodosLongLineTodoPastPrefix(t *testing.T) {
	dir := t.TempDir()
	treeRun(t, dir, "init", "-q", "-b", "main")

	// Padding comfortably past the 4KiB line cap, with the marker only
	// after it.
	padding := strings.Repeat("x", 8<<10)
	content := "// " + padding + " TODO: trailing marker\n"
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	treeRun(t, dir, "add", ".")
	treeRun(t, dir, "commit", "-qm", "big line, todo past the prefix")
	head := treeRun(t, dir, "rev-parse", "HEAD")

	todos, err := git.GrepTodos(context.Background(), dir, head, 10)
	if err != nil {
		t.Fatalf("GrepTodos must not fail on an over-long line: %v", err)
	}
	if len(todos) != 1 {
		t.Fatalf("len(todos) = %d, want 1: %+v", len(todos), todos)
	}
	if todos[0].Path != "big.txt" || todos[0].Line != 1 {
		t.Errorf("todos[0] = %+v, want big.txt:1", todos[0])
	}
	if n := len([]rune(todos[0].Text)); n > 300 {
		t.Errorf("len(Text) = %d runes, want <= 300", n)
	}
	if strings.Contains(todos[0].Text, "trailing marker") {
		t.Errorf("Text = %q, want it clipped from the kept prefix, not to include text past it", todos[0].Text)
	}
}

func TestGrepTodosNoMatches(t *testing.T) {
	dir := t.TempDir()
	treeRun(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("nothing to see\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	treeRun(t, dir, "add", ".")
	treeRun(t, dir, "commit", "-qm", "base")
	head := treeRun(t, dir, "rev-parse", "HEAD")

	todos, err := git.GrepTodos(context.Background(), dir, head, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(todos) != 0 {
		t.Errorf("todos = %+v, want none", todos)
	}
}
