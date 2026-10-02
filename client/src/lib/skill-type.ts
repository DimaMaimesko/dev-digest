/* skill-type.ts — how a skill's type looks, shared by the Skills Lab and the
   agent editor's Skills tab. */
import type { SkillType } from "@devdigest/shared";

/** Badge colours per skill type (CSS variables from the design system). */
const TYPE_COLOR: Record<SkillType, { c: string; bg: string }> = {
  rubric: { c: "var(--accent)", bg: "var(--accent-bg)" },
  convention: { c: "var(--ok)", bg: "var(--ok-bg)" },
  security: { c: "var(--crit)", bg: "var(--crit-bg)" },
  custom: { c: "var(--text-secondary)", bg: "var(--bg-hover)" },
};

/** The badge colours for a skill type (unknown → neutral). */
export function typeColor(type: SkillType): { c: string; bg: string } {
  return TYPE_COLOR[type] ?? TYPE_COLOR.custom;
}
