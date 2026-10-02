import type { Skill } from "@devdigest/shared";

/** The skill as a review prompt holds it: its name as a heading, then its body. */
export function promptText(skill: Pick<Skill, "name" | "body">): string {
  return `## ${skill.name}\n${skill.body}`;
}
