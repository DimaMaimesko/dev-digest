import { describe, it, expect } from "vitest";
import type { FindingRecord } from "@devdigest/shared";
import { parsePatch } from "./helpers";
import { isOpen, pathsWithOpenFindings, placeFindings } from "./findings";

function finding(overrides: Partial<FindingRecord>): FindingRecord {
  return {
    id: "f1",
    severity: "CRITICAL",
    category: "security",
    title: "A finding",
    file: "src/config.ts",
    start_line: 1,
    end_line: 1,
    rationale: "rationale",
    suggestion: null,
    confidence: 0.9,
    kind: "finding",
    trifecta_components: null,
    evidence: null,
    review_id: "r1",
    accepted_at: null,
    dismissed_at: null,
    ...overrides,
  };
}

// @@ -1,2 +1,4 @@
//  const a = 1;      ctx  old=1 new=1
// -const b = 2;       del  old=2
// +const b = 3;       add       new=2
// +const c = 4;       add       new=3
//  const d = 5;       ctx  old=3 new=4
const PATCH =
  "@@ -1,2 +1,4 @@\n const a = 1;\n-const b = 2;\n+const b = 3;\n+const c = 4;\n const d = 5;";

describe("isOpen", () => {
  it("is true only when neither accepted nor dismissed", () => {
    expect(isOpen(finding({}))).toBe(true);
    expect(isOpen(finding({ accepted_at: "2026-01-01T00:00:00Z" }))).toBe(false);
    expect(isOpen(finding({ dismissed_at: "2026-01-01T00:00:00Z" }))).toBe(false);
  });
});

describe("pathsWithOpenFindings", () => {
  it("only includes paths of open findings", () => {
    const findings = [
      finding({ file: "a.ts" }),
      finding({ file: "b.ts", dismissed_at: "2026-01-01T00:00:00Z" }),
    ];
    expect(pathsWithOpenFindings(findings)).toEqual(new Set(["a.ts"]));
  });
});

describe("placeFindings", () => {
  it("anchors to the line whose newNo equals end_line", () => {
    const lines = parsePatch(PATCH);
    const f = finding({ start_line: 2, end_line: 2 });
    const { byLine, notInDiff } = placeFindings(lines, [f]);
    expect(notInDiff).toEqual([]);
    const anchoredIndex = lines.findIndex((l) => l.kind === "add" && l.newNo === 2);
    expect(byLine.get(anchoredIndex)).toEqual([f]);
  });

  it("falls back to the last rendered line in range when end_line isn't rendered", () => {
    const lines = parsePatch(PATCH);
    // range 1..3 covers new lines 1 (ctx), 2 (add), 3 (add); end_line itself (3)
    // IS rendered here, so use a range whose end isn't a candidate: 1..10.
    const f = finding({ start_line: 1, end_line: 10 });
    const { byLine, notInDiff } = placeFindings(lines, [f]);
    expect(notInDiff).toEqual([]);
    // last candidate (add/ctx with newNo) in [1,10] is "const d = 5;" (new=4)
    const lastIndex = lines.findIndex((l) => l.text === "const d = 5;");
    expect(byLine.get(lastIndex)).toEqual([f]);
  });

  it("never anchors to a deleted line", () => {
    const lines = parsePatch(PATCH);
    const delIndex = lines.findIndex((l) => l.kind === "del");
    expect(delIndex).toBeGreaterThanOrEqual(0);
    // start_line/end_line match the deleted line's old number (2), which has
    // no newNo, so it can never be a candidate.
    const f = finding({ start_line: 2, end_line: 2 });
    const { byLine } = placeFindings(lines, [f]);
    expect(byLine.get(delIndex)).toBeUndefined();
  });

  it("sends a finding on a no-patch file to notInDiff", () => {
    const lines = parsePatch(null);
    const f = finding({});
    const { byLine, notInDiff } = placeFindings(lines, [f]);
    expect(byLine.size).toBe(0);
    expect(notInDiff).toEqual([f]);
  });

  it("stacks findings anchored to the same line in severity order and uses the highest severity", () => {
    const lines = parsePatch(PATCH);
    const anchorIndex = lines.findIndex((l) => l.kind === "add" && l.newNo === 2);
    const warning = finding({ id: "w1", severity: "WARNING", start_line: 2, end_line: 2 });
    const critical = finding({ id: "c1", severity: "CRITICAL", start_line: 2, end_line: 2 });
    const suggestion = finding({ id: "s1", severity: "SUGGESTION", start_line: 2, end_line: 2 });
    const { byLine, stripe, label } = placeFindings(lines, [warning, critical, suggestion]);
    expect(byLine.get(anchorIndex)?.map((f) => f.id)).toEqual(["c1", "w1", "s1"]);
    expect(stripe.get(anchorIndex)).toBe("CRITICAL");
    expect(label.get(anchorIndex)).toBe("CRITICAL");
  });

  it("does not stripe or label a line whose only finding is triaged", () => {
    const lines = parsePatch(PATCH);
    const anchorIndex = lines.findIndex((l) => l.kind === "add" && l.newNo === 2);
    const triaged = finding({
      severity: "CRITICAL",
      start_line: 2,
      end_line: 2,
      dismissed_at: "2026-01-01T00:00:00Z",
    });
    const { byLine, stripe, label } = placeFindings(lines, [triaged]);
    // still rendered inline (AC-30), just not striped/labeled.
    expect(byLine.get(anchorIndex)).toEqual([triaged]);
    expect(stripe.get(anchorIndex)).toBeUndefined();
    expect(label.get(anchorIndex)).toBeUndefined();
  });
});
