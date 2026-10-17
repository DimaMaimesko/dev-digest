package tour

import (
	"path"
	"slices"
	"strings"
)

// secretDenyBasenames are exact basenames (case-insensitive) Excluded always
// rejects, regardless of directory.
var secretDenyBasenames = []string{
	".npmrc", ".pypirc", ".netrc", ".git-credentials",
}

// secretDenyPrefixes are case-insensitive basename prefixes Excluded
// rejects: id_rsa, id_rsa.pub, id_dsa, id_ecdsa, id_ed25519, and so on.
var secretDenyPrefixes = []string{
	"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519",
}

// secretDenySuffixes are case-insensitive basename suffixes Excluded
// rejects: *.pem, *.key, *.p12, *.pfx, *.jks, *.keystore, *.kdbx, *.tfvars,
// and the *.tfstate family.
var secretDenySuffixes = []string{
	".pem", ".key", ".p12", ".pfx", ".jks", ".keystore", ".kdbx", ".tfvars", ".tfstate",
}

// secretDenyContains are case-insensitive substrings of a basename that
// Excluded rejects wherever they appear.
var secretDenyContains = []string{"secret", "credential", "serviceaccount"}

// secretDenyDirs are directory names Excluded rejects anything under.
var secretDenyDirs = []string{".ssh", ".aws"}

// envAllow are the only dotenv-shaped basenames Excluded does not reject.
var envAllow = []string{".env.example", ".env.sample", ".env.template"}

// Excluded reports whether p's contents must never be read: the secret
// denylist (Q8). p's path may still appear in a rendered tree; only its
// contents are denied. internal/onboarding calls Excluded before any
// ReadBlob, and to filter TODO-scan results, so a secret file's contents
// never reach a model even by way of a line the scan matched.
func Excluded(p string) bool {
	p = normalizeSlashes(p)
	base := strings.ToLower(path.Base(p))

	if strings.HasPrefix(base, ".env") && !slices.Contains(envAllow, base) {
		return true
	}
	if slices.Contains(secretDenyBasenames, base) {
		return true
	}
	for _, pre := range secretDenyPrefixes {
		if strings.HasPrefix(base, pre) {
			return true
		}
	}
	for _, suf := range secretDenySuffixes {
		if strings.HasSuffix(base, suf) {
			return true
		}
	}
	if strings.Contains(base, ".tfstate") {
		return true
	}
	for _, needle := range secretDenyContains {
		if strings.Contains(base, needle) {
			return true
		}
	}
	for _, seg := range strings.Split(p, "/") {
		seg = strings.ToLower(seg)
		if slices.Contains(secretDenyDirs, seg) {
			return true
		}
	}
	return false
}

// readableBasenames are the exact, case-sensitive basenames Readable allows,
// beyond the glob patterns in readableGlobs.
var readableBasenames = []string{
	"Makefile", "justfile", "Procfile",
	".nvmrc", ".tool-versions",
	".env.example", ".env.sample", ".env.template",
}

// readableGlobs are shell-style (path.Match) basename patterns Readable
// allows.
var readableGlobs = []string{
	"README*", "CONTRIBUTING*", "CLAUDE.md", "AGENTS.md",
	"package.json", "go.mod", "Cargo.toml", "pyproject.toml", "requirements*.txt",
	"pom.xml", "build.gradle*",
	"Taskfile.y*ml",
	"Dockerfile*",
	"docker-compose*.y*ml", "compose*.y*ml",
}

// Readable reports whether p is on the allowlist of files internal/onboarding
// may read for the run_locally and architecture sections: at the repo root
// or in a top-level directory (depth <= 2), and matching one of the
// allowlisted names. It does not check Excluded; callers must check both.
func Readable(p string) bool {
	p = normalizeSlashes(p)
	if depth(p) > 2 {
		return false
	}
	base := path.Base(p)
	if slices.Contains(readableBasenames, base) {
		return true
	}
	for _, g := range readableGlobs {
		if ok, _ := path.Match(g, base); ok {
			return true
		}
	}
	return false
}

// depth is how many directory levels p is below the repo root: "README.md"
// is 0, "api/README.md" is 1, "api/internal/README.md" is 2.
func depth(p string) int {
	p = strings.Trim(p, "/")
	if p == "" {
		return 0
	}
	return strings.Count(p, "/")
}

func normalizeSlashes(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}
