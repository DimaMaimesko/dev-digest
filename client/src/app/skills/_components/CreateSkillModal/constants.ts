import type { SkillType } from "@devdigest/shared";

/** Skill types, in the order the type picker lists them (SkillType in the contracts). */
export const SKILL_TYPES: readonly SkillType[] = ["rubric", "convention", "security", "custom"];

/** Type preselected for a new skill. */
export const DEFAULT_TYPE: SkillType = "custom";

/** Modal width (px). */
export const MODAL_WIDTH = 620;
