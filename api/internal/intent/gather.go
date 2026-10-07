package intent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"
)

// Sentinel errors a FileReader returns. They let Gather report why a
// plan/spec link couldn't be read, without this package knowing anything
// about git or clones.
var (
	// ErrNoClone means the PR's repository has no local clone yet.
	ErrNoClone = errors.New("intent: repository not cloned")
	// ErrNoCommit means the head commit a link should be read at isn't
	// available, even after the FileReader tried to fetch it.
	ErrNoCommit = errors.New("intent: head commit not available")
	// ErrNotFound means path doesn't exist at the commit asked about.
	ErrNotFound = errors.New("intent: path not found")
)

// Sentinel errors an IssueReader returns, so Gather can report why a
// referenced issue couldn't be fetched without this package knowing
// anything about GitHub's API.
var (
	// ErrIssueNotFound means the issue doesn't exist (or isn't visible).
	ErrIssueNotFound = errors.New("intent: issue not found")
	// ErrRateLimited means GitHub rate-limited the request.
	ErrRateLimited = errors.New("intent: rate limited")
)

// FileReader reads a file from the PR's repository at its head commit.
// internal/runner implements it over internal/git.
type FileReader interface {
	// Read returns path's content as it is at the commit FileReader was
	// built for. symlink reports that data is the symlink's target path,
	// not file content, as git stores it for a tree entry with mode
	// "120000". Read returns ErrNoClone, ErrNoCommit or ErrNotFound as
	// appropriate; any other error is reported as the GitHub/git adapter's
	// own message.
	Read(ctx context.Context, path string) (data []byte, symlink bool, err error)
}

// IssueReader fetches a GitHub issue of the PR's own repository.
// internal/runner implements it over internal/github. A nil IssueReader
// means no GitHub token is configured.
type IssueReader interface {
	// Issue returns the title and body of issue number n. It returns
	// ErrIssueNotFound or ErrRateLimited as appropriate; any other error is
	// reported as "GitHub error: <message>".
	Issue(ctx context.Context, n int) (title, body string, err error)
}

// FileStat is a file a pull request changed, with its line counts.
type FileStat struct {
	Path      string
	Additions int
	Deletions int
}

// GatherInput is everything about a pull request Gather needs to collect
// its sources.
type GatherInput struct {
	Title       string
	Description string
	Owner       string // the PR's repository owner
	Name        string // the PR's repository name
	Self        int    // the PR's own number, so a self-reference is skipped
	Branch      string
	Commits     []string // commit subject lines (first line only), oldest or newest first
	Files       []FileStat
}

// Content is one source's text, ready to go into the intent model's prompt,
// wrapped as untrusted data by Derive.
type Content struct {
	Label string // "pr-title", "pr-description", "issue-#N", "doc:<path>", "branch", "commits", "changed-files"
	Text  string
}

// Sources is what Gather collected for a pull request: the content Derive
// sends to the model, the metadata Derive (on success) stores on the
// resulting Intent, and the confidence that metadata earns by rule (AC-15).
type Sources struct {
	Content    []Content
	Found      []Source
	Unresolved []Unresolved
	Confidence Level
}

// Caps on what Gather reads, so a PR's prompt to the intent model stays
// bounded regardless of how much text or how many links it contains.
const (
	maxDocs         = 5
	maxDocRunes     = 20_000
	maxIssues       = 3
	maxIssueRunes   = 4_000
	maxDescRunes    = 4_000
	maxCommitLines  = 50
	textCheckLength = 8_000 // bytes checked for NUL / UTF-8 validity
)

