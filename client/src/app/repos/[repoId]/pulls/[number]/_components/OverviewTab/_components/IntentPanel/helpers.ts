import type { IntentConfidence } from "@/lib/types";

/** Colour pair per confidence level — the badge's text still carries the
    level as a word (AC's accessibility rule: never colour alone). */
export function confidenceColor(level: IntentConfidence): { c: string; bg: string } {
  switch (level) {
    case "high":
      return { c: "var(--ok)", bg: "var(--ok-bg)" };
    case "medium":
      return { c: "var(--warn)", bg: "var(--warn-bg)" };
    case "low":
    default:
      return { c: "var(--info)", bg: "var(--info-bg)" };
  }
}
