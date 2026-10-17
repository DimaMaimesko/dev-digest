package onboarding

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/git"
	"github.com/DimaMaimesko/dev-digest/api/internal/tour"
)

func gatherWriteFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gatherInitGitRepo(t *testing.T, dir string) string {
	t.Helper()
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")
	run("add", ".")
	run("commit", "-q", "-m", "first")
	return run("rev-parse", "HEAD")
}

// TestReadDocs_RootManifestsWinTheCap proves the fix for "root manifests
// pushed out of the 20-file cap": many deep, allowlisted docs must not push
// the repository's own root manifests (go.mod, package.json, README.md) out
// of maxDocsFiles — candidates are ordered root README first, then by path
// depth, then by path.
func TestReadDocs_RootManifestsWinTheCap(t *testing.T) {
	dir := t.TempDir()
	gatherWriteFile(t, dir, "README.md", "# demo\n")
	gatherWriteFile(t, dir, "go.mod", "module demo\n")
	gatherWriteFile(t, dir, "package.json", "{}\n")
	// More deep, allowlisted docs than fit alongside the 3 root files within
	// maxDocsFiles (20): 25 nested READMEs at depth 2, which Readable still
	// allows (depth <= 2) but should lose the cap to the root's own files.
	for i := 0; i < 25; i++ {
		gatherWriteFile(t, dir, fmt.Sprintf("pkg%02d/sub/README.md", i), "nested\n")
	}
	commit := gatherInitGitRepo(t, dir)

	entries, err := git.ListTree(context.Background(), dir, commit)
	if err != nil {
		t.Fatal(err)
	}

	s := &Store{log: slog.New(slog.DiscardHandler)}
	docs, readme := s.readDocs(context.Background(), dir, commit, entries)

	if readme != "# demo\n" {
		t.Fatalf("readme = %q, want the root README's content", readme)
	}
	if len(docs) != maxDocsFiles {
		t.Fatalf("%d docs read, want the cap of %d", len(docs), maxDocsFiles)
	}

	var sawRootReadme, sawGoMod, sawPackageJSON bool
	for _, d := range docs {
		switch d.Path {
		case "README.md":
			sawRootReadme = true
		case "go.mod":
			sawGoMod = true
		case "package.json":
			sawPackageJSON = true
		}
	}
	if !sawRootReadme || !sawGoMod || !sawPackageJSON {
		t.Errorf("docs = %+v, want the root's README.md, go.mod and package.json all kept under the cap",
			pathsOf(docs))
	}
}

func pathsOf(docs []tour.Doc) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.Path
	}
	return out
}
