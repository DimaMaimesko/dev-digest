package repointel

import (
	"regexp"
	"strings"
)

// The endpoint and cron heuristics of the TS server's extract.ts, line by
// line. JavaScript's \s is wider than Go's, and its (['"`])…\2 needs the
// same quote to close; each quote gets its own alternative instead.
var (
	verbCall = regexp.MustCompile(`(?i)\b(?:app|router|fastify|server|api)\.(get|post|put|patch|delete|options|head)` + jsSpace +
		`*(?:<[^>]*>)?` + jsSpace + `*\(` + jsSpace + `*(?:'([^'"` + "`" + `]+)'|"([^'"` + "`" + `]+)"|` + "`" + `([^'"` + "`" + `]+)` + "`" + `)`)
	routeObject = regexp.MustCompile(`(?i)method` + jsSpace + `*:` + jsSpace + `*['"` + "`" + `](GET|POST|PUT|PATCH|DELETE)['"` + "`" +
		`][\s\S]*?url` + jsSpace + `*:` + jsSpace + `*['"` + "`" + `]([^'"` + "`" + `]+)['"` + "`" + `]`)
	cronExpr = regexp.MustCompile(`(?i)\b(?:cron|schedule|CronJob)` + jsSpace + `*[.(]?` + jsSpace + `*\(?` + jsSpace + `*['"` + "`" +
		`]([^'"` + "`" + `]*(?:\*|\d+` + jsSpace + `+\d+)[^'"` + "`" + `]*)['"` + "`" + `]`)
	jobKind = regexp.MustCompile(`(?i)\b(?:register|enqueue)` + jsSpace + `*\(` + jsSpace + `*(?:[A-Za-z0-9_$.]+` + jsSpace + `*,` +
		jsSpace + `*)?['"` + "`" + `]([a-z][a-z0-9_]*)['"` + "`" + `]`)
	jobWords = regexp.MustCompile(`(?i)poll|index|clone|digest|cron|sync|schedule|job`)
)

// Endpoints returns the HTTP routes a file registers, such as
// "GET /repos", each once, in the order found.
func Endpoints(source string) []string {
	var out []string
	for _, line := range strings.Split(source, "\n") {
		if m := verbCall.FindStringSubmatch(line); m != nil {
			out = appendNew(out, strings.ToUpper(m[1])+" "+m[2]+m[3]+m[4])
		}
		if m := routeObject.FindStringSubmatch(line); m != nil {
			out = appendNew(out, strings.ToUpper(m[1])+" "+m[2])
		}
	}
	return out
}

// Crons returns the schedules and job kinds a file names, such as
// "*/5 * * * *" or "job:clone", each once, in the order found.
func Crons(source string) []string {
	var out []string
	for _, line := range strings.Split(source, "\n") {
		if m := cronExpr.FindStringSubmatch(line); m != nil {
			out = appendNew(out, jsTrim(m[1]))
		}
		if m := jobKind.FindStringSubmatch(line); m != nil && jobWords.MatchString(line) {
			out = appendNew(out, "job:"+m[1])
		}
	}
	return out
}

func appendNew(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}
