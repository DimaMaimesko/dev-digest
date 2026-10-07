package intent

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Refs is every reference Extract found in a PR's title and description, in
// order of first appearance and de-duplicated.
//
// Docs and Issues can still be resolved (Gather attempts them). DocsUnresolved
// and IssuesUnresolved are references Extract could already tell can't be
// resolved — another host, or another repository — so Gather lists them as
// unresolved without making any reader call for them (AC-9).
type Refs struct {
	Docs             []string
	Issues           []int
	DocsUnresolved   []Unresolved
	IssuesUnresolved []Unresolved
}

// Every pattern scan tries at a position is anchored with \A, so a miss at
// that position fails immediately (RE2 only has to try matching at offset 0
// of the remaining text) instead of scanning the rest of the text looking
// for a match anywhere in it. That anchor alone isn't enough: a pattern that
// matches at offset 0 (barePathRe, crossIssueRe, mdLinkRe's "[^\]]*", …) can
// still walk all the way to the end of a long run of non-space characters
// before it decides whether it matches, so scan's per-byte loop is bounded
// by maxRefLen per position, not by the text's full length. Without that
// bound, a long token of non-space bytes (or of "[" characters, for
// mdLinkRe) makes the scan quadratic in the token's length (a 65 KB PR
// description took minutes; a 16,000-byte token took longer still).
var (
	// imageLinkRe matches a Markdown image; its target is never a reference.
	imageLinkRe = regexp.MustCompile(`\A!\[[^\]]*\]\(([^)\s]+)\)`)
	// mdLinkRe matches a (non-image) Markdown link.
	mdLinkRe = regexp.MustCompile(`\A\[[^\]]*\]\(([^)\s]+)\)`)
	// autolinkRe matches a Markdown autolink, <https://…>.
	autolinkRe = regexp.MustCompile(`\A<(https?://[^>\s]+)>`)
	// bareURLRe matches a bare http(s) URL in prose.
	bareURLRe = regexp.MustCompile(`\Ahttps?://[^\s)]+`)

	// closesRe matches "Fixes #123", "Closes #123", "Resolves #123" (any case).
	closesRe = regexp.MustCompile(`(?i)\A\b(?:fixes|closes|resolves)\s+#(\d+)\b`)
	// crossIssueRe matches "owner/repo#123" written as plain text (not a URL).
	crossIssueRe = regexp.MustCompile(`\A\b([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)#(\d+)\b`)
	// hashIssueRe matches a bare "#123".
	hashIssueRe = regexp.MustCompile(`\A#(\d+)\b`)
	// barePathRe matches a bare relative path in prose with a text-document
	// extension and a "/" (recommendation 1): a mention such as
	// "src/config.ts" is not a plan/spec reference, but "specs/x.md" is.
	barePathRe = regexp.MustCompile(`\A\b[\w.-]+(?:/[\w.-]+)+\.(?:md|mdx|txt|rst|adoc)\b`)

	// ghBlobRe matches a github.com blob URL: …/<owner>/<name>/blob/<ref>/<path>.
	ghBlobRe = regexp.MustCompile(`^(?:https?://)?(?:www\.)?github\.com/([^/\s]+)/([^/\s]+)/blob/[^/\s]+/(.+)$`)
	// ghIssueRe matches a github.com issue or pull request URL.
	ghIssueRe = regexp.MustCompile(`^(?:https?://)?(?:www\.)?github\.com/([^/\s]+)/([^/\s]+)/(?:issues|pull)/(\d+)/?$`)
)

// Extract finds every document and issue reference in title and body (the
// PR's title and description), for owner/name (the PR's own repository).
// self is the PR's own number, so a self-reference such as "#482" inside
// PR #482's own text is skipped silently: it isn't a source.
func Extract(title, body, owner, name string, self int) Refs {
	ex := &extractor{owner: owner, name: name, self: self,
		seenDocs: map[string]bool{}, seenIssues: map[int]bool{}, seenUnresolved: map[string]bool{}}
	ex.scan(title)
	ex.scan(body)
	return Refs{
		Docs:             ex.docs,
		Issues:           ex.issues,
		DocsUnresolved:   ex.docsUnresolved,
		IssuesUnresolved: ex.issuesUnresolved,
	}
}

type extractor struct {
	owner, name string
	self        int

	docs             []string
	issues           []int
	docsUnresolved   []Unresolved
	issuesUnresolved []Unresolved

	seenDocs       map[string]bool
	seenIssues     map[int]bool
	seenUnresolved map[string]bool // keyed "doc:<ref>" or "issue:<ref>"
}

// maxRefLen bounds how many bytes from each position a pattern is allowed
// to examine. A real reference (a path, an issue number, a Markdown link
// target) is always far shorter than this, so clipping to it never changes
// what scan finds; it only stops a pattern that matches at offset 0 from
// walking to the end of a long run of non-space bytes while deciding
// whether the match holds (see the doc comment above the pattern vars).
const maxRefLen = 512

