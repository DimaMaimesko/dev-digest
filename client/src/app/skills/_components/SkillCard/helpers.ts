import type { SkillType } from "@devdigest/shared";
import { TYPE_COLOR } from "./constants";

/** The badge colours for a skill type (unknown → neutral). */
export function typeColor(type: SkillType): { c: string; bg: string } {
  return TYPE_COLOR[type] ?? TYPE_COLOR.custom;
}
