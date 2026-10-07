import { describe, it, expect } from "vitest";
import { classifyPath, ROLE_ORDER, type FileRole } from "./file-role";

/** Spec "Pinned classification cases" (specs/smart-diff.md), reproduced verbatim. */
const PINNED_CASES: [string, FileRole][] = [
  ["src/__tests__/__snapshots__/x.snap", "boilerplate"],
  [".claude/skills/security/SKILL.md", "wiring"],
  ["e2e/README.md", "tests"],
  ["pnpm-lock.yaml", "boilerplate"],
  ["client/pnpm-lock.yaml", "boilerplate"],
  ["package-lock.json", "boilerplate"],
  ["go.sum", "boilerplate"],
  ["api/go.sum", "boilerplate"],
  ["dist/app.min.js", "boilerplate"],
  ["src/api/x.generated.ts", "boilerplate"],
  ["src/foo.test.tsx", "tests"],
  ["api/internal/review/run_test.go", "tests"],
  ["tests/README.md", "tests"],
  ["src/__tests__/index.ts", "tests"],
  ["e2e/specs/05-pr-diff.flow.json", "tests"],
  ["src/api/public/index.ts", "wiring"],
  ["vite.config.ts", "wiring"],
  ["tsconfig.build.json", "wiring"],
  [".env.example", "wiring"],
  [".github/workflows/api.yml", "wiring"],
  ["docs/index.ts", "wiring"],
  ["docs/guide.md", "docs"],
  ["README.md", "docs"],
  ["LICENSE", "docs"],
  ["src/middleware/ratelimit.ts", "core"],
  ["src/config.ts", "core"],
  ["src/server.ts", "core"],
  ["package.json", "core"],
  ["go.mod", "core"],
  ["api/internal/foo/foo.go", "core"],
];

describe("classifyPath", () => {
  it.each(PINNED_CASES)("classifies %s as %s", (path, role) => {
    expect(classifyPath(path)).toBe(role);
  });

  it("covers every pinned row from the spec", () => {
    expect(PINNED_CASES).toHaveLength(30);
  });

  it("is deterministic: the same path always gives the same role", () => {
    for (const [path] of PINNED_CASES) {
      const first = classifyPath(path);
      const second = classifyPath(path);
      expect(second).toBe(first);
    }
  });
});

describe("ROLE_ORDER", () => {
  it("lists every role in reading order", () => {
    expect(ROLE_ORDER).toEqual(["core", "tests", "wiring", "docs", "boilerplate"]);
  });
});
