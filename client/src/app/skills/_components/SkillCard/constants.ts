import type { SkillType } from "@devdigest/shared";

/** Badge colours per skill type (CSS variables from the design system). */
export const TYPE_COLOR: Record<SkillType, { c: string; bg: string }> = {
  rubric: { c: "var(--accent)", bg: "var(--accent-bg)" },
  convention: { c: "var(--ok)", bg: "var(--ok-bg)" },
  security: { c: "var(--crit)", bg: "var(--crit-bg)" },
  custom: { c: "var(--text-secondary)", bg: "var(--bg-hover)" },
};
