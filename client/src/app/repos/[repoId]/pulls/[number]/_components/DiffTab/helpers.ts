/* Pure helpers for the Files changed tab (Smart Diff).
   - "Shown findings" come from each agent's *latest* review on the PR, with
     a null `agent_id` treated as one shared bucket (user decision, plan
     "Clarifications & recommendations"). Only findings whose `file` equals a
     listed file's path are kept (spec "Which findings the tab shows").
   - Grouping buckets listed files by `classifyPath`, keeping each group in
     the API's relative (Original) order (AC-3), and drops empty groups
     (AC-5). */
import type { FindingRecord, PrFile, ReviewRecord } from "@devdigest/shared";
import { ROLE_ORDER, classifyPath, type FileRole } from "@/lib/file-role";

/** One role group: its role and the listed files assigned to it, in order. */
export interface RoleFiles {
  role: FileRole;
  files: PrFile[];
}

/** AC-7: docs and boilerplate start collapsed; core, tests, wiring start open. */
export const DEFAULT_COLLAPSED: Record<FileRole, boolean> = {
  core: false,
  tests: false,
  wiring: false,
  docs: true,
  boilerplate: true,
};

/** Key used to bucket a review by its agent — a null `agent_id` is one
    shared bucket, distinct from any real agent id. */
function agentBucketKey(review: ReviewRecord): string {
  return review.agent_id ?? "__no_agent__";
}

/**
 * Keeps, per agent bucket (null `agent_id` included, as one bucket), only the
 * most recently created review. Ties are broken by the greater `id` so the
 * pick is deterministic (AC-27, AC-28).
 */
export function latestReviewPerAgent(reviews: ReviewRecord[]): ReviewRecord[] {
  const latest = new Map<string, ReviewRecord>();
  for (const review of reviews) {
    const key = agentBucketKey(review);
    const current = latest.get(key);
    if (!current) {
      latest.set(key, review);
      continue;
    }
    const isNewer =
      review.created_at > current.created_at ||
      (review.created_at === current.created_at && review.id > current.id);
    if (isNewer) latest.set(key, review);
  }
  return [...latest.values()];
}

/** The findings the Files changed tab shows: each agent's latest review's
    findings, restricted to the listed files (spec "Which findings the tab
    shows"; findings on files not listed are hidden silently, assumption 11). */
export function shownFindings(reviews: ReviewRecord[], files: PrFile[]): FindingRecord[] {
  const listedPaths = new Set(files.map((f) => f.path));
  return latestReviewPerAgent(reviews)
    .flatMap((review) => review.findings)
    .filter((f) => listedPaths.has(f.file));
}

/** Groups listed files by role, in `ROLE_ORDER`, keeping each file's
    Original-order position within its group (AC-1, AC-3). Empty groups are
    dropped (AC-5). */
export function groupByRole(files: PrFile[]): RoleFiles[] {
  const byRole = new Map<FileRole, PrFile[]>();
  for (const file of files) {
    const role = classifyPath(file.path);
    const list = byRole.get(role);
    if (list) list.push(file);
    else byRole.set(role, [file]);
  }
  return ROLE_ORDER.filter((role) => (byRole.get(role)?.length ?? 0) > 0).map((role) => ({
    role,
    files: byRole.get(role)!,
  }));
}

/** Where `onRunStart` sends the tab: a run started from Files changed stays
    there (AC-29); any other tab keeps today's behavior (switch to Agent
    runs). */
export function nextTabAfterRunStart(currentTab: string): string {
  return currentTab === "diff" ? currentTab : "findings";
}
