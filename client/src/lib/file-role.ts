/**
 * Classifies a repo-relative file path into a reading-order role for the
 * Files changed tab (Smart Diff). Pure, deterministic, path-only: no I/O,
 * no network, no model call (spec "Classification rules", AC-2).
 *
 * Rules are fixed linear predicates, checked top to bottom, first match
 * wins. Every pattern is static (never built from PR data) and every check
 * below runs in time linear in the path length, with no nested quantifiers,
 * per spec "Non-functional / Security".
 */

export type FileRole = "core" | "tests" | "wiring" | "docs" | "boilerplate";

/** Display / grouping order for the Files changed tab (AC-1). */
export const ROLE_ORDER: FileRole[] = ["core", "tests", "wiring", "docs", "boilerplate"];

/** Escapes everything but `*`, so a literal pattern segment is matched exactly. */
function escapeRegexExceptStar(s: string): string {
  return s.replace(/[.+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * Matches a single path segment (no `/`) against a glob pattern where `*`
 * matches zero or more characters within that segment. Case-sensitive.
 */
function matchesSegmentGlob(segment: string, pattern: string): boolean {
  const escaped = pattern.split("*").map(escapeRegexExceptStar).join("[^/]*");
  return new RegExp(`^${escaped}$`).test(segment);
}

/** `*.config.*` / `*.generated.*`: text is required both before and after the infix. */
function matchesRequiredInfix(basename: string, infix: string): boolean {
  const escapedInfix = escapeRegexExceptStar(infix);
  return new RegExp(`^.+${escapedInfix}.+$`).test(basename);
}

type Matcher = (path: string, basename: string, segments: string[]) => boolean;

/** A pattern without `/`: matches the basename at any depth. */
function basenamePattern(pattern: string): Matcher {
  return (_path, basename) => matchesSegmentGlob(basename, pattern);
}

/** `*.config.*` / `*.generated.*`: basename infix match, non-empty on both sides. */
function requiredInfixPattern(infix: string): Matcher {
  return (_path, basename) => matchesRequiredInfix(basename, infix);
}

/** `x/**`: root-anchored directory match (this repo's assumption: root only, no deeper nesting). */
function rootAnchoredDirPattern(prefix: string): Matcher {
  const withSlash = prefix.endsWith("/") ? prefix : `${prefix}/`;
  return (path) => path.startsWith(withSlash);
}

/** `**\/x/**`: matches any non-final path segment named exactly `x`. */
function anyDepthDirPattern(name: string): Matcher {
  return (_path, _basename, segments) => segments.slice(0, -1).includes(name);
}

interface Rule {
  role: FileRole;
  matchers: Matcher[];
}

const RULES: Rule[] = [
  {
    role: "boilerplate",
    matchers: [
      basenamePattern("*.lock"),
      basenamePattern("pnpm-lock.yaml"),
      basenamePattern("package-lock.json"),
      basenamePattern("yarn.lock"),
      basenamePattern("go.sum"),
      rootAnchoredDirPattern("dist/"),
      rootAnchoredDirPattern("build/"),
      anyDepthDirPattern("__snapshots__"),
      basenamePattern("*.snap"),
      requiredInfixPattern(".generated."),
      basenamePattern("*.min.js"),
    ],
  },
  {
    role: "tests",
    matchers: [
      basenamePattern("*.test.ts"),
      basenamePattern("*.test.tsx"),
      basenamePattern("*.it.test.ts"),
      basenamePattern("*.spec.ts"),
      basenamePattern("*_test.go"),
      anyDepthDirPattern("test"),
      anyDepthDirPattern("tests"),
      anyDepthDirPattern("__tests__"),
      rootAnchoredDirPattern("e2e/"),
    ],
  },
  {
    role: "wiring",
    matchers: [
      basenamePattern("index.ts"),
      basenamePattern("index.js"),
      requiredInfixPattern(".config."),
      basenamePattern("tsconfig*.json"),
      basenamePattern(".eslintrc*"),
      basenamePattern(".env*"),
      basenamePattern("docker-compose*.yml"),
      rootAnchoredDirPattern(".github/"),
      rootAnchoredDirPattern(".claude/"),
    ],
  },
  {
    role: "docs",
    matchers: [
      basenamePattern("*.md"),
      rootAnchoredDirPattern("docs/"),
      basenamePattern("README*"),
      basenamePattern("CHANGELOG*"),
      basenamePattern("LICENSE"),
    ],
  },
];

/**
 * Classifies a repo-relative path (using `/` as the separator) into a
 * `FileRole`, applying the Classification rules top to bottom with the
 * first match winning, and `"core"` otherwise.
 */
export function classifyPath(path: string): FileRole {
  const segments = path.split("/");
  const basename = segments[segments.length - 1] ?? path;
  for (const rule of RULES) {
    for (const matcher of rule.matchers) {
      if (matcher(path, basename, segments)) return rule.role;
    }
  }
  return "core";
}