// Gather collects a pull request's sources: its title and description, the
// plan/spec documents and issues its text links to (read through files and
// issues), and its branch, commit subjects and changed files. Every cap is
// applied here (AC-10, AC-11, AC-13).
func Gather(ctx context.Context, in GatherInput, files FileReader, issues IssueReader) Sources {
	// Extract's scan is bounded by maxRefLen per position, not by the
	// text's full length (no pattern is unanchored, and none is allowed to
	// examine more than maxRefLen bytes from a given position), but clip
	// the raw description anyway: nothing past a few times maxDescRunes
	// can add a reference Gather would ever read.
	extractDesc, _ := clipRunes(in.Description, maxDescRunes*4)
	refs := Extract(in.Title, extractDesc, in.Owner, in.Name, in.Self)

	var (
		content             []Content
		found               []Source
		unresolved          []Unresolved
		documentOrIssueRead bool
		documentUnresolved  bool
	)

	if !blank(in.Title) {
		found = append(found, Source{Kind: KindTitle, Label: "pr-title"})
		content = append(content, Content{Label: "pr-title", Text: in.Title})
	}
	if !blank(in.Description) {
		desc, _ := clipRunes(in.Description, maxDescRunes)
		found = append(found, Source{Kind: KindDescription, Label: "pr-description"})
		content = append(content, Content{Label: "pr-description", Text: desc})
	}
	if !blank(in.Branch) {
		found = append(found, Source{Kind: KindBranch, Label: "branch", Ref: in.Branch})
		content = append(content, Content{Label: "branch", Text: in.Branch})
	}
	if len(in.Commits) > 0 {
		subjects := in.Commits
		if len(subjects) > maxCommitLines {
			subjects = subjects[:maxCommitLines]
		}
		found = append(found, Source{Kind: KindCommits, Label: "commits"})
		content = append(content, Content{Label: "commits", Text: strings.Join(subjects, "\n")})
	}
	if len(in.Files) > 0 {
		lines := make([]string, len(in.Files))
		for i, f := range in.Files {
			lines[i] = fmt.Sprintf("%s (+%d -%d)", f.Path, f.Additions, f.Deletions)
		}
		found = append(found, Source{Kind: KindFiles, Label: "changed-files"})
		content = append(content, Content{Label: "changed-files", Text: strings.Join(lines, "\n")})
	}

	// Refs Extract already knows can't be resolved (another host, another
	// repository): no reader call is made for them (AC-9).
	if len(refs.DocsUnresolved) > 0 {
		documentUnresolved = true
	}
	unresolved = append(unresolved, refs.DocsUnresolved...)

	for i, p := range refs.Docs {
		if i >= maxDocs {
			unresolved = append(unresolved, Unresolved{Ref: p, Reason: "limit reached"})
			documentUnresolved = true
			continue
		}
		data, reason := readDoc(ctx, files, p)
		if reason != "" {
			unresolved = append(unresolved, Unresolved{Ref: p, Reason: reason})
			documentUnresolved = true
			continue
		}
		documentOrIssueRead = true
		text, truncated := clipRunes(string(data), maxDocRunes)
		content = append(content, Content{Label: "doc:" + p, Text: text})
		found = append(found, Source{Kind: KindDocument, Label: "doc:" + p, Ref: p, Truncated: truncated})
	}

	unresolved = append(unresolved, refs.IssuesUnresolved...)

	for i, n := range refs.Issues {
		ref := fmt.Sprintf("#%d", n)
		switch {
		case i >= maxIssues:
			unresolved = append(unresolved, Unresolved{Ref: ref, Reason: "limit reached"})
		case issues == nil:
			unresolved = append(unresolved, Unresolved{Ref: ref, Reason: "no token"})
		default:
			title, body, err := issues.Issue(ctx, n)
			if err != nil {
				unresolved = append(unresolved, Unresolved{Ref: ref, Reason: issueErrReason(err)})
				continue
			}
			documentOrIssueRead = true
			body, _ = clipRunes(body, maxIssueRunes)
			content = append(content, Content{Label: "issue-" + ref, Text: "Title: " + title + "\n\n" + body})
			found = append(found, Source{Kind: KindIssue, Label: "issue-" + ref, Ref: ref})
		}
	}

	return Sources{
		Content:    content,
		Found:      found,
		Unresolved: unresolved,
		Confidence: Confidence(in.Description, documentOrIssueRead, documentUnresolved),
	}
}

// safePath reports whether p is safe to read from the repository: not
// absolute, no ".." segment once cleaned, no backslash and no NUL byte.
func safePath(p string) bool {
	if p == "" || strings.ContainsRune(p, 0) || strings.ContainsRune(p, '\\') {
		return false
	}
	if path.IsAbs(p) {
		return false
	}
	clean := path.Clean(p)
	return clean != ".." && !strings.HasPrefix(clean, "../")
}

// readDoc reads p from files, resolving one symlink hop (Clarification 6).
// It returns the content, or a reason it couldn't be read.
func readDoc(ctx context.Context, files FileReader, p string) (data []byte, reason string) {
	if !safePath(p) {
		return nil, "outside repository"
	}
	data, symlink, err := files.Read(ctx, p)
	if err != nil {
		return nil, readErrReason(err)
	}
	if symlink {
		target := strings.TrimSpace(string(data))
		if path.IsAbs(target) {
			return nil, "outside repository"
		}
		resolved := path.Clean(path.Join(path.Dir(p), target))
		if !safePath(resolved) {
			return nil, "outside repository"
		}
		data, symlink, err = files.Read(ctx, resolved)
		if err != nil {
			return nil, readErrReason(err)
		}
		if symlink {
			return nil, "not a text file" // a second hop isn't followed
		}
	}
	if !isText(data) {
		return nil, "not a text file"
	}
	return data, ""
}

func readErrReason(err error) string {
	switch {
	case errors.Is(err, ErrNoClone):
		return "repository not cloned"
	case errors.Is(err, ErrNoCommit):
		return "head commit not available"
	case errors.Is(err, ErrNotFound):
		return "missing at head commit"
	default:
		return err.Error()
	}
}

func issueErrReason(err error) string {
	switch {
	case errors.Is(err, ErrIssueNotFound):
		return "not found"
	case errors.Is(err, ErrRateLimited):
		return "rate limited"
	default:
		return "GitHub error: " + err.Error()
	}
}

// isText reports whether data looks like text: no NUL byte and valid UTF-8
// within the first textCheckLength bytes.
func isText(data []byte) bool {
	check := data
	if len(check) > textCheckLength {
		check = trimIncompleteRune(check[:textCheckLength])
	}
	if bytes.IndexByte(check, 0) >= 0 {
		return false
	}
	return utf8.Valid(check)
}

// trimIncompleteRune drops a trailing rune that b's cut point split in the
// middle, so a byte-length cap doesn't turn valid multi-byte text (Cyrillic,
// CJK, emoji) into something utf8.Valid rejects. It looks back at most
// utf8.UTFMax bytes for the start of the last rune and drops it only if it
// isn't fully present in b.
func trimIncompleteRune(b []byte) []byte {
	limit := len(b) - utf8.UTFMax
	for i := len(b) - 1; i >= 0 && i > limit; i-- {
		if utf8.RuneStart(b[i]) {
			if !utf8.FullRune(b[i:]) {
				return b[:i]
			}
			break
		}
	}
	return b
}

// clipRunes returns the first n runes of s, and whether it had more than n.
func clipRunes(s string, n int) (string, bool) {
	if utf8.RuneCountInString(s) <= n {
		return s, false
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i], true
		}
		count++
	}
	return s, false
}

func blank(s string) bool {
	return strings.TrimSpace(s) == ""
}
