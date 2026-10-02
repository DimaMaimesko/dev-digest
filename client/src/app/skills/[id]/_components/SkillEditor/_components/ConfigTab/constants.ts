import type { SkillType } from "@devdigest/shared";

/** Skill types, in the order the type picker lists them (SkillType in the contracts). */
export const SKILL_TYPES: readonly SkillType[] = ["rubric", "convention", "security", "custom"];

/** Rough characters per token, for the body's size hint. */
export const CHARS_PER_TOKEN = 4;
