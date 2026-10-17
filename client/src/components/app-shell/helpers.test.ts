import { describe, it, expect } from "vitest";
import { activeKeyFor } from "./helpers";

describe("activeKeyFor", () => {
  it.each([
    ["/onboarding", ""],
    ["/repos/x/tour", "onboarding-tour"],
    ["/repos/x/pulls", "pulls"],
    ["/repos/x/pulls/482", "pulls"],
  ])("maps %s to %s", (pathname, want) => {
    expect(activeKeyFor(pathname)).toBe(want);
  });
});
