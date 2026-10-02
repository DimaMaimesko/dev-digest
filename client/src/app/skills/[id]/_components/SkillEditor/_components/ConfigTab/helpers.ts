import { CHARS_PER_TOKEN } from "./constants";

/** A rough token count for a text: enough to see what a skill costs per review. */
export function estimateTokens(text: string): number {
  return Math.ceil(text.length / CHARS_PER_TOKEN);
}
