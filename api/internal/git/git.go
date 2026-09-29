// Package git runs git in a repository's clone.
package git

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

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
	for _, line := range strings.Split(out, "\n") {
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
