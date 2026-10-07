package git

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestHasCommitAndFetchCommit(t *testing.T) {
	url, commit := remote(t)
	dir := filepath.Join(t.TempDir(), "o", "r")
	ctx := context.Background()

	if err := Clone(ctx, dir, url, "", 1); err != nil {
		t.Fatal(err)
	}
	sha := gitOut(t, dir, "rev-parse", "HEAD")
	if !HasCommit(ctx, dir, sha) {
		t.Errorf("HasCommit(%s) = false right after clone", sha)
	}

	commit("second") // a new commit on the remote, made directly in its directory
	originDir := strings.TrimPrefix(url, "file://")
	newSHA := gitOut(t, originDir, "rev-parse", "HEAD")

	if HasCommit(ctx, dir, newSHA) {
		t.Errorf("HasCommit(%s) = true before FetchCommit; the shallow clone shouldn't have it yet", newSHA)
	}
	if err := FetchCommit(ctx, dir, newSHA, ""); err != nil {
		t.Fatal(err)
	}
	if !HasCommit(ctx, dir, newSHA) {
		t.Errorf("HasCommit(%s) = false after FetchCommit", newSHA)
	}

	data, mode, err := ReadBlob(ctx, dir, newSHA, "second.txt", 0)
	if err != nil || string(data) != "second" || mode != "100644" {
		t.Errorf("ReadBlob after FetchCommit: data=%q mode=%q err=%v", data, mode, err)
	}
}

func TestHasCommitUnknown(t *testing.T) {
	url, _ := remote(t)
	dir := filepath.Join(t.TempDir(), "o", "r")
	if err := Clone(context.Background(), dir, url, "", 1); err != nil {
		t.Fatal(err)
	}
	if HasCommit(context.Background(), dir, "0123456789abcdef0123456789abcdef01234567") {
		t.Error("HasCommit = true for an unknown sha")
	}
}
