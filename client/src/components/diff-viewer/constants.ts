/** Constants for the DiffViewer. */

/** Files with this many or fewer changed lines start expanded. */
export const AUTO_EXPAND_MAX_LINES = 200;

/** Matches a unified-diff hunk header, e.g. `@@ -1,2 +1,3 @@`. */
export const HUNK_HEADER_RE = /@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/;

/** Severity → CSS colour token, for the line stripe/label. Mirrors
    FindingCard/constants.ts SEV_COLOR — do not import across folders (that
    file lives under src/app/**, which this folder must not import from). */
export const FINDING_SEV_COLOR: Record<string, string> = {
  CRITICAL: "var(--crit)",
  WARNING: "var(--warn)",
  SUGGESTION: "var(--sugg)",
  INFO: "var(--info)",
};

/** Fallback colour for an unknown severity. */
export const FINDING_SEV_COLOR_FALLBACK = "var(--text-muted)";

/** Fixed color for the open-findings dot (not severity-colored, spec q7). */
export const OPEN_FINDINGS_DOT_COLOR = "var(--crit)";
