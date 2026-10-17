// Package git runs git in a repository's clone.
package git

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ErrNotFound reports that a path doesn't exist at the commit a ReadBlob
// call asked about.
var ErrNotFound = errors.New("git: path not found at commit")

// Diff returns `git diff base...head` in the clone at dir: what head changes
// since it left base, as a pull request shows it.
func Diff(ctx context.Context, dir, base, head string) (string, error) {
	// --end-of-options: a ref starting with "-" is a ref, not an option.
	return run(ctx, dir, "", "diff", "--end-of-options", base+"..."+head)
}

// Clone makes dir a clone of the repository at url, with the last depth
// commits (all of them when depth is 0). When dir already holds a clone, it
// fetches instead. A directory there that isn't a clone, such as one a
// clone cut short left, is replaced.
//
// token, when not empty, authenticates to GitHub for this command only: it
// isn't saved in the clone's configuration.
func Clone(ctx context.Context, dir, url, token string, depth int) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return Fetch(ctx, dir, token)
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	args := []string{"clone"}
	if depth > 0 {
		args = append(args, "--depth", strconv.Itoa(depth))
	}
	args = append(args, "--end-of-options", url, dir)
	_, err := run(ctx, filepath.Dir(dir), token, args...)
	return err
}

// Fetch runs `git fetch` in the clone at dir.
func Fetch(ctx context.Context, dir, token string) error {
	_, err := run(ctx, dir, token, "fetch")
	return err
}

// env is the environment git runs with. It never waits for a password on a
// terminal: it fails instead. token, when not empty, authenticates to GitHub
// with a header set in the environment, which lasts for one command and,
// unlike -c, doesn't show in the process list.
func env(token string) []string {
	vars := []string{"GIT_TERMINAL_PROMPT=0"}
	if token != "" {
		auth := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
		vars = append(vars,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
			"GIT_CONFIG_VALUE_0=Authorization: Basic "+auth)
	}
	return vars
}

