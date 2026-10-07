package intent

import (
	"regexp"
	"unicode/utf8"
)

var (
	htmlCommentRe  = regexp.MustCompile(`(?s)<!--.*?-->`)
	headingLineRe  = regexp.MustCompile(`(?m)^[ \t]*#{1,6}[ \t].*$`)
	checkboxLineRe = regexp.MustCompile(`(?m)^[ \t]*[-*][ \t]*\[ \].*$`)
	whitespaceRe   = regexp.MustCompile(`\s+`)
)

// Trivial reports whether desc is too thin to be the PR's documented source
// (AC-14, recommendation 5): fewer than 50 runes remain after removing HTML
// comments, heading-only lines, unchecked-checkbox lines, and all
// whitespace.
func Trivial(desc string) bool {
	s := htmlCommentRe.ReplaceAllString(desc, "")
	s = headingLineRe.ReplaceAllString(s, "")
	s = checkboxLineRe.ReplaceAllString(s, "")
	s = whitespaceRe.ReplaceAllString(s, "")
	return utf8.RuneCountInString(s) < 50
}

// Confidence sets an intent's confidence level by rule (AC-15), never by the
// model's own claim: "high" when a linked plan/spec document or issue was
// read and no plan/spec link is unresolved; "medium" when the description is
// non-trivial but that doesn't hold; "low" otherwise.
func Confidence(desc string, documentOrIssueRead, documentUnresolved bool) Level {
	switch {
	case documentOrIssueRead && !documentUnresolved:
		return LevelHigh
	case !Trivial(desc):
		return LevelMedium
	default:
		return LevelLow
	}
}
