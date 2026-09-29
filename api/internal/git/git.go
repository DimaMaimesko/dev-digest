// Package git runs git in a repository's clone.
package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Diff returns `git diff base...head` in the clone at dir: what head changes
// since it left base, as a pull request shows it.
func Diff(ctx context.Context, dir, base, head string) (string, error) {
	// --end-of-options: a ref starting with "-" is a ref, not an option.
	return run(ctx, dir, "diff", "--end-of-options", base+"..."+head)
}

func run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}
