import { describe, it, expect } from "vitest";
import type { Skill } from "@devdigest/shared";
import { arrange, move, toggle } from "./helpers";

const skill = (id: string): Skill => ({
  id,
  name: id,
  description: "",
  type: "custom",
  source: "manual",
  body: "b",
  enabled: true,
  version: 1,
  evidence_files: null,
  agent_count: 0,
  created_at: "2026-10-02T12:00:00.000Z",
});

describe("arrange", () => {
  it("puts linked skills first in the agent's order, then the rest in list order", () => {
    const { linked, unlinked } = arrange([skill("a"), skill("b"), skill("c"), skill("d")], ["c", "a"]);
    expect(linked.map((s) => s.id)).toEqual(["c", "a"]);
    expect(unlinked.map((s) => s.id)).toEqual(["b", "d"]);
  });

  it("skips a linked ID whose skill is gone", () => {
    expect(arrange([skill("a")], ["gone", "a"]).linked.map((s) => s.id)).toEqual(["a"]);
  });
});

describe("toggle", () => {
  it.each([
    [["a"], "b", ["a", "b"]],
    [["a", "b"], "a", ["b"]],
    [[], "a", ["a"]],
  ])("%j toggling %s → %j", (ids, id, want) => {
    expect(toggle(ids, id)).toEqual(want);
  });
});

describe("move", () => {
  it.each([
    [0, 2, ["b", "c", "a"]],
    [2, 0, ["c", "a", "b"]],
    [1, 1, ["a", "b", "c"]],
    [0, -1, ["a", "b", "c"]],
    [2, 3, ["a", "b", "c"]],
  ])("from %i to %i → %j", (from, to, want) => {
    expect(move(["a", "b", "c"], from, to)).toEqual(want);
  });
});