// scan walks text left to right, trying each pattern in priority order at
// every position: the first one that matches there wins, and the scan
// continues right after its match. This gives Markdown link syntax priority
// over a bare "#123" or path that happens to sit inside it.
func (ex *extractor) scan(text string) {
	i := 0
	for i < len(text) {
		end := i + maxRefLen
		if end > len(text) {
			end = len(text)
		}
		rest := text[i:end]

		if loc := imageLinkRe.FindStringIndex(rest); loc != nil && loc[0] == 0 {
			i += loc[1] // an image's target is never a reference
			continue
		}
		if loc := mdLinkRe.FindStringSubmatchIndex(rest); loc != nil && loc[0] == 0 {
			ex.link(rest[loc[2]:loc[3]])
			i += loc[1]
			continue
		}
		if loc := autolinkRe.FindStringSubmatchIndex(rest); loc != nil && loc[0] == 0 {
			ex.link(rest[loc[2]:loc[3]])
			i += loc[1]
			continue
		}
		if loc := bareURLRe.FindStringIndex(rest); loc != nil && loc[0] == 0 {
			ex.link(rest[loc[0]:loc[1]])
			i += loc[1]
			continue
		}
		if loc := crossIssueRe.FindStringSubmatchIndex(rest); loc != nil && loc[0] == 0 {
			ex.crossRepoIssue(rest[loc[2]:loc[3]], rest[loc[4]:loc[5]], rest[loc[6]:loc[7]], rest[loc[0]:loc[1]])
			i += loc[1]
			continue
		}
		if loc := closesRe.FindStringSubmatchIndex(rest); loc != nil && loc[0] == 0 {
			ex.sameRepoIssue(rest[loc[2]:loc[3]])
			i += loc[1]
			continue
		}
		if loc := hashIssueRe.FindStringSubmatchIndex(rest); loc != nil && loc[0] == 0 {
			ex.sameRepoIssue(rest[loc[2]:loc[3]])
			i += loc[1]
			continue
		}
		if loc := barePathRe.FindStringIndex(rest); loc != nil && loc[0] == 0 {
			ex.link(rest[loc[0]:loc[1]])
			i += loc[1]
			continue
		}
		i++
	}
}

func (ex *extractor) addDoc(p string) {
	if !ex.seenDocs[p] {
		ex.seenDocs[p] = true
		ex.docs = append(ex.docs, p)
	}
}

func (ex *extractor) addDocUnresolved(ref, reason string) {
	key := "doc:" + ref
	if !ex.seenUnresolved[key] {
		ex.seenUnresolved[key] = true
		ex.docsUnresolved = append(ex.docsUnresolved, Unresolved{Ref: ref, Reason: reason})
	}
}

func (ex *extractor) addIssue(n int) {
	if n == ex.self {
		return // a self-reference isn't a source
	}
	if !ex.seenIssues[n] {
		ex.seenIssues[n] = true
		ex.issues = append(ex.issues, n)
	}
}

func (ex *extractor) addIssueUnresolved(ref, reason string) {
	key := "issue:" + ref
	if !ex.seenUnresolved[key] {
		ex.seenUnresolved[key] = true
		ex.issuesUnresolved = append(ex.issuesUnresolved, Unresolved{Ref: ref, Reason: reason})
	}
}

func (ex *extractor) sameRepoIssue(numStr string) {
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return
	}
	ex.addIssue(n)
}

func (ex *extractor) crossRepoIssue(owner, name, numStr, full string) {
	if strings.EqualFold(owner, ex.owner) && strings.EqualFold(name, ex.name) {
		ex.sameRepoIssue(numStr)
		return
	}
	ex.addIssueUnresolved(full, "other repository")
}

// link classifies target — a Markdown link's destination, an autolink, a
// bare URL, or a bare path found in prose — as a document or issue
// reference of the PR's own repository, another repository, or another
// host.
func (ex *extractor) link(target string) {
	target = strings.TrimSpace(target)
	if target == "" {
		return
	}
	if m := ghIssueRe.FindStringSubmatch(target); m != nil {
		n, err := strconv.Atoi(m[3])
		if err != nil {
			return
		}
		if strings.EqualFold(m[1], ex.owner) && strings.EqualFold(m[2], ex.name) {
			ex.addIssue(n)
		} else {
			ex.addIssueUnresolved(target, "other repository")
		}
		return
	}
	if m := ghBlobRe.FindStringSubmatch(target); m != nil {
		if strings.EqualFold(m[1], ex.owner) && strings.EqualFold(m[2], ex.name) {
			if p := normalizePath(m[3]); isDocPath(p) {
				ex.addDoc(p)
			}
		} else {
			ex.addDocUnresolved(target, "other repository")
		}
		return
	}
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		ex.addDocUnresolved(target, "on another host")
		return
	}
	if p := normalizePath(target); isDocPath(p) {
		ex.addDoc(p)
	}
}

// isDocPath reports whether a normalized path is a real document reference.
// A pure in-page anchor like "#testing" or a self link like "./" normalizes
// to "" or "." (path.Clean("") == "."): neither is a document, and neither
// counts as unresolved — it was never a reference to begin with.
func isDocPath(p string) bool {
	return p != "" && p != "."
}

// normalizePath cleans a repo-relative path reference so the same file
// linked as "specs/x.md", "./specs/x.md" or a blob URL at another ref
// normalizes to one path. It does not check path safety: Gather does that,
// right before any read.
func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	return path.Clean(p)
}
