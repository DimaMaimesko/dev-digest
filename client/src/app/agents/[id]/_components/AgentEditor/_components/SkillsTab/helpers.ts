import type { Skill } from "@devdigest/shared";

/** The agent's linked skills in its order, then the rest in list order. A
 *  linked ID with no skill (deleted meanwhile) is skipped. */
export function arrange(skills: Skill[], linked: string[]): { linked: Skill[]; unlinked: Skill[] } {
  const byId = new Map(skills.map((sk) => [sk.id, sk]));
  return {
    linked: linked.flatMap((id) => byId.get(id) ?? []),
    unlinked: skills.filter((sk) => !linked.includes(sk.id)),
  };
}

/** Links `id` last, or unlinks it when linked. */
export function toggle(ids: string[], id: string): string[] {
  return ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id];
}

/** Moves the ID at `from` to index `to`. Out-of-range moves change nothing. */
export function move(ids: string[], from: number, to: number): string[] {
  if (from === to || from < 0 || to < 0 || from >= ids.length || to >= ids.length) return ids;
  const next = [...ids];
  const [id] = next.splice(from, 1);
  next.splice(to, 0, id!);
  return next;
}

/** Case-insensitive match over a skill's name, description and type. */
export function matches(skill: Skill, search: string): boolean {
  const q = search.trim().toLowerCase();
  return !q || `${skill.name} ${skill.description} ${skill.type}`.toLowerCase().includes(q);
}
