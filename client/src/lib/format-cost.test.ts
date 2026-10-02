import { describe, it, expect } from "vitest";
import { exactCost, formatCost } from "./format-cost";

describe("formatCost", () => {
  it.each([
    [null, "—"],
    [undefined, "—"],
    [0, "$0.00"],
    [0.00042, "$0.0004"],
    [0.0042, "$0.0042"],
    [0.01, "$0.01"],
    [0.1234, "$0.12"],
    [3.4, "$3.40"],
  ])("formats %s as %s", (usd, want) => {
    expect(formatCost(usd)).toBe(want);
  });
});

describe("exactCost", () => {
  it("shows the unrounded value, or nothing when unknown", () => {
    expect(exactCost(0.0123456)).toBe("$0.0123456");
    expect(exactCost(null)).toBeUndefined();
  });
});
