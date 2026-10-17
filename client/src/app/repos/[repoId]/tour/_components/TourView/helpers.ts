/** The first 7 characters of a commit sha, or "—" when absent. */
export function shortSha(sha: string | null | undefined): string {
  return sha ? sha.slice(0, 7) : "—";
}

/**
 * AC-14: the tour is stale once the repo's current indexed commit moves past
 * the sha the tour was generated from. `null` when there's nothing to
 * compare (no tour, or the repo isn't indexed at all).
 */
export function isStale(
  tourSha: string | null | undefined,
  indexSha: string | null | undefined,
): boolean {
  return !!tourSha && !!indexSha && tourSha !== indexSha;
}
