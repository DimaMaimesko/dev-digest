import { describe, it, expect } from "vitest";
import type { FindingRecord, PrFile, ReviewRecord } from "@devdigest/shared";
import { groupByRole, latestReviewPerAgent, nextTabAfterRunStart, shownFindings } from "./helpers";

function finding(overrides: Partial<FindingRecord>): FindingRecord {
  return {
    id: "f1",
    severity: "CRITICAL",
    category: "security",
    title: "A finding",
    file: "a.ts",
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

function review(overrides: Partial<ReviewRecord>): ReviewRecord {
  return {
    id: "r1",
    pr_id: "pr1",
    agent_id: "agent-1",
    run_id: null,
    agent_name: "Agent One",
    kind: "review",
    verdict: null,
    summary: null,
    score: null,
    model: null,
    grounding: null,
    created_at: "2026-01-01T00:00:00Z",
    findings: [],
    ...overrides,
  };
}

const FILES: PrFile[] = [{ path: "a.ts", additions: 1, deletions: 0, patch: null }];

describe("latestReviewPerAgent", () => {
  it("keeps only the newest review per agent, an older review's findings don't show", () => {
    const older = review({
      id: "r1",
      created_at: "2026-01-01T00:00:00Z",
      findings: [finding({ id: "f1", file: "a.ts", review_id: "r1" })],
    });
    const newer = review({
      id: "r2",
      created_at: "2026-01-02T00:00:00Z",
      findings: [],
    });
    const latest = latestReviewPerAgent([older, newer]);
    expect(latest).toHaveLength(1);
    expect(latest[0]!.id).toBe("r2");
    expect(shownFindings([older, newer], FILES)).toHaveLength(0);
  });

  it("buckets all null-agent_id reviews together, keeping only the newest", () => {
    const a = review({ id: "r1", agent_id: null, created_at: "2026-01-01T00:00:00Z" });
    const b = review({ id: "r2", agent_id: null, created_at: "2026-01-02T00:00:00Z" });
    const other = review({ id: "r3", agent_id: "agent-2", created_at: "2026-01-01T12:00:00Z" });
    const latest = latestReviewPerAgent([a, b, other]);
    expect(latest.map((r) => r.id).sort()).toEqual(["r2", "r3"]);
  });

  it("leaves other agents' latest reviews untouched", () => {
    const agent1Old = review({ id: "r1", agent_id: "agent-1", created_at: "2026-01-01T00:00:00Z" });
    const agent1New = review({ id: "r2", agent_id: "agent-1", created_at: "2026-01-02T00:00:00Z" });
    const agent2 = review({ id: "r3", agent_id: "agent-2", created_at: "2026-01-01T00:00:00Z" });
    const latest = latestReviewPerAgent([agent1Old, agent1New, agent2]);
    expect(latest.map((r) => r.id).sort()).toEqual(["r2", "r3"]);
  });

  it("breaks a tie in created_at by the greater id, deterministically", () => {
    const a = review({ id: "r1", created_at: "2026-01-01T00:00:00Z" });
    const b = review({ id: "r2", created_at: "2026-01-01T00:00:00Z" });
    expect(latestReviewPerAgent([a, b])[0]!.id).toBe("r2");
    expect(latestReviewPerAgent([b, a])[0]!.id).toBe("r2");
  });
});

describe("shownFindings", () => {
  it("hides findings on files that aren't listed", () => {
    const r = review({ findings: [finding({ file: "not-listed.ts" })] });
    expect(shownFindings([r], FILES)).toHaveLength(0);
  });

  it("keeps findings on listed files from the latest review", () => {
    const r = review({ findings: [finding({ file: "a.ts" })] });
    expect(shownFindings([r], FILES)).toHaveLength(1);
  });
});

describe("groupByRole", () => {
  it("groups files in ROLE_ORDER, keeping Original order within a group, dropping empty groups", () => {
    const files: PrFile[] = [
      { path: "src/server.ts", additions: 1, deletions: 0, patch: null }, // core
      { path: "src/a.test.ts", additions: 1, deletions: 0, patch: null }, // tests
      { path: "src/b.test.ts", additions: 1, deletions: 0, patch: null }, // tests
      { path: "README.md", additions: 1, deletions: 0, patch: null }, // docs
    ];
    const groups = groupByRole(files);
    expect(groups.map((g) => g.role)).toEqual(["core", "tests", "docs"]);
    expect(groups.find((g) => g.role === "tests")!.files.map((f) => f.path)).toEqual([
      "src/a.test.ts",
      "src/b.test.ts",
    ]);
  });
});

describe("nextTabAfterRunStart", () => {
  it("stays on diff when the run started there", () => {
    expect(nextTabAfterRunStart("diff")).toBe("diff");
  });

  it("switches to findings from any other tab", () => {
    expect(nextTabAfterRunStart("overview")).toBe("findings");
    expect(nextTabAfterRunStart("findings")).toBe("findings");
  });
});