// run runs git in dir; token, when not empty, authenticates to GitHub.
func run(ctx context.Context, dir, token string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// Never wait for a password on a terminal: fail instead.
	cmd.Env = append(os.Environ(), env(token)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// Head returns the commit the clone at dir has checked out.
func Head(ctx context.Context, dir string) (string, error) {
	out, err := run(ctx, dir, "", "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

// ChangedFiles lists the files that changed from commit base to head
// (`git diff --name-only base..head`), relative to the clone's root.
func ChangedFiles(ctx context.Context, dir, base, head string) ([]string, error) {
	if base == head {
		return nil, nil
	}
	out, err := run(ctx, dir, "", "diff", "--name-only", "--end-of-options", base+".."+head)
	if err != nil {
		return nil, err
	}
	var files []string
	for line := range strings.SplitSeq(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// syncDepth is how many commits Sync fetches: more than a clone's one, so
// the last indexed commit is usually there for an incremental index.
const syncDepth = 50

// Sync moves the clone at dir to the latest commit of branch on GitHub:
// it fetches it and resets the checkout to it. The clone is a read-only
// mirror, so nothing is lost.
func Sync(ctx context.Context, dir, branch, token string) error {
	if _, err := run(ctx, dir, token, "fetch", "--depth", strconv.Itoa(syncDepth), "--end-of-options", "origin", branch); err != nil {
		return err
	}
	// "origin/…" can't be read as an option; "--" ends the revisions.
	_, err := run(ctx, dir, "", "reset", "--hard", "origin/"+branch, "--")
	return err
}

// HasCommit reports whether the clone at dir has sha, without fetching.
// A clone is usually shallow, so a pull request's head commit is often
// missing until FetchCommit brings it in.
func HasCommit(ctx context.Context, dir, sha string) bool {
	_, err := run(ctx, dir, "", "cat-file", "-e", "--end-of-options", sha+"^{commit}")
	return err == nil
}

// FetchCommit fetches exactly sha into the clone at dir, as a one-commit
// shallow fetch. token, when not empty, authenticates to GitHub for this
// command only.
func FetchCommit(ctx context.Context, dir, sha, token string) error {
	_, err := run(ctx, dir, token, "fetch", "--depth", "1", "--end-of-options", "origin", sha)
	return err
}

// ReadBlob returns the content of path as it is at commit in the clone at
// dir, and the mode of its tree entry (for example "100644", or "120000"
// for a symlink). It reads at most max bytes of the blob (0 means
// unlimited). It returns ErrNotFound when path doesn't exist at commit.
func ReadBlob(ctx context.Context, dir, commit, path string, max int64) (data []byte, mode string, err error) {
	out, err := run(ctx, dir, "", "ls-tree", "-z", "--end-of-options", commit, "--", path)
	if err != nil {
		return nil, "", err
	}
	out = strings.TrimRight(out, "\x00")
	if out == "" {
		return nil, "", ErrNotFound
	}

	// path can name a directory (or "."): ls-tree then lists its entries,
	// none of whose own paths equal path. Only a record whose path is
	// exactly path names a single file.
	var header string
	found := false
	for entry := range strings.SplitSeq(out, "\x00") {
		h, p, ok := strings.Cut(entry, "\t")
		if !ok {
			return nil, "", fmt.Errorf("git ls-tree: unexpected output %q", entry)
		}
		if p == path {
			header, found = h, true
			break
		}
	}
	if !found {
		return nil, "", ErrNotFound
	}
	fields := strings.Fields(header)
	if len(fields) != 3 {
		return nil, "", fmt.Errorf("git ls-tree: unexpected entry %q", header)
	}
	mode, objType, oid := fields[0], fields[1], fields[2]
	if objType != "blob" {
		// A submodule (commit) or a tree entry asked about by a path that
		// is a directory: there is no blob content to read.
		return nil, "", ErrNotFound
	}

	sizeOut, err := run(ctx, dir, "", "cat-file", "-s", "--end-of-options", oid)
	if err != nil {
		return nil, "", err
	}
	size, err := strconv.ParseInt(strings.TrimSpace(sizeOut), 10, 64)
	if err != nil {
		return nil, "", fmt.Errorf("git cat-file -s: unexpected output %q", sizeOut)
	}

	if max > 0 && size > max {
		data, err = readBlobCapped(ctx, dir, oid, max)
	} else {
		var content string
		content, err = run(ctx, dir, "", "cat-file", "blob", "--end-of-options", oid)
		data = []byte(content)
	}
	if err != nil {
		return nil, "", err
	}
	return data, mode, nil
}

// readBlobCapped runs `git cat-file blob oid` in dir and reads at most max
// bytes of its output, the same way run does (--end-of-options, env,
// stderr on error): size has already proved the blob itself is larger, so
// this stops the process once max bytes are read instead of buffering the
// whole blob first.
func readBlobCapped(ctx context.Context, dir, oid string, max int64) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "cat-file", "blob", "--end-of-options", oid)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env("")...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("git cat-file blob %s: %w", oid, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("git cat-file blob %s: %w", oid, err)
	}

	data, readErr := io.ReadAll(io.LimitReader(stdout, max))
	// The cap was reached (or the process ended on its own first): either
	// way, stop it rather than let it keep writing a pipe nothing reads.
	_ = cmd.Process.Kill()
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, fmt.Errorf("git cat-file blob %s: %w", oid, readErr)
	}
	if int64(len(data)) < max && waitErr != nil {
		// The process ended on its own, before filling the cap: a real
		// failure, not the kill above.
		return nil, fmt.Errorf("git cat-file blob %s: %w: %s", oid, waitErr, strings.TrimSpace(stderr.String()))
	}
	return data, nil
}

// TreeEntry is one path in a tree, as ListTree lists it.
type TreeEntry struct {
	Path string
	Dir  bool
}

// ListTree lists every file and directory under commit in the clone at dir,
// read from the object store (the work tree is never touched). A submodule
// entry (git's "commit" tree-object type) is skipped: it names a different
// repository, not a path in this one.
func ListTree(ctx context.Context, dir, commit string) ([]TreeEntry, error) {
	out, err := run(ctx, dir, "", "ls-tree", "-r", "-t", "-z", "--full-tree", "--end-of-options", commit)
	if err != nil {
		return nil, err
	}
	out = strings.TrimRight(out, "\x00")
	if out == "" {
		return nil, nil
	}

	var entries []TreeEntry
	for entry := range strings.SplitSeq(out, "\x00") {
		header, path, ok := strings.Cut(entry, "\t")
		if !ok {
			return nil, fmt.Errorf("git ls-tree: unexpected output %q", entry)
		}
		fields := strings.Fields(header)
		if len(fields) != 3 {
			return nil, fmt.Errorf("git ls-tree: unexpected entry %q", header)
		}
		objType := fields[1]
		if objType == "commit" {
			continue // a submodule, not a path in this repository
		}
		entries = append(entries, TreeEntry{Path: path, Dir: objType == "tree"})
	}
	return entries, nil
}

// Todo is one TODO/FIXME comment GrepTodos found.
type Todo struct {
	Path string
	Line int
	Text string
}

// todoPattern matches a whole "TODO" or "FIXME" word. git's regex engine
// doesn't support "\b" portably across platforms (it does on Linux's glibc,
// not on macOS's BSD regex library), so -w (--word-regexp) gives the same
// "whole word" match without relying on that escape.
const todoPattern = `(TODO|FIXME)`

// maxTodoTextRunes caps how many runes of a matched line GrepTodos keeps in
// Todo.Text, so one very long source line (a committed sourcemap, say)
// doesn't balloon the result.
const maxTodoTextRunes = 300

// maxGrepLineBytes caps how many bytes of a single `git grep -z` match
// (header, line number and text together) GrepTodos keeps in memory. A
// 20MB minified one-line bundle containing "TODO" would otherwise have its
// whole line read in full by readGrepLine before any clipping happened.
// With -z the header ("commit:path") and the line number come first, so
// they always fit inside this prefix; only the matched text's tail is ever
// lost to the cut.
const maxGrepLineBytes = 4 << 10

// clipTodoText truncates s to at most maxTodoTextRunes runes, cutting on a
// rune boundary. It ranges over s (like tour.clipRunes) instead of
// converting to []rune, so clipping a line that's already been bounded to
// maxGrepLineBytes never allocates a rune slice sized to the original
// (unbounded) line.
func clipTodoText(s string) string {
	count := 0
	for i := range s {
		if count == maxTodoTextRunes {
			return s[:i]
		}
		count++
	}
	return s
}

// trimPartialRune drops a trailing incomplete UTF-8 sequence from b. A
// byte-count cut (maxGrepLineBytes, or a bufio read buffer boundary) can
// land in the middle of a multi-byte rune; this makes sure the kept prefix
// never ends on a split rune, dropping the partial one instead.
func trimPartialRune(b []byte) []byte {
	for i := len(b) - 1; i >= 0 && i > len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if utf8.FullRune(b[i:]) {
				return b
			}
			return b[:i]
		}
	}
	return b
}

// readGrepLine reads the next '\n'-terminated record from r (a `git grep
// -z` stream, so a "line" here is one whole match, header through text),
// keeping at most maxGrepLineBytes of it. ReadSlice reports
// bufio.ErrBufferFull when its internal buffer fills before the delimiter
// is found; readGrepLine keeps calling it until the delimiter (or EOF)
// turns up, discarding whatever didn't fit in the cap along the way. That
// bounds memory to r's buffer size per ReadSlice call, never to the line's
// real length, however long a single minified bundle's line is. The
// returned slice has no trailing '\n' and never ends on a split UTF-8
// rune (trimPartialRune).
func readGrepLine(r *bufio.Reader) (line []byte, err error) {
	for {
		chunk, rerr := r.ReadSlice('\n')
		add := chunk
		if rerr == nil {
			add = chunk[:len(chunk)-1] // strip the delimiter itself
		}
		if len(line) < maxGrepLineBytes {
			n := maxGrepLineBytes - len(line)
			if n > len(add) {
				n = len(add)
			}
			line = append(line, add[:n]...)
		}
		if rerr == nil {
			return trimPartialRune(line), nil
		}
		if rerr == bufio.ErrBufferFull {
			continue
		}
		return trimPartialRune(line), rerr
	}
}

// GrepTodos greps for TODO/FIXME comments at commit in the clone at dir,
// read from the object store like ListTree. It stops reading, and cancels
// the command, once it has max matches; a binary file is skipped (git's
// -I). Exit code 1 (no matches at all) returns an empty slice, not an
// error. Output is read line by line with readGrepLine rather than
// bufio.Scanner or a plain ReadString, so a single matching line far
// longer than a scanner's buffer (a committed sourcemap, say) is still
// returned, with Todo.Text clipped from its first maxGrepLineBytes bytes,
// rather than either failing the call or holding the whole line (which can
// run to tens of megabytes) just to keep maxTodoTextRunes of it.
func GrepTodos(ctx context.Context, dir, commit string, max int) ([]Todo, error) {
	if max <= 0 {
		return nil, nil
	}

	cctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// "--" right after -E ends option parsing, so the pattern and commit
	// below are always read as positional arguments, never as flags
	// ("--end-of-options" doesn't work here: by the time grep reaches it,
	// it's already parsing revisions, not options, and rejects it as an
	// unresolvable one).
	cmd := exec.CommandContext(cctx, "git", "grep", "-n", "-I", "-z", "-w", "-E", "--", todoPattern, commit, "--")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env("")...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("git grep: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("git grep: %w", err)
	}

	prefix := commit + ":"
	var todos []Todo
	reader := bufio.NewReaderSize(stdout, 64<<10)
	hitMax := false
	var readErr error
readLoop:
	for {
		lineBytes, rerr := readGrepLine(reader)
		if len(lineBytes) != 0 {
			if header, lineNum, text, ok := parseGrepMatch(string(lineBytes)); ok {
				todos = append(todos, Todo{
					Path: strings.TrimPrefix(header, prefix),
					Line: lineNum,
					Text: clipTodoText(text),
				})
				if len(todos) >= max {
					hitMax = true
					break readLoop
				}
			}
		}
		if rerr != nil {
			if rerr != io.EOF {
				readErr = rerr
			}
			break readLoop
		}
	}

	if hitMax {
		// The cap stopped reading before the process finished on its own:
		// cancel and kill it rather than let it keep writing a pipe nothing
		// reads. The resulting kill (not a real failure) is why this never
		// calls cmd.Wait's error path below.
		cancel()
		_ = cmd.Wait()
		return todos, nil
	}

	if readErr != nil {
		// A read error other than EOF means the pipe won't be drained any
		// further; cancel first so Wait can't block on a full pipe, same as
		// the max-matches path above.
		cancel()
		_ = cmd.Wait()
		return nil, fmt.Errorf("git grep: %w: %s", readErr, strings.TrimSpace(stderr.String()))
	}

	// Reached EOF on its own, without the cap: nothing to cancel, so wait
	// normally and don't race the process's own exit.
	waitErr := cmd.Wait()

	var exitErr *exec.ExitError
	if waitErr != nil && !(errors.As(waitErr, &exitErr) && exitErr.ExitCode() == 1) {
		return nil, fmt.Errorf("git grep: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return todos, nil
}

// parseGrepMatch splits one line of `git grep -n -z` output into the
// "commit:path" header, the line number and the matched text. With -z, both
// separators that would otherwise be ':' (after the path, and after the
// line number) become NUL.
func parseGrepMatch(line string) (header string, lineNum int, text string, ok bool) {
	parts := strings.SplitN(line, "\x00", 3)
	if len(parts) != 3 {
		return "", 0, "", false
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, "", false
	}
	return parts[0], n, parts[2], true
}
