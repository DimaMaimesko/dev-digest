import { describe, it, expect } from "vitest";
import type { Skill } from "@devdigest/shared";
import { filterSkills } from "./helpers";

const skill = (name: string, description: string, type: Skill["type"]): Skill => ({
  id: name,
  name,
  description,
  type,
  source: "manual",
  body: "b",
  enabled: true,
  version: 1,
  evidence_files: null,
  agent_count: 0,
  created_at: "2026-10-02T12:00:00.000Z",
});

const SKILLS = [
  skill("pr-quality-rubric", "Overall PR quality", "rubric"),
  skill("secret-leakage-gate", "Detects sk_live keys", "security"),
];

describe("filterSkills", () => {
  it.each([
    ["", ["pr-quality-rubric", "secret-leakage-gate"]],
    ["  ", ["pr-quality-rubric", "secret-leakage-gate"]],
    ["RUBRIC", ["pr-quality-rubric"]],
    ["sk_live", ["secret-leakage-gate"]],
    ["security", ["secret-leakage-gate"]],
    ["nothing", []],
  ])("%j → %j", (search, want) => {
    expect(filterSkills(SKILLS, search).map((s) => s.name)).toEqual(want);
  });
});
